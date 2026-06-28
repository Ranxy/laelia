package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type ConversationMessage struct {
	ID        uuid.UUID
	AgentID   sql.NullInt32
	Title     string
	Type      int32
	CreatedBy int
	OwnerID   int
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// insertDirectConversationSQL creates a direct conversation, returning the row.
// ON CONFLICT DO NOTHING is backed by idx_conversation_dm_unique
// (unique on (agent_id, created_by) WHERE type = 1): when two callers race to
// open the same DM, only one INSERT returns a row; the other gets sql.ErrNoRows
// and re-reads the winning row instead of inserting a duplicate. Extracted as a
// named constant so TestGetOrCreateDirectConversationSQL can lock the
// race-free INSERT in place without a live database.
const insertDirectConversationSQL = `
	INSERT INTO conversation (agent_id, title, type, created_by, owner_id)
	VALUES ($1, '', 1, $2, $2)
	ON CONFLICT (agent_id, created_by) WHERE type = 1 DO NOTHING
	RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
`

func (s *Store) GetOrCreateDirectConversation(ctx context.Context, agentID, principalID int) (*ConversationMessage, error) {
	agent, err := s.GetAgentResourceIDByID(ctx, agentID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get agent resource ID")
	}
	if agent == "" {
		return nil, errors.Errorf("agent %d not found", agentID)
	}

	conv, err := s.findDirectConversation(ctx, principalID, agent)
	if err != nil {
		return nil, err
	}
	if conv != nil {
		return conv, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var newConv ConversationMessage
	err = tx.QueryRowContext(ctx, insertDirectConversationSQL, agentID, principalID).Scan(
		&newConv.ID, &newConv.AgentID, &newConv.Title, &newConv.Type, &newConv.CreatedBy, &newConv.OwnerID, &newConv.CreatedAt, &newConv.UpdatedAt, &newConv.Version,
	)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row when another caller won the
		// race to create this DM. Roll back our (empty) tx and return the
		// winning row, which is now committed with its members.
		if errors.Is(err, sql.ErrNoRows) {
			return s.findDirectConversation(ctx, principalID, agent)
		}
		return nil, errors.Wrapf(err, "failed to insert conversation")
	}

	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeUser, fmt.Sprintf("%d", principalID), MemberRoleOwner); err != nil {
		return nil, err
	}
	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeAgent, agent, MemberRoleMember); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Seed the agent's per-channel cursor to the new conversation's version so
	// it starts caught up and only sees future messages. Seeding only on the
	// creation path is intentional: returning an existing conversation must not
	// re-seed (and thus skip) unread messages.
	if seedErr := s.SeedCursorOnJoin(ctx, agentID, newConv.ID); seedErr != nil {
		return nil, errors.Wrapf(seedErr, "failed to seed agent cursor for new direct conversation")
	}

	return &newConv, nil
}

func (s *Store) GetConversation(ctx context.Context, id uuid.UUID) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
		FROM conversation
		WHERE id = $1
	`, id).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("conversation %s not found", id)
		}
		return nil, errors.Wrapf(err, "failed to get conversation")
	}
	return &conv, nil
}

func (s *Store) CreateChannel(ctx context.Context, title string, ownerID int) (*ConversationMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var conv ConversationMessage
	err = tx.QueryRowContext(ctx, `
		INSERT INTO conversation (title, type, created_by, owner_id)
		VALUES ($1, 2, $2, $2)
		RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
	`, title, ownerID).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create channel")
	}

	if err := addConversationMemberTx(ctx, tx, conv.ID, MemberTypeUser, fmt.Sprintf("%d", ownerID), MemberRoleOwner); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &conv, nil
}

func (s *Store) UpdateChannel(ctx context.Context, id uuid.UUID, title string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		UPDATE conversation SET title = $1, updated_at = now()
		WHERE id = $2
		RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
	`, title, id).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("conversation %s not found", id)
		}
		return nil, errors.Wrapf(err, "failed to update channel")
	}
	return &conv, nil
}

func (s *Store) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `DELETE FROM conversation WHERE id = $1`, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete channel")
	}
	return nil
}

func (s *Store) ListUserConversations(ctx context.Context, principalID int, limit, offset int) ([]*ConversationMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version
		FROM conversation c
		JOIN conversation_member cm ON cm.conversation_id = c.id
		WHERE cm.member_type = $1 AND cm.member_id = $2
		ORDER BY c.updated_at DESC
		LIMIT $3 OFFSET $4
	`, MemberTypeUser, fmt.Sprintf("%d", principalID), limit, offset)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list user conversations")
	}
	defer rows.Close()

	var convs []*ConversationMessage
	for rows.Next() {
		var conv ConversationMessage
		if err := rows.Scan(&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version); err != nil {
			return nil, errors.Wrapf(err, "failed to scan conversation")
		}
		convs = append(convs, &conv)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate conversations")
	}

	return convs, nil
}

func (s *Store) GetConversationMemberCount(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversation_member WHERE conversation_id = $1
	`, id).Scan(&count)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get member count")
	}
	return count, nil
}

func (s *Store) GetAgentResourceIDByID(ctx context.Context, agentID int) (string, error) {
	var resourceID string
	err := s.GetDB().QueryRowContext(ctx, `SELECT resource_id FROM agent WHERE id = $1`, agentID).Scan(&resourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.Wrapf(err, "failed to get agent resource ID")
	}
	return resourceID, nil
}
