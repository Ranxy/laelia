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
// messages per channel.
//
// Each channel header carries its address ("#<title>" / "dm:@<peer>") and the
// agent's `processed_version` cursor, so the agent can go straight to
// `thread check <address>` and `message read <address> --version
// <processed_version>` (then `message ack`) without a per-turn `message check`
// round-trip to resolve the name and cursor. `message check` is now only needed
// for channels beyond the batch, which are listed below as unread with the same
// cursor so they can be read directly too. Returns "" when there is no unread
// work (the caller should not have opened a turn, but this keeps it harmless).
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

	// Per-channel blocks (header + preview lines) for the shown channels, plus a
	// summary line per channel left out of the preview (beyond the channel
	// bound). The header carries the address + processed_version so the agent
	// can act without `message check`.
	var (
		blocks   []string
		overflow []string
	)
	shown := 0
	for _, u := range updates {
		target := conversationAddress(ctx, d, u.GetConversation())
		cursor := fmt.Sprintf("%s (your processed_version=%d)", quoteAddress(target), u.GetProcessedVersion())
		if shown >= turnBatchMaxChannels {
			overflow = append(overflow, fmt.Sprintf("- %s: %d unread", cursor, u.GetNewMessageCount()))
			continue
		}
		shown++
		msgs, err := latestChannelMessages(ctx, d, u)
		if err != nil {
			return "", err
		}
		var block strings.Builder
		// The header always states the true new-message count, so a channel with
		// more messages than the preview bound is never silently dropped — the
		// count + cursor tell the agent to `message read` the full delta.
		_, _ = fmt.Fprintf(&block, "%s: %d new\n", cursor, u.GetNewMessageCount())
		for _, m := range msgs {
			_, _ = block.WriteString(formatBatchLine(target, m))
			_, _ = block.WriteString("\n")
		}
		blocks = append(blocks, strings.TrimRight(block.String(), "\n"))
	}

	var b strings.Builder
	_, _ = b.WriteString("New messages received:\n\n")
	_, _ = b.WriteString(strings.Join(blocks, "\n\n"))
	_, _ = b.WriteString("\n\nRespond as appropriate. Complete all your work before stopping.\n")
	_, _ = b.WriteString("Reply in the channel or create/reply in a thread as appropriate; use each message's content to choose the exact target.\n")
	if len(overflow) > 0 {
		_, _ = b.WriteString("\nSome unread channels may not be included in this bounded startup batch:\n")
		_, _ = b.WriteString(strings.Join(overflow, "\n"))
		_, _ = b.WriteString("\n\nUse `message check` or `message read` at a natural breakpoint if you choose to inspect those targets.\n")
	}
	_, _ = b.WriteString("\n" + reminderNudge)
	return b.String(), nil
}

// reminderNudge is appended to every turn prompt so a warm (resumed) turn —
// which does not re-receive the init prompt's step 0 — still checks for due
// reminders. Cold turns carry it too (redundant with the init procedure, but
// harmless and keeps the two paths consistent).
const reminderNudge = "Before ending your turn, also run `laelia-machine reminder list-due` and handle any due scheduled reminders."

// latestChannelMessages fetches the latest turnBatchMaxMessages new messages
// for one channel (those with room_version > the agent's processed_version).
// When there are more new messages than the bound, the newest bound are fetched
// via beforeVersion paging; otherwise the full (chronological) delta is fetched
// via afterVersion. The header emitted by BuildTurnBatch always states the true
// new-message count, so truncation is never silent — the caller no longer needs
// a gotAll signal.
func latestChannelMessages(ctx context.Context, d Deps, u *v1pb.ChannelUpdate) ([]*v1pb.ChatMessage, error) {
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
	resp, err := d.Client.ListConversationMessages(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, wrapManagerError(err)
	}
	return resp.Msg.GetMessages(), nil
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
	return fmt.Sprintf("[target=%s msg=%s time=%s type=%s] %s: %s", quoteAddress(target), msgID, ts, typeShort, sender, content)
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
