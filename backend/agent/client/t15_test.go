package client

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// fakeAgentClient implements v1connect.AgentServiceClient by embedding the
// interface (nil) and overriding only AgentHeartbeat. Other methods would
// nil-panic, but Heartbeat only calls AgentHeartbeat.
type fakeAgentClient struct {
	v1connect.AgentServiceClient
	heartbeatFn func(ctx context.Context, req *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error)
}

func (f *fakeAgentClient) AgentHeartbeat(ctx context.Context, req *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error) {
	return f.heartbeatFn(ctx, req)
}

// TestHeartbeat_PerCallTimeoutDetectsHungManager guards the T15 per-call
// timeout: the Heartbeat RPC must run under a deadline bounded by
// heartbeatTimeout, independent of the long-lived caller ctx. A manager that
// accepts the connection but never replies must fail the heartbeat (and thus
// trigger reconnect) within ~10s, rather than stalling until the agent's ctx
// is cancelled.
func TestHeartbeat_PerCallTimeoutDetectsHungManager(t *testing.T) {
	var gotCtx context.Context
	fake := &fakeAgentClient{
		heartbeatFn: func(ctx context.Context, _ *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error) {
			gotCtx = ctx
			// Return immediately; the assertion is on the ctx the RPC was
			// invoked with, not on wall-clock blocking.
			return nil, context.DeadlineExceeded
		},
	}
	c := &Client{client: fake}

	err := c.Heartbeat(context.Background())
	require.Error(t, err)

	dl, ok := gotCtx.Deadline()
	require.True(t, ok, "heartbeat RPC ctx must carry a per-call deadline")
	remaining := time.Until(dl)
	assert.Less(t, remaining, heartbeatTimeout, "deadline must be bounded by heartbeatTimeout")
	assert.Greater(t, remaining, heartbeatTimeout-3*time.Second, "deadline must be ~heartbeatTimeout, not a tiny guard")
}
