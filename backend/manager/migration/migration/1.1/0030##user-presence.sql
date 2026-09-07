-- Human user presence: last web heartbeat per user handle, powering the
-- online badge and "last seen" hints. A user is online while its last
-- heartbeat is within the manager's presence TTL (store.PresenceTTL). The
-- handle matches principal.handle (no FK: presence is ephemeral runtime
-- state; rows for deleted principals are harmless dead keys that simply
-- answer offline). Only human users are written here — agents carry their
-- connection state in AgentService, not in this table.
CREATE TABLE IF NOT EXISTS user_presence (
    handle       text PRIMARY KEY,
    -- Time of the user's last web heartbeat (upserted on every beat).
    last_seen_at timestamptz NOT NULL
);

-- Backfill last_seen from each principal's stored UserProfile.last_login_time
-- (protojson output, so the key is camelCase and the value is an RFC3339
-- string) so "last seen" is meaningful from the first deploy. Users who never
-- logged in get no row — they are answered offline/never-seen.
INSERT INTO user_presence (handle, last_seen_at)
SELECT handle, (profile ->> 'lastLoginTime')::timestamptz
FROM principal
WHERE deleted = FALSE AND profile ? 'lastLoginTime'
ON CONFLICT (handle) DO NOTHING;