package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
)

// TestMachineInfo_RecomputedOnReconnect guards a reconnect invariant for the
// machine app: collectMachineInfo must reflect the current discovered
// providers, not a value cached once at startup. A re-probe between reconnects
// that finds new providers must surface them in the next MachineInfo.
func TestMachineInfo_RecomputedOnReconnect(t *testing.T) {
	// No providers discovered yet: empty available_providers.
	c0 := &MachineClient{}
	info0 := c0.collectMachineInfo()
	require.NotNil(t, info0)
	assert.Empty(t, info0.AvailableProviders, "machine with no probe yet reports no providers")

	// After a re-probe on reconnect, recomputing MachineInfo carries the fresh
	// provider list — proving collectMachineInfo reads the *current* cache, not
	// a snapshot from startup.
	c1 := &MachineClient{
		discoveredProviders: []provider.Discovered{
			{ProviderID: "opencode", DisplayName: "OpenCode", Models: []provider.ModelOption{{Value: "gpt-5", Name: "GPT-5"}}},
		},
		discoveredAt: time.Now(),
	}
	info1 := c1.collectMachineInfo()
	require.Len(t, info1.AvailableProviders, 1)
	assert.Equal(t, "opencode", info1.AvailableProviders[0].ProviderId)

	// A second probe that finds a different set must be reflected, not the old one.
	c1.mu.Lock()
	c1.discoveredProviders = []provider.Discovered{{ProviderID: "claude"}}
	c1.discoveredAt = time.Now()
	c1.mu.Unlock()
	info2 := c1.collectMachineInfo()
	require.Len(t, info2.AvailableProviders, 1)
	assert.Equal(t, "claude", info2.AvailableProviders[0].ProviderId, "recompute must read the updated provider cache")
}
