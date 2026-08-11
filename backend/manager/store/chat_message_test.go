package store

import (
	"context"
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
