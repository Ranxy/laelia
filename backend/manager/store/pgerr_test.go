package store

import (
	"testing"

	"github.com/jackc/pgconn"
	"github.com/pkg/errors"
)

// TestIsUniqueViolation confirms the helper recognizes a PostgreSQL
// unique-violation (SQLSTATE 23505) both bare and wrapped, and rejects other
// errors. CreateUser/UpdateUser rely on this to surface friendly duplicate
// messages.
func TestIsUniqueViolation(t *testing.T) {
	unique := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
	other := &pgconn.PgError{Code: "23503", Message: "foreign key violation"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare unique", unique, true},
		{"wrapped unique", errors.Wrap(unique, "insert failed"), true},
		{"other pg error", other, false},
		{"plain error", errors.New("something else"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueViolation(tt.err); got != tt.want {
				t.Fatalf("isUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
