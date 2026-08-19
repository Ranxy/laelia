package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// Member type / role values mirror backend/manager/store.ConversationMember, but
// chattools lives in backend/agent and must not import the store package, so the
// integer constants are restated here. Drift would surface as "unknown" in output
// rather than a build break, which is the safer failure mode for a display helper.
const (
	memberTypeUser  int32 = 1
	memberTypeAgent int32 = 2

	memberRoleOwner  int32 = 1
	memberRoleMember int32 = 2
	memberRoleAdmin  int32 = 3
)

// --- Members input -------------------------------------------------------

// ListMembersInput scopes the roster lookup: a conversation, and optionally a
// thread root. With Root empty the roster is the conversation's members; with
// Root set it is the distinct senders of that thread (root + replies). One tool,
// one call — the agent perceives who is present and each co-agent's public
// description in a single lookup.
type ListMembersInput struct {
	Conversation string `json:"conversation"`
	// Root is an optional thread-root message id; when set, the roster is the
	// thread's participants instead of the channel's members.
	Root string `json:"root,omitempty"`
}

// memberTypeString renders a ChannelMember member_type for roster output.
func memberTypeString(t int32) string {
	switch t {
	case memberTypeUser:
		return "user"
	case memberTypeAgent:
		return "agent"
	}
	return "unknown"
}

// memberRoleString renders a ChannelMember member_role; an empty/zero role
// (e.g. thread participants, where role is not meaningful) yields "".
func memberRoleString(r int32) string {
	switch r {
	case memberRoleOwner:
		return "owner"
	case memberRoleMember:
		return "member"
	case memberRoleAdmin:
		return "admin"
	}
	return ""
}

// preferredLanguageString renders a ChannelMember preferred_language for roster
// output, or "" when unset (UNSPECIFIED), in which case the agent picks the most
// appropriate language on its own.
func preferredLanguageString(l v1pb.PreferredLanguage) string {
	switch l {
	case v1pb.PreferredLanguage_PREFERRED_LANGUAGE_ZH_CN:
		return "zh-CN"
	case v1pb.PreferredLanguage_PREFERRED_LANGUAGE_EN_US:
		return "en-US"
	case v1pb.PreferredLanguage_PREFERRED_LANGUAGE_JA_JP:
		return "ja-JP"
	}
	return ""
}

// formatMemberLine renders one roster entry: the header line (type, display
// name, @<handle> mention token for every member, role when meaningful),
// followed by the member's full public description as an indented block when
// present — for users this is their self-description, for agents it is
// Agent.description (the public intro). The agent's private persona_prompt is
// never exposed in rosters. The full text is emitted untruncated so the agent
// gets every co-agent's public description in the one call that produced this
// roster.
//
// The @<handle> token is shown for ALL members (users and agents alike) and is
// the exact text to copy into a reply to mention that member. Before the
// handle migration only agents had an [agents/<id>] suffix and users had no
// handle at all, so agents could not address users by handle and often fell
// back to display names — which no longer resolve. Surfacing @<handle>
// universally closes that gap.
func formatMemberLine(m *v1pb.ChannelMember) string {
	if m == nil {
		return ""
	}
	line := fmt.Sprintf("- [%s] %s", memberTypeString(m.MemberType), m.DisplayName)
	// handle is the member's mention handle (identical to member_id); show it
	// as @<handle> so the agent can copy it straight into a reply.
	handle := m.Handle
	if handle == "" {
		handle = m.MemberId
	}
	if handle != "" {
		line += fmt.Sprintf(" @%s", handle)
	}
	if role := memberRoleString(m.MemberRole); role != "" {
		line += fmt.Sprintf(" (%s)", role)
	}
	if m.MemberType == memberTypeUser {
		if lang := preferredLanguageString(m.PreferredLanguage); lang != "" {
			line += fmt.Sprintf(" (language: %s)", lang)
		}
	}
	line += "\n"
	if desc := strings.TrimSpace(m.Description); desc != "" {
		for _, l := range strings.Split(desc, "\n") {
			line += "  " + l + "\n"
		}
	}
	return line
}

// --- Members operation ----------------------------------------------------

// ListMembers returns the roster of a conversation's members (or, with Root, the
// distinct senders of a thread), each with their full public description inline
// — for users their self-description, for agents Agent.description (the public
// intro). Run this before deciding whom to @mention so the addressing is
// grounded in who is present, each person's role, and each co-agent's public
// description, all in one call.
//
// The agent only writes @<handle> in its reply content; the manager resolves
// the token to the member.
func ListMembers(ctx context.Context, d Deps, in ListMembersInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required (pass the address from the batch header or `laelia-machine message check`, e.g. #general or dm:@alice)", "")
	}

	if root := bareRootID(in.Root); root != "" {
		resp, err := d.Client.ListThreadParticipants(ctx, connect.NewRequest(&v1pb.ListThreadParticipantsRequest{
			Conversation: name,
			ThreadRoot:   root,
		}))
		if err != nil {
			return "", wrapManagerError(err)
		}
		return formatRoster(fmt.Sprintf("Participants in thread %s of %s", root, quoteAddress(conversationAddress(ctx, d, name))), resp.Msg.Members), nil
	}

	resp, err := d.Client.ListChannelMembers(ctx, connect.NewRequest(&v1pb.ListChannelMembersRequest{Conversation: name}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return formatRoster(fmt.Sprintf("Members in %s", quoteAddress(conversationAddress(ctx, d, name))), resp.Msg.Members), nil
}

// formatRoster renders the shared roster body: a header with the count, one
// formatMemberLine per member (or "(none)"), and a footer reminding the agent
// how to address someone.
func formatRoster(header string, members []*v1pb.ChannelMember) string {
	text := fmt.Sprintf("%s (%d):\n", header, len(members))
	if len(members) == 0 {
		text += "(none)\n"
		return text
	}
	for _, m := range members {
		text += formatMemberLine(m)
	}
	text += "\nTo address someone, write @<handle> in your reply content (the manager resolves it). Handles are unique and self-describing (e.g. @ran-user-1, @rei-agent-1).\n"
	return text
}
