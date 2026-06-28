package state

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fastTTLNonceManager builds a NonceManager with a tiny replay TTL so
// expiry/eviction tests do not sleep for the real 45s window.
func fastTTLNonceManager() *NonceManager {
	nm := NewNonceManager()
	nm.replayTTL = 50 * time.Millisecond
	return nm
}

// TestNonce_RejectedOnReplay verifies a nonce accepted once is rejected on a
// second use within the validity window (the core replay fix).
func TestNonce_RejectedOnReplay(t *testing.T) {
	nm := NewNonceManager()
	agent, session := "agents/foo", "sess-1"

	nonce := nm.GenerateNonce(agent, session)
	if !nm.VerifyNonce(nonce, agent, session) {
		t.Fatal("fresh nonce must verify")
	}
	if nm.VerifyNonce(nonce, agent, session) {
		t.Fatal("replayed nonce within window must be rejected")
	}
}

// TestNonce_AcceptsFreshWithinWindow verifies two distinct nonces for the same
// agent/session both verify (only exact reuse is a replay).
func TestNonce_AcceptsFreshWithinWindow(t *testing.T) {
	nm := NewNonceManager()
	agent, session := "agents/foo", "sess-1"

	n1 := nm.GenerateNonce(agent, session)
	n2 := nm.GenerateNonce(agent, session)
	if !nm.VerifyNonce(n1, agent, session) {
		t.Fatal("first fresh nonce must verify")
	}
	if !nm.VerifyNonce(n2, agent, session) {
		t.Fatal("second distinct fresh nonce must verify")
	}
}

// TestNonce_RejectedAfterWindow verifies a nonce whose timestamp is outside the
// [-35s, +5s] window is rejected by the time check (independent of the replay
// cache), using the pure window helper with constructed timestamps — no sleep.
func TestNonce_RejectedAfterWindow(t *testing.T) {
	const now int64 = 1_000_000
	tests := []struct {
		name string
		ts   int64
		want bool
	}{
		{"far past", now - 100, false},
		{"just past edge", now - 36, false},
		{"past boundary", now - 35, true},
		{"now", now, true},
		{"future boundary", now + 5, true},
		{"just future edge", now + 6, false},
		{"far future", now + 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nonceWithinWindow(tt.ts, now); got != tt.want {
				t.Fatalf("nonceWithinWindow(%d, now=%d) = %v, want %v", tt.ts, now, got, tt.want)
			}
		})
	}
}

// TestNonce_ConcurrentReplayOnlyOneSucceeds verifies the atomic check+write:
// many goroutines replaying the same captured nonce, only one verifies.
func TestNonce_ConcurrentReplayOnlyOneSucceeds(t *testing.T) {
	nm := NewNonceManager()
	agent, session := "agents/foo", "sess-1"

	nonce := nm.GenerateNonce(agent, session)

	var wg sync.WaitGroup
	var success atomic.Int64
	const n = 50
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if nm.VerifyNonce(nonce, agent, session) {
				success.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := success.Load(); got != 1 {
		t.Fatalf("concurrent replay of one nonce: %d succeeded, want exactly 1", got)
	}
}

// TestNonce_ReplayCacheEvictsExpired verifies the replay map does not retain
// expired entries (lazy sweep), so it does not leak. Uses a tiny TTL so the
// test sleeps milliseconds, not the real 45s.
func TestNonce_ReplayCacheEvictsExpired(t *testing.T) {
	nm := fastTTLNonceManager()
	agent, session := "agents/foo", "sess-1"

	for range 5 {
		n := nm.GenerateNonce(agent, session)
		if !nm.VerifyNonce(n, agent, session) {
			t.Fatal("fresh nonce must verify")
		}
	}
	if got := len(nm.replay); got != 5 {
		t.Fatalf("expected 5 replay entries, got %d", got)
	}

	// After TTL, a new verification sweeps the expired entries.
	time.Sleep(nm.replayTTL + 200*time.Millisecond)
	fresh := nm.GenerateNonce(agent, session)
	if !nm.VerifyNonce(fresh, agent, session) {
		t.Fatal("fresh nonce after TTL must verify")
	}
	// The sweep on that last call removes the 5 expired entries, leaving only
	// the one just recorded.
	if got := len(nm.replay); got != 1 {
		t.Fatalf("expired replay entries must be swept, got %d remaining, want 1", got)
	}
}

// TestNonce_ReplayTLLCoversWindow locks in the design invariant that the replay
// cache TTL outlives the full validity window: if the entry expired before the
// window closed, a captured nonce could be replayed in-window after the cache
// forgot it. The default TTL must cover [now-35s, now+5s] plus margin.
func TestNonce_ReplayTLLCoversWindow(t *testing.T) {
	window := nonceWindowPast + nonceWindowFuture // 40s full span
	if nonceReplayTTL <= window {
		t.Fatalf("replay TTL %s must exceed the validity window %s so a nonce stays remembered across the whole window", nonceReplayTTL, window)
	}
}
