package outbox

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// BatchWindow is the uploader's tick: how long newly appended records may
// wait before upload. It bounds the UI-visible reporting delay (§3.2).
var BatchWindow = 200 * time.Millisecond

// UploadMaxBytes caps one UploadCommandData request (ingress body limit); the
// uploader shards the log along WAL order and re-uploads from the next batch,
// so a large backlog drains in batches instead of one request.
var UploadMaxBytes = int64(4 << 20)

// TruncateThresholdBytes is the settled-prefix backstop: normally records are
// evicted only when the command's terminal is acked (one big TruncateFront per
// turn, amortized off the hot path); a single turn that outgrows this triggers
// a mid-turn truncation instead.
var TruncateThresholdBytes = int64(64 << 20)

// backoffBase/backoffMax bound the uploader's retry sleep after a failed
// upload. The reaper's grace (10 minutes) exceeds 2× this maximum, so a
// flapping control stream can never cause a live machine's command to be
// reaped for uploading lag. Variables so tests can shorten the waits.
var (
	backoffBase = 2 * time.Second
	backoffMax  = 1 * time.Minute
)

// UploadFunc is the transport the uploader drives: one UploadCommandData call.
// Tests replace it with a fake.
type UploadFunc func(ctx context.Context, entries []*Entry) (*v1pb.UploadCommandDataResponse, error)

// Uploader drains one agent's outbox to the manager. One uploader per agent
// keeps backoff fair (one agent's backlog never starves another agent's
// barrier) and lets a barrier flush-now reach exactly its own queue.
type Uploader struct {
	ob     *Outbox
	upload UploadFunc

	// flushNow interrupts the backoff sleep so a barrier (or a fresh
	// terminal) does not wait out a full backoff cycle after the manager
	// recovers (§3.2 flush-now).
	flushNow chan struct{}

	mu      sync.Mutex
	backoff time.Duration
	nextTry time.Time
	// readFrom is the upload cursor: the next WAL index to read. It advances
	// through the settled boundary only, so unsettled records are re-attempted
	// every cycle while acked ones are not re-sent. Restart resets it to the
	// log start — the manager's dedup makes the replay idempotent.
	readFrom uint64
	// settledSinceEvict accumulates settled-but-un-evicted bytes so the
	// mid-turn truncate backstop can fire on total WAL growth, not just one
	// batch.
	settledSinceEvict int64
}

// NewUploader wires an uploader over ob with the given transport.
func NewUploader(ob *Outbox, upload UploadFunc) *Uploader {
	return &Uploader{ob: ob, upload: upload, flushNow: make(chan struct{}, 1)}
}

// FlushNow requests an immediate upload cycle, interrupting any pending
// backoff sleep. Buffered: coalesced like a wake.
func (u *Uploader) FlushNow() {
	select {
	case u.flushNow <- struct{}{}:
	default:
	}
}

// WaitDrained is the turn-start barrier: it blocks until the outbox holds no
// records, so the next command's records can never mix with the previous
// command's still-unacked tail (§3.4). It sends a flush-now on entry so an
// uploader sleeping in backoff retries immediately, and waits on ctx so a
// dead manager (where the next command cannot proceed anyway) cannot wedge
// the drain loop. A poison terminal releases the barrier via the fold's
// group truncation.
func (u *Uploader) WaitDrained(ctx context.Context) error {
	u.FlushNow()
	for {
		empty, err := u.ob.Empty()
		if err != nil {
			return err
		}
		if empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-u.flushNow:
		case <-time.After(BatchWindow):
			// Re-check on the uploader's cadence; the uploader itself is
			// draining in parallel.
		}
	}
}

// Run drains the outbox until ctx is cancelled: on each cycle it flushes and
// uploads one batch, folds the response's acks/rejections into an eviction
// boundary, and enforces the retention cap. Failures back off exponentially;
// FlushNow interrupts the sleep.
func (u *Uploader) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		u.uploadCycle(ctx)
		wait := u.currentWait()
		select {
		case <-ctx.Done():
			return
		case <-u.flushNow:
		case <-time.After(wait):
		}
	}
}

// currentWait returns how long to sleep before the next cycle: the remaining
// backoff after a failure, otherwise the batch window.
func (u *Uploader) currentWait() time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.nextTry.After(time.Now()) {
		return time.Until(u.nextTry)
	}
	return BatchWindow
}

// recordFailure arms the exponential backoff for the next attempt. Failures
// between sleeps (e.g. the same ctx racing several cycles) do not double the
// backoff without a wait.
func (u *Uploader) recordFailure() {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	if !u.nextTry.IsZero() && u.nextTry.After(now) {
		return
	}
	if u.backoff == 0 {
		u.backoff = backoffBase
	} else {
		u.backoff *= 2
		if u.backoff > backoffMax {
			u.backoff = backoffMax
		}
	}
	u.nextTry = now.Add(u.backoff)
}

// recordSuccess resets the backoff after a successful upload.
func (u *Uploader) recordSuccess() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.backoff = 0
	u.nextTry = time.Time{}
}

// BypassUpload is the terminal-state side path (§3.1): when the WAL cannot
// persist and the turn must still report its failure, the synthetic terminal
// is sent directly, bypassing the log. Best-effort: an unreachable manager
// leaves the command to the reaper.
func (u *Uploader) BypassUpload(ctx context.Context, entry *Entry) {
	if entry == nil {
		return
	}
	if _, err := u.upload(ctx, []*Entry{entry}); err != nil {
		slog.Warn("terminal bypass upload failed; the manager's reaper owns the command",
			"dir", u.ob.Dir(), "commandID", entry.GetCommandId(), "error", err)
	}
}

// uploadCycle reads the cursor's batch, uploads it, and folds the response
// into eviction and cursor advancement. One call = at most one
// UploadCommandData request; failed cycles leave the cursor untouched so the
// same records are retried (idempotent on the manager).
func (u *Uploader) uploadCycle(ctx context.Context) {
	records, err := u.ob.ReadRecords(ReadBatchMaxEntries, UploadMaxBytes, u.readFromLocked())
	if err != nil {
		slog.Warn("outbox upload: read failed", "dir", u.ob.Dir(), "error", err)
		u.recordFailure()
		return
	}
	if len(records) == 0 {
		return
	}

	entries := make([]*Entry, 0, len(records))
	for _, r := range records {
		if r.Entry != nil {
			entries = append(entries, r.Entry)
		}
	}

	resp, err := u.upload(ctx, entries)
	if err != nil {
		slog.Warn("outbox upload failed; backing off", "dir", u.ob.Dir(), "entries", len(entries), "error", err)
		u.recordFailure()
		return
	}
	u.recordSuccess()
	u.fold(records, resp)
	u.ob.EnforceCap()
}

// readFromLocked snapshots the upload cursor.
func (u *Uploader) readFromLocked() uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.readFrom
}

// setReadFrom advances the upload cursor, never backwards.
func (u *Uploader) setReadFrom(index uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if index > u.readFrom {
		u.readFrom = index
	}
}

// addSettledBytes accumulates settled bytes and reports whether the mid-turn
// truncate threshold is now due (the caller resets it after evicting).
func (u *Uploader) addSettledBytes(n int64) (due bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.settledSinceEvict += n
	return u.settledSinceEvict >= TruncateThresholdBytes
}

// resetSettledBytes clears the amortization accumulator after a truncation.
func (u *Uploader) resetSettledBytes() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.settledSinceEvict = 0
}

// fold folds the batch response into the settled boundary F and applies the
// eviction policy: normally records are evicted only when the command's
// terminal is acked (one big TruncateFront per turn, amortized off the hot
// path); a settled prefix over TruncateThresholdBytes triggers the mid-turn
// truncation instead. The upload cursor always advances through F, so settled
// records are uploaded once while unsettled ones are re-attempted next cycle.
// A rejected terminal record poisons the whole command: every record of the
// command is treated as settled and truncated as a group, releasing the
// barrier (the manager's reaper owns the command; §3.4).
func (u *Uploader) fold(records []Record, resp *v1pb.UploadCommandDataResponse) {
	acks := make(map[string]*v1pb.UploadCommandDataAck, len(resp.GetAcks()))
	for _, a := range resp.GetAcks() {
		acks[a.GetCommandId()] = a
	}
	rejected := make(map[string]string, len(resp.GetRejected()))
	terminalRejected := make(map[string]bool)
	for _, r := range resp.GetRejected() {
		rejected[rejectionKey(r.GetCommandId(), r.GetKind(), r.GetSeqNo())] = r.GetReason()
		if r.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			terminalRejected[r.GetCommandId()] = true
		}
	}

	boundary := uint64(0) // highest index whose whole prefix is settled
	var settledBytes int64
	terminalSettled := false
	terminalRejectedSeen := false
	for _, r := range records {
		if !recordSettled(r, acks, rejected, terminalRejected) {
			break // boundary F stops here: everything after stays queued
		}
		boundary = r.Index
		if r.Entry == nil {
			continue
		}
		settledBytes += int64(len(marshalEnvelope(r.Entry)))
		if r.Entry.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			// The whole command group (the log, under the barrier) is
			// persisted: full eviction is safe.
			terminalSettled = true
		}
	}
	if len(terminalRejected) > 0 {
		// A rejected terminal folds the command's records as a group; the
		// settled prefix up to the boundary carries that group out of the log.
		terminalRejectedSeen = true
	}

	if boundary > 0 {
		// TruncateFront rewrites segment files, so it is amortized: only on a
		// settled terminal (whole log), a rejected terminal (group drop), or
		// the large-prefix backstop.
		due := terminalSettled || terminalRejectedSeen || u.addSettledBytes(settledBytes)
		if due {
			if err := u.ob.EvictThrough(boundary); err != nil {
				slog.Warn("outbox eviction failed", "dir", u.ob.Dir(), "error", err)
			} else {
				u.resetSettledBytes()
			}
		}
	}
	if boundary > 0 {
		u.setReadFrom(boundary + 1)
	}
}

// recordSettled reports whether one uploaded record is fully settled: acked by
// the per-kind watermark, explicitly rejected (never retransmit), a poison
// (torn) record, or any record of a command whose terminal was rejected
// (group truncation).
func recordSettled(
	r Record,
	acks map[string]*v1pb.UploadCommandDataAck,
	rejected map[string]string,
	terminalRejected map[string]bool,
) bool {
	if r.Entry == nil {
		return true
	}
	e := r.Entry
	if terminalRejected[e.GetCommandId()] {
		return true
	}
	if _, ok := rejected[rejectionKey(e.GetCommandId(), e.GetKind(), e.GetSeqNo())]; ok {
		return true
	}
	a := acks[e.GetCommandId()]
	if a == nil {
		return false
	}
	switch e.GetKind() {
	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
		return e.GetSeqNo() <= a.GetLastProgressSeq()
	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT:
		return e.GetSeqNo() <= a.GetLastEventSeq()
	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
		return a.GetResultAcked()
	default:
		return false
	}
}

// rejectionKey identifies one rejected entry.
func rejectionKey(commandID string, kind v1pb.UploadEntryKind, seq int32) string {
	return commandID + "|" + strconv.Itoa(int(kind)) + "|" + strconv.Itoa(int(seq))
}
