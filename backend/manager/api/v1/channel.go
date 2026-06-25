package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *CommandService) CreateChannel(ctx context.Context, req *connect.Request[v1pb.CreateChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	if req.Msg.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title must not be empty"))
	}

	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	conv, err := s.store.CreateChannel(ctx, req.Msg.Title, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create channel"))
	}

	return connect.NewResponse(convertToV1Conversation(conv, user.Name, 1)), nil
}

func (s *CommandService) ListChannels(ctx context.Context, req *connect.Request[v1pb.ListChannelsRequest]) (*connect.Response[v1pb.ListChannelsResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	convs, err := s.store.ListUserConversations(ctx, user.ID, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channels"))
	}

	nextPageToken := ""
	if len(convs) == limitPlusOne {
		convs = convs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	var v1Convs []*v1pb.Conversation
	for _, conv := range convs {
		memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
		ownerName := user.Name
		if conv.OwnerID != user.ID {
			ownerName = resolveUserName(ctx, s.store, conv.OwnerID)
		}
		v1Convs = append(v1Convs, convertToV1Conversation(conv, ownerName, memberCount))
	}

	return connect.NewResponse(&v1pb.ListChannelsResponse{
		Channels:      v1Convs,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) GetChannel(ctx context.Context, req *connect.Request[v1pb.GetChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	convID, err := parseConversationID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)

	return connect.NewResponse(convertToV1Conversation(conv, ownerName, memberCount)), nil
}

func (s *CommandService) UpdateChannel(ctx context.Context, req *connect.Request[v1pb.UpdateChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	conv := req.Msg.Conversation
	convID, err := parseConversationID(conv.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	if conv.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title must not be empty"))
	}

	updated, err := s.store.UpdateChannel(ctx, convID, conv.Title)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update channel"))
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, updated.ID)
	ownerName := resolveUserName(ctx, s.store, updated.OwnerID)

	return connect.NewResponse(convertToV1Conversation(updated, ownerName, memberCount)), nil
}

func (s *CommandService) DeleteChannel(ctx context.Context, req *connect.Request[v1pb.DeleteChannelRequest]) (*connect.Response[emptypb.Empty], error) {
	convID, err := parseConversationID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	user, _ := GetUserFromContext(ctx)
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if user != nil && conv.OwnerID != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only channel owner can delete the channel"))
	}

	if err := s.store.DeleteChannel(ctx, convID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to delete channel"))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *CommandService) AddChannelMember(ctx context.Context, req *connect.Request[v1pb.AddChannelMemberRequest]) (*connect.Response[v1pb.ChannelMember], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	user, _ := GetUserFromContext(ctx)
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if user != nil && conv.OwnerID != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only channel owner can add members"))
	}

	memberType := req.Msg.MemberType
	memberID := req.Msg.MemberId
	var addedAgent *store.AgentMessage
	if memberType == store.MemberTypeAgent {
		agent, agentErr := s.store.GetAgentByResourceID(ctx, memberID)
		if agentErr != nil || agent == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", memberID))
		}
		addedAgent = agent
	}
	if memberType == store.MemberTypeUser {
		if _, uidErr := strconv.Atoi(memberID); uidErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user member_id, must be principal id"))
		}
	}

	if err := s.store.AddConversationMember(ctx, convID, memberType, memberID, store.MemberRoleMember); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to add member"))
	}

	// Seed the agent's per-channel cursor to the current room version so a
	// newly joined agent starts "caught up" and only sees future messages.
	// SeedCursorOnJoin is monotonic, so re-adding an agent never rewinds an
	// existing cursor.
	if addedAgent != nil {
		if seedErr := s.store.SeedCursorOnJoin(ctx, addedAgent.ID, convID); seedErr != nil {
			slog.Warn("failed to seed agent channel cursor on join", "agent", addedAgent.ResourceID, "conversationID", convID, "error", seedErr)
		}
	}

	displayName := resolveMemberDisplayName(ctx, s.store, memberType, memberID)
	return connect.NewResponse(&v1pb.ChannelMember{
		MemberType:  memberType,
		MemberId:    memberID,
		DisplayName: displayName,
		MemberRole:  store.MemberRoleMember,
		JoinedAt:    timestamppb.Now(),
	}), nil
}

func (s *CommandService) RemoveChannelMember(ctx context.Context, req *connect.Request[v1pb.RemoveChannelMemberRequest]) (*connect.Response[emptypb.Empty], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	user, _ := GetUserFromContext(ctx)
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if user != nil && conv.OwnerID != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only channel owner can remove members"))
	}

	memberID := req.Msg.MemberId
	memberType := req.Msg.MemberType

	ownerMemberID := fmt.Sprintf("%d", conv.OwnerID)
	if memberType == store.MemberTypeUser && memberID == ownerMemberID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot remove the channel owner"))
	}

	if err := s.store.RemoveConversationMember(ctx, convID, memberType, memberID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to remove member"))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *CommandService) ListChannelMembers(ctx context.Context, req *connect.Request[v1pb.ListChannelMembersRequest]) (*connect.Response[v1pb.ListChannelMembersResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list members"))
	}

	var v1Members []*v1pb.ChannelMember
	for _, m := range members {
		v1Members = append(v1Members, &v1pb.ChannelMember{
			MemberType:  m.MemberType,
			MemberId:    m.MemberID,
			DisplayName: resolveMemberDisplayName(ctx, s.store, m.MemberType, m.MemberID),
			MemberRole:  m.MemberRole,
			JoinedAt:    timestamppb.New(m.JoinedAt),
		})
	}

	return connect.NewResponse(&v1pb.ListChannelMembersResponse{Members: v1Members}), nil
}

func (s *CommandService) SendMessage(ctx context.Context, req *connect.Request[v1pb.SendMessageRequest]) (*connect.Response[v1pb.ChatMessage], error) {
	if req.Msg.Content == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("content must not be empty"))
	}

	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Wrapf(err, "failed to get conversation"))
	}

	user, _ := GetUserFromContext(ctx)
	principalID := 1
	principalName := "system"
	if user != nil {
		principalID = user.ID
		principalName = user.Name
	}

	// Atomically bump conversation.version and write the user message with that
	// room_version. This is the single source of truth for the room cursor.
	msg, _, err := s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
		ConversationID: convID,
		PrincipalID:    principalID,
		PrincipalName:  principalName,
		Role:           1, // USER
		Content:        req.Msg.Content,
		SenderType:     store.SenderTypeUser,
		Mentions:       req.Msg.Mentions,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create message"))
	}

	// Agent-first: the manager never dispatches work on a user message. It only
	// notifies every agent member of the conversation that new messages are
	// available; each agent's autonomous drain loop then decides whether and
	// how to respond. (Agents are conversation_member rows of their direct
	// conversations too, so this covers 1:1 chats.)
	s.notifyConversationAgents(ctx, conv.ID, msg.RoomVersion, nil)

	return connect.NewResponse(&v1pb.ChatMessage{
		Name:          msg.ID.String(),
		Conversation:  msg.ConversationID.String(),
		PrincipalName: msg.PrincipalName,
		Role:          msg.Role,
		Content:       msg.Content,
		CreatedAt:     timestamppb.New(msg.CreatedAt),
		SenderName:    msg.PrincipalName,
		SenderType:    v1pb.SenderType(msg.SenderType),
		RoomVersion:   msg.RoomVersion,
		Mentions:      msg.Mentions,
	}), nil
}

// notifyConversationAgents sends NewMessagesAvailable to every connected
// agent that is a member of the conversation, except the agent identified by
// exceptAgentID (used by PostMessage so an agent's own reply does not wake
// itself). A nil exceptAgentID notifies all agent members. This covers both
// direct conversations (type=1) and multi-agent channels (type=2), and is the
// single wake path that lets agents talk to each other.
func (s *CommandService) notifyConversationAgents(ctx context.Context, convID uuid.UUID, version int64, exceptAgentID *int) {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list conversation members for notification", "conversationID", convID, "error", err)
		return
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		agent, agentErr := s.store.GetAgentByResourceID(ctx, m.MemberID)
		if agentErr != nil || agent == nil {
			slog.Warn("failed to resolve agent for notification", "agentResourceID", m.MemberID, "error", agentErr)
			continue
		}
		if exceptAgentID != nil && agent.ID == *exceptAgentID {
			continue
		}
		s.dispatcher.NotifyNewMessages(ctx, agent.ID, convID.String(), version)
	}
}

func convertToV1Conversation(conv *store.ConversationMessage, ownerName string, memberCount int) *v1pb.Conversation {
	return &v1pb.Conversation{
		Name:        fmt.Sprintf("conversations/%s", conv.ID.String()),
		Title:       conv.Title,
		Type:        conv.Type,
		MemberCount: int32(memberCount),
		OwnerId:     fmt.Sprintf("%d", conv.OwnerID),
		OwnerName:   ownerName,
		CreatedAt:   timestamppb.New(conv.CreatedAt),
		UpdatedAt:   timestamppb.New(conv.UpdatedAt),
	}
}

func resolveUserName(ctx context.Context, s *store.Store, principalID int) string {
	if principalID == 0 {
		return ""
	}
	u, err := s.GetUserByID(ctx, principalID)
	if err != nil || u == nil {
		slog.Warn("failed to resolve user name", "principalID", principalID, "error", err)
		return fmt.Sprintf("%d", principalID)
	}
	return u.Name
}

// FetchConversationActivity returns the execution status of every agent in a
// conversation. The frontend polls this to show real-time agent status in the
// channel header.
func (s *CommandService) FetchConversationActivity(ctx context.Context, req *connect.Request[v1pb.FetchConversationActivityRequest]) (*connect.Response[v1pb.FetchConversationActivityResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation"))
	}
	activities, err := s.dispatcher.FetchConversationActivity(ctx, convID.String())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to fetch conversation activity"))
	}
	return connect.NewResponse(&v1pb.FetchConversationActivityResponse{Activities: activities}), nil
}

func resolveMemberDisplayName(ctx context.Context, s *store.Store, memberType int32, memberID string) string {
	if memberType == store.MemberTypeUser {
		uid, err := strconv.Atoi(memberID)
		if err != nil {
			return memberID
		}
		return resolveUserName(ctx, s, uid)
	}
	if memberType == store.MemberTypeAgent {
		agent, err := s.GetAgentByResourceID(ctx, memberID)
		if err != nil || agent == nil {
			return memberID
		}
		return agent.Name
	}
	return memberID
}
