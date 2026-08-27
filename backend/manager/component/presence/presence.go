// Package presence tracks which human users are currently online, powering
// the chat page's green presence badge. It is an in-process registry: the web
// frontend heartbeats via CommandService.SyncPresence every 30s, and a user
// counts as online while its last heartbeat is within the TTL. Single-process
// only, like roomhub; a multi-instance deployment needs a shared store behind
// the same interface.
package presence

import (
	"sync"
	"time"
)

// DefaultTTL is the sliding online window: a principal whose last heartbeat is
// older than this counts as offline. With the frontend's 30s heartbeat it
// tolerates two missed beats, and a background tab throttled to one beat per
// minute still stays inside the window.
const DefaultTTL = 90 * time.Second

// Registry maps principal resource names ("users/<handle>") to the time of
// their last heartbeat. Entries are never swept: the map is bounded by the
// workspace's principal count, and a stale entry simply answers offline.
type Registry struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{lastSeen: make(map[string]time.Time)}
}

// Touch records a heartbeat for name at the given time.
func (r *Registry) Touch(name string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSeen[name] = now
}

// Online answers whether each of names had a heartbeat within ttl of now.
// Names never touched are answered false.
func (r *Registry) Online(names []string, now time.Time, ttl time.Duration) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]bool, len(names))
	for _, name := range names {
		seen, ok := r.lastSeen[name]
		result[name] = ok && now.Sub(seen) <= ttl
	}
	return result
}
