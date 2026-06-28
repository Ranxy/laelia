package store

import (
	"errors"

	"github.com/jackc/pgconn"
)

// pgUniqueViolationCode is the PostgreSQL SQLSTATE for a unique-constraint
// violation, used to surface friendly duplicate errors (email already exists,
// duplicate DM) instead of leaking the raw driver error.
const pgUniqueViolationCode = "23505"

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation. It unwraps wrapped errors so callers that Wrap a returned driver
// error still detect it.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if pge, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pge.Code == pgUniqueViolationCode
	}
	return false
}
