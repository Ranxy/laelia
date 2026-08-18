package chattools

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// fakeAgentListClient implements only ListPeerAgents by embedding the interface
// and overriding that one method; the rest stay nil and are never reached.
type fakeAgentListClient struct {
	v1connect.CommandServiceClient
	agents []*v1pb.PeerAgent
	err    error
}

func (f *fakeAgentListClient) ListPeerAgents(_ context.Context, _ *connect.Request[v1pb.ListPeerAgentsRequest]) (*connect.Response[v1pb.ListPeerAgentsResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&v1pb.ListPeerAgentsResponse{Agents: f.agents}), nil
}

func TestListPeerAgentsRendersRoster(t *testing.T) {
	c := &fakeAgentListClient{agents: []*v1pb.PeerAgent{
		{
			Name:            "agents/rei-agent-1",
			Handle:          "rei-agent-1",
			DisplayName:     "rei",
			PersonaPrompt:   "精通后端, 专注构建 agent。\n前端任务请转给 @ui-expert。",
			ConnectionState: v1pb.AgentStatus_ONLINE,
			Enabled:         true,
		},
		{
			Name:            "agents/ui-expert-agent-1",
			Handle:          "ui-expert-agent-1",
			DisplayName:     "ui-expert",
			PersonaPrompt:   "",
			ConnectionState: v1pb.AgentStatus_OFFLINE,
			Enabled:         true,
		},
		{
			Name:            "agents/archived-agent-1",
			Handle:          "archived-agent-1",
			DisplayName:     "archived",
			PersonaPrompt:   "",
			ConnectionState: v1pb.AgentStatus_STOPPED,
			Enabled:         false,
		},
	}}

	out, err := ListPeerAgents(context.Background(), Deps{Client: c}, ListPeerAgentsInput{})
	require.NoError(t, err)
	// Header carries the count.
	assert.Contains(t, out, "Peer agents (3):")
	// rei: handle + online state, then the full (multi-line, untruncated) persona.
	assert.Contains(t, out, "- [agent] rei @rei-agent-1 (online)")
	assert.Contains(t, out, "  精通后端, 专注构建 agent。")
	assert.Contains(t, out, "  前端任务请转给 @ui-expert。")
	// ui-expert: no persona → no indented block; offline state.
	assert.Contains(t, out, "- [agent] ui-expert @ui-expert-agent-1 (offline)")
	// archived: stopped state carries an explicit do-not-delegate hint.
	assert.Contains(t, out, "- [agent] archived @archived-agent-1 (stopped)")
	assert.Contains(t, out, "do NOT delegate work to this agent")
	// The delegation hint names dm:@<handle>; handles are unique so no
	// display-name disambiguation fallback exists.
	assert.Contains(t, out, "message send dm:@<handle>")
	assert.Contains(t, out, "dm:@rei-agent-1")
}

func TestListPeerAgentsEmpty(t *testing.T) {
	out, err := ListPeerAgents(context.Background(), Deps{Client: &fakeAgentListClient{}}, ListPeerAgentsInput{})
	require.NoError(t, err)
	assert.Contains(t, out, "Peer agents (0):")
	assert.Contains(t, out, "(none — you are the only agent)")
}

func TestListPeerAgentsWrapsManagerError(t *testing.T) {
	_, err := ListPeerAgents(context.Background(), Deps{Client: &fakeAgentListClient{
		err: connect.NewError(connect.CodePermissionDenied, nilStrErr("denied")),
	}}, ListPeerAgentsInput{})
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
}

func TestConnectionStateString(t *testing.T) {
	assert.Equal(t, "online", connectionStateString(v1pb.AgentStatus_ONLINE))
	assert.Equal(t, "offline", connectionStateString(v1pb.AgentStatus_OFFLINE))
	assert.Equal(t, "error", connectionStateString(v1pb.AgentStatus_ERROR))
	assert.Equal(t, "kicked", connectionStateString(v1pb.AgentStatus_KICKED))
	assert.Equal(t, "stopped", connectionStateString(v1pb.AgentStatus_STOPPED))
	assert.Equal(t, "unknown", connectionStateString(v1pb.AgentStatus_CONNECTION_STATE_UNSPECIFIED))
}

// nilStrErr returns a trivial error so connect.NewError gets a non-nil cause.
func nilStrErr(msg string) error { return &strErr{s: msg} }

type strErr struct{ s string }

func (e *strErr) Error() string { return e.s }
