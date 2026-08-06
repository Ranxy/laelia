package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
