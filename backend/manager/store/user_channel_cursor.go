package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// UserChannelCursor records how far a user has read a conversation. The
// frontend compares conversation.version against ReadVersion to render unread
// badges. A missing row is treated as "caught up to current version" (see
// ListUserConversationsWithUnread), so a newly joined user sees only future
// messages unless they fetch history explicitly.
type UserChannelCursor struct {
	PrincipalID    int
	ConversationID uuid.UUID
	ReadVersion    int64
	UpdatedAt      time.Time
}

// UpsertUserReadCursor advances the user's read cursor for a conversation to
// readVersion. The update is monotonic: a lower value never overwrites a higher
// one (GREATEST), so a stale mark cannot rewind progress. It returns the
// resulting read_version.
func (s *Store) UpsertUserReadCursor(ctx context.Context, principalID int, conversationID uuid.UUID, readVersion int64) (int64, error) {
	var result int64
	err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO user_channel_cursor (principal_id, conversation_id, read_version, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (principal_id, conversation_id) DO UPDATE
		   SET read_version = GREATEST(user_channel_cursor.read_version, EXCLUDED.read_version),
		       updated_at = now()
		RETURNING read_version
	`, principalID, conversationID, readVersion).Scan(&result)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to upsert user read cursor")
	}
	return result, nil
}

// upsertUserReadCursorTx is the tx-scoped variant used when seeding the cursor
// inside the same transaction that creates the conversation, so a failure rolls
// back the seed together with the conversation.
func upsertUserReadCursorTx(ctx context.Context, tx *sql.Tx, principalID int, conversationID uuid.UUID, readVersion int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO user_channel_cursor (principal_id, conversation_id, read_version, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (principal_id, conversation_id) DO UPDATE
		   SET read_version = GREATEST(user_channel_cursor.read_version, EXCLUDED.read_version),
		       updated_at = now()
	`, principalID, conversationID, readVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert user read cursor in tx")
	}
	return nil
}

// GetUserReadCursor returns the user's read_version for a conversation. found is
// false when no cursor row exists; callers should treat that as "caught up to
// current version" (i.e. no unread messages), not as "read from zero".
func (s *Store) GetUserReadCursor(ctx context.Context, principalID int, conversationID uuid.UUID) (readVersion int64, found bool, err error) {
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT read_version FROM user_channel_cursor
		WHERE principal_id = $1 AND conversation_id = $2
	`, principalID, conversationID).Scan(&readVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, errors.Wrapf(err, "failed to get user read cursor")
	}
	return readVersion, true, nil
}

// SeedUserReadCursorOnJoin initializes a user's cursor for a conversation to
// the conversation's current version, so a newly joined user starts "caught up"
// and only sees future messages as unread. Idempotent: it never lowers an
// existing cursor (UpsertUserReadCursor is monotonic). Callers should only seed
// on the creation/join path, never when re-opening an existing conversation
// (that would erase unread state).
func (s *Store) SeedUserReadCursorOnJoin(ctx context.Context, principalID int, conversationID uuid.UUID) error {
	var currentVersion int64
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT version FROM conversation WHERE id = $1
	`, conversationID).Scan(&currentVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to read conversation version for user cursor seed")
	}
	if _, err := s.UpsertUserReadCursor(ctx, principalID, conversationID, currentVersion); err != nil {
		return errors.Wrapf(err, "failed to seed user read cursor")
	}
	return nil
}
