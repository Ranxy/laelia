package presence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTouchThenOnline(t *testing.T) {
	r := New()
	now := time.Now()

	r.Touch("users/alice", now)
	r.Touch("users/bob", now)

	got := r.Online([]string{"users/alice", "users/bob"}, now, DefaultTTL)
	assert.True(t, got["users/alice"])
	assert.True(t, got["users/bob"])
}

func TestUnknownNameIsOffline(t *testing.T) {
	r := New()
	now := time.Now()

	got := r.Online([]string{"users/ghost"}, now, DefaultTTL)
	assert.False(t, got["users/ghost"])
}

func TestTTLBoundary(t *testing.T) {
	r := New()
	t0 := time.Now()

	r.Touch("users/alice", t0)

	// Exactly at the TTL boundary the heartbeat still counts.
	assert.True(t, r.Online([]string{"users/alice"}, t0.Add(DefaultTTL), DefaultTTL)["users/alice"])
	// One nanosecond past it, the user is offline.
	assert.False(t, r.Online([]string{"users/alice"}, t0.Add(DefaultTTL).Add(time.Nanosecond), DefaultTTL)["users/alice"])
}

func TestTouchOverwrites(t *testing.T) {
	r := New()
	t0 := time.Now()

	r.Touch("users/alice", t0)
	r.Touch("users/alice", t0.Add(DefaultTTL)) // refresh while the old beat is stale

	assert.True(t, r.Online([]string{"users/alice"}, t0.Add(DefaultTTL), DefaultTTL)["users/alice"])
	assert.True(t, r.Online([]string{"users/alice"}, t0.Add(2*DefaultTTL), DefaultTTL)["users/alice"])
}

func TestOnlineNamesAreIndependent(t *testing.T) {
	r := New()
	t0 := time.Now()

	r.Touch("users/stale", t0)
	later := t0.Add(DefaultTTL + time.Second)
	r.Touch("users/fresh", later)

	got := r.Online([]string{"users/fresh", "users/stale"}, later, DefaultTTL)
	assert.True(t, got["users/fresh"])
	assert.False(t, got["users/stale"])
}
