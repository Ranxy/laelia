package outbox

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// max32 returns the larger of two int32 values.
func max32(a, b int32) int32 {
	if b > a {
		return b
	}
	return a
}

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
// backoff sleep. Buffered: coalesced like a wake. Counts on §8.2's flush-now
// metric (with the barrier wait distribution it evidences flush-now's effect).
func (u *Uploader) FlushNow() {
	uploadFlushNowTotal.WithLabelValues(agentLabel(u.ob.Dir())).Inc()
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
	start := time.Now()
	defer func() {
		barrierWaitSeconds.WithLabelValues(agentLabel(u.ob.Dir())).Observe(float64(time.Since(start).Milliseconds()))
	}()
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

// orphanGroup is one command whose records block the turn-start barrier and
// whose turn died before reporting a terminal.
type orphanGroup struct {
	commandID       string
	lastProgressSeq int32
}

// WaitClearFor is the turn-start barrier (§3.4): it blocks until the outbox
// holds no records for any command other than commandID. A turn's records must
// not share the log with another command's un-acked group — that is what lets
// one acked terminal evict the whole log and keeps a rejected terminal's group
// truncation from discarding another command's data. Records for commandID
// itself may remain: a resumed turn continues its own interrupted tail.
//
// A blocking group without a terminal record is an orphan: its turn died
// before reporting (machine crash, or a WAL fault that routed the terminal
// through the bypass). result_acked would never arrive, so the barrier appends
// the synthetic FAILED terminal and lets the group drain like any other. The
// barrier only runs between turns (the drain loop is serial), so every
// other-command group it sees belongs to a dead turn.
//
// The wait sends flush-now so an uploader sleeping in backoff retries
// immediately, polls at the batch-window cadence, and watches ctx so a dead
// connection releases the barrier (the caller aborts the turn).
func (u *Uploader) WaitClearFor(ctx context.Context, commandID string) error {
	start := time.Now()
	defer func() {
		barrierWaitSeconds.WithLabelValues(agentLabel(u.ob.Dir())).Observe(float64(time.Since(start).Milliseconds()))
	}()
	for {
		blocked, orphans, err := u.blockingFor(commandID)
		if err != nil {
			return err
		}
		if !blocked {
			return nil
		}
		for _, o := range orphans {
			slog.Warn("outbox holds an unterminated command group; synthesizing the terminal",
				"dir", u.ob.Dir(), "commandID", o.commandID)
			if aErr := u.ob.Append(syntheticTerminal(o, agentStoppedMessage)); aErr != nil {
				return aErr
			}
		}
		u.FlushNow()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-u.flushNow:
		case <-time.After(BatchWindow):
			// Re-check on the uploader's cadence; the uploader drains in
			// parallel.
		}
	}
}

// blockingFor scans the log from its start and reports whether any record
// belongs to a command other than commandID, plus the orphan groups among
// them (no terminal record in the whole log). One shard suffices: the barrier
// runs between turns, so the log holds at most one other command's records —
// WAL order is append order, so they are always in the first shard when
// present. A terminal beyond the shard makes the orphan detection false-
// positive; the synthesized duplicate is a harmless no-op (the manager's
// result_acked is idempotent).
func (u *Uploader) blockingFor(commandID string) (blocked bool, orphans []orphanGroup, err error) {
	records, err := u.ob.ReadRecords(ReadBatchMaxEntries, UploadMaxBytes, 0)
	if err != nil {
		return false, nil, err
	}
	type group struct {
		hasTerminal bool
		lastProgSeq int32
	}
	others := make(map[string]*group)
	for _, r := range records {
		e := r.Entry
		if e == nil || e.GetCommandId() == commandID {
			continue
		}
		blocked = true
		g := others[e.GetCommandId()]
		if g == nil {
			g = &group{}
			others[e.GetCommandId()] = g
		}
		switch e.GetKind() {
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
			g.hasTerminal = true
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
			g.lastProgSeq = max32(g.lastProgSeq, e.GetSeqNo())
		default:
		}
	}
	for id, g := range others {
		if !g.hasTerminal {
			orphans = append(orphans, orphanGroup{commandID: id, lastProgressSeq: g.lastProgSeq})
		}
	}
	slices.SortFunc(orphans, func(a, b orphanGroup) int {
		return strings.Compare(a.commandID, b.commandID)
	})
	return blocked, orphans, nil
}

// agentStoppedMessage is the synthetic terminal the turn-start barrier appends
// when it finds an orphan group mid-run.
const agentStoppedMessage = "agent stopped before the turn reported a result"

// syntheticTerminal builds the FAILED result a barrier appends for an orphan
// group (seq 1 in the unused result space; LastSeqNo covers the group's stored
// progress so the manager's ack cursor stays complete). The message names the
// cause so the audit trail distinguishes a barrier synthesis from the shutdown
// / restart syntheses.
func syntheticTerminal(o orphanGroup, message string) *Entry {
	return &Entry{
		CommandId: o.commandID,
		Kind:      v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT,
		SeqNo:     1,
		Payload: &v1pb.UploadCommandDataEntry_Result{
			Result: &v1pb.CommandResult{
				CommandId:    o.commandID,
				ExitCode:     -1,
				LastSeqNo:    o.lastProgressSeq,
				ErrorMessage: message,
			},
		},
	}
}

// SynthesizeInterruptedTerminals is the §3.7 self-check: it scans the whole
// log, and for every command whose records lack a terminal appends the synthetic
// FAILED terminal with message as the cause, then flushes so the terminal is
// durable before the caller continues. Wired at machine startup (crash backstop,
// "machine restarted mid-turn") and on runner teardown (a turn that outlived
// its bounded cancel). Idempotent: groups with a terminal record are skipped.
func (u *Uploader) SynthesizeInterruptedTerminals(message string) error {
	orphans, err := u.orphanGroups()
	if err != nil {
		return err
	}
	for _, o := range orphans {
		slog.Warn("outbox holds an interrupted command group; synthesizing the terminal",
			"dir", u.ob.Dir(), "commandID", o.commandID, "message", message)
		if err := u.ob.Append(syntheticTerminal(o, message)); err != nil {
			return err
		}
	}
	return u.ob.Flush()
}

// orphanGroups reads the whole log and returns one orphanGroup per command
// that has records but no terminal record anywhere in the log. Unlike the
// barrier's single-shard scan (which runs per turn and may false-positive
// past the shard), this is the full scan used where the log is already known
// to be writer-free (startup, after the runner's loops exited).
func (u *Uploader) orphanGroups() ([]orphanGroup, error) {
	type group struct {
		hasTerminal bool
		lastProgSeq int32
	}
	groups := make(map[string]*group)
	var from uint64
	for {
		records, err := u.ob.ReadRecords(ReadBatchMaxEntries, UploadMaxBytes, from)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			break
		}
		for _, r := range records {
			e := r.Entry
			if e == nil {
				continue
			}
			g := groups[e.GetCommandId()]
			if g == nil {
				g = &group{}
				groups[e.GetCommandId()] = g
			}
			switch e.GetKind() {
			case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
				g.hasTerminal = true
			case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
				g.lastProgSeq = max32(g.lastProgSeq, e.GetSeqNo())
			default:
			}
		}
		from = records[len(records)-1].Index + 1
	}
	var orphans []orphanGroup
	for id, g := range groups {
		if !g.hasTerminal {
			orphans = append(orphans, orphanGroup{commandID: id, lastProgressSeq: g.lastProgSeq})
		}
	}
	slices.SortFunc(orphans, func(a, b orphanGroup) int {
		return strings.Compare(a.commandID, b.commandID)
	})
	return orphans, nil
}

// Drain uploads until the log is empty or ctx is done. It is the graceful
// shutdown path (§3.7): the synthetic terminals are already durable in the
// WAL, so a drain that fails (manager unreachable) only delays their delivery
// to the next startup's replay — the drain is the best-effort "healthy manager
// gets the terminal now" bonus.
func (u *Uploader) Drain(ctx context.Context) {
	for {
		empty, err := u.ob.Empty()
		if err != nil || empty {
			return
		}
		u.uploadCycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(BatchWindow):
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
// is sent directly, bypassing the log. The error reports delivery: a failed
// bypass leaves the command to the manager's reaper.
func (u *Uploader) BypassUpload(ctx context.Context, entry *Entry) error {
	if entry == nil {
		return nil
	}
	if _, err := u.upload(ctx, []*Entry{entry}); err != nil {
		slog.Warn("terminal bypass upload failed; the manager's reaper owns the command",
			"dir", u.ob.Dir(), "commandID", entry.GetCommandId(), "error", err)
		return err
	}
	return nil
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
		u.updateGauges(0)
		return
	}

	entries := make([]*Entry, 0, len(records))
	poison := 0
	for _, r := range records {
		if r.Entry != nil {
			entries = append(entries, r.Entry)
		} else {
			poison++
		}
	}

	label := agentLabel(u.ob.Dir())
	if poison > 0 {
		uploadPoisonTotal.WithLabelValues(label).Add(float64(poison))
	}
	uploadBatchSize.WithLabelValues(label).Observe(float64(len(entries)))
	start := time.Now()
	resp, err := u.upload(ctx, entries)
	uploadRTT.WithLabelValues(label).Observe(time.Since(start).Seconds())
	if err != nil {
		slog.Warn("outbox upload failed; backing off", "dir", u.ob.Dir(), "entries", len(entries), "error", err)
		u.recordFailure()
		return
	}
	u.recordSuccess()
	// Rejected entries are poison: the manager refused them and the fold below
	// drops them for good (never retransmitted), so leave a local trace. A
	// silent drop here is how a whole stream kind can vanish with no signal.
	for _, r := range resp.GetRejected() {
		slog.Warn("outbox upload: entry rejected by the manager; dropping",
			"dir", u.ob.Dir(), "commandID", r.GetCommandId(),
			"kind", r.GetKind().String(), "seq", r.GetSeqNo(), "reason", r.GetReason())
	}
	u.fold(records, resp)
	u.updateGauges(u.readFromLocked())
	u.ob.EnforceCap()
}

// updateGauges refreshes the per-agent outbox gauges (§8.2): lag is the record
// count still queued past the settled boundary (LastIndex − F; 0 for an empty
// log — boundary 0 here means "nothing settled in this range", the cursor
// itself is the settled frontier), bytes the WAL's segment footprint.
func (u *Uploader) updateGauges(boundary uint64) {
	label := agentLabel(u.ob.Dir())
	first, last, err := u.ob.rangeForMetrics()
	if err != nil {
		return
	}
	var lag int64
	if last >= first {
		from := boundary
		if from < first {
			from = first
		}
		if from <= last {
			lag = int64(last - from + 1)
		}
	}
	outboxLagRecords.WithLabelValues(label).Set(float64(lag))
	if size, sErr := dirSize(u.ob.dir); sErr == nil {
		outboxBytes.WithLabelValues(label).Set(float64(size))
	}
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
