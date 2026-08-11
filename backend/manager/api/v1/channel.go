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
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
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

	return connect.NewResponse(convertToV1Conversation(conv, user.Name, "", "", 1, 0, conv.Title, 0)), nil
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
		if conv.OwnerID != user.ID {
			ownerName = resolveUserName(ctx, s.store, conv.OwnerID)
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
				peerName = agent.Name
				peerResource = common.FormatAgentUID(agent.ResourceID)
			}
		} else if conv.Type == 4 {
			// For user-user DMs (type=4) the title is empty in the DB; surface
			// the peer user's display name instead so the left rail renders the
			// DM row without an extra member fetch. The peer is the user member
			// that is not the viewer.
			if peer := s.resolveUserDMPeer(ctx, conv.ID, user.ID); peer != nil {
				title = peer.Name
				peerName = peer.Name
				peerResource = common.FormatUserUID(peer.ID)
			}
		}
		convV1 := convertToV1Conversation(&conv, ownerName, peerName, peerResource, memberCount, uc.UnreadCount, title, 0)
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
		viewer = &store.ConversationMemberFilter{MemberType: store.MemberTypeUser, MemberID: fmt.Sprintf("%d", user.ID)}
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
			peerName = ownerName
			peerResource = common.FormatUserUID(conv.OwnerID)
			title = ownerName
		case store.ConversationTypeAgentDM:
			peer := s.resolveAgentDMPeer(ctx, conv.ID, resourceID)
			if peer != nil {
				peerName = peer.Name
				peerResource = common.FormatAgentUID(peer.ResourceID)
				title = peer.Name
			}
		default:
			// type 2 channels have no peer; title is already the channel title.
		}
		v1Convs = append(v1Convs, convertToV1Conversation(&conv, ownerName, peerName, peerResource, memberCount, uc.UnreadCount, title, 0))
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
		title, peerName, peerResource := s.resolveAccessibleDisplay(ctx, &conv, uc.IsMember, agent)
		channels = append(channels, &v1pb.AccessibleChannel{
			Channel:  convertToV1Conversation(&conv, ownerName, peerName, peerResource, memberCount, 0, title, 0),
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
				peerName = resolveUserName(ctx, s.store, conv.OwnerID)
				peerResource = common.FormatUserUID(conv.OwnerID)
			} else {
				// The owner's DM with another agent.
				peerName = a.Name
				peerResource = common.FormatAgentUID(a.ResourceID)
			}
			title = peerName
		}
	case store.ConversationTypeAgentDM:
		if peer := s.resolveAgentDMPeer(ctx, conv.ID, agent.ResourceID); peer != nil {
			peerName = peer.Name
			peerResource = common.FormatAgentUID(peer.ResourceID)
			title = peerName
		}
	case store.ConversationTypeUserDM:
		if peer := s.resolveUserDMPeer(ctx, conv.ID, 0); peer != nil {
			peerName = peer.Name
			peerResource = common.FormatUserUID(peer.ID)
			title = peerName
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
	return connect.NewResponse(&v1pb.JoinChannelResponse{
		Conversation: convertToV1Conversation(conv, ownerName, "", "", memberCount, 0, conv.Title, 0),
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
	if peer.name != "" && conv.Type != store.ConversationTypeChannel {
		// DMs store no title; surface the peer name so the row matches the
		// list endpoints (which set title = peer for DMs).
		title = peer.name
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

	resp := convertToV1Conversation(conv, ownerName, peer.name, peer.resource, memberCount, 0, title, readVersion)
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

	return connect.NewResponse(convertToV1Conversation(updated, ownerName, "", "", memberCount, 0, updated.Title, 0)), nil
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

// validateChannelUserMember rejects a user member that cannot join a channel:
// missing/deleted accounts and the internal SYSTEM_BOT (which only serves as
// owner-of-record for system-created conversations, never as a real member).
func validateChannelUserMember(memberID string, user *store.UserMessage) error {
	if user == nil || user.MemberDeleted {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user %s not found or deleted", memberID))
	}
	if user.Type == storepb.PrincipalType_SYSTEM_BOT {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("cannot add the system bot to a channel"))
	}
	return nil
}

func (s *CommandService) AddChannelMember(ctx context.Context, req *connect.Request[v1pb.AddChannelMemberRequest]) (*connect.Response[v1pb.AddChannelMemberResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if len(req.Msg.Members) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("at least one member must be specified"))
	}

	// Reject duplicate (member_type, member_id) pairs in one request — adding
	// the same member twice is a caller bug and would silently upsert twice.
	seen := make(map[string]bool, len(req.Msg.Members))
	inputs := make([]store.ConversationMemberInput, 0, len(req.Msg.Members))
	for _, m := range req.Msg.Members {
		var expireAt *time.Time
		if m.ExpireTime != nil {
			t := m.ExpireTime.AsTime()
			if !t.After(time.Now()) {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expire_time must be in the future"))
			}
			expireAt = &t
		}

		// Group snapshot: add every current member of the group as a real user
		// member. Users already in the channel (including the owner), deleted
		// users, and users already added earlier in this request are skipped, so
		// re-adding a group is idempotent and never downgrades an Admin/Owner.
		if groupName := m.GetGroup(); groupName != "" {
			if m.GetMemberType() != 0 || m.GetMemberId() != "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group is mutually exclusive with member_type/member_id"))
			}
			group, groupErr := s.store.GetGroupByName(ctx, groupName)
			if groupErr != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(groupErr, "failed to get group %q", groupName))
			}
			if group == nil {
				return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", groupName))
			}
			for _, gm := range group.Payload.GetMembers() {
				userID, uidErr := common.GetUserID(gm.GetMember())
				if uidErr != nil {
					// Malformed member inside a group payload: skip, never fail
					// the whole snapshot for one bad row.
					continue
				}
				memberID := fmt.Sprintf("%d", userID)
				key := fmt.Sprintf("%d:%s", store.MemberTypeUser, memberID)
				if seen[key] || memberID == fmt.Sprintf("%d", conv.OwnerID) {
					continue
				}
				existingRole, _, memErr := s.store.GetConversationMembership(ctx, convID, store.MemberTypeUser, memberID)
				if memErr != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to check existing membership"))
				}
				if existingRole != 0 {
					continue
				}
				user, userErr := s.store.GetUserByID(ctx, userID)
				if userErr != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(userErr, "failed to look up user %d", userID))
				}
				if user == nil || user.MemberDeleted || user.Type == storepb.PrincipalType_SYSTEM_BOT {
					continue
				}
				seen[key] = true
				inputs = append(inputs, store.ConversationMemberInput{MemberType: store.MemberTypeUser, MemberID: memberID, ExpireAt: expireAt})
			}
			continue
		}

		memberType := m.MemberType
		memberID := m.MemberId
		key := fmt.Sprintf("%d:%s", memberType, memberID)
		if seen[key] {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("duplicate member %d:%s in request", memberType, memberID))
		}
		seen[key] = true

		// Refuse to re-add an existing member: the batch insert upserts
		// member_role=Member, which would silently downgrade an Admin (or the
		// Owner) to Member. The owner-of-record check below covers the Owner; the
		// membership check covers Admins and plain members — re-inviting someone
		// who is already in the channel is a no-op at best and a privilege strip
		// at worst, so reject it and direct the caller to
		// UpdateChannelMemberRole for a role change.
		if memberType == store.MemberTypeUser && memberID == fmt.Sprintf("%d", conv.OwnerID) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot add the channel owner as a member"))
		}
		existingRole, _, err := s.store.GetConversationMembership(ctx, convID, memberType, memberID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check existing membership"))
		}
		if existingRole != 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("%s is already a member of this channel", memberID))
		}

		if memberType == store.MemberTypeAgent {
			agent, agentErr := s.store.GetAgentByResourceID(ctx, memberID)
			if agentErr != nil || agent == nil {
				return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", memberID))
			}
			// Agent-side rule: a private agent (allow_add_to_channel=false) may
			// only be added by its owner or a workspace admin. The channel-side
			// rule (conversations.manage, enforced by the IAM interceptor) is
			// unchanged.
			if !agent.AllowAddToChannel {
				if err := s.checkAgentAddableByCaller(ctx, agent); err != nil {
					return nil, err
				}
			}
		}
		if memberType == store.MemberTypeUser {
			userID, uidErr := strconv.Atoi(memberID)
			if uidErr != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user member_id, must be principal id"))
			}
			user, userErr := s.store.GetUserByID(ctx, userID)
			if userErr != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(userErr, "failed to look up user %d", userID))
			}
			if err := validateChannelUserMember(memberID, user); err != nil {
				return nil, err
			}
		}
		inputs = append(inputs, store.ConversationMemberInput{MemberType: memberType, MemberID: memberID, ExpireAt: expireAt})
	}

	if len(inputs) == 0 {
		// A group snapshot whose members are all already in the channel is an
		// idempotent no-op, not an error.
		return connect.NewResponse(&v1pb.AddChannelMemberResponse{}), nil
	}

	if err := s.store.AddConversationMembers(ctx, convID, inputs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to add members"))
	}

	// Seed each newly added agent's per-channel cursor to the current room
	// version so it starts "caught up" and only sees future messages.
	// SeedCursorOnJoin is monotonic, so a re-added agent never rewinds an
	// existing cursor. Best-effort: a seed failure does not fail the add.
	members := make([]*v1pb.ChannelMember, 0, len(inputs))
	for _, m := range inputs {
		if m.MemberType == store.MemberTypeAgent {
			if agent, agentErr := s.store.GetAgentByResourceID(ctx, m.MemberID); agentErr == nil && agent != nil {
				if seedErr := s.store.SeedCursorOnJoin(ctx, agent.ID, convID); seedErr != nil {
					slog.Warn("failed to seed agent channel cursor on join", "agent", agent.ResourceID, "conversationID", convID, "error", seedErr)
				}
			}
		}
		members = append(members, buildChannelMember(ctx, s.store, m.MemberType, m.MemberID, store.MemberRoleMember, time.Now()))
	}

	return connect.NewResponse(&v1pb.AddChannelMemberResponse{Members: members}), nil
}

// checkAgentAddableByCaller enforces the agent-side allow_add_to_channel rule:
// when the agent does not allow being added to channels, the caller must be the
// agent's owner or a workspace admin. The channel-side manage check is already
// enforced by the IAM interceptor, so this only adds the agent's own opt-in gate.
// An agent caller is never a user, so it can never satisfy the owner/admin
// bypass — the error explains the reason and the recovery so the agent knows to
// ask the target's owner to enable the switch.
func (s *CommandService) checkAgentAddableByCaller(ctx context.Context, agent *store.AgentMessage) error {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return agentNotAddableError(agent)
	}
	if agent.OwnerID != 0 && agent.OwnerID == user.ID {
		return nil
	}
	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve workspace admin"))
	}
	if isAdmin {
		return nil
	}
	return agentNotAddableError(agent)
}

// agentNotAddableError builds the permission-denied error for the
// allow_add_to_channel gate. The message is self-contained: it names the target
// agent, states the reason (the switch is off), and tells the caller the
// recovery (ask the target's owner to enable it) — an agent caller reads this
// verbatim and must know what to do next.
func agentNotAddableError(target *store.AgentMessage) error {
	display := target.Name
	if display == "" {
		display = target.ResourceID
	}
	return connect.NewError(connect.CodePermissionDenied, errors.Errorf(
		"agent %s does not allow being added to channels (allow_add_to_channel is off); only its owner or a workspace admin may add it; ask %s's owner to enable 'allow being added to channels' on the agent, then retry",
		target.ResourceID, display))
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

// TransferChannelOwnership hands channel ownership from the calling owner to
// another user member. The interceptor grants conversations.manage (Admin+
// Owner); this handler enforces that the caller is the current Owner and that
// the target is an existing user member. Ownership only moves via this RPC —
// UpdateChannelMemberRole cannot set Owner. Only channels (type 2) support
// transfer (DMs/agent-DMs have no transferable owner).
func (s *CommandService) TransferChannelOwnership(ctx context.Context, req *connect.Request[v1pb.TransferChannelOwnershipRequest]) (*connect.Response[v1pb.TransferChannelOwnershipResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if conv.Type != store.ConversationTypeChannel {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only channels support ownership transfer"))
	}
	if err := requireChannelOwner(ctx, conv); err != nil {
		return nil, err
	}

	if req.Msg.MemberType != store.MemberTypeUser {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only users can own a channel"))
	}
	newOwnerID := req.Msg.MemberId
	if _, uidErr := strconv.Atoi(newOwnerID); uidErr != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user member_id, must be principal id"))
	}
	if newOwnerID == fmt.Sprintf("%d", conv.OwnerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("caller is already the owner"))
	}

	// The target must already be a member.
	role, _, memErr := s.store.GetConversationMembership(ctx, convID, store.MemberTypeUser, newOwnerID)
	if memErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to resolve target membership"))
	}
	if role == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("target is not a channel member"))
	}

	newOwnerPrincipalID, _ := strconv.Atoi(newOwnerID)
	if err := s.store.TransferChannelOwnership(ctx, convID, conv.OwnerID, newOwnerPrincipalID, newOwnerID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to transfer ownership"))
	}

	updated, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to reload conversation"))
	}
	memberCount, _ := s.store.GetConversationMemberCount(ctx, updated.ID)
	ownerName := resolveUserName(ctx, s.store, updated.OwnerID)
	return connect.NewResponse(&v1pb.TransferChannelOwnershipResponse{
		Conversation: convertToV1Conversation(updated, ownerName, "", "", memberCount, 0, updated.Title, 0),
	}), nil
}

// UpdateChannelMemberRole grants or revokes channel admin. The interceptor
// grants conversations.manage (Admin+Owner); this handler enforces that the
// caller is the Owner and that the target role is Member or Admin (never Owner
// — ownership only moves via TransferChannelOwnership).
func (s *CommandService) UpdateChannelMemberRole(ctx context.Context, req *connect.Request[v1pb.UpdateChannelMemberRoleRequest]) (*connect.Response[v1pb.ChannelMember], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if conv.Type != store.ConversationTypeChannel {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only channels support member roles"))
	}
	if err := requireChannelOwner(ctx, conv); err != nil {
		return nil, err
	}

	targetRole := req.Msg.TargetRole
	if targetRole != store.MemberRoleMember && targetRole != store.MemberRoleAdmin {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("target_role must be member (2) or admin (3)"))
	}
	memberType := req.Msg.MemberType
	memberID := req.Msg.MemberId
	if memberType == store.MemberTypeUser && memberID == fmt.Sprintf("%d", conv.OwnerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot change the owner's role; use transferOwnership instead"))
	}

	role, _, memErr := s.store.GetConversationMembership(ctx, convID, memberType, memberID)
	if memErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to resolve target membership"))
	}
	if role == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("target is not a channel member"))
	}

	if err := s.store.UpdateConversationMemberRole(ctx, convID, memberType, memberID, targetRole); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update member role"))
	}

	return connect.NewResponse(buildChannelMember(ctx, s.store, memberType, memberID, targetRole, time.Time{})), nil
}

// LeaveChannel removes the calling member from a channel. The interceptor grants
// conversations.read (any member); this handler rejects the current Owner — an
// owner must transfer ownership or delete the channel first to avoid
// orphaning it. Only channels (type 2) support leaving (a DM is left by
// deleting it).
func (s *CommandService) LeaveChannel(ctx context.Context, req *connect.Request[v1pb.LeaveChannelRequest]) (*connect.Response[emptypb.Empty], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if conv.Type != store.ConversationTypeChannel {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only channels support leaving"))
	}

	user, _ := GetUserFromContext(ctx)
	agent, _ := GetAgentFromContext(ctx)
	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	// The owner cannot leave (would orphan the channel); transfer or delete first.
	if user != nil && conv.OwnerID == user.ID {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("channel owner cannot leave; transfer ownership or delete the channel first"))
	}

	role, _, memErr := s.store.GetConversationMembership(ctx, convID, memberType, memberID)
	if memErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to resolve membership"))
	}
	if role == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("not a channel member"))
	}

	if err := s.store.RemoveConversationMember(ctx, convID, memberType, memberID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to leave channel"))
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
		v1Members = append(v1Members, buildChannelMember(ctx, s.store, m.MemberType, m.MemberID, m.MemberRole, m.JoinedAt))
	}

	return connect.NewResponse(&v1pb.ListChannelMembersResponse{Members: v1Members}), nil
}

// ListThreadParticipants lists the distinct users and agents that posted in a
// thread (the root message plus its replies), derived from message senders. The
// caller must be a member of the conversation.
func (s *CommandService) ListThreadParticipants(ctx context.Context, req *connect.Request[v1pb.ListThreadParticipantsRequest]) (*connect.Response[v1pb.ListThreadParticipantsResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
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

	// Agent-DM conversations (type 3) are agent-only. Users with
	// conversations.reviewAgentDM may read them but must never send into one.
	conv, convErr := s.store.GetConversation(ctx, convID)
	if convErr != nil {
		return nil, connect.NewError(connect.CodeNotFound, convErr)
	}
	if conv.Type == store.ConversationTypeAgentDM {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("agent-DM conversations are agent-only; users can view but cannot send"))
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

	// Server-parse mentions from the content (mirrors the agent PostMessage path)
	// and merge with client-supplied mentions so user→user/@agent mentions reliably
	// drive thread subscription and activity generation even when the client does
	// not construct Mention structs. Self-mention (the caller's own id) is dropped.
	parsedMentions := s.parseContentMentions(ctx, convID, req.Msg.Content, "")
	mentions := mergeMentions(parsedMentions, req.Msg.Mentions, user.ID)

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
			Mentions:       mentions,
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
			Mentions:            mentions,
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
		// user sender has no agent id, so all subscribers are woken. The posting
		// user and any @mentioned users are subscribed via user_thread_participant
		// so they get THREAD activity on subsequent replies.
		s.subscribeAndNotifyThread(ctx, convID, threadRoot.UUID, msg.RoomVersion, mentions, nil, &user.ID)
	} else {
		// Agent-first: the manager never dispatches work on a user message. It
		// only notifies every agent member of the conversation that new messages
		// are available; each agent's autonomous drain loop then decides whether
		// and how to respond. (Agents are conversation policy members of their
		// direct conversations too, so this covers 1:1 chats.)
		s.notifyConversationAgents(ctx, convID, msg.RoomVersion, nil)
	}

	// Generate per-user activity for this message (mention/task/reminder/thread).
	// Best-effort: failures are logged, never fatal. A top-level as_task message
	// is a task root; a thread reply rooted at a task/reminder carries that kind.
	rootIsTask := req.Msg.AsTask
	rootIsReminder := false
	if threadRoot.Valid {
		rootIsTask, rootIsReminder, err = s.store.RootMessageKinds(ctx, threadRoot.UUID)
		if err != nil {
			slog.Warn("failed to resolve thread root kinds for activity", "rootID", threadRoot.UUID, "error", err)
		}
	}
	s.store.GenerateActivityForMessage(msg, rootIsTask, rootIsReminder)

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
// non-nil, plus the agent that authored the thread root, so an agent is woken
// by replies to its own messages) to the thread, and any user @mentioned (plus
// the posting user, when posterUserID is non-nil) via user_thread_participant,
// then wakes every current agent subscriber except posterAgentID that a new
// reply landed. Subscription is persistent — once an agent is subscribed (via
// @mention, its own reply, or its own root message) it is woken on every
// subsequent reply in the thread, even without a fresh @mention; a user
// subscriber gets a THREAD activity on every subsequent reply. Used by
// SendMessage (user, posterAgentID=nil, posterUserID=&user.ID) and PostMessage
// (agent, posterUserID=nil) for thread replies.
func (s *CommandService) subscribeAndNotifyThread(ctx context.Context, convID, rootID uuid.UUID, version int64, mentions []*v1pb.Mention, posterAgentID *int, posterUserID *int) {
	var agentIDs []int
	seen := make(map[int]bool)
	addAgent := func(id int) {
		if id > 0 && !seen[id] {
			seen[id] = true
			agentIDs = append(agentIDs, id)
		}
	}
	var userIDs []int
	userSeen := make(map[int]bool)
	addUser := func(id int) {
		if id > 0 && !userSeen[id] {
			userSeen[id] = true
			userIDs = append(userIDs, id)
		}
	}
	for _, m := range mentions {
		if m.Type == "agent" && m.Id != "" {
			agent, err := s.store.GetAgentByResourceID(ctx, m.Id)
			if err != nil || agent == nil {
				slog.Warn("failed to resolve mentioned agent for thread subscription", "resourceID", m.Id, "error", err)
				continue
			}
			addAgent(agent.ID)
		}
		if m.Type == "user" && m.Id != "" {
			if uid, err := strconv.Atoi(m.Id); err == nil {
				addUser(uid)
			}
		}
	}
	if posterAgentID != nil {
		addAgent(*posterAgentID)
	}
	if posterUserID != nil {
		addUser(*posterUserID)
	}
	// The thread root's author is an implicit participant: when an agent
	// authored the root (e.g. it uploaded the markdown/html file being
	// commented on), subscribe it so every reply in the thread wakes it even
	// without a fresh @mention. This is what lets a user's anchored comment on
	// an agent's attachment reach the agent. Best-effort: a failed lookup only
	// skips the implicit subscription, never the explicit ones above.
	senderType, senderAgentID, err := s.store.GetThreadRootSender(ctx, rootID)
	if err != nil {
		slog.Warn("failed to resolve thread root sender for subscription", "rootID", rootID, "error", err)
	} else if senderType == store.SenderTypeAgent && senderAgentID.Valid {
		addAgent(int(senderAgentID.Int32))
	}
	if err := s.store.AddThreadParticipants(ctx, rootID, agentIDs); err != nil {
		slog.Warn("failed to subscribe thread participants", "rootID", rootID, "error", err)
		// Still notify existing subscribers below.
	}
	if err := s.store.AddUserThreadParticipants(ctx, rootID, userIDs); err != nil {
		slog.Warn("failed to subscribe user thread participants", "rootID", rootID, "error", err)
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

// conversationPeer carries the DM peer's display name and resource name so a
// single resolver call can populate both Conversation.Address (from name) and
// Conversation.Peer (from resource). Zero value (both empty) means no peer.
type conversationPeer struct {
	name     string // display name, used for the "dm:@<name>" address
	resource string // resource name e.g. "users/<id>" or "agents/<id>", used for Conversation.Peer
}

// maxListPreviewLen caps the single-line last-message preview embedded in the
// left-rail conversation list. The client truncates visually via CSS; this
// only bounds the payload while keeping the preview one line.
const maxListPreviewLen = 120

// convertToV1Conversation is the single builder for v1 Conversation. It
// populates Address — the name-based form agents write and read — from the
// conversation type and a caller-resolved peerName:
//   - type 2 (channel): "#<title>"; peerName is unused.
//   - type 1 (user DM): "dm:@<peerName>", where peerName is the DM peer from
//     the viewer's perspective (the agent for a user viewer, the user for an
//     agent viewer).
//   - type 3 (agent DM): "dm:@<peerName>", where peerName is the other agent.
//
// peerResourceName is the DM peer's resource name ("users/<id>" or
// "agents/<id>") from the viewer's perspective, surfaced on Conversation.Peer
// so list viewers can fetch the peer's avatar without an extra member lookup.
// Empty for channels and when no peer can be resolved.
//
// Callers resolve peerName/peerResourceName (they already resolve owner/title
// for their view) so the builder stays free of lookups. Empty peerName leaves
// a DM address empty rather than emitting a malformed "dm:@".
func convertToV1Conversation(conv *store.ConversationMessage, ownerName string, peerName string, peerResourceName string, memberCount int, unreadCount int32, title string, readVersion int64) *v1pb.Conversation {
	var address string
	switch conv.Type {
	case store.ConversationTypeChannel:
		address = "#" + title
	case store.ConversationTypeDM, store.ConversationTypeAgentDM, store.ConversationTypeUserDM:
		if peerName != "" {
			address = "dm:@" + peerName
		}
	default:
		// no address for unknown types
	}
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
		Address:     address,
		ReadVersion: readVersion,
		Peer:        peerResourceName,
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

// resolveAgentDMPeer returns the other agent in a type-3 agent DM — the first
// agent member whose resource_id is not selfResourceID. When selfResourceID is
// empty (no caller-agent perspective, e.g. an admin fetching a single type-3
// conversation via GetChannel) the first resolvable agent member is returned.
// Returns nil when there is no resolvable non-self peer agent (well-formed
// type-3 DMs always have two agent members, so this only happens on a
// degenerate roster); it never returns the viewer's own agent.
func (s *CommandService) resolveAgentDMPeer(ctx context.Context, convID uuid.UUID, selfResourceID string) *store.AgentMessage {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list members for agent-DM peer", "conversationID", convID, "error", err)
		return nil
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		if selfResourceID != "" && m.MemberID == selfResourceID {
			continue
		}
		agent, err := s.store.GetAgentByResourceID(ctx, m.MemberID)
		if err != nil || agent == nil || agent.Name == "" {
			continue
		}
		return agent
	}
	return nil
}

// resolvePeerNameForViewer resolves the DM peer display name for
// convertToV1Conversation from the viewer's perspective:
//   - type 1 (user DM): when the viewer is the agent participant, the peer is
//     the user (owner); otherwise the peer is the agent (conv.AgentID).
//   - type 3 (agent DM): the other agent (the agent member that is not the
//     viewer; the first agent when the viewer is not an agent).
//   - type 4 (user DM): the other user (the user member that is not the
//     viewer; the first user when the viewer is not a user).
//   - type 2 (channel): zero value (channels have no peer).
//
// The viewer's agent resource id is "" when the caller is a user/admin; the
// viewer's user id is 0 when the caller is an agent. Used by GetChannel; the
// list endpoints resolve the peer per-row from their own viewer context.
func (s *CommandService) resolvePeerForViewer(ctx context.Context, conv *store.ConversationMessage, viewerAgentResourceID string, viewerUserID int) conversationPeer {
	switch conv.Type {
	case store.ConversationTypeDM:
		if viewerAgentResourceID != "" {
			// The agent viewer's peer is the user owner.
			return conversationPeer{name: resolveUserName(ctx, s.store, conv.OwnerID), resource: common.FormatUserUID(conv.OwnerID)}
		}
		if !conv.AgentID.Valid {
			return conversationPeer{}
		}
		agent, err := s.store.GetAgent(ctx, int(conv.AgentID.Int32))
		if err != nil || agent == nil {
			return conversationPeer{}
		}
		return conversationPeer{name: agent.Name, resource: common.FormatAgentUID(agent.ResourceID)}
	case store.ConversationTypeAgentDM:
		peer := s.resolveAgentDMPeer(ctx, conv.ID, viewerAgentResourceID)
		if peer == nil {
			return conversationPeer{}
		}
		return conversationPeer{name: peer.Name, resource: common.FormatAgentUID(peer.ResourceID)}
	case store.ConversationTypeUserDM:
		peer := s.resolveUserDMPeer(ctx, conv.ID, viewerUserID)
		if peer == nil {
			return conversationPeer{}
		}
		return conversationPeer{name: peer.Name, resource: common.FormatUserUID(peer.ID)}
	}
	return conversationPeer{}
}

// resolveUserDMPeer returns the other user in a type-4 user DM — the first
// user member whose principal id is not viewerUserID. When viewerUserID is 0
// (no caller-user perspective, e.g. an admin fetching a single type-4
// conversation via GetChannel) the first resolvable user member is returned.
// Returns nil when there is no resolvable non-self peer user (well-formed
// type-4 DMs always have two user members, so this only happens on a
// degenerate roster); it never returns the viewer's own user.
func (s *CommandService) resolveUserDMPeer(ctx context.Context, convID uuid.UUID, viewerUserID int) *store.UserMessage {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list members for user-DM peer", "conversationID", convID, "error", err)
		return nil
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeUser {
			continue
		}
		pid, parseErr := strconv.Atoi(m.MemberID)
		if parseErr != nil {
			continue
		}
		if viewerUserID != 0 && pid == viewerUserID {
			continue
		}
		user, err := s.store.GetUserByID(ctx, pid)
		if err != nil || user == nil || user.Name == "" {
			continue
		}
		return user
	}
	return nil
}

// FetchConversationActivity returns the execution status of every agent in a
// conversation. The frontend polls this to show real-time agent status in the
// channel header.
func (s *CommandService) FetchConversationActivity(ctx context.Context, req *connect.Request[v1pb.FetchConversationActivityRequest]) (*connect.Response[v1pb.FetchConversationActivityResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
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
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
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
	// Advance the user's unread activity rows in this conversation to READ.
	// Threads share the conversation's room_version space, so reading the
	// channel marks all thread activity in it read too. Best-effort: a failure
	// only leaves an activity as UNREAD (still visible under Unread), never data
	// corruption, so it is logged rather than failing the read itself.
	if err := s.store.MarkConversationActivitiesRead(ctx, user.ID, convID, readVersion); err != nil {
		slog.Warn("failed to mark conversation activities read", "conversationID", convID, "userID", user.ID, "error", err)
	}
	return connect.NewResponse(&v1pb.MarkConversationReadResponse{ReadVersion: readVersion}), nil
}

func (s *CommandService) SetConversationPinned(ctx context.Context, req *connect.Request[v1pb.SetConversationPinnedRequest]) (*connect.Response[v1pb.SetConversationPinnedResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SetConversationPinned is for authenticated users"))
	}
	// SetConversationPinned updates the caller's own membership row; a missing
	// row (non-member) returns ErrConversationMemberNotFound, which doubles as
	// the membership gate so only members can pin.
	if err := s.store.SetConversationPinned(ctx, convID, user.ID, req.Msg.Pinned); err != nil {
		if errors.Is(err, store.ErrConversationMemberNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("must be a member to pin a conversation"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set conversation pinned"))
	}
	return connect.NewResponse(&v1pb.SetConversationPinnedResponse{}), nil
}

func (s *CommandService) SetConversationClosed(ctx context.Context, req *connect.Request[v1pb.SetConversationClosedRequest]) (*connect.Response[v1pb.SetConversationClosedResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SetConversationClosed is for authenticated users"))
	}
	// SetConversationClosed updates the caller's own membership row; a missing
	// row (non-member) returns ErrConversationMemberNotFound, which doubles as
	// the membership gate so only members can close.
	if err := s.store.SetConversationClosed(ctx, convID, user.ID, req.Msg.Closed); err != nil {
		if errors.Is(err, store.ErrConversationMemberNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("must be a member to close a conversation"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set conversation closed"))
	}
	return connect.NewResponse(&v1pb.SetConversationClosedResponse{}), nil
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

// resolveMemberProfile returns a member's self-description, avatar resource
// name, and preferred language from a single user/agent lookup. For users the
// description is User.description, the avatar is users/{id}/avatar when the
// user has uploaded one (empty otherwise), and the language is the user's
// chat_preferences preferred_language (UNSPECIFIED when unset); for agents the
// description is the admin-authored persona_prompt, the avatar is
// agents/{agent}/avatar when uploaded (empty otherwise), and the language is
// always UNSPECIFIED. Surfaced in channel/thread rosters so an agent can
// perceive who a member is, render avatars without a per-user lookup, and
// converse in the member's preferred language.
func resolveMemberProfile(ctx context.Context, s *store.Store, memberType int32, memberID string) (string, string, v1pb.PreferredLanguage) {
	if memberType == store.MemberTypeUser {
		uid, err := strconv.Atoi(memberID)
		if err != nil {
			return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
		}
		u, err := s.GetUserByID(ctx, uid)
		if err != nil || u == nil {
			return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
		}
		avatar := ""
		if u.AvatarS3Key != "" {
			avatar = common.FormatUserAvatar(u.ID)
		}
		return u.Description, avatar, v1pb.PreferredLanguage(u.ChatPreferences.GetPreferredLanguage())
	}
	if memberType == store.MemberTypeAgent {
		agent, err := s.GetAgentByResourceID(ctx, memberID)
		if err != nil || agent == nil {
			return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
		}
		avatar := ""
		if agent.AvatarS3Key != "" {
			avatar = common.FormatAgentAvatar(agent.ResourceID)
		}
		return agent.Info.GetAcpConfig().GetPersonaPrompt(), avatar, v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
	}
	return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
}

// buildChannelMember assembles a v1 ChannelMember from a membership row, resolving
// the display name and self-description. Shared by ListChannelMembers and
// ListThreadParticipants so both rosters render identity consistently.
func buildChannelMember(ctx context.Context, s *store.Store, memberType int32, memberID string, role int32, joinedAt time.Time) *v1pb.ChannelMember {
	description, avatar, language := resolveMemberProfile(ctx, s, memberType, memberID)
	m := &v1pb.ChannelMember{
		MemberType:        memberType,
		MemberId:          memberID,
		DisplayName:       resolveMemberDisplayName(ctx, s, memberType, memberID),
		MemberRole:        role,
		Description:       description,
		Avatar:            avatar,
		PreferredLanguage: language,
	}
	if !joinedAt.IsZero() {
		m.JoinedAt = timestamppb.New(joinedAt)
	}
	return m
}
