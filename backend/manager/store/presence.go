package store

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// PresenceTTL is the sliding online window: a user whose last heartbeat is
// older than this counts as offline. With the frontend's 30s heartbeat it
// tolerates two missed beats, and a background tab throttled to one beat per
// minute still stays inside the window.
const PresenceTTL = 90 * time.Second

// touchPresenceSQL upserts one heartbeat. The PK conflict clause is the
// race-free write path: concurrent beats from multiple tabs/devices all land
// as single-row upserts instead of duplicate-key errors.
const touchPresenceSQL = `
	INSERT INTO user_presence (handle, last_seen_at)
	VALUES ($1, $2)
	ON CONFLICT (handle) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at`

// listPresenceSQL reads the whole presence table. The set is defined by the
// server (bounded by the workspace's user count) — callers cannot ask for
// specific names.
const listPresenceSQL = `
	SELECT handle, last_seen_at
	FROM user_presence`

// PresenceRow is one user's last-heartbeat record. The handle matches
// principal.handle; only human users are tracked (agents carry their
// connection state in AgentService, not here).
type PresenceRow struct {
	Handle     string
	LastSeenAt time.Time
}

// TouchPresence upserts one heartbeat for handle at the given time.
func (s *Store) TouchPresence(ctx context.Context, handle string, now time.Time) error {
	if _, err := s.GetDB().ExecContext(ctx, touchPresenceSQL, handle, now); err != nil {
		return errors.Wrap(err, "failed to touch user presence")
	}
	return nil
}

// ListPresence returns every tracked user's heartbeat record. Online state is
// derived per row with PresenceOnline.
func (s *Store) ListPresence(ctx context.Context) ([]PresenceRow, error) {
	rows, err := s.GetDB().QueryContext(ctx, listPresenceSQL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user presence")
	}
	defer rows.Close()

	var result []PresenceRow
	for rows.Next() {
		var row PresenceRow
		if err := rows.Scan(&row.Handle, &row.LastSeenAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan user presence row")
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate user presence rows")
	}
	return result, nil
}

// PresenceOnline answers whether a last-heartbeat time counts as online at
// now under the given TTL. Pure so handlers and tests share one derivation.
func PresenceOnline(lastSeen, now time.Time, ttl time.Duration) bool {
	return now.Sub(lastSeen) <= ttl
}
