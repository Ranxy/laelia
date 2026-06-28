package store

import (
	"strings"
	"testing"
)

// TestGetOrCreateDirectConversationSQL locks in the race-free DM creation:
// INSERT ... ON CONFLICT DO NOTHING backed by the partial unique index
// idx_conversation_dm_unique so two concurrent callers cannot both insert the
// same direct conversation. A DB-backed concurrency test is T27's remit; this
// guard ensures the conflict clause is present.
func TestGetOrCreateDirectConversationSQL(t *testing.T) {
	if !strings.Contains(insertDirectConversationSQL, "ON CONFLICT") {
		t.Fatal("GetOrCreateDirectConversation must use ON CONFLICT to be race-free against idx_conversation_dm_unique")
	}
	if !strings.Contains(insertDirectConversationSQL, "DO NOTHING") {
		t.Fatal("conflict must DO NOTHING so the losing caller re-reads the winning row")
	}
	if !strings.Contains(insertDirectConversationSQL, "type = 1") {
		t.Fatal("conflict target must be scoped to direct conversations (type = 1)")
	}
}
