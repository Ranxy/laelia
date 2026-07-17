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

// TestUniqueConstraintsPresent locks in the three unique indexes that close
// the T10 race/correctness gaps: at most one direct conversation per
// (agent, user), unique active principal.email, and unique agent_token.token_hash.
// All are declared idempotently.
func TestUniqueConstraintsPresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_dm_unique",
		"ON conversation(agent_id, created_by) WHERE type = 1",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_principal_unique_email",
		"ON principal(email) WHERE deleted = FALSE",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_token_hash ON agent_token(token_hash)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing unique-constraint declaration: %q", want)
		}
	}
	// The DM index must be partial (type = 1) so channels (type=2, agent_id NULL)
	// are not constrained; the email index must be partial (deleted = FALSE) so a
	// soft-deleted address can be reused.
	if !strings.Contains(sql, "DROP INDEX IF EXISTS idx_agent_token_hash") {
		t.Fatal("non-unique idx_agent_token_hash must be dropped before recreating as unique")
	}
}

// TestReminderTablePresent locks in the reminder table that backs scheduled/
// recurring agent tasks. It mirrors task (1:1 with its trigger message, thread
// rooted at it) plus schedule columns (fire_at, cron_expr, tz) and retry-audit
// columns. The assignee is NOT NULL (atomic create+claim). All declarations are
// idempotent so re-applying the schema is safe.
func TestReminderTablePresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS reminder",
		"message_id UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE",
		"conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE",
		"assignee_agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE",
		"task_content TEXT NOT NULL",
		"fire_at TIMESTAMPTZ NOT NULL",
		"cron_expr TEXT",
		"tz TEXT NOT NULL DEFAULT 'UTC'",
		"CONSTRAINT reminder_status_check CHECK (status IN (1,2,3,4,5,6))",
		"CREATE INDEX IF NOT EXISTS idx_reminder_assignee_status ON reminder(assignee_agent_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_reminder_fire_at ON reminder(fire_at) WHERE status = 1",
		"CREATE INDEX IF NOT EXISTS idx_reminder_retry ON reminder(next_retry_at) WHERE status = 2",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing reminder declaration: %q", want)
		}
	}
}

// TestUserChannelCursorPresent locks in the user_channel_cursor table that backs
// the user-facing unread badge. It mirrors agent_channel_cursor: monotonic
// read_version, cascade deletes, and a missing row treated as caught-up. All
// declarations are idempotent so re-applying the schema is safe.
func TestUserChannelCursorPresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS user_channel_cursor",
		"principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE",
		"conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE",
		"read_version BIGINT NOT NULL DEFAULT 0",
		"PRIMARY KEY (principal_id, conversation_id)",
		"CREATE INDEX IF NOT EXISTS idx_user_channel_cursor_user",
		"CREATE INDEX IF NOT EXISTS idx_user_channel_cursor_conv",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing user_channel_cursor declaration: %q", want)
		}
	}
}

// TestAgentDMUniqueIndexPresent locks in the agent-to-agent DM (conversation
// type 3) dedup infrastructure: the ordered agent_dm_a/agent_dm_b columns, the
// order CHECK, and the partial unique index that makes GetOrCreateAgentDM
// race-free, mirroring idx_conversation_dm_unique for type-1 user DMs. All
// declarations are idempotent (IF NOT EXISTS / DO-block guarded) so re-applying
// the schema is safe.
func TestAgentDMUniqueIndexPresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_a INTEGER REFERENCES agent(id) ON DELETE SET NULL",
		"ALTER TABLE conversation ADD COLUMN IF NOT EXISTS agent_dm_b INTEGER REFERENCES agent(id) ON DELETE SET NULL",
		"conversation_agent_dm_order_check",
		"agent_dm_a < agent_dm_b",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_agent_dm_unique",
		"ON conversation(agent_dm_a, agent_dm_b) WHERE type = 3",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing agent-DM declaration: %q", want)
		}
	}
}

// TestChannelTitleUniqueIndexPresent locks in the unique partial index on
// conversation.title for channels (type=2) so a "#<title>" address resolves to
// exactly one conversation. Pre-launch, so no backfill is needed. Idempotent.
func TestChannelTitleUniqueIndexPresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_channel_title_unique",
		"ON conversation(title) WHERE type = 2",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing channel-title unique index declaration: %q", want)
		}
	}
}

// TestActivityFoldingSchemaPresent locks in the activity table's thread-folding
// identity: the activity_key column and the (principal_id, activity_key) PK, plus
// the idempotent upgrade path (ADD COLUMN IF NOT EXISTS, backfill, DO-block PK
// swap) that brings pre-folding tables up to the new key. A MENTION row is keyed
// by its message id; a TASK/REMINDER/THREAD row is keyed by the thread root so the
// root and every reply fold into one row.
func TestActivityFoldingSchemaPresent(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"activity_key UUID NOT NULL",
		"PRIMARY KEY (principal_id, activity_key)",
		"ALTER TABLE activity ADD COLUMN IF NOT EXISTS activity_key UUID",
		"UPDATE activity SET activity_key = message_id WHERE activity_key IS NULL",
		"ALTER TABLE activity ALTER COLUMN activity_key SET NOT NULL",
		"ALTER TABLE activity DROP CONSTRAINT IF EXISTS activity_pkey",
		"ALTER TABLE activity ADD PRIMARY KEY (principal_id, activity_key)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing activity folding declaration: %q", want)
		}
	}
}
