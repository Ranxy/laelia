package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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
	SenderType  int32
	Mentions    []*v1pb.Mention
	Attachments []*v1pb.Attachment
}

// chatMessageScanner scans a chat_message row from the common column order
// produced by scanChatMessageRow: id, conversation_id, principal_id,
// principal_name, sender_agent_id, agent_resource_id, agent_name, role,
// content, command_id, created_at, room_version, sender_type, mentions,
// attachments.
func scanChatMessageRow(row interface {
	Scan(dest ...any) error
}) (*ChatMessage, error) {
	var msg ChatMessage
	var mentionsBytes []byte
	var attachmentsBytes []byte
	if err := row.Scan(
		&msg.ID, &msg.ConversationID, &msg.PrincipalID, &msg.PrincipalName,
		&msg.SenderAgentID, &msg.AgentResourceID, &msg.AgentName,
		&msg.Role, &msg.Content, &msg.CommandID, &msg.CreatedAt, &msg.RoomVersion, &msg.SenderType,
		&mentionsBytes, &attachmentsBytes,
	); err != nil {
		return nil, errors.Wrapf(err, "failed to scan chat message")
	}
	if len(mentionsBytes) > 0 {
		var mentions []*v1pb.Mention
		if err := json.Unmarshal(mentionsBytes, &mentions); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal mentions")
		}
		msg.Mentions = mentions
	}
	if len(attachmentsBytes) > 0 {
		var attachments []*v1pb.Attachment
		if err := json.Unmarshal(attachmentsBytes, &attachments); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal attachments")
		}
		msg.Attachments = attachments
	}
	return &msg, nil
}

const chatMessageColumns = `cm.id, cm.conversation_id, cm.principal_id, COALESCE(p.name, ''),
       cm.sender_agent_id, COALESCE(a.resource_id, ''), COALESCE(a.name, ''),
       cm.role, cm.content, cm.command_id, cm.created_at, cm.room_version, cm.sender_type, cm.mentions, cm.attachments`

func (s *Store) CreateChatMessage(ctx context.Context, msg *ChatMessage) (*ChatMessage, error) {
	var id uuid.UUID
	var createdAt time.Time
	var roomVersion int64

	mentionsBytes, err := marshalMentions(msg.Mentions)
	if err != nil {
		return nil, err
	}
	attachmentsBytes, err := marshalAttachments(msg.Attachments)
	if err != nil {
		return nil, err
	}
	err = s.GetDB().QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type, mentions, attachments)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, room_version
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, msg.RoomVersion, msg.SenderType, mentionsBytes, attachmentsBytes).Scan(&id, &createdAt, &roomVersion)
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
		Mentions:        msg.Mentions,
		Attachments:     msg.Attachments,
	}, nil
}

// conversationVersionBumpSQL is the room-version bump statement. It also
// advances conversation.updated_at so that activity-ordered listings
// (ListChannelsWithUpdates / ListUserConversations, both ORDER BY
// updated_at DESC) reflect new messages, not just metadata edits. Extracted as
// a named constant so the regression guard TestCreateChatMessageBumpVersionSQL
// can lock the updated_at clause in place without a live database.
const conversationVersionBumpSQL = `
	UPDATE conversation SET version = version + 1, updated_at = now() WHERE id = $1
	RETURNING version
`

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
	if err := tx.QueryRowContext(ctx, conversationVersionBumpSQL, msg.ConversationID).Scan(&newVersion); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to bump conversation version")
	}

	mentionsBytes, err := marshalMentions(msg.Mentions)
	if err != nil {
		return nil, 0, err
	}
	attachmentsBytes, err := marshalAttachments(msg.Attachments)
	if err != nil {
		return nil, 0, err
	}

	var id uuid.UUID
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type, mentions, attachments)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, newVersion, msg.SenderType, mentionsBytes, attachmentsBytes).Scan(&id, &createdAt); err != nil {
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
		Mentions:        msg.Mentions,
		Attachments:     msg.Attachments,
	}, newVersion, nil
}

func (s *Store) ListConversationMessages(ctx context.Context, conversationID uuid.UUID, afterVersion, beforeVersion int64, limit, offset int) ([]*ChatMessage, int64, error) {
	if afterVersion > 0 && beforeVersion > 0 {
		return nil, 0, errors.New("after_version and before_version are mutually exclusive")
	}

	var whereClause string
	args := []any{conversationID}
	argIdx := 2
	orderClause := ` ORDER BY cm.created_at ASC`
	if afterVersion > 0 {
		whereClause = ` AND cm.room_version > $` + itoa(argIdx)
		args = append(args, afterVersion)
		argIdx++
	} else if beforeVersion > 0 {
		// Fetch the most recent messages before the pivot in DESC order (uses
		// idx_chat_message_room_version via a reverse range scan), then reverse
		// below so callers always receive chronological order.
		whereClause = ` AND cm.room_version < $` + itoa(argIdx)
		args = append(args, beforeVersion)
		argIdx++
		orderClause = ` ORDER BY cm.room_version DESC`
	}

	query := `SELECT ` + chatMessageColumns + `
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.conversation_id = $1` + whereClause + orderClause + `
		LIMIT $` + itoa(argIdx) + ` OFFSET $` + itoa(argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to list conversation messages")
	}
	defer rows.Close()

	var msgs []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to iterate chat messages")
	}

	// Restore chronological (oldest -> newest) order for the before path.
	if beforeVersion > 0 {
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}

	var currentVersion int64
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT version FROM conversation WHERE id = $1`, conversationID,
	).Scan(&currentVersion); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get conversation version")
	}

	return msgs, currentVersion, nil
}

func itoa(n int) string {
	return strconv.Itoa(n)
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

// SetChatMessageCommandID links a chat_message to the command that was
// internally created for it (e.g. by dispatchDirectConversation inside
// SendMessage). This lets the SendMessage response carry the command_id so
// the frontend can stream execution progress.
func (s *Store) SetChatMessageCommandID(ctx context.Context, messageID, commandID uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE chat_message SET command_id = $1 WHERE id = $2
	`, commandID, messageID)
	if err != nil {
		return errors.Wrapf(err, "failed to set chat message command ID")
	}
	return nil
}

func marshalMentions(mentions []*v1pb.Mention) ([]byte, error) {
	if mentions == nil {
		mentions = []*v1pb.Mention{}
	}
	b, err := json.Marshal(mentions)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal mentions")
	}
	return b, nil
}

func marshalAttachments(attachments []*v1pb.Attachment) ([]byte, error) {
	if attachments == nil {
		attachments = []*v1pb.Attachment{}
	}
	b, err := json.Marshal(attachments)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal attachments")
	}
	return b, nil
}
