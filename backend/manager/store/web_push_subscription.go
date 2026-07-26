package store

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// WebPushSubscription is one registered browser push endpoint for a user. One
// user may have many rows (multiple devices/browsers). The push sender reads
// these to deliver Web Push notifications; the 404/410 cleanup path deletes
// stale endpoints.
type WebPushSubscription struct {
	PrincipalID int
	Endpoint    string
	P256dh      string
	Auth        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WebPushSender dispatches a Web Push notification payload to all of a user's
// registered browser subscriptions. Implemented by component/webpush.Sender and
// injected via Store.SetWebPushSender to avoid a circular dependency (the
// webpush component imports the store). The implementation must be safe to call
// on a detached goroutine and must not block the caller — it owns its own
// fan-out. SendToUser is best-effort: a missed push is not data corruption.
type WebPushSender interface {
	SendToUser(ctx context.Context, principalID int, payload []byte)
}

// SetWebPushSender injects the Web Push dispatcher. Called once from server
// wiring after both the store and the webpush sender are constructed. Passing
// nil disables push dispatch (generateActivityRows treats nil as a no-op).
func (s *Store) SetWebPushSender(sender WebPushSender) {
	s.webPushSender = sender
}

const upsertWebPushSubscriptionSQL = `INSERT INTO web_push_subscription (principal_id, endpoint, p256dh, auth, created_at, updated_at)
VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (principal_id, endpoint) DO UPDATE
   SET p256dh = EXCLUDED.p256dh,
       auth = EXCLUDED.auth,
       updated_at = now()`

// UpsertWebPushSubscription registers or refreshes a browser push subscription
// for a user. Idempotent on (principal_id, endpoint); re-subscribing the same
// browser refreshes its p256dh/auth keys (browsers can rotate them).
func (s *Store) UpsertWebPushSubscription(ctx context.Context, principalID int, endpoint, p256dh, auth string) error {
	_, err := s.GetDB().ExecContext(ctx, upsertWebPushSubscriptionSQL, principalID, endpoint, p256dh, auth)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert web push subscription")
	}
	return nil
}

const deleteWebPushSubscriptionSQL = `DELETE FROM web_push_subscription WHERE principal_id = $1 AND endpoint = $2`

// DeleteWebPushSubscription removes a single subscription. Used when the user
// toggles notifications off in settings.
func (s *Store) DeleteWebPushSubscription(ctx context.Context, principalID int, endpoint string) error {
	_, err := s.GetDB().ExecContext(ctx, deleteWebPushSubscriptionSQL, principalID, endpoint)
	if err != nil {
		return errors.Wrapf(err, "failed to delete web push subscription")
	}
	return nil
}

const deleteWebPushSubscriptionByEndpointSQL = `DELETE FROM web_push_subscription WHERE endpoint = $1`

// DeleteWebPushSubscriptionByEndpoint removes every subscription for an
// endpoint regardless of owner. Used by the push sender's 404/410 cleanup path,
// where the push service reports the endpoint dead but the sender may not know
// the principal_id.
func (s *Store) DeleteWebPushSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.GetDB().ExecContext(ctx, deleteWebPushSubscriptionByEndpointSQL, endpoint)
	if err != nil {
		return errors.Wrapf(err, "failed to delete web push subscription by endpoint")
	}
	return nil
}

const listWebPushSubscriptionsSQL = `SELECT principal_id, endpoint, p256dh, auth, created_at, updated_at
FROM web_push_subscription
WHERE principal_id = $1
ORDER BY created_at`

// ListWebPushSubscriptions returns every push subscription registered for a
// user (one row per device/browser). The push sender fans a notification out to
// all of them.
func (s *Store) ListWebPushSubscriptions(ctx context.Context, principalID int) ([]*WebPushSubscription, error) {
	rows, err := s.GetDB().QueryContext(ctx, listWebPushSubscriptionsSQL, principalID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list web push subscriptions")
	}
	defer rows.Close()

	var subs []*WebPushSubscription
	for rows.Next() {
		var sub WebPushSubscription
		if err := rows.Scan(&sub.PrincipalID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan web push subscription")
		}
		subs = append(subs, &sub)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate web push subscriptions")
	}
	return subs, nil
}
