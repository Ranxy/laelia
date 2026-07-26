-- Web Push subscriptions for browser notifications on directed messages
-- (mentions, thread replies, task/reminder updates, 1:1 DMs). One user may
-- have many subscriptions (multiple devices/browsers). PK (principal_id,
-- endpoint) makes re-subscribing the same browser idempotent; ON DELETE CASCADE
-- drops a user's subscriptions with the account. Keys (p256dh, auth) are
-- refreshed on upsert since browsers can rotate them.
CREATE TABLE IF NOT EXISTS web_push_subscription (
    principal_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    endpoint     TEXT NOT NULL,
    p256dh       TEXT NOT NULL,
    auth         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, endpoint)
);

CREATE INDEX IF NOT EXISTS idx_web_push_subscription_user
    ON web_push_subscription (principal_id);