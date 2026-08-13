package chattools

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// fakeAddrClient implements only the four RPCs the address resolver calls
// (ResolveChannelByTitle, GetOrCreateUserDM, GetOrCreateAgentDM,
// ListPeerAgents) by embedding the interface and overriding them; the rest
// stay nil and are never reached.
type fakeAddrClient struct {
	v1connect.CommandServiceClient

	channels map[string]*v1pb.Conversation // title -> conversation
	userDMs  map[string]*v1pb.Conversation // peer user handle -> conversation
	agentDMs map[string]*v1pb.Conversation // peer agent resource name -> conversation
	peers    []*v1pb.PeerAgent

	// callErr returns an error for a given RPC name; nil to succeed.
	callErr func(rpc string) error
}

func (f *fakeAddrClient) ResolveChannelByTitle(_ context.Context, req *connect.Request[v1pb.ResolveChannelByTitleRequest]) (*connect.Response[v1pb.ResolveChannelByTitleResponse], error) {
	if e := f.callErr("ResolveChannelByTitle"); e != nil {
		return nil, e
	}
	conv, ok := f.channels[req.Msg.GetTitle()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("channel not found"))
	}
	return connect.NewResponse(&v1pb.ResolveChannelByTitleResponse{Conversation: conv}), nil
}

func (f *fakeAddrClient) GetOrCreateUserDM(_ context.Context, req *connect.Request[v1pb.GetOrCreateUserDMRequest]) (*connect.Response[v1pb.GetOrCreateUserDMResponse], error) {
	if e := f.callErr("GetOrCreateUserDM"); e != nil {
		return nil, e
	}
	conv, ok := f.userDMs[req.Msg.GetPeerUserHandle()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return connect.NewResponse(&v1pb.GetOrCreateUserDMResponse{Conversation: conv}), nil
}

func (f *fakeAddrClient) GetOrCreateAgentDM(_ context.Context, req *connect.Request[v1pb.GetOrCreateAgentDMRequest]) (*connect.Response[v1pb.GetOrCreateAgentDMResponse], error) {
	if e := f.callErr("GetOrCreateAgentDM"); e != nil {
		return nil, e
	}
	conv, ok := f.agentDMs[req.Msg.GetPeerAgent()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("agent not found"))
	}
	return connect.NewResponse(&v1pb.GetOrCreateAgentDMResponse{Conversation: conv}), nil
}

func (f *fakeAddrClient) ListPeerAgents(_ context.Context, _ *connect.Request[v1pb.ListPeerAgentsRequest]) (*connect.Response[v1pb.ListPeerAgentsResponse], error) {
	if e := f.callErr("ListPeerAgents"); e != nil {
		return nil, e
	}
	return connect.NewResponse(&v1pb.ListPeerAgentsResponse{Agents: f.peers}), nil
}

func addrDeps(c *fakeAddrClient) Deps { return Deps{Client: c, Agent: "agents/rei"} }

func newAddrClient() *fakeAddrClient {
	return &fakeAddrClient{
		channels: map[string]*v1pb.Conversation{
			"general": {Name: "conversations/c-general", Address: "#general"},
		},
		userDMs: map[string]*v1pb.Conversation{
			"alice-user-1": {Name: "conversations/dm-alice", Address: "dm:@alice-user-1"},
		},
		agentDMs: map[string]*v1pb.Conversation{
			"agents/asuka-agent-1": {Name: "conversations/adm-rei-asuka", Address: "dm:@asuka-agent-1"},
		},
		peers: []*v1pb.PeerAgent{
			{Name: "agents/asuka-agent-1", DisplayName: "asuka"},
		},
		callErr: func(string) error { return nil },
	}
}

func TestResolveConversationAddress_Channel(t *testing.T) {
	name, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "#general")
	require.NoError(t, err)
	assert.Equal(t, "conversations/c-general", name)
}

func TestResolveConversationAddress_ChannelNotFound(t *testing.T) {
	_, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "#nope")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "NOT_FOUND_FAILED", e.Code)
}

func TestResolveConversationAddress_AgentDMByHandle(t *testing.T) {
	name, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "dm:@asuka-agent-1")
	require.NoError(t, err)
	assert.Equal(t, "conversations/adm-rei-asuka", name)
}

func TestResolveConversationAddress_AgentDMByResourceID(t *testing.T) {
	name, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "dm:@agents/asuka-agent-1")
	require.NoError(t, err)
	assert.Equal(t, "conversations/adm-rei-asuka", name)
}

func TestResolveConversationAddress_UserDM(t *testing.T) {
	// "bob-user-1" is a user handle, so the resolver takes the user path.
	c := newAddrClient()
	c.userDMs["bob-user-1"] = &v1pb.Conversation{Name: "conversations/dm-bob"}
	name, err := resolveConversationAddress(context.Background(), addrDeps(c), "dm:@bob-user-1")
	require.NoError(t, err)
	assert.Equal(t, "conversations/dm-bob", name)
}

func TestResolveConversationAddress_UserDMNotFound(t *testing.T) {
	// "nobody-user-1" is a well-formed user handle but no such user exists.
	_, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "dm:@nobody-user-1")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "NOT_FOUND_FAILED", e.Code)
}

func TestResolveConversationAddress_DisplayNameNotResolved(t *testing.T) {
	// A display name is never a valid DM peer: only handles resolve. Even when
	// a peer agent carries the display name, "dm:@asuka" is not a handle and is
	// rejected locally.
	c := newAddrClient()
	c.peers = []*v1pb.PeerAgent{
		{Name: "agents/asuka-agent-1", DisplayName: "asuka"},
		{Name: "agents/asuka-agent-2", DisplayName: "asuka"},
	}
	_, err := resolveConversationAddress(context.Background(), addrDeps(c), "dm:@asuka")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestResolveConversationAddress_EmptyPassesThrough(t *testing.T) {
	name, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "")
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestResolveConversationAddress_LegacyRejected(t *testing.T) {
	for _, in := range []string{"abc-123", "conversations/x-1", uuidStr} {
		_, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), in)
		require.Error(t, err, "input %q", in)
		e, ok := err.(*Error)
		require.True(t, ok, "input %q", in)
		assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code, "input %q", in)
	}
}

func TestResolveConversationAddress_EmptyDMPeer(t *testing.T) {
	_, err := resolveConversationAddress(context.Background(), addrDeps(newAddrClient()), "dm:")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestSplitMessageAddress(t *testing.T) {
	cases := []struct {
		in       string
		convAddr string
		msgID    string
	}{
		{"", "", ""},
		// A legacy "conversations/<c>/messages/<m>" token has no ':' and no UUID
		// suffix, so it returns as a bare conversation token (no split); the
		// rejection is owned by resolveConversationAddress downstream.
		{"conversations/c-1/messages/m-2", "conversations/c-1/messages/m-2", ""},
		{"#general:" + uuidStr, "#general", uuidStr},
		{"dm:@alice-user-1:" + uuidStr, "dm:@alice-user-1", uuidStr},
		{"conversations/c-1:" + uuidStr, "conversations/c-1", uuidStr},
		// ':' inside a title is tolerated; only a UUID suffix splits off.
		{"#plan:b:" + uuidStr, "#plan:b", uuidStr},
		// A non-UUID ':' suffix does not split; the whole token is the conversation address.
		{"#plan:b:c", "#plan:b:c", ""},
		// A bare id with no message suffix round-trips as the conversation address.
		{"conversations/c-1", "conversations/c-1", ""},
	}
	for _, tc := range cases {
		convAddr, msgID := splitMessageAddress(tc.in)
		assert.Equal(t, tc.convAddr, convAddr, "input %q convAddr", tc.in)
		assert.Equal(t, tc.msgID, msgID, "input %q msgID", tc.in)
	}
}

func TestResolveMessageName_AddressForm(t *testing.T) {
	name, err := resolveMessageName(context.Background(), addrDeps(newAddrClient()), "#general:"+uuidStr)
	require.NoError(t, err)
	assert.Equal(t, "conversations/c-general/messages/"+uuidStr, name)
}

func TestResolveMessageName_LegacyFullNameRejected(t *testing.T) {
	_, err := resolveMessageName(context.Background(), addrDeps(newAddrClient()), "conversations/c-general/messages/m-9")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestResolveMessageName_RejectsBareID(t *testing.T) {
	_, err := resolveMessageName(context.Background(), addrDeps(newAddrClient()), uuidStr)
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestResolveThreadRoot_ConversationProvided(t *testing.T) {
	conv, root, err := resolveThreadRoot(context.Background(), addrDeps(newAddrClient()), "#general", uuidStr)
	require.NoError(t, err)
	assert.Equal(t, "conversations/c-general", conv)
	assert.Equal(t, uuidStr, root)
}

func TestResolveThreadRoot_DerivesConversationFromRoot(t *testing.T) {
	conv, root, err := resolveThreadRoot(context.Background(), addrDeps(newAddrClient()), "", "#general:"+uuidStr)
	require.NoError(t, err)
	assert.Equal(t, "conversations/c-general", conv)
	assert.Equal(t, uuidStr, root)
}

func TestResolveThreadRoot_BareRootRequiresConversation(t *testing.T) {
	_, _, err := resolveThreadRoot(context.Background(), addrDeps(newAddrClient()), "", uuidStr)
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestResolveThreadRoot_LegacyFullNameRejected(t *testing.T) {
	_, _, err := resolveThreadRoot(context.Background(), addrDeps(newAddrClient()), "", "conversations/c-general/messages/m-9")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestResolveThreadRoot_BareRootWithConversation(t *testing.T) {
	conv, root, err := resolveThreadRoot(context.Background(), addrDeps(newAddrClient()), "#general", uuidStr)
	require.NoError(t, err)
	assert.Equal(t, "conversations/c-general", conv)
	assert.Equal(t, uuidStr, root)
}

// TestResolveDMAddress_AgentMatchCreateNotFoundDoesNotFallThrough guards the
// wrong-recipient fix: when a peer agent's display name matches but the
// GetOrCreateAgentDM call then returns NotFound (e.g. the peer was deleted
// between the list and the create), the resolver must NOT fall through to the
// user path — otherwise a same-named user would silently receive agent DMs.
func TestResolveDMAddress_AgentMatchCreateNotFoundDoesNotFallThrough(t *testing.T) {
	c := newAddrClient()
	// "asuka-agent-1" is an agent handle; make GetOrCreateAgentDM fail with
	// NotFound, and also seed a same-slug user DM that the fall-through would
	// wrongly return.
	c.agentDMs = nil
	c.callErr = func(rpc string) error {
		if rpc == "GetOrCreateAgentDM" {
			return connect.NewError(connect.CodeNotFound, errors.New("agent deleted"))
		}
		return nil
	}
	c.userDMs["asuka-user-1"] = &v1pb.Conversation{Name: "conversations/dm-user-asuka"}
	_, err := resolveConversationAddress(context.Background(), addrDeps(c), "dm:@asuka-agent-1")
	require.Error(t, err)
	e, ok := err.(*Error)
	require.True(t, ok)
	// The NotFound from the agent-DM create propagates; it does not become the
	// user-DM conversation.
	assert.Equal(t, "NOT_FOUND_FAILED", e.Code)
}
