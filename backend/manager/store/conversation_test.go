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

// TestListUserConversationsWithUnreadSQL locks in the left-rail preview query:
// the newest message must be scoped to main-channel messages
// (thread_root_message_id IS NULL) so thread replies never leak into the
// preview, and the sender metadata must resolve display names the same way
// activity rows do (agent name for AGENT senders, principal name otherwise)
// plus the sender handle for USER senders so the client can render "You". The
// LATERAL join walks idx_chat_message_room_version backwards one row per
// conversation, so cost is bounded by page size, not message volume.
func TestListUserConversationsWithUnreadSQL(t *testing.T) {
	if !strings.Contains(listUserConversationsWithUnreadSQL, "LEFT JOIN LATERAL") {
		t.Fatal("preview must join the newest message via LATERAL")
	}
	if strings.Count(listUserConversationsWithUnreadSQL, "thread_root_message_id IS NULL") < 2 {
		t.Fatal("both the unread count and the preview join must be scoped to main-channel messages")
	}
	if !strings.Contains(listUserConversationsWithUnreadSQL, "ORDER BY m.room_version DESC") {
		t.Fatal("preview must pick the newest message by room_version")
	}
	if !strings.Contains(listUserConversationsWithUnreadSQL, "LIMIT 1") {
		t.Fatal("preview join must fetch at most one message per conversation")
	}
	if !strings.Contains(listUserConversationsWithUnreadSQL, "CASE WHEN m.sender_type = 2 THEN COALESCE(ag.name, '')") {
		t.Fatal("preview must resolve AGENT senders via the agent name")
	}
	if !strings.Contains(listUserConversationsWithUnreadSQL, "CASE WHEN m.sender_type = 1 THEN COALESCE(p.handle, '') ELSE '' END") {
		t.Fatal("preview must expose the sender handle only for USER senders")
	}
	if !strings.Contains(listUserConversationsWithUnreadSQL, "AND ($6 OR NOT cm.closed)") {
		t.Fatal("list must exclude closed conversations by default but include them when include_closed is requested; a closed chat only reappears when a new main-channel message clears the flag")
	}
}
