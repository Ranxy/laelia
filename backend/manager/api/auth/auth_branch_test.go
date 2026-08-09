package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
)

// TestPeekTokenAudience_SelectsBranch guards the token-branching optimization:
// the unsigned payload peek must classify user/agent/machine tokens into their
// branch without verification, and unknown audiences must map to no branch.
func TestPeekTokenAudience_SelectsBranch(t *testing.T) {
	const secret = "test-secret"
	expected := []string{
		"ll.agent.access.dev",
		"ll.machine.access.dev",
		"ll.user.access.dev",
	}

	userTok, err := GenerateAccessToken("alice", 1, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)
	agentTok, err := GenerateAgentToken("agent-1", "agents/agent-1", 1, TokenTypeAccess, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)
	machineTok, err := GenerateMachineToken("machine-1", "machines/machine-1", 1, TokenTypeAccess, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)

	assert.Equal(t, 2, audienceKind(peekTokenAudience(userTok), expected))
	assert.Equal(t, 0, audienceKind(peekTokenAudience(agentTok), expected))
	assert.Equal(t, 1, audienceKind(peekTokenAudience(machineTok), expected))

	// A token for a different release mode must not match any branch.
	prodTok, err := GenerateAccessToken("alice", 1, common.ReleaseModeProd, secret, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, -1, audienceKind(peekTokenAudience(prodTok), expected))

	// Garbage and empty tokens peek to nil (no branch), never panic.
	assert.Nil(t, peekTokenAudience(""))
	assert.Nil(t, peekTokenAudience("not-a-jwt"))
	assert.Equal(t, -1, audienceKind(peekTokenAudience("a.b"), expected))
}

// TestGetAuthContext_CachedAndStable guards the per-method AuthContext memo: a
// second lookup returns the cached value and the cached value is stable (same
// pointer) for the process lifetime.
func TestGetAuthContext_CachedAndStable(t *testing.T) {
	ctx1, err := getAuthContext("/laelia.v1.AgentService/AgentHeartbeat")
	require.NoError(t, err)
	ctx2, err := getAuthContext("/laelia.v1.AgentService/AgentHeartbeat")
	require.NoError(t, err)
	assert.Same(t, ctx1, ctx2, "repeated lookups must return the cached pointer")

	// An unknown method is an error and must not poison the cache.
	_, err = getAuthContext("/laelia.v1.NonexistentService/Nope")
	assert.Error(t, err)
}
