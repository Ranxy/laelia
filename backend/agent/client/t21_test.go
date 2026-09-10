package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestBeginSession_NoStaleResponseAcrossReconnect guards the T21 reset: a
// BeginSessionResponse left over in beginRespCh from a previous connection
// (the drain loop's ctx cancelled mid-begin) must not be consumed by the first
// beginSession of the next connection. Replies are bound to their connection
// (agentConn.beginResps), so a fresh beginSession on the new connection blocks
// waiting for a new response rather than picking up the stale command.
func TestBeginSession_NoStaleResponseAcrossReconnect(t *testing.T) {
	cs := &commandStream{
		wakeCh:    make(chan struct{}, 1),
		connReady: make(chan struct{}, 1),
	}

	// The prior connection's pump delivered a reply nobody consumed.
	oldConn := &agentConn{
		sender:     &scriptedStreamSender{},
		done:       make(chan struct{}),
		beginResps: make(chan *v1pb.BeginSessionResponse, 1),
	}
	oldConn.beginResps <- &v1pb.BeginSessionResponse{CommandId: "STALE-CMD", Idle: false}
	cs.setConn(oldConn)

	// Reconnect installs a fresh connection with its own reply channel; the
	// stale value stays bound to the dead one.
	fresh := &agentConn{
		sender:     &scriptedStreamSender{},
		done:       make(chan struct{}),
		beginResps: make(chan *v1pb.BeginSessionResponse, 1),
	}
	cs.setConn(fresh)

	// A fresh beginSession must block for a *new* response, not the stale one.
	// Use a short ctx so the call returns promptly via ctx.Done rather than
	// consuming anything.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var gotCmdID string
	go func() {
		resp, _ := cs.beginSession(ctx, fresh)
		if resp != nil {
			gotCmdID = resp.CommandId
		}
		close(done)
	}()

	// beginSession should return via ctx expiry (resp == nil). The test fails
	// only if it returned a command id — and especially the stale one.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("beginSession never returned")
	}
	assert.NotEqual(t, "STALE-CMD", gotCmdID, "stale response leaked across reconnect")
	assert.Empty(t, gotCmdID, "ctx expiry should yield no command id")
}

// TestMachineInfo_RecomputedOnReconnect guards the T21 invariant for the
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
