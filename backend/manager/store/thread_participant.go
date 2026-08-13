package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// SubscribedThreadUpdate describes one thread the agent is subscribed to that
// has replies beyond the agent's per-channel cursor for the thread's
// conversation. It is the agent's thread inbox entry, surfaced by
// ListThreadUpdates and consumed by `thread check` in the drain loop.
type SubscribedThreadUpdate struct {
	ConversationID uuid.UUID
	// RootMessageID is the thread's root chat_message id.
	RootMessageID uuid.UUID
	// LatestVersion is the maximum room_version among the thread's new
	// replies; the agent should read up to (and ack to) this version.
	LatestVersion int64
	// NewReplyCount is the number of replies with room_version beyond the
	// agent's cursor for this conversation.
	NewReplyCount int32
}

// AddThreadParticipants subscribes the given agents to a thread. Idempotent:
// re-subscribing an already-subscribed agent is a no-op (ON CONFLICT DO
// NOTHING). An empty agent list is a no-op. Subscription is what makes an agent
// get woken on every subsequent reply in the thread (see
// notifyThreadParticipants), even without a fresh @mention.
func (s *Store) AddThreadParticipants(ctx context.Context, rootID uuid.UUID, agentIDs []int) error {
	if len(agentIDs) == 0 {
		return nil
	}
	// Batch insert with a single parameterized statement.
	args := make([]any, 0, len(agentIDs)+1)
	args = append(args, rootID)
	var placeholders strings.Builder
	for i, id := range agentIDs {
		if i > 0 {
			_, _ = placeholders.WriteString(",")
		}
		_, _ = placeholders.WriteString("($1,$")
		_, _ = placeholders.WriteString(itoa(i + 2))
		_, _ = placeholders.WriteString(")")
		args = append(args, id)
	}
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO thread_participant (thread_root_message_id, agent_id)
		VALUES `+placeholders.String()+`
		ON CONFLICT (thread_root_message_id, agent_id) DO NOTHING
	`, args...)
	if err != nil {
		return errors.Wrapf(err, "failed to add thread participants")
	}
	return nil
}

// ListThreadParticipantAgents returns the DB ids of the agents subscribed to a
// thread. Used by notifyThreadParticipants to wake every subscriber on a new
// reply.
func (s *Store) ListThreadParticipantAgents(ctx context.Context, rootID uuid.UUID) ([]int, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT agent_id FROM thread_participant WHERE thread_root_message_id = $1
	`, rootID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list thread participants")
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrapf(err, "failed to scan thread participant")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate thread participants")
	}
	return ids, nil
}

// IsThreadParticipant reports whether an agent is subscribed to a thread.
func (s *Store) IsThreadParticipant(ctx context.Context, rootID uuid.UUID, agentID int) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM thread_participant WHERE thread_root_message_id = $1 AND agent_id = $2)
	`, rootID, agentID).Scan(&exists)
	if err != nil {
		return false, errors.Wrapf(err, "failed to check thread participant")
	}
	return exists, nil
}

// AddUserThreadParticipants subscribes the given users to a thread, mirroring
// AddThreadParticipants (agent-only). Idempotent (ON CONFLICT DO NOTHING); an
// empty list is a no-op. A user is subscribed when they are @mentioned in a
// thread or they post a reply in it; thereafter every new reply in that thread
// generates a THREAD activity for them (see GenerateActivityForMessage).
func (s *Store) AddUserThreadParticipants(ctx context.Context, rootID uuid.UUID, principalIDs []int) error {
	if len(principalIDs) == 0 {
		return nil
	}
	args := make([]any, 0, len(principalIDs)+1)
	args = append(args, rootID)
	var placeholders strings.Builder
	for i, id := range principalIDs {
		if i > 0 {
			_, _ = placeholders.WriteString(",")
		}
		_, _ = placeholders.WriteString("($1,$")
		_, _ = placeholders.WriteString(itoa(i + 2))
		_, _ = placeholders.WriteString(")")
		args = append(args, id)
	}
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO user_thread_participant (thread_root_message_id, principal_id)
		VALUES `+placeholders.String()+`
		ON CONFLICT (thread_root_message_id, principal_id) DO NOTHING
	`, args...)
	if err != nil {
		return errors.Wrapf(err, "failed to add user thread participants")
	}
	return nil
}

// ListUserThreadParticipants returns the principal ids subscribed to a thread.
func (s *Store) ListUserThreadParticipants(ctx context.Context, rootID uuid.UUID) ([]int, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT principal_id FROM user_thread_participant WHERE thread_root_message_id = $1
	`, rootID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list user thread participants")
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrapf(err, "failed to scan user thread participant")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate user thread participants")
	}
	return ids, nil
}

// ThreadSender is one distinct sender of a thread (the root message or any reply).
// PrincipalID is always populated (it is the conversation owner for agent messages);
// AgentID is valid only for agent senders (SenderType == SenderTypeAgent). The
// handler maps these to a ChannelMember: users by PrincipalID, agents by AgentID.
type ThreadSender struct {
	SenderType  int32
	PrincipalID int
	Handle      string // user mention handle, empty for agent senders
	AgentID     sql.NullInt32
}

// ListThreadSenders returns the distinct users and agents that posted in a thread
// (the root message plus its replies), ordered by first appearance. System
// senders are excluded. This is the basis for the thread-participants roster: it
// reflects who actually took part, derived from message senders rather than a
// membership table.
func (s *Store) ListThreadSenders(ctx context.Context, conversationID, rootID uuid.UUID) ([]ThreadSender, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT DISTINCT m.sender_type, m.principal_id, COALESCE(p.handle, ''), m.sender_agent_id
		FROM chat_message m
		LEFT JOIN principal p ON p.id = m.principal_id
		WHERE m.conversation_id = $2
		  AND (m.id = $1 OR m.thread_root_message_id = $1)
		  AND m.sender_type IN ($3, $4)
		ORDER BY m.sender_type, m.principal_id, m.sender_agent_id
	`, rootID, conversationID, SenderTypeUser, SenderTypeAgent)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list thread senders")
	}
	defer rows.Close()
	var senders []ThreadSender
	for rows.Next() {
		var ts ThreadSender
		if err := rows.Scan(&ts.SenderType, &ts.PrincipalID, &ts.Handle, &ts.AgentID); err != nil {
			return nil, errors.Wrapf(err, "failed to scan thread sender")
		}
		senders = append(senders, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate thread senders")
	}
	return senders, nil
}

// ListSubscribedThreadUpdates returns every thread the agent is subscribed to
// (and is still a channel member of) that has replies with room_version beyond
// the agent's per-channel cursor for that conversation. A missing cursor is
// treated as caught-up (COALESCE to conversation.version), so a freshly
// subscribed agent with no cursor only sees future replies. Ordered by the
// latest new reply first.
func (s *Store) ListSubscribedThreadUpdates(ctx context.Context, agentID int) ([]*SubscribedThreadUpdate, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT r.conversation_id, r.id, max(rep.room_version), count(*)::int
		FROM thread_participant tp
		JOIN chat_message r ON r.id = tp.thread_root_message_id
		JOIN chat_message rep ON rep.thread_root_message_id = r.id
		JOIN conversation cv ON cv.id = r.conversation_id
		JOIN conversation_member_meta cm
		  ON cm.conversation_id = r.conversation_id
		 AND cm.member_type = $2
		 AND cm.member_id = (SELECT resource_id FROM agent WHERE id = $1)
		LEFT JOIN agent_channel_cursor acc
		  ON acc.agent_id = $1
		 AND acc.conversation_id = r.conversation_id
		WHERE tp.agent_id = $1
		  AND rep.room_version > COALESCE(acc.processed_version, cv.version)
		GROUP BY r.conversation_id, r.id
		ORDER BY max(rep.room_version) DESC
	`, agentID, MemberTypeAgent)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list subscribed thread updates")
	}
	defer rows.Close()
	var updates []*SubscribedThreadUpdate
	for rows.Next() {
		var u SubscribedThreadUpdate
		if err := rows.Scan(&u.ConversationID, &u.RootMessageID, &u.LatestVersion, &u.NewReplyCount); err != nil {
			return nil, errors.Wrapf(err, "failed to scan subscribed thread update")
		}
		updates = append(updates, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate subscribed thread updates")
	}
	return updates, nil
}
