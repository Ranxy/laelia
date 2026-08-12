package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/component/mailer"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// verificationTokenTTL is how long a verification link stays valid. Unverified
// accounts are cleaned up by the scheduler after the same window.
const verificationTokenTTL = 72 * time.Hour

// newEmailVerificationToken returns a random token and its SHA-256 hex hash.
// Only the hash is persisted (aligned with agent_token), so a database leak
// cannot be used to verify arbitrary accounts.
func newEmailVerificationToken() (token, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

// hashEmailVerificationToken hashes a received token for lookup.
func hashEmailVerificationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// issueVerificationEmail creates a fresh one-time token for the user and sends
// the verification email with the clickable link. baseURL is the public
// frontend origin used to build the link; when the workspace external URL is
// unset the caller falls back to the request Origin. Sending is best-effort:
// a failure is returned so callers can log it; the user can request a resend.
func issueVerificationEmail(ctx context.Context, sender *mailer.Sender, st *store.Store, user *store.UserMessage, baseURL string) error {
	token, tokenHash, err := newEmailVerificationToken()
	if err != nil {
		return errors.Wrap(err, "failed to generate email verification token")
	}
	// Only the newest link stays valid (each resend invalidates the previous).
	if err := st.InvalidateEmailVerificationTokens(ctx, user.ID); err != nil {
		return errors.Wrap(err, "failed to invalidate previous verification tokens")
	}
	if err := st.CreateEmailVerificationToken(ctx, user.ID, tokenHash, time.Now().Add(verificationTokenTTL)); err != nil {
		return errors.Wrap(err, "failed to store email verification token")
	}
	link := fmt.Sprintf("%s/auth/verify-email?token=%s", baseURL, token)
	if err := sender.SendVerificationEmail(ctx, user.Email, link); err != nil {
		return errors.Wrap(err, "failed to send verification email")
	}
	return nil
}
