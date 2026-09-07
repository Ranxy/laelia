package store

import (
	"strings"
	"testing"
	"time"
)

// TestTouchPresenceSQL locks in the upsert-shaped write path: concurrent
// beats from multiple tabs/devices must land as single-row upserts against
// the handle PK, never duplicate-key errors, and a beat must overwrite the
// stored last_seen_at.
func TestTouchPresenceSQL(t *testing.T) {
	if !strings.Contains(touchPresenceSQL, "ON CONFLICT (handle) DO UPDATE") {
		t.Fatal("TouchPresence must upsert on the handle PK to be race-free under concurrent beats")
	}
	if !strings.Contains(touchPresenceSQL, "SET last_seen_at = EXCLUDED.last_seen_at") {
		t.Fatal("conflict must refresh last_seen_at so a newer beat overwrites the stored one")
	}
}

// TestPresenceOnlineTTLBoundary locks in the sliding-window semantics: a
// heartbeat exactly at the TTL upper bound is still online, one nanosecond
// later is offline.
func TestPresenceOnlineTTLBoundary(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lastSeen := now.Add(-PresenceTTL)

	if !PresenceOnline(lastSeen, now, PresenceTTL) {
		t.Fatal("a heartbeat exactly at the TTL bound must count as online")
	}
	if PresenceOnline(lastSeen.Add(-time.Nanosecond), now, PresenceTTL) {
		t.Fatal("a heartbeat one nanosecond past the TTL bound must count as offline")
	}
	if !PresenceOnline(now, now, PresenceTTL) {
		t.Fatal("a just-sent heartbeat must count as online")
	}
}
