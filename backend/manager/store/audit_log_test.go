package store

import "testing"

// TestNormalizeAuditPayload locks in the jsonb coercion contract: an empty or
// malformed payload (e.g. the interceptor's "" when a call carries no IAM
// change) must be stored as valid JSON, or Postgres rejects the row with
// SQLSTATE 22P02.
func TestNormalizeAuditPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"empty becomes empty object", "", "{}"},
		{"whitespace becomes empty object", "  ", "{}"},
		{"malformed becomes empty object", "not-json", "{}"},
		{"valid object preserved", `{"delta":1}`, `{"delta":1}`},
		{"valid null preserved", "null", "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAuditPayload(tt.payload); got != tt.want {
				t.Fatalf("normalizeAuditPayload(%q) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}
