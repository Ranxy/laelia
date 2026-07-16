package store

import "testing"

// TestEtagMismatch locks in the optimistic-concurrency contract of the
// SetIamPolicy setters: an empty provided etag skips the check (first write,
// or a caller that did not round-trip a Get), while a non-empty provided etag
// must equal the policy's current etag or the write is rejected as stale.
func TestEtagMismatch(t *testing.T) {
	tests := []struct {
		name        string
		currentEtag string
		provided    string
		want        bool
	}{
		{"empty provided skips check", "v1", "", false},
		{"matching etag accepted", "v1", "v1", false},
		{"stale etag rejected", "v1", "v0", true},
		{"stale etag against absent policy", "", "v0", true},
		{"first write against absent policy", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := etagMismatch(tt.currentEtag, tt.provided); got != tt.want {
				t.Fatalf("etagMismatch(%q, %q) = %v, want %v", tt.currentEtag, tt.provided, got, tt.want)
			}
		})
	}
}
