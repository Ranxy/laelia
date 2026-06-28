package migration

import (
	"os"
	"strings"
	"testing"
)

// latestSQL loads the canonical schema file. It is applied externally (a
// migration framework is T23's remit), so the tests here guard the file's
// contents directly rather than executing it against a live database.
func latestSQL(t *testing.T) string {
	t.Helper()
	// This test file lives in backend/manager/migration/, so latest.sql is a
	// sibling. go test runs with the package directory as the working directory.
	bytes, err := os.ReadFile("latest.sql")
	if err != nil {
		t.Fatalf("read latest.sql: %v", err)
	}
	return string(bytes)
}

// TestSearchChatHistoryTrgmIndexPresent locks in the pg_trgm GIN index that
// makes SearchChatHistory's leading-wildcard `content ILIKE '%q%'` an index
// scan instead of a full table scan. Both the extension and the index must be
// declared idempotently (IF NOT EXISTS) so re-applying the schema is safe.
func TestSearchChatHistoryTrgmIndexPresent(t *testing.T) {
	sql := latestSQL(t)

	if !strings.Contains(sql, "CREATE EXTENSION IF NOT EXISTS pg_trgm") {
		t.Fatal("pg_trgm extension must be created idempotently for trigram ILIKE search")
	}
	if !strings.Contains(sql, "idx_chat_message_content_trgm") {
		t.Fatal("GIN trgm index on chat_message.content is missing; SearchChatHistory would full-scan")
	}
	if !strings.Contains(sql, "gin_trgm_ops") {
		t.Fatal("trgm index must use gin_trgm_ops to serve ILIKE")
	}
	if !strings.Contains(sql, "CREATE INDEX IF NOT EXISTS idx_chat_message_content_trgm") {
		t.Fatal("trgm index must be created with IF NOT EXISTS so re-applying the schema is safe")
	}
}
