package v1

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// auditLogWriter is the narrow store dependency the buffer flushes to.
// *store.Store satisfies it; tests may pass a fake. Keeping it an interface
// (rather than *store.Store) lets the buffer be unit-tested without a
// Postgres connection.
type auditLogWriter interface {
	CreateAuditLogs(ctx context.Context, logs []*store.AuditLogMessage) error
}

// AuditBuffer batches audit-log records in memory and flushes them with a
// single multi-row INSERT per interval, instead of spawning one goroutine and
// one INSERT per audited call. Same shape as state.HeartbeatBuffer: snapshot
// swap on flush so a slow Postgres cannot stall new records, single-flight
// Start, final flush on cancel.
type AuditBuffer struct {
	mu       sync.Mutex
	logs     []*store.AuditLogMessage
	store    auditLogWriter
	interval time.Duration

	// startMu guards the single-flight Start so a second call cannot overwrite
	// cancel and leak the first flush goroutine.
	startMu sync.Mutex
	cancel  context.CancelFunc
}

func NewAuditBuffer(store *store.Store, interval time.Duration) *AuditBuffer {
	if interval == 0 {
		interval = 2 * time.Second
	}
	return &AuditBuffer{
		store:    store,
		interval: interval,
	}
}

func (b *AuditBuffer) Record(log *store.AuditLogMessage) {
	b.mu.Lock()
	b.logs = append(b.logs, log)
	b.mu.Unlock()
}

// Start launches the flush ticker. It is idempotent: a second call returns
// immediately instead of overwriting b.cancel and leaking the first flush
// goroutine. The goroutine exits when ctx is cancelled and does a final flush.
func (b *AuditBuffer) Start(ctx context.Context) {
	b.startMu.Lock()
	if b.cancel != nil {
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

func (b *AuditBuffer) Stop() {
	b.startMu.Lock()
	cancel := b.cancel
	b.startMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *AuditBuffer) flush() {
	b.mu.Lock()
	snapshot := b.logs
	b.logs = nil
	b.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}

	// Bound the DB write so a hung Postgres does not block the flush loop (and
	// thus shutdown's final flush) indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.store.CreateAuditLogs(ctx, snapshot); err != nil {
		slog.Error("failed to flush audit logs", "count", len(snapshot), "error", err)
	}
}
