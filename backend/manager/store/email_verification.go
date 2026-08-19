package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pkg/errors"
)

// Email verification token sentinel errors. VerifyUserEmail maps them to
// client-facing codes in the API layer.
var (
	ErrVerificationTokenNotFound = errors.New("verification token not found")
	ErrVerificationTokenExpired  = errors.New("verification token expired")
	ErrVerificationTokenConsumed = errors.New("verification token already used")
)

// EmailVerificationTokenMessage is a single-use token row. Only the SHA-256
// hash of the plaintext token is stored (aligned with agent_token), so a
// database leak cannot be used to verify arbitrary accounts.
type EmailVerificationTokenMessage struct {
	ID          int64
	TokenHash   string
	PrincipalID int
	Email       string
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	CreatedAt   time.Time
}

// CreateEmailVerificationToken inserts a single-use verification token for the
// principal. Callers generate the token with crypto/rand and store its hash.
func (s *Store) CreateEmailVerificationToken(ctx context.Context, principalID int, tokenHash string, expiresAt time.Time) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO email_verification_token (token_hash, principal_id, expires_at)
		VALUES ($1, $2, $3)`,
		tokenHash, principalID, expiresAt); err != nil {
		return errors.Wrap(err, "failed to create email verification token")
	}
	return nil
}

// VerifyUserEmail verifies the email of the principal owning the token. The
// token must exist, be unconsumed, and be unexpired; the verification succeeds
// even when the email is already verified (idempotent re-click of the link).
// On success the token is consumed so each link works exactly once. The user
// caches are evicted so the next lookup reflects email_verified_at.
func (s *Store) VerifyUserEmail(ctx context.Context, tokenHash string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var token EmailVerificationTokenMessage
	var consumedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT t.id, t.principal_id, p.email, t.expires_at, t.consumed_at
		FROM email_verification_token t
		JOIN principal p ON p.id = t.principal_id
		WHERE t.token_hash = $1`, tokenHash).
		Scan(&token.ID, &token.PrincipalID, &token.Email, &token.ExpiresAt, &consumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVerificationTokenNotFound
		}
		return errors.Wrap(err, "failed to find email verification token")
	}
	if consumedAt.Valid {
		return ErrVerificationTokenConsumed
	}
	if token.ExpiresAt.Before(time.Now()) {
		return ErrVerificationTokenExpired
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE principal SET email_verified_at = now()
		WHERE id = $1 AND email_verified_at IS NULL`, token.PrincipalID); err != nil {
		return errors.Wrap(err, "failed to mark email verified")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE email_verification_token SET consumed_at = now()
		WHERE id = $1 AND consumed_at IS NULL`, token.ID); err != nil {
		return errors.Wrap(err, "failed to consume email verification token")
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit email verification")
	}

	s.invalidateUserCache(token.PrincipalID, token.Email)
	return nil
}

// InvalidateEmailVerificationTokens consumes every outstanding token of the
// principal so only the newest link stays valid (called on resend).
func (s *Store) InvalidateEmailVerificationTokens(ctx context.Context, principalID int) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE email_verification_token SET consumed_at = now()
		WHERE principal_id = $1 AND consumed_at IS NULL`, principalID); err != nil {
		return errors.Wrap(err, "failed to invalidate email verification tokens")
	}
	return nil
}

// DeleteUnverifiedUsersOlderThan soft-deletes END_USER accounts that never
// verified their email within the window. Unverified users cannot sign in, so
// they never own agents, conversations, or IAM bindings; soft-delete matches
// the account-deactivation path and frees the email for re-registration via
// the partial unique index. Returns the number of deactivated accounts.
func (s *Store) DeleteUnverifiedUsersOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE principal SET deleted = TRUE
		WHERE type = 'END_USER' AND deleted = FALSE
		  AND email_verified_at IS NULL AND created_at < $1`, before)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete unverified users")
	}
	s.InvalidateGlobalMentionIndex()
	return res.RowsAffected()
}
