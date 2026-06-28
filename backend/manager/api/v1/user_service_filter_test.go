package v1

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestParseListUserFilter_RejectsSQLMeta verifies the CEL `project` filter
// rejects payloads crafted to break out of a SQL string literal. Before the
// fix, `project == "projects/x' OR '1'='1"` passed GetNameParentTokens (which
// only checked structural prefixes) and the value was interpolated into the
// ListUsers query. The resource-name hardening now rejects the SQL
// meta-characters, so the filter returns InvalidArgument and never sets
// find.ProjectID.
func TestParseListUserFilter_RejectsSQLMeta(t *testing.T) {
	payloads := []string{
		`project == "projects/x' OR '1'='1"`,
		`project == "projects/x\" OR \"1\"=\"1"`,
		`project == "projects/x; DROP TABLE policy;--"`,
		`project == "projects/x\) OR (1=1"`,
	}
	for _, filter := range payloads {
		t.Run(filter, func(t *testing.T) {
			find := &store.FindUserMessage{}
			err := parseListUserFilter(find, filter)
			if err == nil {
				t.Fatalf("expected rejection of SQL-meta payload, but filter parsed cleanly; ProjectID=%v", find.ProjectID)
			}
			connErr, ok := err.(*connect.Error)
			if !ok {
				t.Fatalf("expected *connect.Error, got %T: %v", err, err)
			}
			if connErr.Code() != connect.CodeInvalidArgument {
				t.Fatalf("expected CodeInvalidArgument, got %s: %v", connErr.Code(), err)
			}
			if find.ProjectID != nil {
				t.Fatalf("ProjectID must not be set for rejected payload, got %q", *find.ProjectID)
			}
		})
	}
}

// TestParseListUserFilter_ProjectFilter_Legitimate confirms a normal project
// filter is accepted and records the project ID for downstream parameterized
// query construction.
func TestParseListUserFilter_ProjectFilter_Legitimate(t *testing.T) {
	find := &store.FindUserMessage{}
	if err := parseListUserFilter(find, `project == "projects/foo"`); err != nil {
		t.Fatalf("expected acceptance of legitimate project filter, got %v", err)
	}
	if find.ProjectID == nil || *find.ProjectID != "foo" {
		got := "<nil>"
		if find.ProjectID != nil {
			got = *find.ProjectID
		}
		t.Fatalf("expected ProjectID=foo, got %q", got)
	}
}

// TestParseListUserFilter_MatchesOperator_Parameterized guards the
// `name.matches()` / `email.matches()` filter: the user-supplied substring
// must be carried as a SQL parameter, not interpolated into the LIKE pattern
// (which previously allowed a `'` to break out of the literal).
func TestParseListUserFilter_MatchesOperator_Parameterized(t *testing.T) {
	find := &store.FindUserMessage{}
	payload := `name.matches("x' OR '1'='1")`
	if err := parseListUserFilter(find, payload); err != nil {
		t.Fatalf("expected matches() payload to parse (parameterized), got %v", err)
	}
	if find.Filter == nil {
		t.Fatal("expected filter to be set")
	}
	// The tainted value must be in args, not in the WHERE text. The value is
	// lowercased before being wrapped in the LIKE pattern, so compare against the
	// case-agnostic `'1'='1` fragment (digits and quotes survive ToLower).
	if strings.Contains(find.Filter.Where, "'1'='1") {
		t.Fatalf("matches() value leaked into WHERE text: %q args=%v", find.Filter.Where, find.Filter.Args)
	}
	var found bool
	for _, a := range find.Filter.Args {
		if s, ok := a.(string); ok && strings.Contains(s, "'1'='1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("matches() tainted substring not found in args %v", find.Filter.Args)
	}
	if !strings.Contains(find.Filter.Where, "LIKE $") {
		t.Fatalf("expected parameterized LIKE, got %q", find.Filter.Where)
	}
}
