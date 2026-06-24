package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// IncrementConversationVersion bumps the room version for a conversation and
// returns the new value. It is the basis for Agent pull cursors and the Phase 2
// Held Draft base_version check.
func (s *Store) IncrementConversationVersion(ctx context.Context, id uuid.UUID) (int64, error) {
	var version int64
	err := s.GetDB().QueryRowContext(ctx, `
		UPDATE conversation SET version = version + 1 WHERE id = $1
		RETURNING version
	`, id).Scan(&version)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to increment conversation version")
	}
	return version, nil
}

// GetConversationVersion returns the current room version of a conversation.
func (s *Store) GetConversationVersion(ctx context.Context, id uuid.UUID) (int64, error) {
	var version int64
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT version FROM conversation WHERE id = $1
	`, id).Scan(&version)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get conversation version")
	}
	return version, nil
}

// GetMessagesAfterVersion returns chat messages in a conversation whose
// room_version is strictly greater than afterVersion, ordered ascending.
func (s *Store) GetMessagesAfterVersion(ctx context.Context, conversationID uuid.UUID, afterVersion int64) ([]*ChatMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT cm.id, cm.conversation_id, cm.principal_id, COALESCE(p.name, ''),
		       cm.sender_agent_id, COALESCE(a.resource_id, ''), COALESCE(a.name, ''),
		       cm.role, cm.content, cm.command_id, cm.created_at, cm.room_version, cm.sender_type, cm.mentions
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.conversation_id = $1 AND cm.room_version > $2
		ORDER BY cm.room_version ASC
	`, conversationID, afterVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get messages after version")
	}
	defer rows.Close()

	var msgs []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate messages after version")
	}
	return msgs, nil
}
