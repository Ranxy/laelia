package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestListConversationFilesSQL locks in the file-drawer query: it must join
// each file to the message that carried it (via the attachments JSONB id),
// surface the sender/content/thread/room context, pick the latest carrying
// message, and return newest-first. Run without a live database.
func TestListConversationFilesSQL(t *testing.T) {
	assert.Contains(t, listConversationFilesSQL, "LEFT JOIN LATERAL",
		"file drawer must join the carrying message")
	assert.Contains(t, listConversationFilesSQL,
		"cm.attachments @> jsonb_build_array(jsonb_build_object('id', f.id::text))",
		"file drawer must match attachments by file id")
	assert.Contains(t, listConversationFilesSQL, "ORDER BY f.created_at DESC",
		"file drawer must return newest-first")
	assert.Contains(t, listConversationFilesSQL, "ORDER BY cm.created_at DESC",
		"file drawer must pick the latest carrying message")
	assert.Contains(t, listConversationFilesSQL, "COALESCE(cm.content, '')",
		"file drawer must surface the carrying message content")
	assert.Contains(t, listConversationFilesSQL, "COALESCE(p.name, '')",
		"file drawer must surface the sender name")
	assert.Contains(t, listConversationFilesSQL, "cm.thread_root_message_id",
		"file drawer must surface the thread root for reply context")
	assert.Contains(t, listConversationFilesSQL, "COALESCE(cm.room_version, 0)",
		"file drawer must surface the carrying message room version")
	assert.True(t, strings.Contains(listConversationFilesSQL, "WHERE f.conversation_id = $1"),
		"file drawer must scope to the conversation")
}
