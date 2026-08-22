//nolint:revive
package common

import (
	"testing"
)

// TestGetNameParentTokens_RejectsQuotes guards the SQL-injection vector where a
// CEL `project == "projects/x' OR '1'='1"` payload would otherwise pass
// structural validation and reach a query that interpolates the token. The
// token value must be rejected for containing SQL/path-dangerous characters.
func TestGetNameParentTokens_RejectsQuotes(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"single quote breakout", "projects/x' OR '1'='1"},
		{"double quote", `projects/x" OR "1"="1`},
		{"semicolon", "projects/x; DROP TABLE policy;--"},
		{"backslash", `projects/x\`},
		{"paren", "projects/x) OR (1=1"},
		{"nul", "projects/x\x00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, err := GetNameParentTokens(tc.input, ProjectNamePrefix)
			if err == nil {
				t.Fatalf("expected rejection of %q, got tokens %v", tc.input, tokens)
			}
		})
	}
}

// TestGetNameParentTokens_AcceptsValidNames ensures the hardening does not
// reject legitimate resource name tokens (numeric IDs, slugs, emails with the
// common gmail `+` alias separator).
func TestGetNameParentTokens_AcceptsValidNames(t *testing.T) {
	cases := []struct {
		input   string
		prefix  string
		wantTok string
	}{
		{"projects/123", ProjectNamePrefix, "123"},
		{"projects/my-project", ProjectNamePrefix, "my-project"},
		{"users/user+tag@example.com", UserNamePrefix, "user+tag@example.com"},
		{"agents/550e8400-e29b-41d4-a716-446655440000", AgentNamePrefix, "550e8400-e29b-41d4-a716-446655440000"},
		{"roles/workspaceAdmin", RolePrefix, "workspaceAdmin"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			tokens, err := GetNameParentTokens(tc.input, tc.prefix)
			if err != nil {
				t.Fatalf("expected acceptance of %q, got error %v", tc.input, err)
			}
			if tokens[0] != tc.wantTok {
				t.Fatalf("expected token %q, got %q", tc.wantTok, tokens[0])
			}
		})
	}
}
