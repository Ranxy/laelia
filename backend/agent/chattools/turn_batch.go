package chattools

import (
	"context"
	"fmt"
	"log/slog"
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
//
// Due reminders are part of the wake reason and are rendered into the batch
// directly (see reminderSection): the agent sees scheduled work without
// polling `reminder list-due`, and a message-driven turn carries no reminder
// instruction at all.
func BuildTurnBatch(ctx context.Context, d Deps) (string, error) {
	reminderBlock := reminderSection(ctx, d)

	updatesResp, err := d.Client.ListChannelUpdates(ctx, connect.NewRequest(&v1pb.ListChannelUpdatesRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	updates := updatesResp.Msg.GetUpdates()
	if len(updates) == 0 {
		// A turn may be opened for a due reminder even when no channel has
		// unread messages; the reminder block is then the whole point of the
		// turn. With neither, the BeginSession gate should have kept the agent
		// idle — this only happens when state changed between the gate and this
		// build, so return a harmless no-work text the agent can end on.
		if reminderBlock == "" {
			return "No new messages or due reminders this turn.", nil
		}
		return "No new channel messages this turn.\n\n" + reminderBlock, nil
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
	if reminderBlock != "" {
		// Due reminders come first: the init prompt's step 0 handles them
		// before the message batch. When empty, no reminder text appears —
		// nothing is scheduled, so there is nothing to check.
		_, _ = b.WriteString(reminderBlock)
		_, _ = b.WriteString("\nHandle every due reminder first (step 0 of your init prompt), then the message batch below.\n\n")
	}
	_, _ = b.WriteString("New messages received:\n\n")
	_, _ = b.WriteString(strings.Join(blocks, "\n\n"))
	_, _ = b.WriteString("\n\nRespond as appropriate. Complete all your work before stopping.\n")
	_, _ = b.WriteString("Reply in the channel or create/reply in a thread as appropriate; use each message's content to choose the exact target.\n")
	if len(overflow) > 0 {
		_, _ = b.WriteString("\nSome unread channels may not be included in this bounded startup batch:\n")
		_, _ = b.WriteString(strings.Join(overflow, "\n"))
		_, _ = b.WriteString("\n\nUse `message check` or `message read` at a natural breakpoint if you choose to inspect those targets.\n")
	}
	return b.String(), nil
}

// reminderSection renders the DUE reminders owned by the calling agent for the
// turn batch, or "" when there are none. The batch is the only place the agent
// learns about scheduled work: the manager wakes the agent — and lists the
// reminders here — only when one is actually due, so a turn never needs to
// poll `reminder list-due` itself. A query failure degrades to no section: the
// reminder stays DUE, so the drain loop's next BeginSession (whose gate still
// reports it) re-opens a turn and retries.
func reminderSection(ctx context.Context, d Deps) string {
	resp, err := d.Client.ListDueReminders(ctx, connect.NewRequest(&v1pb.ListDueRemindersRequest{}))
	if err != nil {
		slog.Warn("failed to list due reminders for turn batch", "error", err)
		return ""
	}
	if len(resp.Msg.GetReminders()) == 0 {
		return ""
	}
	var b strings.Builder
	_, _ = b.WriteString("Due reminders:\n")
	for _, r := range resp.Msg.GetReminders() {
		_, _ = b.WriteString(formatReminderLine(r))
	}
	_, _ = b.WriteString("\nFor each due reminder: do the work, then run `laelia-machine reminder complete <name> --result \"...\"` (or `reminder fail <name> --error \"...\"`).\n")
	return b.String()
}

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
