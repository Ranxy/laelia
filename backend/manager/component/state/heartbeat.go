package state

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/laelia/backend/manager/store"
)

type HeartbeatUpdate struct {
	AgentID         int
	LastHeartbeatAt int64
	SessionID       string
}

// heartbeatWriter is the narrow store dependency the buffer flushes to. *store.Store
// satisfies it; tests may pass a fake. Keeping it an interface (rather than
// *store.Store) lets the buffer be unit-tested without a Postgres connection.
type heartbeatWriter interface {
	TouchAgentHeartbeat(ctx context.Context, agentID int, lastHeartbeatAt int64) error
}

type HeartbeatBuffer struct {
	mu       sync.Mutex
	updates  map[int]*HeartbeatUpdate
	store    heartbeatWriter
	interval time.Duration

	// startMu guards the single-flight Start so a second call cannot overwrite
	// cancel and leak the first flush goroutine.
	startMu sync.Mutex
	cancel  context.CancelFunc
}

func NewHeartbeatBuffer(store *store.Store, interval time.Duration) *HeartbeatBuffer {
	if interval == 0 {
		interval = 10 * time.Second
	}
	return &HeartbeatBuffer{
		updates:  make(map[int]*HeartbeatUpdate),
		store:    store,
		interval: interval,
	}
}

func (b *HeartbeatBuffer) Record(update *HeartbeatUpdate) {
	b.mu.Lock()
	b.updates[update.AgentID] = update
	b.mu.Unlock()
}

func (b *HeartbeatBuffer) GetLatest(agentID int) *HeartbeatUpdate {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.updates[agentID]
}

// Start launches the flush ticker. It is idempotent: a second call (e.g. if
// the server wiring ever double-starts it) returns immediately instead of
// overwriting b.cancel and leaving the first goroutine running with no way
// to stop it. The goroutine exits when ctx is cancelled and does a final flush.
func (b *HeartbeatBuffer) Start(ctx context.Context) {
	b.startMu.Lock()
	if b.cancel != nil {
		// Already running.
		b.startMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.startMu.Unlock()

	go func() {
		ticker := time.NewTicker(b.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				b.flush()
				return
			case <-ticker.C:
				b.flush()
			}
		}
	}()
}

func (b *HeartbeatBuffer) Stop() {
	b.startMu.Lock()
	cancel := b.cancel
	b.startMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *HeartbeatBuffer) flush() {
	b.mu.Lock()
	snapshot := b.updates
	b.updates = make(map[int]*HeartbeatUpdate)
	b.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}

	// Bound the DB write so a hung Postgres does not block the flush loop (and
	// thus shutdown's final flush) indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, update := range snapshot {
		if err := b.store.TouchAgentHeartbeat(ctx, update.AgentID, update.LastHeartbeatAt); err != nil {
			slog.Error("failed to batch update agent heartbeat", "agent_id", update.AgentID, "error", err)
		}
	}
}
