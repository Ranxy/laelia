package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
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
	if memberType == store.MemberTypeAgent {
		agent, agentErr := s.store.GetAgentByResourceID(ctx, memberID)
		if agentErr != nil || agent == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", memberID))
		}
	}
	if memberType == store.MemberTypeUser {
		if _, uidErr := strconv.Atoi(memberID); uidErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user member_id, must be principal id"))
		}
	}

	if err := s.store.AddConversationMember(ctx, convID, memberType, memberID, store.MemberRoleMember); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to add member"))
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
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create message"))
	}

	// Phase 1: for a direct conversation (type=1) backed by a single agent,
	// produce and dispatch a command so behavior matches pre-Phase-1 chat tasks.
	// Multi-agent channels only persist the message in Phase 1; the agent
	// autonomy gate (PullMessages-driven SubmitAction) arrives in Phase 2.
	if conv.Type == 1 && conv.AgentID.Valid {
		s.dispatchDirectConversation(ctx, conv, msg, principalID, user)
	}

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
	}), nil
}

// dispatchDirectConversation is the Phase 1 stand-in for the (now-deprecated)
// SendCommand with source=CHAT: it materializes a command record scoped to the
// conversation's agent, injects recent chat context, and queues it for
// dispatch. Errors are logged but do not fail SendMessage — the user message
// is already persisted, and execution can be retried via PullMessages / Phase 2.
func (s *CommandService) dispatchDirectConversation(ctx context.Context, conv *store.ConversationMessage, triggerMsg *store.ChatMessage, principalID int, user *store.UserMessage) {
	agent, err := s.store.GetAgent(ctx, int(conv.AgentID.Int32))
	if err != nil || agent == nil {
		slog.Warn("failed to resolve conversation agent for dispatch", "conversationID", conv.ID, "error", err)
		return
	}

	// Validate the agent supports ACP before dispatching. We deliberately do
	// not surface validation failures as user-visible errors here (the message
	// is already persisted); they are logged for operator visibility.
	if vErr := s.validateACPCapability(ctx, agent, v1pb.ExecutorKind_ACP, "", false, 0, user); vErr != nil {
		slog.Warn("agent not ACP-capable; skipping dispatch", "agent", agent.ResourceID, "error", vErr)
		return
	}

	// Inject recent chat context into the instruction so the ACP session has
	// the same conversational grounding that the legacy SendCommand path
	// provided. PullMessages remains available for agents that prefer to pull
	// more context actively.
	instruction := triggerMsg.Content
	if recent, recentErr := s.store.GetRecentChatMessages(ctx, conv.ID, 6); recentErr == nil && len(recent) > 0 {
		if chatCtx := buildLightChatContext(recent); chatCtx != "" {
			instruction = chatCtx + "\n---\n" + instruction
		}
	}

	envBytes, _ := json.Marshal(map[string]string{})
	cmd := &store.CommandMessage{
		AgentID:        agent.ID,
		PrincipalID:    principalID,
		Command:        "",
		Instruction:    instruction,
		ExecutorKind:   int32(v1pb.ExecutorKind_ACP),
		AllowDiff:      false,
		Status:         1, // PENDING
		Env:            string(envBytes),
		TimeoutSeconds: 0,
		SourceType:     int32(v1pb.CommandSource_CHAT),
		ConversationID: &conv.ID,
	}

	created, err := s.store.CreateCommand(ctx, cmd)
	if err != nil {
		slog.Error("failed to create command for message dispatch", "conversationID", conv.ID, "error", err)
		return
	}

	created.AgentResourceID = agent.ResourceID
	created.PrincipalName = ""

	if err := s.dispatcher.EnqueueCommand(ctx, created); err != nil {
		slog.Warn("failed to enqueue command for dispatch", "commandID", created.ID, "error", err)
	}
	s.dispatcher.NotifyNewMessages(ctx, agent.ID, conv.ID.String(), triggerMsg.RoomVersion)
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
