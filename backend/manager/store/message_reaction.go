package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// A reaction is a lightweight sideband on a message: it never bumps the
// conversation's room version, never wakes agents, never counts as unread, and
// never generates activity. It lives entirely in message_reaction. The actor is
// exactly one of a user (principal_id) or an agent (agent_id); the partial
// unique indexes uq_message_reaction_user/agent guarantee one row per
// (message, actor, emoji) and make adds idempotent
// (INSERT ... ON CONFLICT DO NOTHING) and removes naturally idempotent (a
// DELETE of a non-existent row is a no-op).

// ReactionRemoveResult reports the outcome of a remove so the API layer can
// enforce "only the reactor removes its own reaction":
//   - Removed is true when the caller actually had a reaction row that was
//     deleted.
//   - Others is true when the emoji still has at least one reaction row placed
//     by someone other than the caller (i.e. the caller tried to remove an
//     emoji they did not place, which exists).
type ReactionRemoveResult struct {
	Removed   bool
	Others    bool
	Reactions []*v1pb.Reaction
}

// addReactionSQL is the idempotent add. Extracted as a named constant so
// TestAddReactionSQL can lock the ON CONFLICT DO NOTHING clause in place
// without a live database.
const addReactionSQL = `
	INSERT INTO message_reaction (message_id, principal_id, agent_id, emoji)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT DO NOTHING
`

// reactionInspectSQL determines whether the caller has this emoji and whether
// anyone else does, in one query. Extracted for the SQL guard tests.
const reactionInspectSQL = `
	SELECT
		EXISTS (SELECT 1 FROM message_reaction
		        WHERE message_id = $1 AND emoji = $2
		          AND principal_id IS NOT DISTINCT FROM $3
		          AND agent_id IS NOT DISTINCT FROM $4),
		EXISTS (SELECT 1 FROM message_reaction
		        WHERE message_id = $1 AND emoji = $2
		          AND (principal_id IS DISTINCT FROM $3 OR agent_id IS DISTINCT FROM $4))
`

// removeReactionSQL is the caller-scoped delete. Extracted for the SQL guard
// tests.
const removeReactionSQL = `
	DELETE FROM message_reaction
	WHERE message_id = $1 AND emoji = $2
	  AND principal_id IS NOT DISTINCT FROM $3
	  AND agent_id IS NOT DISTINCT FROM $4
`

// aggregateReactionsSQL groups reactions per (message, emoji), joining reactor
// display names and computing the caller-relative `reacted` flag. Extracted
// for the SQL guard tests.
const aggregateReactionsSQL = `
	SELECT r.message_id, r.emoji, count(*)::int,
	       COALESCE(array_agg(COALESCE(p.name, a.name) ORDER BY r.created_at), '{}'),
	       bool_or(COALESCE(r.principal_id = $2, false) OR COALESCE(r.agent_id = $3, false))
	FROM message_reaction r
	LEFT JOIN principal p ON p.id = r.principal_id
	LEFT JOIN agent a ON a.id = r.agent_id
	WHERE r.message_id = ANY($1)
	GROUP BY r.message_id, r.emoji
	ORDER BY r.message_id, r.emoji
`

// AddReaction places the caller's emoji reaction on a message. Idempotent: if
// the caller already placed this emoji, the INSERT is a no-op. Returns the
// message's updated aggregate, with `reacted` computed for the caller
// (callerPrincipalID for a user caller, callerAgentID for an agent caller;
// the other is nil). After the write it wakes long-polling frontend readers so
// they re-fetch the message's reactions in realtime; this never bumps the
// conversation's room version or wakes agents.
func (s *Store) AddReaction(ctx context.Context, conversationID, messageID uuid.UUID, callerPrincipalID, callerAgentID *int, emoji string) ([]*v1pb.Reaction, error) {
	_, err := s.GetDB().ExecContext(ctx, addReactionSQL, messageID, optionalInt(callerPrincipalID), optionalInt(callerAgentID), emoji)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to add message reaction")
	}
	if s.roomNotifier != nil {
		s.roomNotifier.NotifyConversation(conversationID)
	}
	return s.reactionsForMessage(ctx, messageID, callerPrincipalID, callerAgentID)
}

// RemoveReaction deletes the caller's own emoji reaction on a message.
// Idempotent: a non-existent (message, emoji, caller) row is a no-op. It does
// not delete other actors' reactions — the caller can only remove its own. The
// returned ReactionRemoveResult lets the API layer distinguish a successful
// removal from an attempt to remove someone else's reaction (Removed=false,
// Others=true) vs. a no-op on a non-existent emoji (Removed=false,
// Others=false). After a write it wakes long-polling frontend readers.
func (s *Store) RemoveReaction(ctx context.Context, conversationID, messageID uuid.UUID, callerPrincipalID, callerAgentID *int, emoji string) (ReactionRemoveResult, error) {
	// Does the caller have this reaction, and does anyone else?
	var callerHas, othersHas bool
	if err := s.GetDB().QueryRowContext(ctx, reactionInspectSQL, messageID, emoji, optionalInt(callerPrincipalID), optionalInt(callerAgentID)).Scan(&callerHas, &othersHas); err != nil {
		return ReactionRemoveResult{}, errors.Wrapf(err, "failed to inspect message reaction for removal")
	}

	if callerHas {
		if _, err := s.GetDB().ExecContext(ctx, removeReactionSQL, messageID, emoji, optionalInt(callerPrincipalID), optionalInt(callerAgentID)); err != nil {
			return ReactionRemoveResult{}, errors.Wrapf(err, "failed to remove message reaction")
		}
		if s.roomNotifier != nil {
			s.roomNotifier.NotifyConversation(conversationID)
		}
	}

	reactions, err := s.reactionsForMessage(ctx, messageID, callerPrincipalID, callerAgentID)
	if err != nil {
		return ReactionRemoveResult{}, err
	}
	return ReactionRemoveResult{Removed: callerHas, Others: othersHas, Reactions: reactions}, nil
}

// ListReactionsForMessages returns, keyed by message id, the aggregated
// reactions for each of the given messages (empty slice for a message with
// none). It is the batch filler for ListConversationMessages /
// ListThreadMessages so reaction display does not incur an N+1 query. The
// `reacted` flag is computed for the caller identity.
func (s *Store) ListReactionsForMessages(ctx context.Context, messageIDs []uuid.UUID, callerPrincipalID, callerAgentID *int) (map[uuid.UUID][]*v1pb.Reaction, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID][]*v1pb.Reaction{}, nil
	}
	return s.queryReactions(ctx, messageIDs, callerPrincipalID, callerAgentID)
}

// reactionsForMessage aggregates the reactions of a single message.
func (s *Store) reactionsForMessage(ctx context.Context, messageID uuid.UUID, callerPrincipalID, callerAgentID *int) ([]*v1pb.Reaction, error) {
	byMessage, err := s.queryReactions(ctx, []uuid.UUID{messageID}, callerPrincipalID, callerAgentID)
	if err != nil {
		return nil, err
	}
	return byMessage[messageID], nil
}

// queryReactions aggregates reactions for a set of message ids, grouped by
// message id then emoji. Reactors are the display names (principal.name for
// users, agent.name for agents); `reacted` is true when the caller identity
// (exactly one of callerPrincipalID / callerAgentID) placed that emoji. The
// partial unique indexes guarantee one row per (message, actor, emoji), so
// count(*) is the number of distinct reactors and the LEFT JOINs are 1:1.
func (s *Store) queryReactions(ctx context.Context, messageIDs []uuid.UUID, callerPrincipalID, callerAgentID *int) (map[uuid.UUID][]*v1pb.Reaction, error) {
	rows, err := s.GetDB().QueryContext(ctx, aggregateReactionsSQL, messageIDs, optionalInt(callerPrincipalID), optionalInt(callerAgentID))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query message reactions")
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]*v1pb.Reaction, len(messageIDs))
	for rows.Next() {
		var (
			msgID    uuid.UUID
			emoji    string
			count    int32
			reactors pq.StringArray
			reacted  bool
		)
		if err := rows.Scan(&msgID, &emoji, &count, &reactors, &reacted); err != nil {
			return nil, errors.Wrapf(err, "failed to scan message reaction")
		}
		out[msgID] = append(out[msgID], &v1pb.Reaction{
			Emoji:    emoji,
			Count:    count,
			Reactors: reactors,
			Reacted:  reacted,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate message reactions")
	}
	// Ensure messages with no reactions map to an empty (non-nil) slice so the
	// API can emit a stable empty list.
	for _, id := range messageIDs {
		if out[id] == nil {
			out[id] = []*v1pb.Reaction{}
		}
	}
	return out, nil
}

// optionalInt converts a *int identity into a driver value for a nullable
// column: nil maps to NULL, non-nil to the integer.
func optionalInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
