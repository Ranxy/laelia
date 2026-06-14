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

type HeartbeatBuffer struct {
	mu       sync.Mutex
	updates  map[int]*HeartbeatUpdate
	store    *store.Store
	interval time.Duration
	cancel   context.CancelFunc
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

func (b *HeartbeatBuffer) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

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
}

func (b *HeartbeatBuffer) Stop() {
	if b.cancel != nil {
		b.cancel()
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

	for _, update := range snapshot {
		if err := b.store.TouchAgentSessionForHeartbeat(context.Background(), update.AgentID, update.LastHeartbeatAt); err != nil {
			slog.Error("failed to batch update agent heartbeat", "agent_id", update.AgentID, "error", err)
		}
	}
}
