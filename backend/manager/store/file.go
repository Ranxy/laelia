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
func (s *Store) CreateFile(ctx context.Context, f *File) (*File, error) {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.S3Key == "" {
		f.S3Key = "files/" + f.ID.String() + "/" + f.OriginalName
	}
	err := s.GetDB().QueryRowContext(ctx, `
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
