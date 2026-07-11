package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// Turn-batch bounds. The "New messages received:" batch that opens a drain turn
// is a preview, not the full inbox: it surfaces the latest few messages across
// the few most-recently-active channels so the agent can start work without a
// `message check` round-trip, while channels/messages beyond the bounds are
// listed as unread counts the agent pulls with `message read`/`thread read` at a
// natural breakpoint. Tunable from one place.
const (
	turnBatchMaxChannels = 5
	turnBatchMaxMessages = 3
)

// BuildTurnBatch renders the "New messages received:" prompt that opens a drain
// turn. It reuses the same auth-bearing CommandServiceClient the CLI uses (via
// Deps) — no new manager RPC and no proto change: ListChannelUpdates gives the
// unread channels + counts, GetChannel resolves each channel's title/type/peer
// for the target= prefix, and ListConversationMessages fetches the latest few
// messages per channel. Returns "" when there is no unread work (the caller
// should not have opened a turn, but this keeps it harmless).
func BuildTurnBatch(ctx context.Context, d Deps) (string, error) {
	updatesResp, err := d.Client.ListChannelUpdates(ctx, connect.NewRequest(&v1pb.ListChannelUpdatesRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	updates := updatesResp.Msg.GetUpdates()
	if len(updates) == 0 {
		// A turn may be opened for a due reminder even when no channel has
		// unread messages. Return a non-empty reminder nudge so a warm turn
		// still prompts the agent to run `reminder list-due` (the init prompt's
		// step 0 is only sent once, at cold start, so warm turns need this).
		return "No new channel messages this turn.\n\n" + reminderNudge, nil
	}

	var (
		lines      []string
		overflow   []string // "- <target>: N unread" for channels beyond the bound
		overflowCh []string // channels within the bound but with more msgs than shown
	)
	shown := 0
	for _, u := range updates {
		target, ok := resolveChannelTarget(ctx, d, u.GetConversation())
		if !ok {
			// Could not resolve metadata; fall back to the conversation name so
			// the agent still has a usable reply target.
			target = u.GetConversation()
		}
		if shown >= turnBatchMaxChannels {
			overflow = append(overflow, fmt.Sprintf("- %s: %d unread", target, u.GetNewMessageCount()))
			continue
		}
		shown++
		msgs, gotAll, err := latestChannelMessages(ctx, d, u)
		if err != nil {
			return "", err
		}
		for _, m := range msgs {
			lines = append(lines, formatBatchLine(target, m))
		}
		if !gotAll {
			overflowCh = append(overflowCh, fmt.Sprintf("- %s: %d unread", target, u.GetNewMessageCount()))
		}
	}

	var b strings.Builder
	_, _ = b.WriteString("New messages received:\n\n")
	if len(lines) == 0 {
		_, _ = b.WriteString("(no message bodies could be surfaced; use `laelia-agent message check` to inspect your inbox.)\n")
	} else {
		_, _ = b.WriteString(strings.Join(lines, "\n"))
		_, _ = b.WriteString("\n")
	}
	_, _ = b.WriteString("\nRespond as appropriate. Complete all your work before stopping.\n")
	_, _ = b.WriteString("Reply in the channel or create/reply in a thread as appropriate; use each message's content to choose the exact target.\n")
	overflow = append(overflow, overflowCh...)
	if len(overflow) > 0 {
		_, _ = b.WriteString("\nSome unread channels may not be included in this bounded startup batch:\n")
		_, _ = b.WriteString(strings.Join(overflow, "\n"))
		_, _ = b.WriteString("\n\nUse the inbox/read commands at a natural breakpoint if you choose to inspect those targets.\n")
	}
	_, _ = b.WriteString("\n" + reminderNudge)
	return b.String(), nil
}

// reminderNudge is appended to every turn prompt so a warm (resumed) turn —
// which does not re-receive the init prompt's step 0 — still checks for due
// reminders. Cold turns carry it too (redundant with the init procedure, but
// harmless and keeps the two paths consistent).
const reminderNudge = "Before ending your turn, also run `laelia-agent reminder list-due` and handle any due scheduled reminders."

// resolveChannelTarget fetches conversation metadata and renders the batch
// target label: "#<title>" for a channel (type 2), "dm:@<peer>" for a direct
// message (type 1, peer = the other member, surfaced as owner_name). ok is false
// when GetChannel failed, in which case the caller falls back to the bare
// conversation name.
func resolveChannelTarget(ctx context.Context, d Deps, conversation string) (target string, ok bool) {
	resp, err := d.Client.GetChannel(ctx, connect.NewRequest(&v1pb.GetChannelRequest{Name: conversation}))
	if err != nil {
		return "", false
	}
	conv := resp.Msg
	switch conv.GetType() {
	case 1: // direct conversation
		peer := strings.TrimSpace(conv.GetOwnerName())
		if peer == "" {
			peer = conversation
		}
		return "dm:@" + peer, true
	default: // channel (type 2) or anything else
		title := strings.TrimSpace(conv.GetTitle())
		if title == "" {
			title = conversation
		}
		return "#" + title, true
	}
}

// latestChannelMessages fetches the latest turnBatchMaxMessages new messages
// for one channel (those with room_version > the agent's processed_version).
// gotAll is false when the channel had more new messages than the bound, so the
// caller can list it as unread. When there are more new messages than the bound,
// the newest bound are fetched via beforeVersion paging; otherwise the full
// (chronological) delta is fetched via afterVersion.
func latestChannelMessages(ctx context.Context, d Deps, u *v1pb.ChannelUpdate) (msgs []*v1pb.ChatMessage, gotAll bool, err error) {
	count := u.GetNewMessageCount()
	limit := int32(turnBatchMaxMessages)
	req := &v1pb.ListConversationMessagesRequest{
		Conversation: u.GetConversation(),
		PageSize:     limit,
	}
	if count > limit {
		// More new messages than the bound: fetch the newest `limit` by paging
		// back from the current version. Since count > limit, all of the
		// newest `limit` are within the unread delta (no already-read messages
		// resurface). The store returns them in chronological order.
		req.BeforeVersion = u.GetCurrentVersion() + 1
	} else {
		// Fetch the full unread delta (chronological).
		req.AfterVersion = u.GetProcessedVersion()
	}
	resp, lerr := d.Client.ListConversationMessages(ctx, connect.NewRequest(req))
	if lerr != nil {
		return nil, false, wrapManagerError(lerr)
	}
	msgs = resp.Msg.GetMessages()
	gotAll = count <= limit
	return msgs, gotAll, nil
}

// formatBatchLine renders one message in the batch's [target=...] header form:
// the target label, short message id (last path segment of the name), created-at
// timestamp, sender type, "@<sender>" label, and trimmed content.
func formatBatchLine(target string, m *v1pb.ChatMessage) string {
	msgID := lastSegment(m.GetName())
	ts := ""
	if t := m.GetCreatedAt(); t != nil {
		ts = t.AsTime().Format("2006-01-02 15:04:05")
	}
	typeShort := batchTypeShort(m.GetSenderType())
	sender := batchSenderLabel(m.GetSenderType(), m.GetSenderName())
	content := strings.TrimSpace(m.GetContent())
	return fmt.Sprintf("[target=%s msg=%s time=%s type=%s] %s: %s", target, msgID, ts, typeShort, sender, content)
}

func batchTypeShort(t v1pb.SenderType) string {
	switch t {
	case v1pb.SenderType_SENDER_TYPE_USER:
		return "human"
	case v1pb.SenderType_SENDER_TYPE_AGENT:
		return "agent"
	case v1pb.SenderType_SENDER_TYPE_SYSTEM:
		return "system"
	default:
		return "unknown"
	}
}

func batchSenderLabel(t v1pb.SenderType, name string) string {
	if t == v1pb.SenderType_SENDER_TYPE_SYSTEM {
		return "@system"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "@unknown"
	}
	if strings.HasPrefix(name, "@") {
		return name
	}
	return "@" + name
}

func lastSegment(name string) string {
	if name == "" {
		return ""
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
