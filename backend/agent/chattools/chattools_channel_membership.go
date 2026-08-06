package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// This file implements the agent's channel membership mutation tools: leaving a
// channel it is a member of, and adding members (users or agents) to a channel
// it manages. Both call the manager's existing LeaveChannel / AddChannelMember
// RPCs; the permission model is identical to a user's — the caller must hold
// conversations.manage (channel Admin/Owner) to add, and a private agent
// (allow_add_to_channel=false) can never be added by another agent.

// LeaveChannelInput names the channel to leave as a conversation address.
type LeaveChannelInput struct {
	Conversation string `json:"conversation"`
}

// LeaveChannel removes the agent from a channel it is a member of. The manager
// rejects the channel owner (agents are never owners) and non-members; after a
// successful leave the channel stops appearing in `message check` and the agent
// can no longer post to it.
func LeaveChannel(ctx context.Context, d Deps, in LeaveChannelInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "channel is required (pass the address, e.g. '#support')", "Pass the channel address, e.g. `channel leave '#support'`.")
	}

	if _, err := d.Client.LeaveChannel(ctx, connect.NewRequest(&v1pb.LeaveChannelRequest{Conversation: name})); err != nil {
		return "", wrapManagerError(err)
	}
	return fmt.Sprintf("Left %s. You are no longer a member: you stop receiving `message check` updates and can no longer post. Rejoin with `channel join` if you can still read it.\n", quoteAddress(conversationAddress(ctx, d, name))), nil
}

// AddChannelMemberInput names the channel and the members to add. Each member is
// a display name (resolved agent-first, then user), an agents/<id> handle, or a
// users/<id> handle.
type AddChannelMemberInput struct {
	Conversation string   `json:"conversation"`
	Members      []string `json:"members"`
}

// AddChannelMember adds members to a channel the agent manages. The manager
// enforces the same rules as a user adding members: the caller must hold
// conversations.manage (channel Admin/Owner), and a private agent
// (allow_add_to_channel=false) cannot be added by another agent.
func AddChannelMember(ctx context.Context, d Deps, in AddChannelMemberInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "channel is required (pass the address, e.g. '#general')", "Pass the channel address, e.g. `channel add-member '#general' @alice`.")
	}
	if len(in.Members) == 0 {
		return "", localError("INVALID_ARGUMENT_FAILED", "at least one member is required", "Pass display names or agents/<id> / users/<id> handles, e.g. `channel add-member '#general' @alice agents/abc-123`.")
	}

	inputs := make([]*v1pb.AddChannelMemberInput, 0, len(in.Members))
	for _, m := range in.Members {
		memberType, memberID, err := resolveMember(ctx, d, m)
		if err != nil {
			return "", err
		}
		inputs = append(inputs, &v1pb.AddChannelMemberInput{MemberType: memberType, MemberId: memberID})
	}

	resp, err := d.Client.AddChannelMember(ctx, connect.NewRequest(&v1pb.AddChannelMemberRequest{
		Conversation: name,
		Members:      inputs,
	}))
	if err != nil {
		e := wrapManagerError(err)
		// The private-agent gate (allow_add_to_channel=false) is a specific,
		// recoverable denial: the agent should ask the target's owner to enable
		// the switch, not give up. The server message already explains the reason;
		// override the generic NextAction with the recovery hint.
		if e != nil && e.Code == "PERMISSION_FAILED" && strings.Contains(e.Message, "does not allow being added") {
			e.NextAction = "The target agent does not allow being added to channels. Ask its owner to enable 'allow being added to channels' (allow_add_to_channel) on the agent, then retry."
		}
		return "", e
	}

	addr := quoteAddress(conversationAddress(ctx, d, name))
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Added %d member(s) to %s:\n", len(resp.Msg.GetMembers()), addr)
	for _, m := range resp.Msg.GetMembers() {
		_, _ = b.WriteString(formatMemberLine(m))
	}
	_, _ = b.WriteString("They are now members and can post in the channel.\n")
	return b.String(), nil
}

// RemoveChannelMemberInput names the channel and the members to remove.
type RemoveChannelMemberInput struct {
	Conversation string   `json:"conversation"`
	Members      []string `json:"members"`
}

// RemoveChannelMember removes members from a channel the agent manages. The
// manager enforces the same rules as a user removing members: the caller must
// hold conversations.manageMembers (channel Admin/Owner, or an agent whose
// owner is a channel Admin/Owner with can_manage_channel_members enabled), and
// the channel owner cannot be removed.
func RemoveChannelMember(ctx context.Context, d Deps, in RemoveChannelMemberInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "channel is required (pass the address, e.g. '#general')", "Pass the channel address, e.g. `channel remove-member '#general' @alice`.")
	}
	if len(in.Members) == 0 {
		return "", localError("INVALID_ARGUMENT_FAILED", "at least one member is required", "Pass display names or agents/<id> / users/<id> handles, e.g. `channel remove-member '#general' @alice agents/abc-123`.")
	}

	removed := make([]string, 0, len(in.Members))
	for _, m := range in.Members {
		memberType, memberID, err := resolveMember(ctx, d, m)
		if err != nil {
			return "", err
		}
		if _, err := d.Client.RemoveChannelMember(ctx, connect.NewRequest(&v1pb.RemoveChannelMemberRequest{
			Conversation: name,
			MemberType:   memberType,
			MemberId:     memberID,
		})); err != nil {
			return "", wrapManagerError(err)
		}
		removed = append(removed, m)
	}

	addr := quoteAddress(conversationAddress(ctx, d, name))
	return fmt.Sprintf("Removed %d member(s) from %s: %s.\n", len(removed), addr, strings.Join(removed, ", ")), nil
}

// resolveMember turns a member argument into the (member_type, member_id) pair
// the manager's AddChannelMember expects. An explicit agents/<id> or users/<id>
// handle is passed through; a bare display name resolves agent-first (matching
// the DM address resolver), then to a user.
func resolveMember(ctx context.Context, d Deps, member string) (int32, string, error) {
	member = strings.TrimSpace(member)
	switch {
	case strings.HasPrefix(member, "agents/"):
		id := strings.TrimSpace(strings.TrimPrefix(member, "agents/"))
		if id == "" {
			return 0, "", localError("INVALID_ARGUMENT_FAILED", "agents/ requires a resource id", "Use the [agents/<id>] handle from `agent list` or `members`.")
		}
		return memberTypeAgent, id, nil
	case strings.HasPrefix(member, "users/"):
		id := strings.TrimSpace(strings.TrimPrefix(member, "users/"))
		if id == "" {
			return 0, "", localError("INVALID_ARGUMENT_FAILED", "users/ requires a principal id", "Use users/<id>.")
		}
		return memberTypeUser, id, nil
	default:
		agent, err := findPeerAgentByName(ctx, d, member)
		if err != nil {
			return 0, "", err
		}
		if agent != nil {
			return memberTypeAgent, strings.TrimPrefix(agent.GetName(), "agents/"), nil
		}
		user, err := findUserByName(ctx, d, member)
		if err != nil {
			return 0, "", err
		}
		if user != nil {
			return memberTypeUser, strings.TrimPrefix(user.GetName(), "users/"), nil
		}
		return 0, "", localError("NOT_FOUND_FAILED", fmt.Sprintf("no agent or user named %q", member), "Run `agent list` for agents, or use users/<id> for a user.")
	}
}
