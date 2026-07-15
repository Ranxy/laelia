package v1

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// callerMemberInfo resolves the caller (user or agent) into the
// (memberType, memberID) pair used by the conversation_member table. Agents are
// members by their resource ID; users by their principal ID. Shared by the
// handler-gated checks that still read the chat-membership table directly
// (requireCommandAccess, requireReminderAccess) — the IAM interceptor handles
// the per-conversation read/send/manage and per-agent edit checks.
func callerMemberInfo(user *store.UserMessage, agent *store.AgentMessage) (memberType int32, memberID string, ok bool) {
	switch {
	case user != nil:
		return store.MemberTypeUser, fmt.Sprintf("%d", user.ID), true
	case agent != nil:
		return store.MemberTypeAgent, agent.ResourceID, true
	}
	return 0, "", false
}

// requireChannelOwner ensures the caller is the channel's owner of record. Chat
// authorization is membership-based and admins are not special (decision 5): the
// IAM interceptor already granted conversations.manage to Admin+Owner, so this
// gate enforces the owner-only operations (delete channel, transfer ownership,
// grant/revoke admin) that go beyond manage. Channel owners are always users
// (agents are members, never owners), so an agent caller is denied. The owner of
// record is conversation.owner_id, kept in sync with the Owner membership role.
func requireChannelOwner(ctx context.Context, conv *store.ConversationMessage) error {
	user, _ := GetUserFromContext(ctx)
	if user == nil || conv.OwnerID != user.ID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("only the channel owner can perform this action"))
	}
	return nil
}

// requireCommandAccess gates command-scoped RPCs (WatchCommand,
// WatchCommandEvents, GetCommandContext, RespondPermission, CancelCommand).
// Access is granted to:
//   - the agent that owns the command (agents read their own command output),
//   - a user holding laelia.conversations.reviewAll (cross-conversation
//     oversight, including raw structured command events),
//   - any member of a conversation the command is linked to.
//
// A multi-channel drain turn may link its command to several conversations via
// command_conversation, so membership is checked against ANY linked
// conversation — not just the first-wins "primary" on command.conversation_id.
// Commands without any linked conversation (e.g. a not-yet-linked autonomous
// session) are only accessible to the owning agent or a reviewAll holder.
func (s *CommandService) requireCommandAccess(ctx context.Context, cmd *store.CommandMessage) error {
	agent, _ := GetAgentFromContext(ctx)
	if agent != nil && agent.ID == cmd.AgentID {
		return nil
	}

	user, _ := GetUserFromContext(ctx)
	if user != nil {
		ok, err := s.iam.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check reviewAll permission"))
		}
		if ok {
			return nil
		}
	}

	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	// Grant access if the caller is a member of any conversation this command
	// touched (junction-linked). Covers the primary too, since the junction is
	// always populated alongside command.conversation_id.
	isMember, err := s.store.IsCommandConversationMember(ctx, cmd.ID, memberType, memberID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check command conversation membership"))
	}
	if isMember {
		return nil
	}

	return connect.NewError(connect.CodePermissionDenied, errors.New("no access to command"))
}
