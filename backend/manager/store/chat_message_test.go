package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListConversationMessagesMutualExclusion guards the before/after version
// contract: both bounds must not be set at once. The guard runs before any DB
// access, so a zero-value Store is enough to exercise it.
func TestListConversationMessagesMutualExclusion(t *testing.T) {
	s := &Store{}
	_, _, err := s.ListConversationMessages(context.Background(), uuid.New(), 1, 1, 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestCreateChatMessageBumpVersionSQL locks in that sending a message advances
// conversation.updated_at, not just version. ListChannelsWithUpdates and
// ListUserConversations order by updated_at DESC, so omitting it froze channel
// ordering at the last rename. A DB-backed assertion (updated_at actually
// advances) is T27's domain; this guard ensures the bump statement carries the
// updated_at clause. Run without a live database.
func TestCreateChatMessageBumpVersionSQL(t *testing.T) {
	assert.Contains(t, conversationVersionBumpSQL, "updated_at = now()",
		"bump statement must advance updated_at so activity-ordered listings reflect new messages")
	assert.Contains(t, conversationVersionBumpSQL, "version = version + 1")
}

// TestGetThreadRootSenderSQL locks in that the thread-root sender lookup
// returns the sender_type and sender_agent_id of the root message by id — the
// columns subscribeAndNotifyThread needs to subscribe the agent that authored
// a thread root (so replies to its own messages wake it). Run without a live
// database.
func TestGetThreadRootSenderSQL(t *testing.T) {
	assert.Contains(t, threadRootSenderSQL, "sender_type")
	assert.Contains(t, threadRootSenderSQL, "sender_agent_id")
	assert.Contains(t, threadRootSenderSQL, "WHERE id = $1")
}

// TestClearConversationClosedSQL locks in the "closed chat reappears" behavior:
// a new main-channel message clears the per-member close flag for the whole
// conversation (closed_at reset too), and only rows actually closed are
// touched. The thread-scoping guard lives in createChatMessageInTx (the
// single choke point for both message insert paths), which skips the clear for
// thread replies — mirroring the unread-badge scoping. Run without a live
// database.
func TestClearConversationClosedSQL(t *testing.T) {
	assert.Contains(t, clearConversationClosedSQL, "closed = false",
		"a new message must un-close the conversation so it reappears in the left rail")
	assert.Contains(t, clearConversationClosedSQL, "closed_at = NULL",
		"closed_at must reset on un-close so it does not linger from the last close")
	assert.Contains(t, clearConversationClosedSQL, "closed = true",
		"the clear must be scoped to members who actually closed the conversation")
	assert.Contains(t, clearConversationClosedSQL, "WHERE conversation_id = $1",
		"the clear must target exactly the conversation receiving the message")
}

// TestListChannelThreadsPreviewSQL locks in the inline thread preview query:
// the CTE ranks replies per thread by room_version (the window function caps
// each thread to the newest N via a parameterized rn bound), rows project
// through the shared chatMessageColumns so scanChatMessageRow can read them,
// and output is oldest-first within each thread so previews append in
// chronological order. Run without a live database.
func TestListChannelThreadsPreviewSQL(t *testing.T) {
	if !strings.Contains(listChannelThreadPreviewsSQL, "PARTITION BY thread_root_message_id") {
		t.Fatal("preview ranking must be per thread, not whole-conversation")
	}
	if !strings.Contains(listChannelThreadPreviewsSQL, "ORDER BY room_version DESC") {
		t.Fatal("ranking must pick the newest replies")
	}
	if !strings.Contains(listChannelThreadPreviewsSQL, "ranked.rn <= $2") {
		t.Fatal("each thread's preview must be capped by the rn bound")
	}
	if !strings.Contains(listChannelThreadPreviewsSQL, "ORDER BY cm.thread_root_message_id, cm.room_version ASC") {
		t.Fatal("previews must come back oldest-first within each thread so the inline preview reads chronologically")
	}
	if channelThreadPreviewReplies != 3 {
		t.Fatal("the channel list renders 3 preview replies per thread; changing the cap is a frontend contract change")
	}
}

// TestListChannelThreadsNewCountsSQL locks in the "M new" scoping: replies past
// the user's read cursor, excluding the user's own USER-sender replies (agent
// replies carry the conversation owner's principal, so the sender_type guard —
// mirroring the left-rail unread query — keeps them from being misattributed
// as own), grouped per thread. Run without a live database.
func TestListChannelThreadsNewCountsSQL(t *testing.T) {
	if !strings.Contains(listChannelThreadNewCountsSQL, "room_version > $2") {
		t.Fatal("new counts must be scoped to replies beyond the user's read cursor")
	}
	if !strings.Contains(listChannelThreadNewCountsSQL, "NOT (sender_type = 1 AND principal_id = $3)") {
		t.Fatal("new counts must exclude the user's own replies: own replies are never 'new' to their sender")
	}
	if !strings.Contains(listChannelThreadNewCountsSQL, "GROUP BY thread_root_message_id") {
		t.Fatal("new counts must be aggregated per thread")
	}
}
