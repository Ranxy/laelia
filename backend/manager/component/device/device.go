// Package device implements the in-memory session store for the OAuth2-style
// device code flow (DeviceService). A session is created by the machine CLI
// (StartDeviceLogin), displayed on the public approval page via its user code,
// and resolved by the CLI's poll loop via its device code. Sessions are
// short-lived (10 min TTL) and single-instance: a manager restart loses them
// and the CLI simply re-runs setup.
package device

import (
	"context"
	"sync"
	"time"
)

// Status is the lifecycle state of a device login session.
type Status int

const (
	StatusPending Status = iota
	StatusApproved
	StatusExpired
	StatusDenied
)

// SessionTTL is how long a pending session stays valid before it expires.
const SessionTTL = 10 * time.Minute

// GraceWindow is how long an APPROVED session keeps returning its result to
// PollDeviceLogin after approval, so a CLI that crashed between approval and
// saving its state can recover by re-polling.
const GraceWindow = 10 * time.Minute

// Result is the approval outcome delivered to the CLI on poll.
type Result struct {
	MachineID    string
	MachineTitle string
	RefreshToken string
}

// Session is one device login attempt.
type Session struct {
	DeviceCode string
	UserCode   string
	Status     Status

	// MachineID is the existing machine to re-authenticate, or "" for a
	// first-time registration (the manager creates a new machine at approval).
	MachineID string

	Hostname    string
	OS          string
	Arch        string
	IP          string
	Version     string
	Fingerprint string

	CreatedAt time.Time
	ExpiresAt time.Time
	// LastPolledAt is the last PollDeviceLogin time; the server enforces a
	// minimum poll interval to keep anonymous polling cheap.
	LastPolledAt time.Time

	ApprovedAt time.Time
	ApprovedBy int

	Result       *Result
	DenialReason string
}

// Store is a mutex-guarded in-memory session registry keyed by both the
// device code (CLI poll) and the user code (approval page). Expired and
// denied sessions are purged lazily on access and by a background sweep.
type Store struct {
	mu       sync.Mutex
	byDevice map[string]*Session
	byUser   map[string]*Session
}

// New returns an empty device session store.
func New() *Store {
	return &Store{
		byDevice: make(map[string]*Session),
		byUser:   make(map[string]*Session),
	}
}

// Start registers a new pending session. The caller generates the codes.
func (s *Store) Start(sess *Session) {
	now := time.Now()
	sess.CreatedAt = now
	sess.ExpiresAt = now.Add(SessionTTL)
	sess.Status = StatusPending

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	s.byDevice[sess.DeviceCode] = sess
	s.byUser[sess.UserCode] = sess
}

// GetByDeviceCode returns the session for a device code, or nil. A session
// past its TTL is reported as expired and purged.
func (s *Store) GetByDeviceCode(code string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	return s.byDevice[code]
}

// GetByUserCode returns the session for a user code, or nil.
func (s *Store) GetByUserCode(code string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	return s.byUser[code]
}

// Approve marks the session approved with the given result.
func (s *Store) Approve(sess *Session, approvedBy int, result *Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Status = StatusApproved
	sess.ApprovedAt = time.Now()
	sess.ApprovedBy = approvedBy
	sess.Result = result
}

// Deny marks the session denied with a human-readable reason.
func (s *Store) Deny(sess *Session, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.Status = StatusDenied
	sess.DenialReason = reason
}

// TouchPoll records a poll and reports whether it is allowed under the
// minimum interval. Returns false when the caller polled too recently.
func (s *Store) TouchPoll(sess *Session, minInterval time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if !sess.LastPolledAt.IsZero() && now.Sub(sess.LastPolledAt) < minInterval {
		return false
	}
	sess.LastPolledAt = now
	return true
}

// StartSweeper launches the background sweep loop. It stops when ctx is
// cancelled.
func (s *Store) StartSweeper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.mu.Lock()
				s.sweepLocked(now)
				s.mu.Unlock()
			}
		}
	}()
}

// sweepLocked purges sessions that are expired, denied, or approved beyond
// their grace window. Callers must hold s.mu.
func (s *Store) sweepLocked(now time.Time) {
	for code, sess := range s.byDevice {
		if s.expiredLocked(sess, now) {
			delete(s.byDevice, code)
			delete(s.byUser, sess.UserCode)
		}
	}
}

// expiredLocked reports whether a session should be purged. Callers must
// hold s.mu.
func (*Store) expiredLocked(sess *Session, now time.Time) bool {
	switch sess.Status {
	case StatusPending:
		return now.After(sess.ExpiresAt)
	case StatusApproved:
		return now.After(sess.ApprovedAt.Add(GraceWindow))
	case StatusDenied, StatusExpired:
		return true
	default:
		return true
	}
}
