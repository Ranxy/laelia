package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type ConversationMessage struct {
	ID        uuid.UUID
	AgentID   int
	Title     string
	Type      int32
	CreatedBy int
	CreatedAt time.Time
}

func (s *Store) GetOrCreateDirectConversation(ctx context.Context, agentID, principalID int) (*ConversationMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var conv ConversationMessage
	err = tx.QueryRowContext(ctx, `
		INSERT INTO conversation (agent_id, title, type, created_by)
		VALUES ($1, '', 1, $2)
		ON CONFLICT DO NOTHING
		RETURNING id, agent_id, title, type, created_by, created_at
	`, agentID, principalID).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.CreatedAt,
	)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Wrapf(err, "failed to insert conversation")
		}
		err = tx.QueryRowContext(ctx, `
			SELECT id, agent_id, title, type, created_by, created_at
			FROM conversation
			WHERE agent_id = $1 AND created_by = $2 AND type = 1
			LIMIT 1
		`, agentID, principalID).Scan(
			&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.CreatedAt,
		)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to find existing conversation")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &conv, nil
}

func (s *Store) GetConversation(ctx context.Context, id uuid.UUID) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, created_at
		FROM conversation
		WHERE id = $1
	`, id).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("conversation %s not found", id)
		}
		return nil, errors.Wrapf(err, "failed to get conversation")
	}
	return &conv, nil
}
