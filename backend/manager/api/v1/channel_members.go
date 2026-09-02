package v1

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

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
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
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
				userHandle, uidErr := common.GetUserHandle(gm.GetMember())
				if uidErr != nil {
					// Malformed member inside a group payload: skip, never fail
					// the whole snapshot for one bad row.
					continue
				}
				memberID := userHandle
				key := fmt.Sprintf("%d:%s", store.MemberTypeUser, memberID)
				if seen[key] || memberID == ownerHandle {
					continue
				}
				existingRole, _, memErr := s.store.GetConversationMembership(ctx, convID, store.MemberTypeUser, memberID)
				if memErr != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(memErr, "failed to check existing membership"))
				}
				if existingRole != 0 {
					continue
				}
				user, userErr := s.store.GetUserByHandle(ctx, userHandle)
				if userErr != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(userErr, "failed to look up user %s", userHandle))
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
		if memberType == store.MemberTypeUser && memberID == ownerHandle {
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
			user, userErr := s.store.GetUserByHandle(ctx, memberID)
			if userErr != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(userErr, "failed to look up user %s", memberID))
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

	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	if memberType == store.MemberTypeUser && memberID == ownerHandle {
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
	newOwnerUser, userErr := s.store.GetUserByHandle(ctx, req.Msg.MemberId)
	if userErr != nil || newOwnerUser == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user member_id, must be a user handle"))
	}
	newOwnerID := newOwnerUser.Handle
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	if newOwnerID == ownerHandle {
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

	if err := s.store.TransferChannelOwnership(ctx, convID, ownerHandle, newOwnerID, newOwnerUser.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to transfer ownership"))
	}

	updated, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to reload conversation"))
	}
	memberCount, _ := s.store.GetConversationMemberCount(ctx, updated.ID)
	ownerName := resolveUserName(ctx, s.store, updated.OwnerID)
	ownerHandle = resolveUserHandle(ctx, s.store, updated.OwnerID)
	return connect.NewResponse(&v1pb.TransferChannelOwnershipResponse{
		Conversation: convertToV1Conversation(updated, ownerName, ownerHandle, "", "", memberCount, 0, updated.Title, 0),
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
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	if memberType == store.MemberTypeUser && memberID == ownerHandle {
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
			memberID = ts.Handle
			if memberID == "" {
				continue
			}
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
