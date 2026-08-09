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
