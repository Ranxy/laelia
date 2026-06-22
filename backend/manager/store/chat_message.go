package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// SenderType values mirror the laelia.v1.SenderType enum (sender_type column
// on chat_message). Kept as untyped int32 constants on the store side so the
// persistence layer does not depend on the generated proto package.
const (
	SenderTypeUser   int32 = 1
	SenderTypeAgent  int32 = 2
	SenderTypeSystem int32 = 3
)

type ChatMessage struct {
	ID              uuid.UUID
	ConversationID  uuid.UUID
	PrincipalID     int
	PrincipalName   string
	SenderAgentID   sql.NullInt32
	AgentResourceID string
	AgentName       string
	Role            int32
	Content         string
	CommandID       uuid.NullUUID
	CreatedAt       time.Time
	// RoomVersion is conversation.version at message creation. Used by agents to
	// track their pull cursor and (Phase 2) as a Held Draft base_version
	// reference.
	RoomVersion int64
	// SenderType: 1=USER, 2=AGENT, 3=SYSTEM.
	SenderType int32
}

// chatMessageScanner scans a chat_message row from the common column order
// produced by scanChatMessageRow: id, conversation_id, principal_id,
// principal_name, sender_agent_id, agent_resource_id, agent_name, role,
// content, command_id, created_at, room_version, sender_type.
func scanChatMessageRow(row interface {
	Scan(dest ...any) error
}) (*ChatMessage, error) {
	var msg ChatMessage
	if err := row.Scan(
		&msg.ID, &msg.ConversationID, &msg.PrincipalID, &msg.PrincipalName,
		&msg.SenderAgentID, &msg.AgentResourceID, &msg.AgentName,
		&msg.Role, &msg.Content, &msg.CommandID, &msg.CreatedAt, &msg.RoomVersion, &msg.SenderType,
	); err != nil {
		return nil, errors.Wrapf(err, "failed to scan chat message")
	}
	return &msg, nil
}

const chatMessageColumns = `cm.id, cm.conversation_id, cm.principal_id, COALESCE(p.name, ''),
       cm.sender_agent_id, COALESCE(a.resource_id, ''), COALESCE(a.name, ''),
       cm.role, cm.content, cm.command_id, cm.created_at, cm.room_version, cm.sender_type`

func (s *Store) CreateChatMessage(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	var id uuid.UUID
	var createdAt time.Time
	var roomVersion int64
	err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, room_version
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, msg.RoomVersion, msg.SenderType).Scan(&id, &createdAt, &roomVersion)
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
		RoomVersion:     roomVersion,
		SenderType:      msg.SenderType,
	}, nil
}

// CreateChatMessageBumpVersion atomically increments the conversation's room
// version and inserts a chat_message carrying that new version. It is the
// single entry point for both user (SendMessage) and assistant (HandleResult)
// messages so that every chat_message strictly tracks conversation.version.
// Returns the created message (with RoomVersion populated) and the new
// conversation version.
func (s *Store) CreateChatMessageBumpVersion(ctx context.Context, msg *ChatMessage) (*ChatMessage, int64, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	var newVersion int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE conversation SET version = version + 1 WHERE id = $1
		RETURNING version
	`, msg.ConversationID).Scan(&newVersion); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to bump conversation version")
	}

	var id uuid.UUID
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, newVersion, msg.SenderType).Scan(&id, &createdAt); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to create chat message")
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to commit chat message tx")
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
		RoomVersion:     newVersion,
		SenderType:      msg.SenderType,
	}, newVersion, nil
}

func (s *Store) ListConversationMessages(ctx context.Context, conversationID uuid.UUID, limit, offset int) ([]*ChatMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+chatMessageColumns+`
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
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat messages")
	}

	return msgs, nil
}

func (s *Store) GetRecentChatMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]*ChatMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+chatMessageColumns+`
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
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat messages")
	}

	return msgs, nil
}
