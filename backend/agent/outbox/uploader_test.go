package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// fakeUploader is a scripted UploadCommandData transport.
type fakeUploader struct {
	mu       sync.Mutex
	calls    [][]*Entry
	respond  func(call int, entries []*Entry) (*v1pb.UploadCommandDataResponse, error)
	failNext int
}

func (f *fakeUploader) upload(_ context.Context, entries []*Entry) (*v1pb.UploadCommandDataResponse, error) {
	f.mu.Lock()
	call := len(f.calls)
	f.calls = append(f.calls, entries)
	fail := f.failNext > 0
	if fail {
		f.failNext--
	}
	respond := f.respond
	f.mu.Unlock()
	if fail {
		return nil, errors.New("manager unreachable")
	}
	if respond != nil {
		return respond(call, entries)
	}
	return fullAck(entries), nil
}

func (f *fakeUploader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fullAck accepts everything: per-kind watermarks equal the batch's maxima and
// every result acked.
func fullAck(entries []*Entry) *v1pb.UploadCommandDataResponse {
	resp := &v1pb.UploadCommandDataResponse{}
	type ack struct {
		progress, event int32
		result          bool
	}
	byCmd := map[string]*ack{}
	for _, e := range entries {
		a := byCmd[e.GetCommandId()]
		if a == nil {
			a = &ack{}
			byCmd[e.GetCommandId()] = a
		}
		switch e.GetKind() {
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
			a.progress = e.GetSeqNo()
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT:
			a.event = e.GetSeqNo()
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
			a.result = true
		default:
		}
	}
	for id, a := range byCmd {
		resp.Acks = append(resp.Acks, &v1pb.UploadCommandDataAck{
			CommandId:       id,
			LastProgressSeq: a.progress,
			LastEventSeq:    a.event,
			ResultAcked:     a.result,
		})
	}
	return resp
}

// fastUploaderTunables shrinks every timing for hermetic tests.
func fastUploaderTunables(t *testing.T) {
	t.Helper()
	oldWindow, oldMaxBytes, oldBase, oldMax := BatchWindow, UploadMaxBytes, backoffBase, backoffMax
	BatchWindow = 5 * time.Millisecond
	UploadMaxBytes = 1 << 20
	backoffBase = 10 * time.Millisecond
	backoffMax = 50 * time.Millisecond
	t.Cleanup(func() {
		BatchWindow, UploadMaxBytes, backoffBase, backoffMax = oldWindow, oldMaxBytes, oldBase, oldMax
	})
}

// TestUploaderAcksEvictRecords locks the primary drain loop: records upload,
// the per-kind watermarks fold into eviction, and the log empties; the
// barrier releases exactly when the acked terminal evicts the group.
func TestUploaderAcksEvictRecords(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	for i := int32(1); i <= 4; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := o.Append(resultEntry("cmd-1", 0)); err != nil {
		t.Fatalf("append result: %v", err)
	}

	fake := &fakeUploader{}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	if err := u.WaitDrained(context.Background()); err != nil {
		t.Fatalf("barrier: %v", err)
	}
	if len(fake.calls) == 0 {
		t.Fatal("uploader must have called the transport")
	}
	if len(fake.calls[0]) != 5 {
		t.Fatalf("first batch should carry all 5 entries, got %d", len(fake.calls[0]))
	}
	if empty, _ := o.Empty(); !empty {
		t.Fatal("log must be empty after the acked terminal evicts the group")
	}
}

// TestUploaderRetainsUntilTerminalAcked locks the eviction amortization:
// without a terminal ack the settled prefix stays in the log (one big
// truncate per turn), so the barrier does not release.
func TestUploaderRetainsUntilTerminalAcked(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	for i := int32(1); i <= 3; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// The transport acks progress but never a terminal.
	fake := &fakeUploader{respond: func(_ int, entries []*Entry) (*v1pb.UploadCommandDataResponse, error) {
		resp := &v1pb.UploadCommandDataResponse{}
		for _, e := range entries {
			if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS {
				resp.Acks = append(resp.Acks, &v1pb.UploadCommandDataAck{
					CommandId:       e.GetCommandId(),
					LastProgressSeq: e.GetSeqNo(),
				})
			}
		}
		return resp, nil
	}}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	if err := waitFor(2*time.Second, func() bool { return fake.callCount() > 0 }); err != nil {
		t.Fatalf("no upload cycle: %v", err)
	}
	if empty, _ := o.Empty(); empty {
		t.Fatal("records must stay queued while the terminal is unacked")
	}
}

// TestUploaderFlushNowInterruptsBackoff locks decision ⑨: a flush-now wakes
// the uploader out of its backoff sleep immediately, so a barrier does not
// wait out a full backoff cycle after the manager recovers.
func TestUploaderFlushNowInterruptsBackoff(t *testing.T) {
	fastUploaderTunables(t)
	oldMax := backoffMax
	backoffMax = 10 * time.Second // a long backoff the flush-now must cut short
	t.Cleanup(func() { backoffMax = oldMax })

	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Append(resultEntry("cmd-1", 0)); err != nil {
		t.Fatalf("append result: %v", err)
	}

	// The transport fails once, then succeeds.
	fake := &fakeUploader{failNext: 1}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	if err := waitFor(2*time.Second, func() bool { return fake.callCount() == 1 }); err != nil {
		t.Fatalf("first cycle missing: %v", err)
	}
	u.FlushNow()
	if err := waitFor(500*time.Millisecond, func() bool { return fake.callCount() == 2 }); err != nil {
		t.Fatalf("flush-now did not interrupt the backoff: %v", err)
	}
	if empty, _ := o.Empty(); !empty {
		t.Fatal("log must drain after the successful retry")
	}
}

// TestUploaderShardsByRequestBytes locks the request cap: a backlog larger
// than UploadMaxBytes uploads in sequential batches, each within the cap.
func TestUploaderShardsByRequestBytes(t *testing.T) {
	fastUploaderTunables(t)
	oldMax := UploadMaxBytes
	UploadMaxBytes = 1024 // small shards for the test
	t.Cleanup(func() { UploadMaxBytes = oldMax })

	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	payload := string(make([]byte, 300))
	for i := 1; i <= 10; i++ {
		if err := o.Append(progressEntry("cmd-1", int32(i), payload)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := o.Append(resultEntry("cmd-1", 0)); err != nil {
		t.Fatalf("append result: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	fake := &fakeUploader{}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)
	if err := waitFor(3*time.Second, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		total := 0
		for _, c := range fake.calls {
			total += len(c)
		}
		return total >= 10
	}); err != nil {
		t.Fatalf("backlog did not drain in shards: %v", err)
	}
	// Every request stayed within the cap.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for i, call := range fake.calls {
		var total int64
		for _, e := range call {
			total += int64(len(marshalEnvelope(e)))
		}
		if total > UploadMaxBytes {
			t.Fatalf("shard %d exceeded the request cap: %d > %d", i, total, UploadMaxBytes)
		}
	}
}

// TestUploaderPoisonTerminalDropsGroupAndReleasesBarrier locks §3.4's
// deadlock guard: a rejected terminal folds the command's whole record group
// as settled, so the barrier releases instead of wedging on a poison record.
func TestUploaderPoisonTerminalDropsGroupAndReleasesBarrier(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	for i := int32(1); i <= 2; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := o.Append(resultEntry("cmd-1", -1)); err != nil {
		t.Fatalf("append result: %v", err)
	}

	// The transport rejects the terminal record.
	fake := &fakeUploader{respond: func(_ int, entries []*Entry) (*v1pb.UploadCommandDataResponse, error) {
		resp := &v1pb.UploadCommandDataResponse{}
		for _, e := range entries {
			if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
				resp.Rejected = append(resp.Rejected, &v1pb.UploadCommandDataRejection{
					CommandId: e.GetCommandId(),
					Kind:      e.GetKind(),
					SeqNo:     e.GetSeqNo(),
					Reason:    "result payload missing",
				})
			}
		}
		return resp, nil
	}}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	// The barrier must release even though no ack ever covers the records.
	if err := u.WaitDrained(context.Background()); err != nil {
		t.Fatalf("barrier must release after poison terminal: %v", err)
	}
	if empty, _ := o.Empty(); !empty {
		t.Fatal("log must be truncated as a group after the poison terminal")
	}
}

// TestUploaderRetransmitsUntilAcked locks at-least-once: an upload whose
// transport errors leaves records queued and retransmits them; the manager's
// dedup makes that idempotent.
func TestUploaderRetransmitsUntilAcked(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Append(resultEntry("cmd-1", 0)); err != nil {
		t.Fatalf("append result: %v", err)
	}

	// Fail twice, then succeed.
	fake := &fakeUploader{failNext: 2}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	if err := u.WaitDrained(context.Background()); err != nil {
		t.Fatalf("barrier: %v", err)
	}
	if fake.callCount() < 3 {
		t.Fatalf("expected failed attempts before the success, saw %d calls", fake.callCount())
	}
	if empty, _ := o.Empty(); !empty {
		t.Fatal("log must empty after the successful retransmission")
	}
}

// TestUploaderBypassUpload locks the terminal side path: the bypass sends the
// entry through the transport without touching the log, and a failure is
// tolerated (best-effort).
func TestUploaderBypassUpload(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	fake := &fakeUploader{}
	u := NewUploader(o, fake.upload)
	if err := u.BypassUpload(context.Background(), resultEntry("cmd-9", -1)); err != nil {
		t.Fatalf("bypass: %v", err)
	}
	if fake.callCount() != 1 {
		t.Fatalf("bypass must send the terminal directly, saw %d calls", fake.callCount())
	}

	// A failing transport must not panic: the bypass is best-effort.
	failing := &fakeUploader{failNext: 1}
	u2 := NewUploader(o, failing.upload)
	if err := u2.BypassUpload(context.Background(), resultEntry("cmd-9", -1)); err == nil {
		t.Fatal("a failing bypass must report its error")
	}
}

// TestWaitDrainedHonorsContext locks that the barrier cannot wedge past its
// context: a dead manager (records never drain) releases with ctx error.
func TestWaitDrainedHonorsContext(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}

	// A transport that never succeeds: the barrier must honor ctx.
	fake := &fakeUploader{failNext: 1 << 30}
	u := NewUploader(o, fake.upload)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := u.WaitDrained(ctx); err == nil {
		t.Fatal("barrier must fail with ctx error when the manager never drains the queue")
	}
}

// TestWaitClearForAllowsOwnCommandRecords locks the command-aware barrier: the
// barrier only blocks on OTHER commands' records, so a resumed turn proceeds
// while its own interrupted tail is still queued.
func TestWaitClearForAllowsOwnCommand(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}

	// A transport that never succeeds: nothing drains.
	fake := &fakeUploader{failNext: 1 << 30}
	u := NewUploader(o, fake.upload)

	// Own-command records must not block (the resume path).
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := u.WaitClearFor(ctx, "cmd-1"); err != nil {
		t.Fatalf("own-command records must not block the barrier: %v", err)
	}

	// Another command's records block (released on drain).
	if err := o.Append(progressEntry("cmd-2", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	if err := u.WaitClearFor(ctx2, "cmd-1"); err == nil {
		t.Fatal("other-command records must block the barrier")
	}
}

// TestWaitClearForSynthesizesOrphanTerminal locks the orphan guard: a group
// whose turn died before reporting can never drain on its own (result_acked
// never arrives), so the barrier synthesizes the FAILED terminal and the group
// drains, releasing the barrier.
func TestWaitClearForSynthesizesOrphanTerminal(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	if err := o.Append(progressEntry("cmd-1", 3, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Append(eventEntry("cmd-1", 1)); err != nil {
		t.Fatalf("append: %v", err)
	}

	fake := &fakeUploader{}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	if err := u.WaitClearFor(context.Background(), "cmd-2"); err != nil {
		t.Fatalf("barrier must release after the orphan terminal synthesizes: %v", err)
	}
	if empty, _ := o.Empty(); !empty {
		t.Fatal("the orphan group must drain after the synthesized terminal")
	}

	// The synthesized terminal is a FAILED result covering the stored progress.
	var synth *v1pb.CommandResult
	for _, call := range fake.calls {
		for _, e := range call {
			if e.GetCommandId() == "cmd-1" && e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
				synth = e.GetResult()
			}
		}
	}
	if synth == nil {
		t.Fatal("barrier must append the synthetic terminal for the orphan group")
	}
	if synth.GetExitCode() != -1 {
		t.Fatalf("synthetic terminal must be a failure, got exit %d", synth.GetExitCode())
	}
	if synth.GetLastSeqNo() != 3 {
		t.Fatalf("synthetic terminal must cover the group's progress watermark, got %d", synth.GetLastSeqNo())
	}
	if synth.GetErrorMessage() == "" {
		t.Fatal("synthetic terminal must carry an explanatory error message")
	}
}

// TestWaitClearForSkipsOrphanForOwnCommand locks that the barrier never
// synthesizes a terminal for the command about to run: the resumed turn's own
// tail must stay intact for the continuation.
func TestWaitClearForSkipsOrphanForOwnCommand(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	if err := o.Append(progressEntry("cmd-1", 2, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}

	fake := &fakeUploader{failNext: 1 << 30}
	u := NewUploader(o, fake.upload)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := u.WaitClearFor(ctx, "cmd-1"); err != nil {
		t.Fatalf("own unterminated tail must pass the barrier: %v", err)
	}
	for _, call := range fake.calls {
		for _, e := range call {
			if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
				t.Fatal("the barrier must not synthesize a terminal for the command about to run")
			}
		}
	}
}

// runUploader starts the uploader's Run loop and stops it (waiting for exit)
// during cleanup, before fastUploaderTunables restores the globals.
func runUploader(t *testing.T, u *Uploader) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		u.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(timeout time.Duration, cond func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return errors.Wrapf(nil, "condition not met within %v", timeout)
}

// filepathJoinTemp opens a WAL under the test's temp dir.
func filepathJoinTemp(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/outbox"
}
