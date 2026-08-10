package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestAgentNotAddableError verifies the private-agent gate error is
// self-contained: it names the target, states the reason (allow_add_to_channel
// is off), and tells the caller the recovery (ask the target's owner to enable
// the switch) — an agent caller reads this verbatim and must know what to do.
func TestAgentNotAddableError(t *testing.T) {
	err := agentNotAddableError(&store.AgentMessage{ResourceID: "pow2", Name: "pow2"})
	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "does not allow being added to channels")
	assert.Contains(t, err.Error(), "ask pow2's owner to enable")
}

// TestValidateChannelUserMember covers the AddChannelMember user gate: active
// end users pass, while missing/deleted accounts and the internal SYSTEM_BOT
// are rejected so no surface can ever put the system bot into a channel.
func TestValidateChannelUserMember(t *testing.T) {
	t.Run("active end user allowed", func(t *testing.T) {
		err := validateChannelUserMember("42", &store.UserMessage{ID: 42, Email: "u@x", Type: storepb.PrincipalType_END_USER})
		require.NoError(t, err)
	})

	t.Run("nil user rejected", func(t *testing.T) {
		err := validateChannelUserMember("42", nil)
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "not found or deleted")
	})

	t.Run("deleted user rejected", func(t *testing.T) {
		err := validateChannelUserMember("42", &store.UserMessage{ID: 42, MemberDeleted: true})
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("system bot rejected", func(t *testing.T) {
		err := validateChannelUserMember("1", &store.UserMessage{ID: 1, Name: "SYSTEM", Type: storepb.PrincipalType_SYSTEM_BOT})
		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "cannot add the system bot to a channel")
	})
}
