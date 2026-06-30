package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// AgentChannelCursor records how far an agent has processed a conversation.
// The autonomous drain loop compares conversation.version against
// ProcessedVersion to decide whether a channel has unread messages. A missing
// row is treated as "caught up to the current version" (see
// ListChannelsWithUpdates), so a newly joined agent sees only future messages
// unless it fetches history explicitly.
type AgentChannelCursor struct {
	AgentID          int
	ConversationID   uuid.UUID
	ProcessedVersion int64
	UpdatedAt        time.Time
}

// ChannelUpdate describes one conversation that has unread messages for an
// agent. NewMessageCount is the number of relevant chat_message rows (root
// messages plus replies in threads the agent is subscribed to) with
// room_version greater than the agent's cursor.
type ChannelUpdate struct {
	ConversationID   uuid.UUID
	CurrentVersion   int64
	ProcessedVersion int64
	NewMessageCount  int32
}

// agentRelevantMessageCondition is the per-message predicate selecting
// chat_message rows beyond the agent's cursor that the agent cares about: a
// root message (thread_root_message_id IS NULL — normal channel traffic) OR a
// reply in a thread the agent is subscribed to (thread_participant). Used by
// ListChannelsWithUpdates and HasUpdates so non-subscribed thread replies do
// not surface the channel for a non-participant agent (@mention-pull-in /
// subscription model). The $1 placeholder is the agent id bind parameter.
const agentRelevantMessageCondition = `m.thread_root_message_id IS NULL
	OR EXISTS (
		SELECT 1 FROM thread_participant tp
		WHERE tp.thread_root_message_id = m.thread_root_message_id AND tp.agent_id = $1
	)`

// UpsertCursor advances the agent's cursor for a conversation to
// processedVersion. The update is monotonic: a lower value never overwrites a
// higher one (GREATEST), so an out-of-order or stale ack cannot rewind progress.
// It returns the resulting processed_version.
func (s *Store) UpsertCursor(ctx context.Context, agentID int, conversationID uuid.UUID, processedVersion int64) (int64, error) {
	var result int64
	err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO agent_channel_cursor (agent_id, conversation_id, processed_version, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (agent_id, conversation_id) DO UPDATE
		   SET processed_version = GREATEST(agent_channel_cursor.processed_version, EXCLUDED.processed_version),
		       updated_at = now()
			RETURNING processed_version
	`, agentID, conversationID, processedVersion).Scan(&result)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to upsert agent channel cursor")
	}
	return result, nil
}

// GetCursor returns the agent's processed_version for a conversation. found is
// false when no cursor row exists; callers should treat that as "caught up to
// current version" (i.e. no unread messages), not as "read from zero".
func (s *Store) GetCursor(ctx context.Context, agentID int, conversationID uuid.UUID) (processedVersion int64, found bool, err error) {
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT processed_version FROM agent_channel_cursor
		WHERE agent_id = $1 AND conversation_id = $2
	`, agentID, conversationID).Scan(&processedVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, errors.Wrapf(err, "failed to get agent channel cursor")
	}
	return processedVersion, true, nil
}

// ListChannelsWithUpdates returns every conversation the agent is a member of
// that has at least one new message the agent cares about — a new root message
// or a new reply in a thread the agent is subscribed to. A missing cursor row
// is treated as caught-up (COALESCE to conversation.version), so newly joined
// agents without a seeded cursor do not re-read history as unread. Channels
// are ordered by most recently updated first. NewMessageCount counts only the
// relevant new messages (root + subscribed-thread replies).
func (s *Store) ListChannelsWithUpdates(ctx context.Context, agentID int) ([]*ChannelUpdate, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT c.id,
		       c.version,
		       COALESCE(acc.processed_version, c.version),
		       (
			         SELECT count(*)::int
			         FROM chat_message m
			         WHERE m.conversation_id = c.id
			           AND m.room_version > COALESCE(acc.processed_version, c.version)
			           AND (`+agentRelevantMessageCondition+`)
			       )
			FROM conversation c
			JOIN conversation_member cm
			  ON cm.conversation_id = c.id
			 AND cm.member_type = $2
			 AND cm.member_id = (SELECT resource_id FROM agent WHERE id = $1)
			LEFT JOIN agent_channel_cursor acc
			  ON acc.agent_id = $1
			 AND acc.conversation_id = c.id
			WHERE c.version > COALESCE(acc.processed_version, c.version)
			  AND EXISTS (
			        SELECT 1 FROM chat_message m
			        WHERE m.conversation_id = c.id
			          AND m.room_version > COALESCE(acc.processed_version, c.version)
			          AND (`+agentRelevantMessageCondition+`)
			      )
			ORDER BY c.updated_at DESC
	`, agentID, MemberTypeAgent)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list channels with updates")
	}
	defer rows.Close()

	var updates []*ChannelUpdate
	for rows.Next() {
		var u ChannelUpdate
		if scanErr := rows.Scan(&u.ConversationID, &u.CurrentVersion, &u.ProcessedVersion, &u.NewMessageCount); scanErr != nil {
			return nil, errors.Wrapf(scanErr, "failed to scan channel update")
		}
		updates = append(updates, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate channel updates")
	}
	return updates, nil
}

// HasUpdates reports whether any conversation the agent is a member of has a
// relevant new message (root or subscribed-thread reply) beyond the agent's
// cursor. It is the cheap gate the drain loop uses to decide whether to open a
// session at all (and the basis of BeginSession's idle reply).
func (s *Store) HasUpdates(ctx context.Context, agentID int) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM conversation c
			JOIN conversation_member cm
			  ON cm.conversation_id = c.id
			 AND cm.member_type = $2
			 AND cm.member_id = (SELECT resource_id FROM agent WHERE id = $1)
			LEFT JOIN agent_channel_cursor acc
			  ON acc.agent_id = $1
			 AND acc.conversation_id = c.id
			WHERE c.version > COALESCE(acc.processed_version, c.version)
			  AND EXISTS (
			        SELECT 1 FROM chat_message m
			        WHERE m.conversation_id = c.id
			          AND m.room_version > COALESCE(acc.processed_version, c.version)
			          AND (`+agentRelevantMessageCondition+`)
			      )
		)
	`, agentID, MemberTypeAgent).Scan(&exists)
	if err != nil {
		return false, errors.Wrapf(err, "failed to check agent channel updates")
	}
	return exists, nil
}

// SeedCursorOnJoin initializes an agent's cursor for a conversation to the
// conversation's current version, so a newly joined agent starts "caught up"
// and only sees future messages. Idempotent: it never lowers an existing
// cursor (UpsertCursor is monotonic). Callers should only seed for agent
// members (member_type=AGENT).
func (s *Store) SeedCursorOnJoin(ctx context.Context, agentID int, conversationID uuid.UUID) error {
	var currentVersion int64
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT version FROM conversation WHERE id = $1
	`, conversationID).Scan(&currentVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to read conversation version for cursor seed")
	}
	if _, err := s.UpsertCursor(ctx, agentID, conversationID, currentVersion); err != nil {
		return errors.Wrapf(err, "failed to seed agent channel cursor")
	}
	return nil
}
