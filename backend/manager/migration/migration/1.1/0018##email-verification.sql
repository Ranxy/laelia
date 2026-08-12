-- Email verification for self-service signup.
--
-- NULL email_verified_at marks an account created by self-service signup with
-- email verification required; such accounts cannot sign in until the
-- verification link is clicked. Every pre-existing and admin-created account
-- is backfilled as verified so only new self-signups are affected.
ALTER TABLE principal ADD COLUMN IF NOT EXISTS email_verified_at timestamptz;
UPDATE principal SET email_verified_at = now() WHERE email_verified_at IS NULL;

-- Single-use email verification tokens. Only the SHA-256 hash of the token is
-- stored (aligned with agent_token), so a database leak cannot be used to
-- verify arbitrary accounts.
CREATE TABLE IF NOT EXISTS email_verification_token (
    id bigserial PRIMARY KEY,
    token_hash text NOT NULL UNIQUE,
    principal_id int NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_email_verification_token_principal
    ON email_verification_token(principal_id);
