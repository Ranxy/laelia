package v1

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// callerMemberInfo resolves the caller (user or agent) into the
// (memberType, memberID) pair used by the conversation_member table. Agents are
// members by their resource ID; users by their principal ID. Used by the IAM
// engine's object branches and by the remaining owner-of-record handler
// checks.
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
