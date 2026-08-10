package store

import (
	"strings"
	"testing"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestBuildListUsersQuery_ProjectFilter_Parameterized verifies that a
// user-controlled project filter value is passed as a SQL parameter, never
// interpolated into the query text. This is the regression guard for the
// `project == "projects/x' OR '1'='1"` SQL-injection vector: even if a tainted
// value reaches listUserImpl through an internal caller (bypassing the CEL
// parser's GetNameParentTokens check), it cannot break out of the literal.
func TestBuildListUsersQuery_ProjectFilter_Parameterized(t *testing.T) {
	payload := "x' OR '1'='1"
	find := &FindUserMessage{ProjectID: &payload}

	query, args := buildListUsersQuery(find)

	// The raw payload (and its `projects/`-prefixed form) must NOT appear in the
	// query text; it must be carried by a positional argument instead.
	if strings.Contains(query, payload) {
		t.Fatalf("query interpolates tainted project value %q:\n%s", payload, query)
	}
	if strings.Contains(query, "projects/"+payload) {
		t.Fatalf("query interpolates prefixed tainted project value:\n%s", query)
	}

	// The placeholder must reference the project arg, and the arg must carry the
	// prefixed value that the CTE compares against `policy.resource`.
	if !strings.Contains(query, "resource = $") {
		t.Fatalf("query does not parameterize the project resource:\n%s", query)
	}
	var found bool
	for _, a := range args {
		if s, ok := a.(string); ok && s == "projects/"+payload {
			found = true
		}
	}
	if !found {
		t.Fatalf("project arg %q not present in args %v", "projects/"+payload, args)
	}
}

// TestBuildListUsersQuery_ProjectFilter_Legitimate confirms a normal project
// filter still produces a parameterized query referencing the project resource.
func TestBuildListUsersQuery_ProjectFilter_Legitimate(t *testing.T) {
	project := "foo"
	find := &FindUserMessage{ProjectID: &project}

	query, args := buildListUsersQuery(find)

	if !strings.Contains(query, "resource = $") {
		t.Fatalf("expected parameterized resource clause, got:\n%s", query)
	}
	var found bool
	for _, a := range args {
		if s, ok := a.(string); ok && s == "projects/foo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected projects/foo in args, got %v", args)
	}
}

// TestBuildListUsersQuery_GroupsNullFilter guards against the NULL-array
// regression where ARRAY_AGG over the LEFT JOINed user_groups CTE produced
// arrays containing NULL for users with no groups (breaking the pq.StringArray
// scan on login). The FILTER must exclude unmatched LEFT JOIN rows.
func TestBuildListUsersQuery_GroupsNullFilter(t *testing.T) {
	query, _ := buildListUsersQuery(&FindUserMessage{})
	if !strings.Contains(query, "FILTER (WHERE user_group.id IS NOT NULL)") {
		t.Fatalf("user_groups CTE must filter out NULL LEFT JOIN rows:\n%s", query)
	}
}

// TestBuildListUsersQuery_ExcludeSystemBot verifies the system-bot exclusion:
// the default query keeps every principal (store-internal lookups such as
// agent-DM owner resolution must still read id=1), while ExcludeSystemBot adds
// a parameterized `principal.type != 'SYSTEM_BOT'` predicate so the value is
// carried as an argument, never interpolated into the query text.
func TestBuildListUsersQuery_ExcludeSystemBot(t *testing.T) {
	t.Run("default keeps system bot", func(t *testing.T) {
		query, _ := buildListUsersQuery(&FindUserMessage{})
		if strings.Contains(query, "SYSTEM_BOT") {
			t.Fatalf("default query must not exclude the system bot:\n%s", query)
		}
	})

	t.Run("explicit exclusion adds parameterized predicate", func(t *testing.T) {
		query, args := buildListUsersQuery(&FindUserMessage{ExcludeSystemBot: true})
		if !strings.Contains(query, "principal.type != $") {
			t.Fatalf("expected parameterized type exclusion, got:\n%s", query)
		}
		var found bool
		for _, a := range args {
			if s, ok := a.(string); ok && s == "SYSTEM_BOT" {
				found = true
			}
		}
		if !found {
			t.Fatalf("SYSTEM_BOT arg not present in args %v", args)
		}
	})

	t.Run("exclusion wins over type filter", func(t *testing.T) {
		// Even a caller that explicitly filters for SYSTEM_BOT must not see it
		// without opting in: the exclusion is unconditional.
		botType := models.PrincipalType_SYSTEM_BOT
		query, args := buildListUsersQuery(&FindUserMessage{
			ExcludeSystemBot: true,
			Type:             &botType,
		})
		if !strings.Contains(query, "principal.type != $") {
			t.Fatalf("expected exclusion to win over the type filter, got:\n%s", query)
		}
		_ = args
	})
}
