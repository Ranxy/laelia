package state

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeHeartbeatWriter counts TouchAgentHeartbeat calls so the buffer can be
// exercised without a Postgres connection.
type fakeHeartbeatWriter struct {
	count atomic.Int64
}

func (f *fakeHeartbeatWriter) TouchAgentHeartbeat(_ context.Context, _ int, _ int64) error {
	f.count.Add(1)
	return nil
}

// TestHeartbeatBuffer_StartIdempotent guards the T11 fix: a second Start must
// not overwrite b.cancel and leak the first flush goroutine. We verify by
// recording one update, double-starting, letting a single tick flush it, and
// asserting exactly one store write occurred (one loop, one flush). The
// previous bug spawned a second loop whose ctx was never cancelled.
func TestHeartbeatBuffer_StartIdempotent(t *testing.T) {
	fake := &fakeHeartbeatWriter{}
	b := NewHeartbeatBuffer(nil, 30*time.Millisecond)
	b.store = fake
	defer b.Stop()

	b.Start(context.Background())
	b.Start(context.Background()) // must be a no-op, not a second loop.

	b.Record(&HeartbeatUpdate{AgentID: 1, LastHeartbeatAt: 1})

	// Wait long enough for the single loop to tick at least once.
	time.Sleep(120 * time.Millisecond)
	b.Stop()

	if got := fake.count.Load(); got != 1 {
		t.Errorf("expected exactly one flush write (idempotent Start), got %d", got)
	}
}

// TestHeartbeatBuffer_StopBeforeStart ensures Stop is safe when Start was never
// called (cancel is nil).
func TestHeartbeatBuffer_StopBeforeStart(_ *testing.T) {
	b := NewHeartbeatBuffer(nil, 30*time.Millisecond)
	b.Stop() // must not panic on nil cancel
}
