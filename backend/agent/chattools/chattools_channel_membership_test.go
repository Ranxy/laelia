package chattools

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// fakeMembershipClient implements the RPCs the channel membership tools call:
// ResolveChannelByTitle + GetChannel (address resolution/display), LeaveChannel,
// AddChannelMember, RemoveChannelMember, and ListPeerAgents (agent-name
// resolution). The rest stay nil and are never reached.
type fakeMembershipClient struct {
	v1connect.CommandServiceClient

	channel    *v1pb.Conversation
	leaveErr   error
	addResp    *v1pb.AddChannelMemberResponse
	addErr     error
	removeErr  error
	peers      []*v1pb.PeerAgent
	lastAddReq *v1pb.AddChannelMemberRequest
	removeReqs []*v1pb.RemoveChannelMemberRequest
}

func (f *fakeMembershipClient) ResolveChannelByTitle(_ context.Context, _ *connect.Request[v1pb.ResolveChannelByTitleRequest]) (*connect.Response[v1pb.ResolveChannelByTitleResponse], error) {
	return connect.NewResponse(&v1pb.ResolveChannelByTitleResponse{Conversation: f.channel}), nil
}

func (f *fakeMembershipClient) GetChannel(_ context.Context, _ *connect.Request[v1pb.GetChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	return connect.NewResponse(f.channel), nil
}

func (f *fakeMembershipClient) LeaveChannel(_ context.Context, _ *connect.Request[v1pb.LeaveChannelRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.leaveErr != nil {
		return nil, f.leaveErr
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *fakeMembershipClient) AddChannelMember(_ context.Context, req *connect.Request[v1pb.AddChannelMemberRequest]) (*connect.Response[v1pb.AddChannelMemberResponse], error) {
	f.lastAddReq = req.Msg
	if f.addErr != nil {
		return nil, f.addErr
	}
	return connect.NewResponse(f.addResp), nil
}

func (f *fakeMembershipClient) RemoveChannelMember(_ context.Context, req *connect.Request[v1pb.RemoveChannelMemberRequest]) (*connect.Response[emptypb.Empty], error) {
	f.removeReqs = append(f.removeReqs, req.Msg)
	if f.removeErr != nil {
		return nil, f.removeErr
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *fakeMembershipClient) ListPeerAgents(_ context.Context, _ *connect.Request[v1pb.ListPeerAgentsRequest]) (*connect.Response[v1pb.ListPeerAgentsResponse], error) {
	return connect.NewResponse(&v1pb.ListPeerAgentsResponse{Agents: f.peers}), nil
}

// fakeUserClient implements ListUsers for user-name resolution.
type fakeUserClient struct {
	v1connect.UserServiceClient
	users []*v1pb.User
}

func (f *fakeUserClient) ListUsers(_ context.Context, _ *connect.Request[v1pb.ListUsersRequest]) (*connect.Response[v1pb.ListUsersResponse], error) {
	return connect.NewResponse(&v1pb.ListUsersResponse{Users: f.users}), nil
}

func membershipDeps(c *fakeMembershipClient, u *fakeUserClient) Deps {
	return Deps{Client: c, UserClient: u, Agent: "agents/rei"}
}

func newMembershipClient() *fakeMembershipClient {
	return &fakeMembershipClient{
		channel: &v1pb.Conversation{Name: "conversations/c-general", Address: "#general"},
	}
}

func TestLeaveChannelSuccess(t *testing.T) {
	c := newMembershipClient()
	out, err := LeaveChannel(context.Background(), membershipDeps(c, nil), LeaveChannelInput{Conversation: "#general"})
	require.NoError(t, err)
	assert.Contains(t, out, "Left '#general'")
	assert.Contains(t, out, "channel join")
}

func TestLeaveChannelErrorMapped(t *testing.T) {
	c := newMembershipClient()
	c.leaveErr = connect.NewError(connect.CodePermissionDenied, nil)
	_, err := LeaveChannel(context.Background(), membershipDeps(c, nil), LeaveChannelInput{Conversation: "#general"})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
}

func TestLeaveChannelMissingConversation(t *testing.T) {
	_, err := LeaveChannel(context.Background(), membershipDeps(newMembershipClient(), nil), LeaveChannelInput{})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestAddChannelMemberAgentHandle(t *testing.T) {
	c := newMembershipClient()
	c.addResp = &v1pb.AddChannelMemberResponse{Members: []*v1pb.ChannelMember{{MemberType: 2, MemberId: "abc-123", DisplayName: "backend-bot"}}}
	out, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"agents/abc-123"}})
	require.NoError(t, err)
	require.NotNil(t, c.lastAddReq)
	require.Len(t, c.lastAddReq.GetMembers(), 1)
	assert.Equal(t, int32(2), c.lastAddReq.GetMembers()[0].GetMemberType())
	assert.Equal(t, "abc-123", c.lastAddReq.GetMembers()[0].GetMemberId())
	assert.Contains(t, out, "Added 1 member(s) to '#general'")
	assert.Contains(t, out, "backend-bot")
}

func TestAddChannelMemberUserHandle(t *testing.T) {
	c := newMembershipClient()
	c.addResp = &v1pb.AddChannelMemberResponse{Members: []*v1pb.ChannelMember{{MemberType: 1, MemberId: "5", DisplayName: "Alice"}}}
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"users/5"}})
	require.NoError(t, err)
	require.NotNil(t, c.lastAddReq)
	require.Len(t, c.lastAddReq.GetMembers(), 1)
	assert.Equal(t, int32(1), c.lastAddReq.GetMembers()[0].GetMemberType())
	assert.Equal(t, "5", c.lastAddReq.GetMembers()[0].GetMemberId())
}

func TestAddChannelMemberBareAgentHandle(t *testing.T) {
	c := newMembershipClient()
	c.addResp = &v1pb.AddChannelMemberResponse{Members: []*v1pb.ChannelMember{{MemberType: 2, MemberId: "backend-bot-agent-1", DisplayName: "backend-bot"}}}
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"backend-bot-agent-1"}})
	require.NoError(t, err)
	require.NotNil(t, c.lastAddReq)
	require.Len(t, c.lastAddReq.GetMembers(), 1)
	assert.Equal(t, int32(2), c.lastAddReq.GetMembers()[0].GetMemberType())
	assert.Equal(t, "backend-bot-agent-1", c.lastAddReq.GetMembers()[0].GetMemberId())
}

func TestAddChannelMemberBareUserHandle(t *testing.T) {
	c := newMembershipClient()
	c.addResp = &v1pb.AddChannelMemberResponse{Members: []*v1pb.ChannelMember{{MemberType: 1, MemberId: "alice-user-1", DisplayName: "Alice"}}}
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"alice-user-1"}})
	require.NoError(t, err)
	require.NotNil(t, c.lastAddReq)
	require.Len(t, c.lastAddReq.GetMembers(), 1)
	assert.Equal(t, int32(1), c.lastAddReq.GetMembers()[0].GetMemberType())
	assert.Equal(t, "alice-user-1", c.lastAddReq.GetMembers()[0].GetMemberId())
}

func TestAddChannelMemberDisplayNameRejected(t *testing.T) {
	// Display names no longer resolve: only handles (or agents/<id> /
	// users/<id> resource names) are accepted, so a bare display name is an
	// invalid argument, never an ambiguous lookup.
	c := newMembershipClient()
	u := &fakeUserClient{users: []*v1pb.User{{Name: "users/5", Title: "Alice"}, {Name: "users/6", Title: "Alice"}}}
	_, err := AddChannelMember(context.Background(), membershipDeps(c, u), AddChannelMemberInput{Conversation: "#general", Members: []string{"Alice"}})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestAddChannelMemberUnknownHandle(t *testing.T) {
	// "nobody-user-1" is a well-formed user handle, so it passes local
	// validation and is forwarded to the manager (which owns existence checks).
	c := newMembershipClient()
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"nobody-user-1"}})
	require.NoError(t, err)
	require.NotNil(t, c.lastAddReq)
	require.Len(t, c.lastAddReq.GetMembers(), 1)
	assert.Equal(t, int32(1), c.lastAddReq.GetMembers()[0].GetMemberType())
	assert.Equal(t, "nobody-user-1", c.lastAddReq.GetMembers()[0].GetMemberId())
}

func TestAddChannelMemberEmptyMembers(t *testing.T) {
	_, err := AddChannelMember(context.Background(), membershipDeps(newMembershipClient(), nil), AddChannelMemberInput{Conversation: "#general"})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestAddChannelMemberErrorMapped(t *testing.T) {
	c := newMembershipClient()
	c.addErr = connect.NewError(connect.CodePermissionDenied, nil)
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"agents/abc-123"}})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
}

// TestAddChannelMemberPrivateAgentNextAction verifies the private-agent gate
// (allow_add_to_channel=false) surfaces a recovery hint in NextAction so the
// agent knows to ask the target's owner to enable the switch, instead of the
// generic "do not retry" guidance.
func TestAddChannelMemberPrivateAgentNextAction(t *testing.T) {
	c := newMembershipClient()
	c.addErr = connect.NewError(connect.CodePermissionDenied, errors.New("permission_denied: agent pow2 does not allow being added to channels (allow_add_to_channel is off); only its owner or a workspace admin may add it; ask pow2's owner to enable 'allow being added to channels' on the agent, then retry"))
	_, err := AddChannelMember(context.Background(), membershipDeps(c, nil), AddChannelMemberInput{Conversation: "#general", Members: []string{"agents/pow2"}})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
	assert.Contains(t, e.NextAction, "Ask its owner to enable")
	assert.NotContains(t, e.NextAction, "do not retry")
}

func TestRemoveChannelMemberSuccess(t *testing.T) {
	c := newMembershipClient()
	out, err := RemoveChannelMember(context.Background(), membershipDeps(c, nil), RemoveChannelMemberInput{Conversation: "#general", Members: []string{"agents/abc-123", "users/5"}})
	require.NoError(t, err)
	require.Len(t, c.removeReqs, 2)
	assert.Equal(t, "conversations/c-general", c.removeReqs[0].GetConversation())
	assert.Equal(t, int32(2), c.removeReqs[0].GetMemberType())
	assert.Equal(t, "abc-123", c.removeReqs[0].GetMemberId())
	assert.Equal(t, int32(1), c.removeReqs[1].GetMemberType())
	assert.Equal(t, "5", c.removeReqs[1].GetMemberId())
	assert.Contains(t, out, "Removed 2 member(s) from '#general'")
	assert.Contains(t, out, "agents/abc-123")
}

func TestRemoveChannelMemberBareHandleResolves(t *testing.T) {
	c := newMembershipClient()
	_, err := RemoveChannelMember(context.Background(), membershipDeps(c, nil), RemoveChannelMemberInput{Conversation: "#general", Members: []string{"backend-bot-agent-1"}})
	require.NoError(t, err)
	require.Len(t, c.removeReqs, 1)
	assert.Equal(t, int32(2), c.removeReqs[0].GetMemberType())
	assert.Equal(t, "backend-bot-agent-1", c.removeReqs[0].GetMemberId())
}

func TestRemoveChannelMemberErrorMapped(t *testing.T) {
	c := newMembershipClient()
	c.removeErr = connect.NewError(connect.CodePermissionDenied, nil)
	_, err := RemoveChannelMember(context.Background(), membershipDeps(c, nil), RemoveChannelMemberInput{Conversation: "#general", Members: []string{"agents/abc-123"}})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "PERMISSION_FAILED", e.Code)
}

func TestRemoveChannelMemberMissingConversation(t *testing.T) {
	_, err := RemoveChannelMember(context.Background(), membershipDeps(newMembershipClient(), nil), RemoveChannelMemberInput{})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestRemoveChannelMemberEmptyMembers(t *testing.T) {
	_, err := RemoveChannelMember(context.Background(), membershipDeps(newMembershipClient(), nil), RemoveChannelMemberInput{Conversation: "#general"})
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}
