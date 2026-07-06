// Package scheduler fires reminders at their scheduled time. It runs two
// background tick loops: a due scan that flips PENDING reminders whose fire_at
// has passed to DUE and wakes the owning agent (or schedules an offline retry),
// and a retry scan that re-attempts delivery for DUE reminders whose agent was
// offline at fire time, with a bounded backoff. After the backoff is exhausted
// a one-shot reminder is marked MISSED; a recurring reminder is rescheduled to
// the next cron fire. The scheduler is crash-safe: it scans the database each
// tick (no in-memory timer heap), so a restart picks up all pending work.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/schedule"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// retryBackoff is the offline-delivery retry schedule. After a reminder fires
// and the owning agent is offline, the scheduler retries at +5s, +10s, +20s,
// +30s, +60s. If the agent is still offline after the 5th retry, the fire is
// missed (one-shot terminal, recurring reschedules).
var retryBackoff = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// tickInterval is the scan cadence for both loops. 1s keeps fire latency tight
// while the partial indexes make each scan a cheap index range scan.
const tickInterval = 1 * time.Second

// Scheduler fires reminders by scanning the reminder table on a ticker. It is
// constructed with the store (for scans and status transitions), the
// dispatcher (to check agent connectivity and wake connected agents), and an
// injectable clock for tests.
type Scheduler struct {
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
	now        func() time.Time

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	wg              sync.WaitGroup
}

// New returns a scheduler that scans store and wakes agents via disp. The clock
// defaults to time.Now; tests may inject a fake.
func New(s *store.Store, disp *dispatcher.Dispatcher) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		store:           s,
		dispatcher:      disp,
		now:             time.Now,
		lifecycleCtx:    ctx,
		lifecycleCancel: cancel,
	}
}

// Start launches the due and retry scan loops. They run until Stop cancels the
// scheduler's lifecycle context, and are tracked on the WaitGroup so shutdown
// joins them. Idempotent: calling Start twice spawns two loops; callers call
// it once from Server.Run.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.runLoop(s.scanDue)
	s.wg.Add(1)
	go s.runLoop(s.scanRetry)
}

// Stop cancels the lifecycle context and waits for both loops to exit.
// Idempotent.
func (s *Scheduler) Stop() {
	s.lifecycleCancel()
	s.wg.Wait()
}

// runLoop runs scan on each tick of tickInterval until the lifecycle context is
// cancelled.
func (s *Scheduler) runLoop(scan func(context.Context)) {
	defer s.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.lifecycleCtx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.lifecycleCtx, 5*time.Second)
			scan(ctx)
			cancel()
		}
	}
}

// scanDue flips PENDING reminders whose fire_at has passed to DUE and either
// wakes the owning agent (if connected) or schedules the first offline retry.
func (s *Scheduler) scanDue(ctx context.Context) {
	now := s.now()
	reminders, err := s.store.ListDuePending(ctx, now)
	if err != nil {
		slog.Warn("scheduler: failed to list due-pending reminders", "error", err)
		return
	}
	for _, r := range reminders {
		if err := s.store.MarkDue(ctx, r.MessageID, now); err != nil {
			slog.Warn("scheduler: failed to mark reminder due", "messageID", r.MessageID, "error", err)
			continue
		}
		s.deliver(ctx, r, 0)
	}
}

// scanRetry re-attempts delivery for DUE reminders whose next_retry_at has
// passed. A connected agent is woken (retry timer cleared); an offline agent
// advances the backoff, or is marked MISSED once the backoff is exhausted.
func (s *Scheduler) scanRetry(ctx context.Context) {
	now := s.now()
	reminders, err := s.store.ListDueRetrying(ctx, now)
	if err != nil {
		slog.Warn("scheduler: failed to list due-retrying reminders", "error", err)
		return
	}
	for _, r := range reminders {
		s.deliver(ctx, r, r.RetryCount)
	}
}

// deliver attempts to wake the owning agent for a DUE reminder. If the agent is
// connected, it is woken and the retry timer is cleared. If offline, the next
// retry is scheduled at now + backoff[retryCount]; when retryCount exceeds the
// backoff length the fire is missed (recurring reminders reschedule to the next
// cron fire, one-shot reminders become terminal MISSED).
func (s *Scheduler) deliver(ctx context.Context, r *store.Reminder, retryCount int32) {
	if s.dispatcher.IsAgentConnected(r.AssigneeAgentID) {
		if err := s.store.ClearRetry(ctx, r.MessageID); err != nil {
			slog.Warn("scheduler: failed to clear reminder retry", "messageID", r.MessageID, "error", err)
		}
		s.dispatcher.NotifyWake(ctx, r.AssigneeAgentID)
		return
	}

	if int(retryCount) >= len(retryBackoff) {
		s.miss(ctx, r)
		return
	}
	next := s.now().Add(retryBackoff[retryCount])
	if err := s.store.SetRetry(ctx, r.MessageID, retryCount+1, next, s.now()); err != nil {
		slog.Warn("scheduler: failed to schedule reminder retry", "messageID", r.MessageID, "error", err)
	}
}

// miss handles an exhausted retry backoff: a recurring reminder reschedules to
// the next cron fire (in PENDING), a one-shot reminder becomes terminal MISSED.
// Either way a single SYSTEM thread message records the miss.
func (s *Scheduler) miss(ctx context.Context, r *store.Reminder) {
	var nextFireAt *time.Time
	if r.CronExpr != "" {
		next, err := schedule.NextFire(r.CronExpr, r.Tz, s.now())
		if err != nil {
			slog.Warn("scheduler: failed to compute next fire for missed reminder", "messageID", r.MessageID, "cron", r.CronExpr, "error", err)
			// Fall back to marking terminal MISSED so the row does not loop.
		} else {
			nextFireAt = &next
		}
	}
	if _, err := s.store.MarkMissedAndPostNotification(ctx, r.MessageID, nextFireAt); err != nil {
		slog.Warn("scheduler: failed to mark reminder missed", "messageID", r.MessageID, "error", err)
	}
}
