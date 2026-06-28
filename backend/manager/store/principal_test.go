package store

import (
	"strings"
	"testing"
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
