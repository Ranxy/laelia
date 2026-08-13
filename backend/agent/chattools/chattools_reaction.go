package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// ReactionInput is the shared add/remove input: the message addressed as a
// "<address>:<message-id>" handle (the form printed by `message read` /
// `thread read`) and the single emoji to react with.
type ReactionInput struct {
	Message string `json:"message"`
	Emoji   string `json:"emoji"`
}

// AddReaction places the agent's emoji reaction on a message. It is
// lightweight feedback: it posts no message, wakes nobody, and is not an ack.
// Idempotent — re-adding an emoji the agent already placed is a no-op. The
// manager validates the emoji; this locally pre-validates for a fast error.
func AddReaction(ctx context.Context, d Deps, in ReactionInput) (string, error) {
	emoji, err := common.NormalizeReactionEmoji(in.Emoji)
	if err != nil {
		return "", localError("INVALID_ARGUMENT_FAILED", err.Error(), "Pass a single emoji (e.g. 👍, ✅).")
	}
	name, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.AddReaction(ctx, connect.NewRequest(&v1pb.AddReactionRequest{
		Message: name,
		Emoji:   emoji,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return formatReactionResult(resp.Msg.Reactions, in.Message, emoji, true), nil
}

// RemoveReaction removes the agent's own emoji reaction from a message.
// Idempotent — removing an emoji the agent did not place is a no-op. Removing
// an emoji placed by someone else fails with PERMISSION_FAILED.
func RemoveReaction(ctx context.Context, d Deps, in ReactionInput) (string, error) {
	emoji, err := common.NormalizeReactionEmoji(in.Emoji)
	if err != nil {
		return "", localError("INVALID_ARGUMENT_FAILED", err.Error(), "Pass a single emoji (e.g. 👍, ✅).")
	}
	name, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.RemoveReaction(ctx, connect.NewRequest(&v1pb.RemoveReactionRequest{
		Message: name,
		Emoji:   emoji,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return formatReactionResult(resp.Msg.Reactions, in.Message, emoji, false), nil
}

// formatReactionResult renders the canonical success line. The handle is
// echoed in the copyable "<address>:<message-id>" form (channel handles
// single-quoted) so the agent can reuse it, matching how `message read` prints
// handles.
func formatReactionResult(_ []*v1pb.Reaction, messageAddr, emoji string, added bool) string {
	verb := "removed from"
	if added {
		verb = "added to"
	}
	handle := strings.TrimSpace(messageAddr)
	if convAddr, msgID := splitMessageAddress(messageAddr); msgID != "" {
		if h := messageHandle(convAddr, msgID); h != "" {
			handle = h
		}
	}
	return fmt.Sprintf("Reaction %s %s %s.", emoji, verb, handle)
}

// formatReactionsLine renders a message's reactions as an indented line, or ""
// when there are none. Each reaction is "<emoji> [×N] (reactor, ...)"; the
// reactors are display names, so the agent can perceive who reacted.
func formatReactionsLine(reactions []*v1pb.Reaction) string {
	if len(reactions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(reactions))
	for _, r := range reactions {
		label := r.GetEmoji()
		if r.GetCount() > 1 {
			label += fmt.Sprintf(" ×%d", r.GetCount())
		}
		if reactors := r.GetReactors(); len(reactors) > 0 {
			label += " (" + strings.Join(reactors, ", ") + ")"
		}
		parts = append(parts, label)
	}
	return "  reactions: " + strings.Join(parts, ", ") + "\n"
}
