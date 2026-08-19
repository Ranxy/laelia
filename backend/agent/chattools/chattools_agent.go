package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// --- Peer-agent discovery -----------------------------------------------

type ListPeerAgentsInput struct{}

// ListPeerAgents renders the global peer-agent roster: every other agent (the
// caller excluded) with its display name, agents/<id> handle, connection state,
// and public description as an indented block — the discovery tool an agent
// uses before delegating work to a peer via `message send dm:@<handle>`. It is
// the cross-conversation counterpart of `members` (which is scoped to one
// channel/thread): one call returns every co-agent's public description, so the
// agent can pick the right peer and address it without a second round-trip. A
// peer that has been stopped (StopAgent) is listed with state "(stopped)": it
// is not processing sessions, so delegating to it will not get a reply until it
// is started again.
func ListPeerAgents(ctx context.Context, d Deps, _ ListPeerAgentsInput) (string, error) {
	resp, err := d.Client.ListPeerAgents(ctx, connect.NewRequest(&v1pb.ListPeerAgentsRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	agents := resp.Msg.GetAgents()
	text := fmt.Sprintf("Peer agents (%d):\n", len(agents))
	if len(agents) == 0 {
		text += "(none — you are the only agent)\n"
		return text, nil
	}
	for _, a := range agents {
		text += formatPeerAgentLine(a)
	}
	text += "\nTo delegate to a peer, run `laelia-machine message send dm:@<handle> --content \"...\" --base-version 0` (the DM is opened if it does not exist). Delegation is ASYNC — post your request and end your turn; the peer's reply wakes you next turn. Do NOT poll or block waiting for a reply. Reuse the same dm:@<handle> for the whole delegation thread. Handles are unique and self-describing (e.g. dm:@rei-agent-1), so no disambiguation is ever needed.\n"
	return text, nil
}

// formatPeerAgentLine renders one peer-agent entry: a header line carrying the
// [agent] type, display name, @<handle> mention token (copyable straight into
// dm:@<handle>), and connection state (online/offline/error/kicked/stopped);
// followed by the agent's public description as an indented block, emitted
// untruncated so one roster call carries every co-agent's public description.
// The peer's private persona_prompt is never sent to other agents. Stopped
// peers additionally get a "(stopped — not processing sessions)" hint line so
// the caller does not delegate to them.
func formatPeerAgentLine(a *v1pb.PeerAgent) string {
	if a == nil {
		return ""
	}
	handle := strings.TrimSpace(a.GetHandle())
	if handle == "" {
		handle = strings.TrimSpace(a.GetName()) // fall back to "agents/<id>" form
	}
	handlePart := ""
	if handle != "" {
		handlePart = fmt.Sprintf(" @%s", handle)
	}
	line := fmt.Sprintf("- [agent] %s%s (%s)\n",
		strings.TrimSpace(a.GetDisplayName()), handlePart, connectionStateString(a.GetConnectionState()))
	// A stopped peer is not processing sessions: say so explicitly so the
	// caller does not delegate to it and then wait for a reply that never
	// comes.
	if a.GetConnectionState() == v1pb.AgentStatus_STOPPED {
		line += "  (stopped — not processing sessions; do NOT delegate work to this agent)\n"
	}
	if desc := strings.TrimSpace(a.GetDescription()); desc != "" {
		for _, l := range strings.Split(desc, "\n") {
			line += "  " + l + "\n"
		}
	}
	return line
}

// connectionStateString renders an AgentStatus.ConnectionState for roster
// output. Mirrors the integer-restatement pattern of memberTypeString: the
// labels are display-only, so drift surfaces as "unknown" rather than a build
// break.
func connectionStateString(s v1pb.AgentStatus_ConnectionState) string {
	switch s {
	case v1pb.AgentStatus_ONLINE:
		return "online"
	case v1pb.AgentStatus_OFFLINE:
		return "offline"
	case v1pb.AgentStatus_ERROR:
		return "error"
	case v1pb.AgentStatus_KICKED:
		return "kicked"
	case v1pb.AgentStatus_STOPPED:
		return "stopped"
	}
	return "unknown"
}
