package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
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

	return connect.NewResponse(convertToV1Conversation(conv, user.Name, 1, 0, conv.Title)), nil
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

	convs, err := s.store.ListUserConversationsWithUnread(ctx, user.ID, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channels"))
	}

	nextPageToken := ""
	if len(convs) == limitPlusOne {
		convs = convs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	var v1Convs []*v1pb.Conversation
	for _, uc := range convs {
		conv := uc.Conversation
		memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
		ownerName := user.Name
		if conv.OwnerID != user.ID {
			ownerName = resolveUserName(ctx, s.store, conv.OwnerID)
		}
		// For direct conversations (type=1) the title is empty in the DB; surface
		// the agent's display name instead so the left rail can render the DM row
		// without an extra member fetch.
		title := conv.Title
		if conv.Type == 1 && conv.AgentID.Valid {
			if agent, agentErr := s.store.GetAgent(ctx, int(conv.AgentID.Int32)); agentErr == nil && agent != nil && agent.Name != "" {
				title = agent.Name
			}
		}
		v1Convs = append(v1Convs, convertToV1Conversation(&conv, ownerName, memberCount, uc.UnreadCount, title))
	}

	return connect.NewResponse(&v1pb.ListChannelsResponse{
		Channels:      v1Convs,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) ListChannelsForAgent(ctx context.Context, req *connect.Request[v1pb.ListChannelsForAgentRequest]) (*connect.Response[v1pb.ListChannelsForAgentResponse], error) {
	if _, ok := GetUserFromContext(ctx); !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid agent name %q", req.Msg.Name))
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

	convs, err := s.store.ListAgentConversations(ctx, resourceID, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channels for agent"))
	}

	nextPageToken := ""
	if len(convs) == limitPlusOne {
		convs = convs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	v1Convs := make([]*v1pb.Conversation, 0, len(convs))
	for _, uc := range convs {
		conv := uc.Conversation
		memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
		ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
		// Direct conversations store no title; surface the agent's display name
		// so the row renders without an extra member fetch, mirroring ListChannels.
		title := conv.Title
		if conv.Type == 1 && conv.AgentID.Valid {
			if agent, agentErr := s.store.GetAgent(ctx, int(conv.AgentID.Int32)); agentErr == nil && agent != nil && agent.Name != "" {
				title = agent.Name
			}
		}
		v1Convs = append(v1Convs, convertToV1Conversation(&conv, ownerName, memberCount, uc.UnreadCount, title))
	}

	return connect.NewResponse(&v1pb.ListChannelsForAgentResponse{
		Channels:      v1Convs,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) GetChannel(ctx context.Context, req *connect.Request[v1pb.GetChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	convID, err := requireConversationMember(ctx, s.store, req.Msg.Name)
	if err != nil {
		return nil, err
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)

	return connect.NewResponse(convertToV1Conversation(conv, ownerName, memberCount, 0, conv.Title)), nil
}

func (s *CommandService) UpdateChannel(ctx context.Context, req *connect.Request[v1pb.UpdateChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	conv := req.Msg.Conversation
	convID, err := parseConversationID(conv.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	existing, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := requireChannelOwner(ctx, s.store, existing); err != nil {
		return nil, err
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

	return connect.NewResponse(convertToV1Conversation(updated, ownerName, memberCount, 0, updated.Title)), nil
}

func (s *CommandService) DeleteChannel(ctx context.Context, req *connect.Request[v1pb.DeleteChannelRequest]) (*connect.Response[emptypb.Empty], error) {
	convID, err := parseConversationID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := requireChannelOwner(ctx, s.store, conv); err != nil {
		return nil, err
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

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := requireChannelOwner(ctx, s.store, conv); err != nil {
		return nil, err
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

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := requireChannelOwner(ctx, s.store, conv); err != nil {
		return nil, err
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
	convID, err := requireConversationMember(ctx, s.store, req.Msg.Conversation)
	if err != nil {
		return nil, err
	}

	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list members"))
	}

	var v1Members []*v1pb.ChannelMember
	for _, m := range members {
		v1Members = append(v1Members, buildChannelMember(ctx, s.store, m.MemberType, m.MemberID, m.MemberRole, m.JoinedAt))
	}

	return connect.NewResponse(&v1pb.ListChannelMembersResponse{Members: v1Members}), nil
}

// ListThreadParticipants lists the distinct users and agents that posted in a
// thread (the root message plus its replies), derived from message senders. The
// caller must be a member of the conversation.
func (s *CommandService) ListThreadParticipants(ctx context.Context, req *connect.Request[v1pb.ListThreadParticipantsRequest]) (*connect.Response[v1pb.ListThreadParticipantsResponse], error) {
	convID, err := requireConversationMember(ctx, s.store, req.Msg.Conversation)
	if err != nil {
		return nil, err
	}

	rootID, parseErr := uuid.Parse(req.Msg.ThreadRoot)
	if parseErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid thread_root"))
	}
	isRoot, rootErr := s.store.IsThreadRoot(ctx, convID, rootID)
	if rootErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(rootErr, "failed to validate thread root"))
	}
	if !isRoot {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root is not a root message in this conversation"))
	}

	senders, err := s.store.ListThreadSenders(ctx, convID, rootID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list thread senders"))
	}

	var v1Members []*v1pb.ChannelMember
	for _, ts := range senders {
		var memberID string
		switch ts.SenderType {
		case store.SenderTypeUser:
			memberID = strconv.Itoa(ts.PrincipalID)
		case store.SenderTypeAgent:
			if !ts.AgentID.Valid {
				continue
			}
			agent, agentErr := s.store.GetAgent(ctx, int(ts.AgentID.Int32))
			if agentErr != nil || agent == nil {
				continue
			}
			memberID = agent.ResourceID
		default:
			continue
		}
		// Thread participation has no role; leave MemberRole 0.
		v1Members = append(v1Members, buildChannelMember(ctx, s.store, ts.SenderType, memberID, 0, time.Time{}))
	}

	return connect.NewResponse(&v1pb.ListThreadParticipantsResponse{Members: v1Members}), nil
}

func (s *CommandService) SendMessage(ctx context.Context, req *connect.Request[v1pb.SendMessageRequest]) (*connect.Response[v1pb.ChatMessage], error) {
	if req.Msg.Content == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("content must not be empty"))
	}

	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		// Agents reply via PostMessage; SendMessage is the user-facing path and
		// must never fall back to the system principal (the previous behavior
		// let an agent token post as principalID=1 "system").
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SendMessage is for authenticated users; agents must use PostMessage"))
	}
	if _, err := requireConversationMember(ctx, s.store, req.Msg.Conversation); err != nil {
		return nil, err
	}

	// thread_root, when set, makes this message a reply in an existing thread
	// rooted at the given message id. Validate the root belongs to this
	// conversation and is itself a root (not a nested thread reply).
	var threadRoot uuid.NullUUID
	if req.Msg.ThreadRoot != "" {
		rootID, parseErr := uuid.Parse(req.Msg.ThreadRoot)
		if parseErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid thread_root"))
		}
		isRoot, rootErr := s.store.IsThreadRoot(ctx, convID, rootID)
		if rootErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(rootErr, "failed to validate thread root"))
		}
		if !isRoot {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root is not a root message in this conversation"))
		}
		threadRoot = uuid.NullUUID{UUID: rootID, Valid: true}
	}

	// as_task creates this top-level message as a task: a task row is inserted
	// in the same transaction with a per-conversation number and status TODO.
	// Tasks are top-level only, so as_task is incompatible with thread_root.
	if req.Msg.AsTask && threadRoot.Valid {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("as_task is only valid for top-level messages, not thread replies"))
	}

	// Validate attachment ids belong to this conversation and normalize their
	// metadata from the file rows. The agent path (PostMessage) already did
	// this; doing it here too closes the gap where user-sent attachments were
	// never membership-checked, and preserves caller-supplied anchor fields
	// (section_anchor/section_id/quoted_text) used by attachment comments.
	attachments, err := s.resolveAttachments(ctx, convID, req.Msg.Attachments)
	if err != nil {
		return nil, err
	}

	// Atomically bump conversation.version and write the user message with that
	// room_version. This is the single source of truth for the room cursor. When
	// as_task is set, the same tx also inserts the task row (status TODO).
	var msg *store.ChatMessage
	if req.Msg.AsTask {
		msg, _, err = s.store.CreateTaskMessageBumpVersion(ctx, &store.ChatMessage{
			ConversationID: convID,
			PrincipalID:    user.ID,
			PrincipalName:  user.Name,
			Role:           1, // USER
			Content:        req.Msg.Content,
			SenderType:     store.SenderTypeUser,
			Mentions:       req.Msg.Mentions,
			Attachments:    attachments,
		})
	} else {
		msg, _, err = s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
			ConversationID:      convID,
			PrincipalID:         user.ID,
			PrincipalName:       user.Name,
			Role:                1, // USER
			Content:             req.Msg.Content,
			SenderType:          store.SenderTypeUser,
			Mentions:            req.Msg.Mentions,
			Attachments:         attachments,
			ThreadRootMessageID: threadRoot,
		})
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create message"))
	}

	if req.Msg.AsTask {
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("📋 %s created task #%d %q", user.Name, msg.TaskInfo.TaskNumber, truncateContent(req.Msg.Content)))
	}

	if threadRoot.Valid {
		// Thread reply: subscribe any @mentioned agents (idempotent) and wake
		// every subscriber of this thread — subscription is persistent, so a
		// subscriber is woken on every reply even without a fresh @mention. The
		// user sender has no agent id, so all subscribers are woken.
		s.subscribeAndNotifyThread(ctx, convID, threadRoot.UUID, msg.RoomVersion, req.Msg.Mentions, nil)
	} else {
		// Agent-first: the manager never dispatches work on a user message. It
		// only notifies every agent member of the conversation that new messages
		// are available; each agent's autonomous drain loop then decides whether
		// and how to respond. (Agents are conversation_member rows of their
		// direct conversations too, so this covers 1:1 chats.)
		s.notifyConversationAgents(ctx, convID, msg.RoomVersion, nil)
	}

	return connect.NewResponse(storeToV1ChatMessage(msg)), nil
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

// subscribeAndNotifyThread handles a thread reply: it subscribes any agent
// @mentioned in the reply (plus the posting agent, when posterAgentID is
// non-nil) to the thread, then wakes every current subscriber except
// posterAgentID that a new reply landed. Subscription is persistent — once an
// agent is subscribed (via @mention or its own reply) it is woken on every
// subsequent reply in the thread, even without a fresh @mention. Used by
// SendMessage (user, posterAgentID=nil) and PostMessage (agent) for thread
// replies.
func (s *CommandService) subscribeAndNotifyThread(ctx context.Context, convID, rootID uuid.UUID, version int64, mentions []*v1pb.Mention, posterAgentID *int) {
	var agentIDs []int
	seen := make(map[int]bool)
	addAgent := func(id int) {
		if id > 0 && !seen[id] {
			seen[id] = true
			agentIDs = append(agentIDs, id)
		}
	}
	for _, m := range mentions {
		if m.Type != "agent" || m.Id == "" {
			continue
		}
		agent, err := s.store.GetAgentByResourceID(ctx, m.Id)
		if err != nil || agent == nil {
			slog.Warn("failed to resolve mentioned agent for thread subscription", "resourceID", m.Id, "error", err)
			continue
		}
		addAgent(agent.ID)
	}
	if posterAgentID != nil {
		addAgent(*posterAgentID)
	}
	if err := s.store.AddThreadParticipants(ctx, rootID, agentIDs); err != nil {
		slog.Warn("failed to subscribe thread participants", "rootID", rootID, "error", err)
		// Still notify existing subscribers below.
	}
	s.notifyThreadParticipants(ctx, convID, rootID, version, posterAgentID)
}

// notifyThreadParticipants wakes every agent subscribed to a thread (except
// exceptAgentID) that a new reply landed, carrying the thread root id so the
// agent can go straight to thread check/read. Best-effort: a missed wake is
// recovered on reconnect via ListThreadUpdates (the durable cursor is the
// source of truth).
func (s *CommandService) notifyThreadParticipants(ctx context.Context, convID, rootID uuid.UUID, version int64, exceptAgentID *int) {
	agentIDs, err := s.store.ListThreadParticipantAgents(ctx, rootID)
	if err != nil {
		slog.Warn("failed to list thread participants for notification", "rootID", rootID, "error", err)
		return
	}
	for _, id := range agentIDs {
		if exceptAgentID != nil && id == *exceptAgentID {
			continue
		}
		s.dispatcher.NotifyThreadMention(ctx, id, convID.String(), version, rootID.String())
	}
}

func convertToV1Conversation(conv *store.ConversationMessage, ownerName string, memberCount int, unreadCount int32, title string) *v1pb.Conversation {
	return &v1pb.Conversation{
		Name:        fmt.Sprintf("conversations/%s", conv.ID.String()),
		Title:       title,
		Type:        conv.Type,
		MemberCount: int32(memberCount),
		OwnerId:     fmt.Sprintf("%d", conv.OwnerID),
		OwnerName:   ownerName,
		CreatedAt:   timestamppb.New(conv.CreatedAt),
		UpdatedAt:   timestamppb.New(conv.UpdatedAt),
		UnreadCount: unreadCount,
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
	convID, err := requireConversationMember(ctx, s.store, req.Msg.Conversation)
	if err != nil {
		return nil, err
	}
	activities, err := s.dispatcher.FetchConversationActivity(ctx, convID.String())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to fetch conversation activity"))
	}
	return connect.NewResponse(&v1pb.FetchConversationActivityResponse{Activities: activities}), nil
}

// MarkConversationRead advances the requesting user's read cursor for a
// conversation to its current room_version, clearing the user-facing unread
// badge. The caller must be a member of the conversation.
func (s *CommandService) MarkConversationRead(ctx context.Context, req *connect.Request[v1pb.MarkConversationReadRequest]) (*connect.Response[v1pb.MarkConversationReadResponse], error) {
	convID, err := requireConversationMember(ctx, s.store, req.Msg.Conversation)
	if err != nil {
		return nil, err
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("MarkConversationRead is for authenticated users"))
	}
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to read conversation version"))
	}
	readVersion, err := s.store.UpsertUserReadCursor(ctx, user.ID, convID, conv.Version)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to mark conversation read"))
	}
	return connect.NewResponse(&v1pb.MarkConversationReadResponse{ReadVersion: readVersion}), nil
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

// resolveMemberDescription returns a member's self-description: for users, the
// user-authored User.description; for agents, the admin-authored persona_prompt
// from AgentACPConfig. Surfaced in channel/thread rosters so an agent can perceive
// who a member is and decide whom to address. Empty when unavailable.
func resolveMemberDescription(ctx context.Context, s *store.Store, memberType int32, memberID string) string {
	if memberType == store.MemberTypeUser {
		uid, err := strconv.Atoi(memberID)
		if err != nil {
			return ""
		}
		u, err := s.GetUserByID(ctx, uid)
		if err != nil || u == nil {
			return ""
		}
		return u.Description
	}
	if memberType == store.MemberTypeAgent {
		agent, err := s.GetAgentByResourceID(ctx, memberID)
		if err != nil || agent == nil {
			return ""
		}
		return agent.Info.GetAcpConfig().GetPersonaPrompt()
	}
	return ""
}

// buildChannelMember assembles a v1 ChannelMember from a membership row, resolving
// the display name and self-description. Shared by ListChannelMembers and
// ListThreadParticipants so both rosters render identity consistently.
func buildChannelMember(ctx context.Context, s *store.Store, memberType int32, memberID string, role int32, joinedAt time.Time) *v1pb.ChannelMember {
	m := &v1pb.ChannelMember{
		MemberType:  memberType,
		MemberId:    memberID,
		DisplayName: resolveMemberDisplayName(ctx, s, memberType, memberID),
		MemberRole:  role,
		Description: resolveMemberDescription(ctx, s, memberType, memberID),
	}
	if !joinedAt.IsZero() {
		m.JoinedAt = timestamppb.New(joinedAt)
	}
	return m
}
