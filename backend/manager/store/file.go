package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// File is the persisted metadata for an S3-backed object.
type File struct {
	ID                  uuid.UUID
	ConversationID      uuid.NullUUID
	UploaderPrincipalID int
	OriginalName        string
	MimeType            string
	SizeBytes           int64
	S3Key               string
	CreatedAt           time.Time
}

// CreateFile inserts a file row and returns it with the generated id and
// created_at. The S3 key is expected to be set by the caller (conventionally
// "files/<file_id>/<original_name>", but the id is generated here so callers
// build the key from the returned ID after the fact, or pass a pre-generated
// uuid).
func (*Store) CreateFile(ctx context.Context, tx *sql.Tx, f *File) (*File, error) {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.S3Key == "" {
		f.S3Key = "files/" + f.ID.String() + "/" + f.OriginalName
	}
	err := tx.QueryRowContext(ctx, `
		INSERT INTO file (id, conversation_id, uploader_principal_id, original_name, mime_type, size_bytes, s3_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`, f.ID, f.ConversationID, f.UploaderPrincipalID, f.OriginalName, f.MimeType, f.SizeBytes, f.S3Key).Scan(&f.CreatedAt)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create file")
	}
	return f, nil
}

// GetFile returns a file row by id.
func (s *Store) GetFile(ctx context.Context, id uuid.UUID) (*File, error) {
	var f File
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, conversation_id, uploader_principal_id, original_name, mime_type, size_bytes, s3_key, created_at
		FROM file
		WHERE id = $1
	`, id).Scan(&f.ID, &f.ConversationID, &f.UploaderPrincipalID, &f.OriginalName, &f.MimeType, &f.SizeBytes, &f.S3Key, &f.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to get file")
	}
	return &f, nil
}

// ListFilesByConversation returns all files attached to a conversation, newest first.
func (s *Store) ListFilesByConversation(ctx context.Context, convID uuid.UUID) ([]*File, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, conversation_id, uploader_principal_id, original_name, mime_type, size_bytes, s3_key, created_at
		FROM file
		WHERE conversation_id = $1
		ORDER BY created_at DESC
	`, convID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list files")
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.ConversationID, &f.UploaderPrincipalID, &f.OriginalName, &f.MimeType, &f.SizeBytes, &f.S3Key, &f.CreatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan file")
		}
		files = append(files, &f)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate files")
	}
	return files, nil
}

// ConversationFile is a file plus the message context that carried it in a
// conversation (sender, send time, message content). Message fields are zero
// when the file was uploaded but never attached to a message.
type ConversationFile struct {
	File
	MessageID        uuid.NullUUID
	MessageContent   string
	MessageCreatedAt sql.NullTime
	SenderName       string
	SenderType       int32
	PrincipalID      string
	AgentResourceID  string
	ThreadRootID     uuid.NullUUID
}

// listConversationFilesSQL joins each file to the earliest message that
// carried it (via the attachments JSONB id) and returns newest-first.
const listConversationFilesSQL = `
	SELECT f.id, f.conversation_id, f.uploader_principal_id, f.original_name, f.mime_type, f.size_bytes, f.s3_key, f.created_at,
	       cm.id, COALESCE(cm.content, ''), cm.created_at, cm.thread_root_message_id,
	       COALESCE(p.name, ''), COALESCE(cm.sender_type, 0), COALESCE(p.handle, ''), COALESCE(a.resource_id, '')
	FROM file f
	LEFT JOIN LATERAL (
		SELECT cm.id, cm.content, cm.created_at, cm.sender_type, cm.principal_id, cm.sender_agent_id, cm.thread_root_message_id
		FROM chat_message cm
		WHERE cm.conversation_id = f.conversation_id
		  AND cm.attachments @> jsonb_build_array(jsonb_build_object('id', f.id::text))
		ORDER BY cm.created_at ASC
		LIMIT 1
	) cm ON true
	LEFT JOIN principal p ON p.id = cm.principal_id
	LEFT JOIN agent a ON a.id = cm.sender_agent_id
	WHERE f.conversation_id = $1
	ORDER BY f.created_at DESC
`

// ListConversationFiles returns all files attached to a conversation, newest
// first, enriched with the message that carried each file. A file uploaded but
// never attached to a message still appears with empty message context.
func (s *Store) ListConversationFiles(ctx context.Context, convID uuid.UUID) ([]*ConversationFile, error) {
	rows, err := s.GetDB().QueryContext(ctx, listConversationFilesSQL, convID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list conversation files")
	}
	defer rows.Close()

	var files []*ConversationFile
	for rows.Next() {
		var cf ConversationFile
		if err := rows.Scan(
			&cf.ID, &cf.ConversationID, &cf.UploaderPrincipalID, &cf.OriginalName, &cf.MimeType, &cf.SizeBytes, &cf.S3Key, &cf.CreatedAt,
			&cf.MessageID, &cf.MessageContent, &cf.MessageCreatedAt, &cf.ThreadRootID,
			&cf.SenderName, &cf.SenderType, &cf.PrincipalID, &cf.AgentResourceID,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan conversation file")
		}
		files = append(files, &cf)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate conversation files")
	}
	return files, nil
}
