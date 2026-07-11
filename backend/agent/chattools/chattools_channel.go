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
)

// --- Channel / thread inputs ---------------------------------------------

type ListChannelMembersInput struct {
	Conversation string `json:"conversation"`
}

type ListThreadParticipantsInput struct {
	Conversation string `json:"conversation"`
	Root         string `json:"root"`
}

type GetAgentProfileInput struct {
	Conversation string `json:"conversation"`
	Agent        string `json:"agent"`
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
	}
	return ""
}

// truncateDescription clips a self-description to roughly n runes for roster
// display, appending an ellipsis when it was truncated. The full text for
// agents is available via GetAgentProfile; the roster only needs a preview so
// it does not drown out the member list.
func truncateDescription(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// formatMemberLine renders one roster entry. Agents carry their "agents/<id>"
// resource handle so the agent can pass it straight to `agent detail`; users
// show only their display name and role. The description, when present,
// follows an em dash so it reads as a short bio.
func formatMemberLine(m *v1pb.ChannelMember) string {
	if m == nil {
		return ""
	}
	line := fmt.Sprintf("- [%s] %s", memberTypeString(m.MemberType), m.DisplayName)
	if m.MemberType == memberTypeAgent && m.MemberId != "" {
		line += fmt.Sprintf(" [agents/%s]", m.MemberId)
	}
	if role := memberRoleString(m.MemberRole); role != "" {
		line += fmt.Sprintf(" (%s)", role)
	}
	if desc := truncateDescription(m.Description, 140); desc != "" {
		line += fmt.Sprintf(" — %s", desc)
	}
	return line + "\n"
}

// --- Channel / thread operations ------------------------------------------

// ListChannelMembers returns the roster of a conversation's users and agents
// with a short description for each (User.description for users,
// persona_prompt preview for agents). Run this before deciding whom to
// @mention for a task so the addressing is grounded in who is actually present
// and what each person does. For an agent's full persona_prompt, use
// GetAgentProfile with the "agents/<id>" handle printed here.
func ListChannelMembers(ctx context.Context, d Deps, in ListChannelMembersInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required (pass the conversation name from `laelia-agent message check`)", "")
	}
	resp, err := d.Client.ListChannelMembers(ctx, connect.NewRequest(&v1pb.ListChannelMembersRequest{Conversation: name}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Members in %s (%d):\n", name, len(resp.Msg.Members))
	if len(resp.Msg.Members) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, m := range resp.Msg.Members {
		text += formatMemberLine(m)
	}
	text += "\nTo address someone, write @<display_name> in your reply content (the manager resolves it). For an agent's full persona_prompt, run `laelia-agent agent detail --conversation <c> --agent agents/<id>` with the [agents/<id>] handle above.\n"
	return text, nil
}

// ListThreadParticipants returns the distinct senders (users and agents) that
// posted in a thread — the root message and its replies — so the agent can see
// exactly who took part in a specific conversation thread and address them.
// Roles are not meaningful for threads, so they are omitted.
func ListThreadParticipants(ctx context.Context, d Deps, in ListThreadParticipantsInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.Root == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "root is required (the thread root message id from `thread check`)", "Pass --root <thread_root>.")
	}
	root := normalizeThreadRoot(in.Root)

	resp, err := d.Client.ListThreadParticipants(ctx, connect.NewRequest(&v1pb.ListThreadParticipantsRequest{
		Conversation: name,
		ThreadRoot:   root,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Participants in thread %s of %s (%d):\n", root, name, len(resp.Msg.Members))
	if len(resp.Msg.Members) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, m := range resp.Msg.Members {
		text += formatMemberLine(m)
	}
	text += "\nTo address someone, write @<display_name> in your thread reply content (the manager resolves it).\n"
	return text, nil
}

// GetAgentProfile fetches one co-member agent's full profile — title, status,
// and the complete persona_prompt (the admin-authored self-awareness prompt)
// — so the agent can decide how to best address a specific agent for a task.
// Pass the "agents/<id>" handle printed by `channel members` / `thread participants`.
func GetAgentProfile(ctx context.Context, d Deps, in GetAgentProfileInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.Agent == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "agent is required (the agents/<id> handle from `channel members`)", "Pass --agent agents/<id>.")
	}
	agent := in.Agent
	if !strings.HasPrefix(agent, "agents/") {
		agent = "agents/" + agent
	}

	resp, err := d.Client.GetConversationAgentProfile(ctx, connect.NewRequest(&v1pb.GetConversationAgentProfileRequest{
		Conversation: name,
		Agent:        agent,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	p := resp.Msg
	text := fmt.Sprintf("Agent profile for %s (agents/%s):\n", p.Name, strings.TrimPrefix(agent, "agents/"))
	if p.Title != "" {
		text += fmt.Sprintf("  title: %s\n", p.Title)
	}
	if p.Status != "" {
		text += fmt.Sprintf("  status: %s\n", p.Status)
	}
	persona := strings.TrimSpace(p.PersonaPrompt)
	if persona == "" {
		text += "  persona_prompt: (none)\n"
	} else {
		text += "  persona_prompt:\n"
		for _, line := range strings.Split(persona, "\n") {
			text += fmt.Sprintf("    %s\n", line)
		}
	}
	return text, nil
}
