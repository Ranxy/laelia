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
	// ThreadRootMessageID is the root message of the thread this message belongs
	// to. Valid (non-NULL) only for thread replies; root messages and normal
	// channel messages have it NULL.
	ThreadRootMessageID uuid.NullUUID
	// ThreadReplyCount is the number of replies in the thread rooted at this
	// message. Populated only for root messages by ListConversationMessages; 0
	// otherwise.
	ThreadReplyCount int32
	// TaskInfo is non-nil when this message is a task (a row exists in the task
	// table for this message id). Populated by fillTaskInfo for root messages
	// via ListConversationMessages / ListThreadMessages / ListTasks /
	// GetTaskMessage; nil for non-task messages and thread replies.
	TaskInfo *TaskInfo
}

// chatMessageScanner scans a chat_message row from the common column order
// produced by scanChatMessageRow: id, conversation_id, principal_id,
// principal_name, sender_agent_id, agent_resource_id, agent_name, role,
// content, command_id, created_at, room_version, sender_type, mentions,
// attachments, thread_root_message_id.
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
		&mentionsBytes, &attachmentsBytes, &msg.ThreadRootMessageID,
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
       cm.role, cm.content, cm.command_id, cm.created_at, cm.room_version, cm.sender_type, cm.mentions, cm.attachments, cm.thread_root_message_id`

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
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type, mentions, attachments, thread_root_message_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, room_version
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, msg.RoomVersion, msg.SenderType, mentionsBytes, attachmentsBytes, msg.ThreadRootMessageID).Scan(&id, &createdAt, &roomVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create chat message")
	}

	return &ChatMessage{
		ID:                  id,
		ConversationID:      msg.ConversationID,
		PrincipalID:         msg.PrincipalID,
		PrincipalName:       msg.PrincipalName,
		SenderAgentID:       msg.SenderAgentID,
		AgentResourceID:     msg.AgentResourceID,
		Role:                msg.Role,
		Content:             msg.Content,
		CommandID:           msg.CommandID,
		CreatedAt:           createdAt,
		RoomVersion:         roomVersion,
		SenderType:          msg.SenderType,
		Mentions:            msg.Mentions,
		Attachments:         msg.Attachments,
		ThreadRootMessageID: msg.ThreadRootMessageID,
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

	id, createdAt, err := createChatMessageInTx(ctx, tx, msg, newVersion)
	if err != nil {
		return nil, 0, err
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to commit chat message tx")
	}

	return &ChatMessage{
		ID:                  id,
		ConversationID:      msg.ConversationID,
		PrincipalID:         msg.PrincipalID,
		PrincipalName:       msg.PrincipalName,
		SenderAgentID:       msg.SenderAgentID,
		AgentResourceID:     msg.AgentResourceID,
		Role:                msg.Role,
		Content:             msg.Content,
		CommandID:           msg.CommandID,
		CreatedAt:           createdAt,
		RoomVersion:         newVersion,
		SenderType:          msg.SenderType,
		Mentions:            msg.Mentions,
		Attachments:         msg.Attachments,
		ThreadRootMessageID: msg.ThreadRootMessageID,
	}, newVersion, nil
}

// createChatMessageInTx inserts a chat_message row carrying roomVersion within
// an existing transaction. It is shared by CreateChatMessageBumpVersion (plain
// message) and CreateTaskMessageBumpVersion (message + task row) so the insert
// SQL and mention/attachment marshaling live in exactly one place.
func createChatMessageInTx(ctx context.Context, tx *sql.Tx, msg *ChatMessage, roomVersion int64) (uuid.UUID, time.Time, error) {
	mentionsBytes, err := marshalMentions(msg.Mentions)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	attachmentsBytes, err := marshalAttachments(msg.Attachments)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	var id uuid.UUID
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO chat_message (conversation_id, principal_id, role, content, command_id, sender_agent_id, room_version, sender_type, mentions, attachments, thread_root_message_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at
	`, msg.ConversationID, msg.PrincipalID, msg.Role, msg.Content, msg.CommandID, msg.SenderAgentID, roomVersion, msg.SenderType, mentionsBytes, attachmentsBytes, msg.ThreadRootMessageID).Scan(&id, &createdAt); err != nil {
		return uuid.Nil, time.Time{}, errors.Wrapf(err, "failed to create chat message")
	}
	return id, createdAt, nil
}

func (s *Store) ListConversationMessages(ctx context.Context, conversationID uuid.UUID, afterVersion, beforeVersion int64, limit, offset int) ([]*ChatMessage, int64, error) {
	if afterVersion > 0 && beforeVersion > 0 {
		return nil, 0, errors.New("after_version and before_version are mutually exclusive")
	}

	var whereClause string
	args := []any{conversationID}
	argIdx := 2
	// afterVersion > 0 returns only the delta (room_version > afterVersion) in
	// chronological (ASC) order — callers append it to their cached tail.
	// beforeVersion > 0 and the no-version default both return the NEWEST N
	// messages: DESC in SQL (reverse range scan on the room_version index for
	// beforeVersion, newest-first by created_at for the default), then reversed
	// below so callers always receive chronological order.
	orderClause := ` ORDER BY cm.created_at DESC`
	if afterVersion > 0 {
		whereClause = ` AND cm.room_version > $` + itoa(argIdx)
		args = append(args, afterVersion)
		argIdx++
		orderClause = ` ORDER BY cm.created_at ASC`
	} else if beforeVersion > 0 {
		whereClause = ` AND cm.room_version < $` + itoa(argIdx)
		args = append(args, beforeVersion)
		argIdx++
		orderClause = ` ORDER BY cm.room_version DESC`
	}

	query := `SELECT ` + chatMessageColumns + `
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.conversation_id = $1 AND cm.thread_root_message_id IS NULL` + whereClause + orderClause + `
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

	// Restore chronological (oldest -> newest) order for the latest-first
	// paths (before_version and the no-version default both queried DESC). The
	// afterVersion delta path is already ASC and must not be reversed.
	if afterVersion == 0 {
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}

	// Populate thread_reply_count on each root message so the frontend can
	// render the reply-count badge. One grouped query for the page's roots.
	if err := s.fillThreadReplyCounts(ctx, msgs); err != nil {
		return nil, 0, err
	}
	// Populate task metadata on any task root messages in the page so the
	// frontend can render the [task #N status=...] badge inline.
	if err := s.fillTaskInfo(ctx, msgs); err != nil {
		return nil, 0, err
	}

	var currentVersion int64
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT version FROM conversation WHERE id = $1`, conversationID,
	).Scan(&currentVersion); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get conversation version")
	}

	return msgs, currentVersion, nil
}

// fillThreadReplyCounts populates ThreadReplyCount on each root message in
// msgs (messages whose ThreadRootMessageID is NULL) by counting the replies
// that point at them. Thread replies in msgs keep count 0. One grouped query
// covers the page; a nil/empty input is a no-op.
func (s *Store) fillThreadReplyCounts(ctx context.Context, msgs []*ChatMessage) error {
	var roots []uuid.UUID
	for _, m := range msgs {
		if m == nil || m.ThreadRootMessageID.Valid {
			continue
		}
		roots = append(roots, m.ID)
	}
	if len(roots) == 0 {
		return nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT thread_root_message_id, count(*)
		FROM chat_message
		WHERE thread_root_message_id = ANY($1)
		GROUP BY thread_root_message_id
	`, roots)
	if err != nil {
		return errors.Wrapf(err, "failed to count thread replies")
	}
	defer rows.Close()
	counts := make(map[uuid.UUID]int32, len(roots))
	for rows.Next() {
		var rootID uuid.UUID
		var cnt int32
		if err := rows.Scan(&rootID, &cnt); err != nil {
			return errors.Wrapf(err, "failed to scan thread reply count")
		}
		counts[rootID] = cnt
	}
	if err := rows.Err(); err != nil {
		return errors.Wrapf(err, "failed to iterate thread reply counts")
	}
	for _, m := range msgs {
		if m == nil || m.ThreadRootMessageID.Valid {
			continue
		}
		m.ThreadReplyCount = counts[m.ID]
	}
	return nil
}

// ListThreadMessages returns the root message followed by its replies, in
// room_version order. The root is always the first element so a reader has
// the thread context. The cursor model mirrors ListConversationMessages but
// applies to replies only (the root is always included):
//   - afterVersion > 0: replies with room_version > afterVersion (chronological tail);
//   - beforeVersion > 0: a page of replies with room_version < beforeVersion;
//   - neither: the newest N replies.
//
// Returns (root+replies, currentVersion, error). rootID must belong to
// conversationID; otherwise the root is not found and an error is returned.
func (s *Store) ListThreadMessages(ctx context.Context, conversationID, rootID uuid.UUID, afterVersion, beforeVersion int64, limit, offset int) ([]*ChatMessage, int64, error) {
	if afterVersion > 0 && beforeVersion > 0 {
		return nil, 0, errors.New("after_version and before_version are mutually exclusive")
	}

	// The root message is always included (first element) regardless of the
	// cursor, so the reader has the thread context even on a delta read.
	rootRow := s.GetDB().QueryRowContext(ctx, `SELECT `+chatMessageColumns+`
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.id = $1 AND cm.conversation_id = $2 AND cm.thread_root_message_id IS NULL`,
		rootID, conversationID)
	root, err := scanChatMessageRow(rootRow)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get thread root message")
	}

	var whereClause string
	args := []any{rootID}
	argIdx := 2
	orderClause := ` ORDER BY cm.created_at DESC`
	if afterVersion > 0 {
		whereClause = ` AND cm.room_version > $` + itoa(argIdx)
		args = append(args, afterVersion)
		argIdx++
		orderClause = ` ORDER BY cm.room_version ASC`
	} else if beforeVersion > 0 {
		whereClause = ` AND cm.room_version < $` + itoa(argIdx)
		args = append(args, beforeVersion)
		argIdx++
		orderClause = ` ORDER BY cm.room_version DESC`
	}

	replyQuery := `SELECT ` + chatMessageColumns + `
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.thread_root_message_id = $1` + whereClause + orderClause + `
		LIMIT $` + itoa(argIdx) + ` OFFSET $` + itoa(argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.GetDB().QueryContext(ctx, replyQuery, args...)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to list thread messages")
	}
	defer rows.Close()
	var replies []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		replies = append(replies, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to iterate thread messages")
	}
	// Chronological order for the DESC paths; afterVersion is already ASC.
	if afterVersion == 0 {
		for i, j := 0, len(replies)-1; i < j; i, j = i+1, j-1 {
			replies[i], replies[j] = replies[j], replies[i]
		}
	}

	var currentVersion int64
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT version FROM conversation WHERE id = $1`, conversationID,
	).Scan(&currentVersion); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get conversation version")
	}

	// Populate the root's total reply count so callers (the thread panel, and
	// the frontend syncing the root badge back into the main channel list) see
	// the authoritative count rather than 0. Replies keep count 0.
	if err := s.fillThreadReplyCounts(ctx, []*ChatMessage{root}); err != nil {
		return nil, 0, err
	}
	// Populate task metadata on the root so the thread panel shows the
	// [task #N status=...] badge when the thread is a task's discussion thread.
	if err := s.fillTaskInfo(ctx, []*ChatMessage{root}); err != nil {
		return nil, 0, err
	}

	return append([]*ChatMessage{root}, replies...), currentVersion, nil
}

// ChannelThread summarizes one active thread (a root with ≥1 reply) in a
// conversation: the root message id, total reply count, and the latest reply's
// room_version / created_at. It is what ListChannelThreads returns so the
// channel page can keep root-message reply-count badges fresh without fetching
// the whole message list.
type ChannelThread struct {
	RootMessageID uuid.UUID
	ReplyCount    int32
	LatestVersion int64
	LatestAt      time.Time
}

// ListChannelThreads returns a summary for every active thread in a
// conversation, ordered by latest reply DESC. A "thread" here is any root
// message that has ≥1 reply (the query groups replies by thread_root_message_id,
// so roots with no replies do not appear). One grouped query covers the page;
// the result is bounded by the number of threads, not the number of replies.
func (s *Store) ListChannelThreads(ctx context.Context, conversationID uuid.UUID) ([]*ChannelThread, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT thread_root_message_id, count(*)::int, max(room_version), max(created_at)
		FROM chat_message
		WHERE conversation_id = $1 AND thread_root_message_id IS NOT NULL
		GROUP BY thread_root_message_id
		ORDER BY max(room_version) DESC
	`, conversationID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list channel threads")
	}
	defer rows.Close()
	var threads []*ChannelThread
	for rows.Next() {
		var t ChannelThread
		if err := rows.Scan(&t.RootMessageID, &t.ReplyCount, &t.LatestVersion, &t.LatestAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan channel thread")
		}
		threads = append(threads, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate channel threads")
	}
	return threads, nil
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// IsThreadRoot reports whether rootID is a root message in conversationID —
// i.e. a chat_message row with that id and conversation, whose
// thread_root_message_id is NULL (so it can anchor a thread). Used by
// SendMessage/PostMessage to validate a thread_root before inserting a reply.
func (s *Store) IsThreadRoot(ctx context.Context, conversationID, rootID uuid.UUID) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM chat_message
			WHERE id = $1 AND conversation_id = $2 AND thread_root_message_id IS NULL
		)
	`, rootID, conversationID).Scan(&exists)
	if err != nil {
		return false, errors.Wrapf(err, "failed to check thread root")
	}
	return exists, nil
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
