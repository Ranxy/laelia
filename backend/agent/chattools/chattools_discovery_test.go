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

// fakeDiscoveryClient implements only the discovery/join methods by embedding
// the interface; the rest stay nil and are never reached.
type fakeDiscoveryClient struct {
	v1connect.CommandServiceClient
	channels []*v1pb.AccessibleChannel
	joinConv *v1pb.Conversation
	err      error
}

func (f *fakeDiscoveryClient) ListAccessibleChannels(_ context.Context, _ *connect.Request[v1pb.ListAccessibleChannelsRequest]) (*connect.Response[v1pb.ListAccessibleChannelsResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&v1pb.ListAccessibleChannelsResponse{Channels: f.channels}), nil
}

func (f *fakeDiscoveryClient) JoinChannel(_ context.Context, _ *connect.Request[v1pb.JoinChannelRequest]) (*connect.Response[v1pb.JoinChannelResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&v1pb.JoinChannelResponse{Conversation: f.joinConv}), nil
}

func TestListAccessibleChannelsRenders(t *testing.T) {
	c := &fakeDiscoveryClient{channels: []*v1pb.AccessibleChannel{
		{Channel: &v1pb.Conversation{Name: "conversations/1", Title: "general", Type: 2, Address: "#general"}, IsMember: true},
		{Channel: &v1pb.Conversation{Name: "conversations/2", Title: "support", Type: 2, Address: "#support"}, IsMember: false},
		// Owner-visible DM: no name-form address; the resource name + title (peer) is shown.
		{Channel: &v1pb.Conversation{Name: "conversations/3", Title: "bob", Type: 1}, IsMember: false},
	}}

	out, err := ListAccessibleChannels(context.Background(), Deps{Client: c}, ListAccessibleChannelsInput{})
	require.NoError(t, err)
	assert.Contains(t, out, "Accessible channels (3):")
	assert.Contains(t, out, "- [joined] '#general'")
	assert.Contains(t, out, "- [visible] '#support'")
	assert.Contains(t, out, "- [visible] conversations/3 (bob)")
	assert.Contains(t, out, "channel join")
}

func TestListAccessibleChannelsEmpty(t *testing.T) {
	out, err := ListAccessibleChannels(context.Background(), Deps{Client: &fakeDiscoveryClient{}}, ListAccessibleChannelsInput{})
	require.NoError(t, err)
	assert.Equal(t, "No accessible channels.\n", out)
}

func TestListAccessibleChannelsErrorMapped(t *testing.T) {
	_, err := ListAccessibleChannels(context.Background(), Deps{Client: &fakeDiscoveryClient{err: connect.NewError(connect.CodePermissionDenied, nil)}}, ListAccessibleChannelsInput{})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
}

// fakePaginatedDiscoveryClient returns a different page per incoming page
// token; page 1 returns a next_page_token so the tool must follow it.
type fakePaginatedDiscoveryClient struct {
	v1connect.CommandServiceClient
	pages map[string][]*v1pb.AccessibleChannel
}

func (f *fakePaginatedDiscoveryClient) ListAccessibleChannels(_ context.Context, req *connect.Request[v1pb.ListAccessibleChannelsRequest]) (*connect.Response[v1pb.ListAccessibleChannelsResponse], error) {
	channels := f.pages[req.Msg.GetPageToken()]
	next := ""
	if req.Msg.GetPageToken() == "" && len(f.pages) > 1 {
		next = "page-2"
	}
	return connect.NewResponse(&v1pb.ListAccessibleChannelsResponse{Channels: channels, NextPageToken: next}), nil
}

// TestListAccessibleChannelsPaginates locks in the "fetch every page" contract:
// the tool must follow next_page_token so an older channel (e.g. CRYSTAL, which
// sorts beyond the default page of 10) is never dropped from discovery.
func TestListAccessibleChannelsPaginates(t *testing.T) {
	c := &fakePaginatedDiscoveryClient{pages: map[string][]*v1pb.AccessibleChannel{
		"": { // page 1 (newest)
			{Channel: &v1pb.Conversation{Name: "conversations/2", Title: "general", Type: 2, Address: "#general"}, IsMember: true},
		},
		"page-2": { // page 2 (older)
			{Channel: &v1pb.Conversation{Name: "conversations/1", Title: "CRYSTAL", Type: 2, Address: "#CRYSTAL"}, IsMember: false},
		},
	}}

	out, err := ListAccessibleChannels(context.Background(), Deps{Client: c}, ListAccessibleChannelsInput{})
	require.NoError(t, err)
	assert.Contains(t, out, "Accessible channels (2):")
	assert.Contains(t, out, "- [joined] '#general'")
	assert.Contains(t, out, "- [visible] '#CRYSTAL'")
}

func TestJoinChannelRequiresConversation(t *testing.T) {
	_, err := JoinChannel(context.Background(), Deps{}, JoinChannelInput{})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestJoinChannelResolvesAddress(t *testing.T) {
	// A channel address is resolved via ResolveChannelByTitle before the join
	// RPC; the fake only implements JoinChannel, so ResolveChannelByTitle stays
	// nil → the resolver call fails locally. Use the raw resource-name form,
	// which passes through without a lookup.
	rawName := "conversations/" + uuidStr
	c := &fakeDiscoveryClient{joinConv: &v1pb.Conversation{Name: rawName, Title: "support", Type: 2, Address: "#support"}}
	out, err := JoinChannel(context.Background(), Deps{Client: c}, JoinChannelInput{Conversation: rawName})
	require.NoError(t, err)
	assert.Contains(t, out, "Joined '#support'")
	assert.Contains(t, out, "message check")
}

func TestFormatAccessibleLine(t *testing.T) {
	assert.Equal(t, "- [joined] '#general'", formatAccessibleLine(&v1pb.AccessibleChannel{
		Channel:  &v1pb.Conversation{Name: "conversations/1", Title: "general", Address: "#general"},
		IsMember: true,
	}))
	assert.Equal(t, "- [visible] conversations/3 (bob)", formatAccessibleLine(&v1pb.AccessibleChannel{
		Channel: &v1pb.Conversation{Name: "conversations/3", Title: "bob"},
	}))
	// A DM the agent is in keeps its dm:@ address (title is not re-appended).
	assert.Equal(t, "- [joined] dm:@alice", formatAccessibleLine(&v1pb.AccessibleChannel{
		Channel:  &v1pb.Conversation{Name: "conversations/4", Title: "alice", Address: "dm:@alice"},
		IsMember: true,
	}))
	assert.Equal(t, "", formatAccessibleLine(nil))
}

// TestResolveConversationAddressRawName guards the discovery escape hatch: a
// "conversations/<id>" name passes through without a manager lookup, so an
// owner-visible DM surfaced by `channel list` is addressable by `message read`.
func TestResolveConversationAddressRawName(t *testing.T) {
	name, err := resolveConversationAddress(context.Background(), Deps{}, "conversations/550e8400-e29b-41d4-a716-446655440000")
	require.NoError(t, err)
	assert.Equal(t, "conversations/550e8400-e29b-41d4-a716-446655440000", name)
}
