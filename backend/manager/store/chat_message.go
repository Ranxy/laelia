package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type ChatMessage struct {
	ID              uuid.UUID
	ConversationID  uuid.UUID
	PrincipalID     int
	PrincipalName   string
	SenderAgentID   sql.NullInt32
	AgentResourceID string
	Role            int32
	Content         string
	CommandID       uuid.NullUUID
	CreatedAt       time.Time
}

func (s *Store) CreateChatMessage(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	var id uuid.UUID
	var createdAt time.Time
	err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID).Scan(&id, &createdAt)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create chat message")
	}

	return &ChatMessage{
		ID:              id,
		ConversationID:  msg.ConversationID,
		PrincipalID:     msg.PrincipalID,
		PrincipalName:   msg.PrincipalName,
		SenderAgentID:   msg.SenderAgentID,
		AgentResourceID: msg.AgentResourceID,
		Role:            msg.Role,
		Content:         msg.Content,
		CommandID:       msg.CommandID,
		CreatedAt:       createdAt,
	}, nil
}

func (s *Store) ListConversationMessages(ctx context.Context, conversationID uuid.UUID, limit, offset int) ([]*ChatMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT cm.id, cm.conversation_id, cm.principal_id, COALESCE(p.name, ''), cm.sender_agent_id, COALESCE(a.resource_id, ''), cm.role, cm.content, cm.command_id, cm.created_at
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.conversation_id = $1
		ORDER BY cm.created_at ASC
		LIMIT $2 OFFSET $3
	`, conversationID, limit, offset)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list conversation messages")
	}
	defer rows.Close()

	var msgs []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.PrincipalID, &msg.PrincipalName,
			&msg.SenderAgentID, &msg.AgentResourceID,
			&msg.Role, &msg.Content, &msg.CommandID, &msg.CreatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan chat message")
		}
		msgs = append(msgs, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat messages")
	}

	return msgs, nil
}

func (s *Store) GetRecentChatMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]*ChatMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT cm.id, cm.conversation_id, cm.principal_id, COALESCE(p.name, ''), cm.sender_agent_id, COALESCE(a.resource_id, ''), cm.role, cm.content, cm.command_id, cm.created_at
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.conversation_id = $1
		ORDER BY cm.created_at DESC
		LIMIT $2
	`, conversationID, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get recent chat messages")
	}
	defer rows.Close()

	var msgs []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.PrincipalID, &msg.PrincipalName,
			&msg.SenderAgentID, &msg.AgentResourceID,
			&msg.Role, &msg.Content, &msg.CommandID, &msg.CreatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan chat message")
		}
		msgs = append(msgs, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat messages")
	}

	return msgs, nil
}
