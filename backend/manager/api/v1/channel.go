package v1

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
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

	return connect.NewResponse(convertToV1Conversation(conv, user.Name, user.Handle, "", "", 1, 0, conv.Title, 0)), nil
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

	convs, err := s.store.ListUserConversationsWithUnread(ctx, user.ID, req.Msg.IncludeClosed, limitPlusOne, offset.offset)
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
		ownerHandle := user.Handle
		if conv.OwnerID != user.ID {
			ownerName = resolveUserName(ctx, s.store, conv.OwnerID)
			ownerHandle = resolveUserHandle(ctx, s.store, conv.OwnerID)
		}
		// For direct conversations (type=1) the title is empty in the DB; surface
		// the agent's display name instead so the left rail can render the DM row
		// without an extra member fetch. The same agent is the DM peer for the
		// user viewer, so it doubles as the address peerName and the avatar peer.
		title := conv.Title
		peerName := ""
		peerResource := ""
		if conv.Type == 1 && conv.AgentID.Valid {
			if agent, agentErr := s.store.GetAgent(ctx, int(conv.AgentID.Int32)); agentErr == nil && agent != nil && agent.Name != "" {
				title = agent.Name
				peerName = agent.ResourceID
				peerResource = common.FormatAgentUID(agent.ResourceID)
			}
		} else if conv.Type == 4 {
			// For user-user DMs (type=4) the title is empty in the DB; surface
			// the peer user's display name instead so the left rail renders the
			// DM row without an extra member fetch. The peer is the user member
			// that is not the viewer.
			if peer := s.resolveUserDMPeer(ctx, conv.ID, user.ID); peer != nil {
				title = peer.Name
				peerName = peer.Handle
				peerResource = common.FormatUserHandle(peer.Handle)
			}
		}
		convV1 := convertToV1Conversation(&conv, ownerName, ownerHandle, peerName, peerResource, memberCount, uc.UnreadCount, title, 0)
		// pinned is the requesting user's per-conversation pin state; the list
		// query already returns pinned-first, this just surfaces the flag so the
		// frontend can render a pin indicator.
		convV1.Pinned = uc.Pinned
		// closed is the requesting user's per-conversation close state, so the
		// members-page roster can badge channels hidden from the left rail
		// (only populated when include_closed was requested).
		convV1.Closed = uc.Closed
		// last_message preview: the newest main-channel message joined by the
		// list query. The sender principal id is only meaningful for USER
		// senders (the store already empties it otherwise) so the frontend can
		// render "You" without mistaking an agent message for the viewer.
		if uc.LastMessage != "" {
			convV1.LastMessage = singleLinePreview(uc.LastMessage, maxListPreviewLen)
			convV1.LastMessageSender = uc.LastMessageSender
			convV1.LastMessagePrincipalId = uc.LastMessagePrincipalID
			if uc.LastMessageAt.Valid {
				convV1.LastMessageAt = timestamppb.New(uc.LastMessageAt.Time)
			}
		}
		v1Convs = append(v1Convs, convV1)
	}

	return connect.NewResponse(&v1pb.ListChannelsResponse{
		Channels:      v1Convs,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) ListChannelsForAgent(ctx context.Context, req *connect.Request[v1pb.ListChannelsForAgentRequest]) (*connect.Response[v1pb.ListChannelsForAgentResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
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

	// Non-reviewAll users only see the agent's channels they are a member of;
	// a user holding conversations.reviewAll (e.g. oversightReviewer / workspace
	// admin) sees every channel the agent is in.
	var viewer *store.ConversationMemberFilter
	reviewAll, err := s.iam.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve reviewAll permission"))
	}
	if !reviewAll {
		viewer = &store.ConversationMemberFilter{MemberType: store.MemberTypeUser, MemberID: user.Handle}
	}

	convs, err := s.store.ListAgentConversations(ctx, resourceID, viewer, limitPlusOne, offset.offset)
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
		ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
		// For type 1 (user DM) the peer is the user, whose name is ownerName
		// (the DM's created_by/owner is the user); the viewed agent is the
		// single agent participant (conv.AgentID == the agent named in the
		// request), so the row must be labeled with the user, not the agent's
		// own name. For type 3 (agent DM) the peer is the other agent; resolve
		// it from the member roster. Type 2 channels have no peer.
		peerName := ""
		peerResource := ""
		title := conv.Title
		switch conv.Type {
		case store.ConversationTypeDM:
			peerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
			peerName = peerHandle
			peerResource = common.FormatUserHandle(peerHandle)
			title = ownerName
		case store.ConversationTypeAgentDM:
			peer := s.resolveAgentDMPeer(ctx, conv.ID, resourceID)
			if peer != nil {
				peerName = peer.ResourceID
				peerResource = common.FormatAgentUID(peer.ResourceID)
				title = peer.Name
			}
		default:
			// type 2 channels have no peer; title is already the channel title.
		}
		v1Convs = append(v1Convs, convertToV1Conversation(&conv, ownerName, ownerHandle, peerName, peerResource, memberCount, uc.UnreadCount, title, 0))
	}

	return connect.NewResponse(&v1pb.ListChannelsForAgentResponse{
		Channels:      v1Convs,
		NextPageToken: nextPageToken,
	}), nil
}

// ListAccessibleChannels is the agent's on-demand discovery of what it can
// read: every conversation the calling agent is a member of, unioned (when
// follow_owner_permissions is enabled) with every conversation its owner is a
// member of. Each entry carries is_member so the agent knows which it has
// actually joined (only joined conversations accept posts and appear in
// `message check`). It is deliberately separate from ListChannelUpdates (the
// drain-loop inbox), which stays limited to joined conversations so the agent
// is not woken for every message in its owner's channels.
func (s *CommandService) ListAccessibleChannels(ctx context.Context, req *connect.Request[v1pb.ListAccessibleChannelsRequest]) (*connect.Response[v1pb.ListAccessibleChannelsResponse], error) {
	agent, err := requireCallingAgent(ctx)
	if err != nil {
		return nil, err
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

	convs, err := s.store.ListAccessibleChannels(ctx, agent.ResourceID, agent.OwnerID, agent.FollowOwnerPermissions, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list accessible channels"))
	}

	nextPageToken := ""
	if len(convs) == limitPlusOne {
		convs = convs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	channels := make([]*v1pb.AccessibleChannel, 0, len(convs))
	for _, uc := range convs {
		conv := uc.Conversation
		memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
		ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
		ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
		title, peerName, peerResource := s.resolveAccessibleDisplay(ctx, &conv, uc.IsMember, agent)
		channels = append(channels, &v1pb.AccessibleChannel{
			Channel:  convertToV1Conversation(&conv, ownerName, ownerHandle, peerName, peerResource, memberCount, 0, title, 0),
			IsMember: uc.IsMember,
		})
	}

	return connect.NewResponse(&v1pb.ListAccessibleChannelsResponse{
		Channels:      channels,
		NextPageToken: nextPageToken,
	}), nil
}

// resolveAccessibleDisplay resolves the title and, for DMs, the peer of a
// conversation in the calling agent's accessible list. Channels keep their
// title and no peer. DMs resolve a peer for display: the agent's own DMs
// (isMember) carry a dm:@<peer> address (addressable by name); owner-visible
// DMs (isMember=false) show the peer in the title but emit no address — the
// agent addresses them by the conversation resource name, which the read gate
// accepts (the dm:@ grammar would open a different conversation).
func (s *CommandService) resolveAccessibleDisplay(ctx context.Context, conv *store.ConversationMessage, isMember bool, agent *store.AgentMessage) (title, peerName, peerResource string) {
	switch conv.Type {
	case store.ConversationTypeChannel:
		return conv.Title, "", ""
	case store.ConversationTypeDM:
		if a, err := s.store.GetAgent(ctx, int(conv.AgentID.Int32)); err == nil && a != nil && a.Name != "" {
			if a.ID == agent.ID {
				// The agent's own DM: the peer is the owner user.
				peerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
				peerName = peerHandle
				peerResource = common.FormatUserHandle(peerHandle)
				title = resolveUserName(ctx, s.store, conv.OwnerID)
			} else {
				// The owner's DM with another agent.
				peerName = a.ResourceID
				peerResource = common.FormatAgentUID(a.ResourceID)
				title = a.Name
			}
		}
	case store.ConversationTypeAgentDM:
		if peer := s.resolveAgentDMPeer(ctx, conv.ID, agent.ResourceID); peer != nil {
			peerName = peer.ResourceID
			peerResource = common.FormatAgentUID(peer.ResourceID)
			title = peer.Name
		}
	case store.ConversationTypeUserDM:
		if peer := s.resolveUserDMPeer(ctx, conv.ID, 0); peer != nil {
			peerName = peer.Handle
			peerResource = common.FormatUserHandle(peer.Handle)
			title = peer.Name
		}
	default:
		// Unknown conversation types have no peer or special title.
	}
	if !isMember {
		// Owner-visible DMs are not addressable by dm:@ (that would create a
		// different conversation); the caller reads them by resource name.
		peerName = ""
		peerResource = ""
	}
	return title, peerName, peerResource
}

// JoinChannel makes the calling agent a real member of a channel it can read
// (via its own membership or owner-follow). Joining seeds the agent's
// per-channel cursor to the current version, so the channel starts appearing in
// `message check` and the agent may post to it. Idempotent for members. The IAM
// interceptor gates this with laelia.conversations.read — an agent may only
// join a channel it can already read (a mutation gated by a read permission is
// deliberate: "join" is "subscribe to a conversation I can see").
func (s *CommandService) JoinChannel(ctx context.Context, req *connect.Request[v1pb.JoinChannelRequest]) (*connect.Response[v1pb.JoinChannelResponse], error) {
	agent, err := requireCallingAgent(ctx)
	if err != nil {
		return nil, err
	}

	convUUID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convUUID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	// DMs are created for the agent by the address resolver; only channels are
	// joinable.
	if conv.Type != store.ConversationTypeChannel {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only channels can be joined"))
	}

	isMember, err := s.store.IsConversationMember(ctx, convUUID, store.MemberTypeAgent, agent.ResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check membership"))
	}
	if !isMember {
		if err := s.store.AddConversationMembers(ctx, convUUID, []store.ConversationMemberInput{
			{MemberType: store.MemberTypeAgent, MemberID: agent.ResourceID},
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to join channel"))
		}
		if err := s.store.SeedCursorOnJoin(ctx, agent.ID, convUUID); err != nil {
			slog.Warn("failed to seed agent channel cursor on join", "agent", agent.ResourceID, "conversationID", convUUID, "error", err)
		}
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, convUUID)
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	return connect.NewResponse(&v1pb.JoinChannelResponse{
		Conversation: convertToV1Conversation(conv, ownerName, ownerHandle, "", "", memberCount, 0, conv.Title, 0),
	}), nil
}

func (s *CommandService) GetChannel(ctx context.Context, req *connect.Request[v1pb.GetChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	convID, err := parseConversationID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	// The DM peer depends on the viewer: the agent daemon calls GetChannel on
	// its own DMs and must see the user (or other agent) as the peer, not
	// itself. Detect the caller's agent and user identity and pass them down.
	viewerAgentResourceID := ""
	if caller, ok := GetAgentFromContext(ctx); ok && caller != nil {
		viewerAgentResourceID = caller.ResourceID
	}
	viewerUserID := 0
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		viewerUserID = user.ID
	}
	peer := s.resolvePeerForViewer(ctx, conv, viewerAgentResourceID, viewerUserID)
	title := conv.Title
	if peer.displayName != "" && conv.Type != store.ConversationTypeChannel {
		// DMs store no title; surface the peer name so the row matches the
		// list endpoints (which set title = peer for DMs).
		title = peer.displayName
	}

	// read_version is the requesting user's per-conversation read cursor, so the
	// Activity detail embed can scroll to the user's last-read position. Only
	// meaningful for a user viewer; an agent caller (or a missing cursor row)
	// yields 0, which the frontend treats as caught-up.
	readVersion := int64(0)
	pinned := false
	if viewerUserID != 0 {
		if rv, found, err := s.store.GetUserReadCursor(ctx, viewerUserID, conv.ID); err != nil {
			slog.Warn("failed to read user channel cursor", "conversationID", conv.ID, "error", err)
		} else if found {
			readVersion = rv
		}
		if p, err := s.store.GetConversationPinned(ctx, conv.ID, viewerUserID); err != nil {
			slog.Warn("failed to read conversation pinned", "conversationID", conv.ID, "error", err)
		} else {
			pinned = p
		}
	}

	resp := convertToV1Conversation(conv, ownerName, ownerHandle, peer.handle, peer.resource, memberCount, 0, title, readVersion)
	resp.Pinned = pinned
	if viewerUserID != 0 {
		if joinedAt, err := s.store.GetConversationJoinedAt(ctx, conv.ID, viewerUserID); err != nil {
			if !errors.Is(err, store.ErrConversationMemberNotFound) {
				slog.Warn("failed to read conversation joined_at", "conversationID", conv.ID, "error", err)
			}
		} else {
			resp.JoinedAt = timestamppb.New(joinedAt)
		}
		if c, err := s.store.GetConversationClosed(ctx, conv.ID, viewerUserID); err != nil {
			slog.Warn("failed to read conversation closed", "conversationID", conv.ID, "error", err)
		} else {
			resp.Closed = c
		}
	}
	return connect.NewResponse(resp), nil
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
	ownerHandle := resolveUserHandle(ctx, s.store, updated.OwnerID)

	return connect.NewResponse(convertToV1Conversation(updated, ownerName, ownerHandle, "", "", memberCount, 0, updated.Title, 0)), nil
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
	if err := requireChannelOwner(ctx, conv); err != nil {
		return nil, err
	}

	if err := s.store.DeleteChannel(ctx, convID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to delete channel"))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// setChannelArchived is the shared core of ArchiveChannel/UnarchiveChannel: it
// loads the conversation, enforces the owner-only gate, flips the archive flag,
// and returns the updated Conversation. Only the channel owner may archive or
// unarchive.
func (s *CommandService) setChannelArchived(ctx context.Context, name string, archived bool) (*v1pb.Conversation, error) {
	convID, err := parseConversationID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid channel name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := requireChannelOwner(ctx, conv); err != nil {
		return nil, err
	}

	updated, err := s.store.SetConversationArchived(ctx, convID, archived)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set channel archived"))
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, updated.ID)
	ownerName := resolveUserName(ctx, s.store, updated.OwnerID)
	ownerHandle := resolveUserHandle(ctx, s.store, updated.OwnerID)
	return convertToV1Conversation(updated, ownerName, ownerHandle, "", "", memberCount, 0, updated.Title, 0), nil
}

func (s *CommandService) ArchiveChannel(ctx context.Context, req *connect.Request[v1pb.ArchiveChannelRequest]) (*connect.Response[v1pb.ArchiveChannelResponse], error) {
	conv, err := s.setChannelArchived(ctx, req.Msg.Name, true)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.ArchiveChannelResponse{Conversation: conv}), nil
}

func (s *CommandService) UnarchiveChannel(ctx context.Context, req *connect.Request[v1pb.UnarchiveChannelRequest]) (*connect.Response[v1pb.UnarchiveChannelResponse], error) {
	conv, err := s.setChannelArchived(ctx, req.Msg.Name, false)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.UnarchiveChannelResponse{Conversation: conv}), nil
}

// validateChannelUserMember rejects a user member that cannot join a channel:
// missing/deleted accounts and the internal SYSTEM_BOT (which only serves as
// owner-of-record for system-created conversations, never as a real member).

// validateSendMessageContent enforces that a user message carries either text
// or at least one attachment, so file-only sends are allowed while a fully
// empty message is still rejected.
