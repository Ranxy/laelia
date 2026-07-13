package v1

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// callerMemberInfo resolves the caller (user or agent) into the
// (memberType, memberID) pair used by the conversation_member table. Agents are
// members by their resource ID; users by their principal ID.
func callerMemberInfo(user *store.UserMessage, agent *store.AgentMessage) (memberType int32, memberID string, ok bool) {
	switch {
	case user != nil:
		return store.MemberTypeUser, fmt.Sprintf("%d", user.ID), true
	case agent != nil:
		return store.MemberTypeAgent, agent.ResourceID, true
	}
	return 0, "", false
}

// requireConversationMember parses convName, resolves the caller, and ensures
// the caller is a member of the conversation. Workspace admins bypass the
// membership check. Both users and agents are resolved from context, which
// closes the previous behavior where an agent caller (user == nil) bypassed
// ownership checks. Returns the parsed conversation ID on success.
func requireConversationMember(ctx context.Context, stores *store.Store, convName string) (uuid.UUID, error) {
	convID, err := parseConversationID(convName)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	user, _ := GetUserFromContext(ctx)
	agent, _ := GetAgentFromContext(ctx)
	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	isMember, err := stores.IsConversationMember(ctx, convID, memberType, memberID)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}

	// Workspace admins can access any conversation.
	if !isMember && user != nil {
		admin, adminErr := isUserWorkspaceAdmin(ctx, stores, user)
		if adminErr != nil {
			return uuid.Nil, connect.NewError(connect.CodeInternal, errors.Wrap(adminErr, "failed to resolve workspace admin"))
		}
		isMember = admin
	}

	if !isMember {
		return uuid.Nil, connect.NewError(connect.CodePermissionDenied, errors.New("not a conversation member"))
	}
	return convID, nil
}

// requireAgentEditor gates agent profile mutations (ACP config, providers, token
// rotate/revoke, delete, force-disconnect): only the agent's creator or a
// workspace admin may proceed. Agent callers are always denied (agents never
// edit agent profiles). A legacy agent with no recorded creator (CreatedBy == 0)
// is admin-only. Mirrors requireChannelOwner's owner-or-admin shape.
func requireAgentEditor(ctx context.Context, stores *store.Store, agent *store.AgentMessage) error {
	user, _ := GetUserFromContext(ctx)
	if user == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("only the agent creator or a workspace admin may modify this agent"))
	}
	if agent.CreatedBy != 0 && user.ID == agent.CreatedBy {
		return nil
	}
	admin, err := isUserWorkspaceAdmin(ctx, stores, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve workspace admin"))
	}
	if admin {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("only the agent creator or a workspace admin may modify this agent"))
}

// requireChannelOwner ensures the caller is the owner of the channel or a
// workspace admin. Channel owners are always users (agents are added as
// members, never owners), so an agent caller is denied. This closes the
// previous `user != nil && conv.OwnerID != user.ID` pattern that let an agent
// token (user == nil) bypass owner-only mutations.
func requireChannelOwner(ctx context.Context, stores *store.Store, conv *store.ConversationMessage) error {
	user, _ := GetUserFromContext(ctx)
	if user == nil {
		// Agent callers cannot perform owner-only channel mutations.
		return connect.NewError(connect.CodePermissionDenied, errors.New("only channel owner can manage the channel"))
	}
	if conv.OwnerID == user.ID {
		return nil
	}
	admin, err := isUserWorkspaceAdmin(ctx, stores, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve workspace admin"))
	}
	if admin {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("only channel owner can manage the channel"))
}

// requireCommandAccess gates command-scoped RPCs (WatchCommand,
// WatchCommandEvents, GetCommandContext, RespondPermission, CancelCommand).
// Access is granted to:
//   - the agent that owns the command (agents read their own command output),
//   - a workspace admin,
//   - any member of a conversation the command is linked to.
//
// A multi-channel drain turn may link its command to several conversations via
// command_conversation, so membership is checked against ANY linked
// conversation — not just the first-wins "primary" on command.conversation_id.
// Commands without any linked conversation (e.g. a not-yet-linked autonomous
// session) are only accessible to the owning agent or an admin.
func requireCommandAccess(ctx context.Context, stores *store.Store, cmd *store.CommandMessage) error {
	agent, _ := GetAgentFromContext(ctx)
	if agent != nil && agent.ID == cmd.AgentID {
		return nil
	}

	user, _ := GetUserFromContext(ctx)
	if user != nil {
		admin, err := isUserWorkspaceAdmin(ctx, stores, user)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve workspace admin"))
		}
		if admin {
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
	isMember, err := stores.IsCommandConversationMember(ctx, cmd.ID, memberType, memberID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check command conversation membership"))
	}
	if isMember {
		return nil
	}

	return connect.NewError(connect.CodePermissionDenied, errors.New("no access to command"))
}
