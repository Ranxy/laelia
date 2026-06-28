package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestBeginSession_NoStaleResponseAcrossReconnect guards the T21 reset: a
// BeginSessionResponse left over in beginRespCh from a previous connection
// (the drain loop's ctx cancelled mid-begin) must not be consumed by the first
// beginSession of the next connection. The reconnect path resets the channel,
// so a fresh beginSession blocks waiting for a new response rather than
// picking up the stale command.
func TestBeginSession_NoStaleResponseAcrossReconnect(t *testing.T) {
	stream, _, cleanup := newTestCommandChannel(t)
	defer cleanup()

	cs := &commandStream{
		beginRespCh: make(chan *v1pb.BeginSessionResponse, 1),
		wakeCh:      make(chan struct{}, 1),
	}

	// Simulate a stale response that the prior connection never consumed.
	cs.beginRespCh <- &v1pb.BeginSessionResponse{CommandId: "STALE-CMD", Idle: false}

	// Reconnect resets the cross-connection channels, dropping the stale value.
	cs.resetCrossConnectionState()

	// A fresh beginSession must block for a *new* response, not the stale one.
	// Use a short ctx so the call returns promptly via ctx.Done rather than
	// consuming anything.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	var gotCmdID string
	go func() {
		resp, _ := cs.beginSession(ctx, stream, make(chan struct{}))
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

// TestAgentInfo_RecomputedOnReconnect guards the T21 move of collectAgentInfo
// into the Run loop: AgentInfo.Capability must reflect the current ACPConfig,
// not a value cached once at startup. Two configs with different capabilities
// must yield different AgentInfo when recomputed.
func TestAgentInfo_RecomputedOnReconnect(t *testing.T) {
	// Unconfigured agent: SupportsAcp=false.
	unconfigured := (*executor.ACPConfig)(nil)
	info0 := collectAgentInfo(unconfigured)
	require.NotNil(t, info0.Capability)
	assert.False(t, info0.Capability.SupportsAcp, "unconfigured agent must report SupportsAcp=false")

	// After the manager configures ACP on a reconnect, recomputing AgentInfo
	// from the new config flips SupportsAcp to true and carries the config's
	// flags — proving collectAgentInfo reads the *current* config, not a cache.
	configured := &executor.ACPConfig{
		Executable:         "/usr/local/bin/opencode",
		MaxTimeoutSeconds:  42,
		SupportsDiff:       true,
		SupportsRawEvents:  false,
		SupportsToolTraces: true,
	}
	info1 := collectAgentInfo(configured)
	require.NotNil(t, info1.Capability)
	assert.True(t, info1.Capability.SupportsAcp, "configured agent must report SupportsAcp=true")
	assert.Equal(t, int32(42), info1.Capability.MaxTimeoutSeconds)
	assert.True(t, info1.Capability.SupportsDiff)
	assert.False(t, info1.Capability.SupportsRawEvents)

	// Snapshot path mirrors the locked read the Run loop now performs.
	c := &Client{acpConfig: configured}
	assert.Same(t, configured, c.acpConfigSnapshot(), "snapshot must return the live config under the lock")

	c.mu.Lock()
	c.acpConfig = nil
	c.mu.Unlock()
	assert.Nil(t, c.acpConfigSnapshot(), "snapshot must reflect an updated config, not a cached one")
}
