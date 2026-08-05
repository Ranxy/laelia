package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// This file implements the agent's channel discovery/join tools: what it can
// read (its memberships +, when follow_owner_permissions is on, its owner's
// channels/DMs), and how to join one it can read. These are the on-demand
// counterpart of ListChannelUpdates (message check), which stays limited to
// joined conversations so the agent is not woken for its owner's channels.

// ListAccessibleChannelsInput scopes the discovery list; PageSize 0 uses the
// server default.
type ListAccessibleChannelsInput struct {
	PageSize int32 `json:"page_size,omitempty"`
}

// JoinChannelInput names the channel to join as a conversation address
// ('#<title>' or a conversations/<id> resource name).
type JoinChannelInput struct {
	Conversation string `json:"conversation"`
}

// ListAccessibleChannels lists every conversation the agent can read: its own
// memberships plus (when follow_owner_permissions is enabled) every channel/DM
// its owner can read. Each line is `- [joined|visible] <address>`, where
// joined conversations accept posts and appear in `message check`, and visible
// ones are readable but not joined. Channels address as '#<title>'; DMs the
// agent is in as 'dm:@<peer>'; owner-visible DMs by their conversation resource
// name (read with `message read conversations/<id>`).
func ListAccessibleChannels(ctx context.Context, d Deps, in ListAccessibleChannelsInput) (string, error) {
	// Fetch every page: the accessible set is "what can I read", so a truncated
	// first page would hide older channels from the agent's discovery (the
	// default page is 10, and the list is ordered newest-first). A page size of
	// 100 is the manager's maximum.
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	var all []*v1pb.AccessibleChannel
	pageToken := ""
	for {
		resp, err := d.Client.ListAccessibleChannels(ctx, connect.NewRequest(&v1pb.ListAccessibleChannelsRequest{
			PageSize:  pageSize,
			PageToken: pageToken,
		}))
		if err != nil {
			return "", wrapManagerError(err)
		}
		all = append(all, resp.Msg.GetChannels()...)
		next := resp.Msg.GetNextPageToken()
		if next == "" || len(resp.Msg.GetChannels()) == 0 {
			break
		}
		pageToken = next
	}
	if len(all) == 0 {
		return "No accessible channels.\n", nil
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Accessible channels (%d):\n", len(all))
	for _, ch := range all {
		_, _ = b.WriteString(formatAccessibleLine(ch))
		_, _ = b.WriteString("\n")
	}
	_, _ = b.WriteString("\nTo read a channel without joining, `message read '<address>'`. To join it, `channel join '<address>'`. Posting requires membership.\n")
	return b.String(), nil
}

// JoinChannel makes the agent a real member of a channel it can read (via its
// own membership or owner-follow), seeding its per-channel cursor so the
// channel starts appearing in `message check` and the agent may post to it.
func JoinChannel(ctx context.Context, d Deps, in JoinChannelInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "channel is required (pass the address, e.g. '#support')", "Pass the channel address, e.g. `channel join '#support'`.")
	}

	resp, err := d.Client.JoinChannel(ctx, connect.NewRequest(&v1pb.JoinChannelRequest{Conversation: name}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	conv := resp.Msg.GetConversation()
	addr := conv.GetAddress()
	if addr == "" {
		addr = conv.GetName()
	}
	return fmt.Sprintf("Joined %s (%s). The channel now appears in `message check` and you may post to it.\n", quoteAddress(addr), conv.GetName()), nil
}

// formatAccessibleLine renders one AccessibleChannel: a [joined|visible] state
// tag plus the address the agent acts on — '#<title>' for channels, 'dm:@<peer>'
// for DMs the agent is in, and the conversations/<id> resource name for
// owner-visible DMs (which have no name-form address). A title is appended when
// it is not already readable from the address.
func formatAccessibleLine(ch *v1pb.AccessibleChannel) string {
	if ch == nil || ch.GetChannel() == nil {
		return ""
	}
	conv := ch.GetChannel()
	state := "visible"
	if ch.GetIsMember() {
		state = "joined"
	}
	addr := conv.GetAddress()
	if addr == "" {
		addr = conv.GetName()
	}
	line := fmt.Sprintf("- [%s] %s", state, quoteAddress(addr))
	if title := strings.TrimSpace(conv.GetTitle()); title != "" && !strings.Contains(addr, title) {
		line += fmt.Sprintf(" (%s)", title)
	}
	return line
}
