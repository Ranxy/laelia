package outbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tidwall/wal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// progressEntry builds a progress envelope for tests.
func progressEntry(commandID string, seq int32, content string) *Entry {
	return &Entry{
		CommandId:          commandID,
		Kind:               v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS,
		SeqNo:              seq,
		AgentSideTimestamp: timestamppb.Now(),
		Payload: &v1pb.UploadCommandDataEntry_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				SeqNo:     seq,
				Type:      v1pb.CommandOutput_STDOUT,
				Content:   content,
			},
		},
	}
}

// resultEntry builds a terminal envelope for tests.
//
// nolint:unused // used by uploader tests
func resultEntry(commandID string, seq int32, exitCode int32) *Entry {
	return &Entry{
		CommandId: commandID,
		Kind:      v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT,
		SeqNo:     seq,
		Payload: &v1pb.UploadCommandDataEntry_Result{
			Result: &v1pb.CommandResult{
				CommandId: commandID,
				ExitCode:  exitCode,
			},
		},
	}
}

// TestAppendReadRoundTrip locks the core durability property: records appended
// (through the buffered write path and explicit flushes) survive in order and
// unmarshal byte-identical.
func TestAppendReadRoundTrip(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	for i := int32(1); i <= 5; i++ {
		if err := o.Append(progressEntry("cmd-1", i, fmt.Sprintf("chunk %d", i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	empty, err := o.Empty()
	if err != nil || empty {
		t.Fatalf("outbox should not be empty (empty=%v err=%v)", empty, err)
	}

	records, err := o.ReadRecords(ReadBatchMaxEntries, 1<<20)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("want 5 records, got %d", len(records))
	}
	for i, r := range records {
		if r.Index != uint64(i+1) {
			t.Fatalf("record %d has index %d, want %d", i, r.Index, i+1)
		}
		if got := r.Entry.GetProgress().GetContent(); got != fmt.Sprintf("chunk %d", i+1) {
			t.Fatalf("record %d content = %q", i, got)
		}
	}
}

// TestAllowEmptyEvictionRoundTrip locks the eviction contract: after evicting
// everything the log is empty AND still reopens readable (AllowEmpty), and the
// index continues without gaps.
func TestAllowEmptyEvictionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	o, err := Open(filepath.Join(dir, "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := int32(1); i <= 3; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := o.EvictThrough(3); err != nil {
		t.Fatalf("evict: %v", err)
	}
	empty, err := o.Empty()
	if err != nil || !empty {
		t.Fatalf("log must be empty after full eviction (empty=%v err=%v)", empty, err)
	}
	// Continue appending: the no-gap invariant must hold after truncation.
	for i := int32(4); i <= 6; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "y")); err != nil {
			t.Fatalf("append after evict: %v", err)
		}
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush after evict: %v", err)
	}
	records, err := o.ReadRecords(10, 1<<20)
	if err != nil {
		t.Fatalf("read after evict: %v", err)
	}
	if len(records) != 3 || records[0].Entry.GetProgress().GetSeqNo() != 4 {
		t.Fatalf("want records 4..6, got %v", records)
	}
	// Reopen (Close+Open): an emptied-then-refilled log must load.
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	o2, err := Open(filepath.Join(dir, "outbox"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	records, err = o2.ReadRecords(10, 1<<20)
	if err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("want 3 records after reopen, got %d", len(records))
	}
	if err := o2.Close(); err != nil {
		t.Fatalf("close reopened outbox: %v", err)
	}
}

// TestPartialEvictionBoundary locks the fold boundary semantics: evicting
// through index F removes records ≤ F and keeps the rest readable in order.
func TestPartialEvictionBoundary(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	for i := int32(1); i <= 6; i++ {
		if err := o.Append(progressEntry("cmd-1", i, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := o.EvictThrough(4); err != nil {
		t.Fatalf("evict: %v", err)
	}
	records, err := o.ReadRecords(10, 1<<20)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 2 || records[0].Index != 5 {
		t.Fatalf("want records 5..6, got %v", records)
	}
}

// TestWALErrorIsolatesAndRecovers locks the §3.1 rule: any WAL write error
// must isolate the instance (no further appends on the poisoned instance) and
// the buffered records must land on a consistent log once the fault clears.
// The failure is simulated by forcing a segment cycle (tiny segment size)
// while the log directory is read-only, so the new segment cannot be created.
func TestWALErrorIsolatesAndRecovers(t *testing.T) {
	oldSegmentSize := segmentSize
	segmentSize = 64 // force a cycle on the second batch
	t.Cleanup(func() { segmentSize = oldSegmentSize })

	dir := t.TempDir()
	o, err := Open(filepath.Join(dir, "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	if err := o.Append(progressEntry("cmd-1", 1, "before")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// A read-only WAL directory makes the next segment cycle fail.
	if err := os.Chmod(o.Dir(), 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() {
		// The failed instance may have quarantined the directory aside; make
		// it removable again.
		_ = os.Chmod(o.Dir(), 0o700)
		quarantined, _ := filepath.Glob(filepath.Join(dir, "outbox.quarantine-*"))
		for _, q := range quarantined {
			_ = os.Chmod(q, 0o700)
		}
	}()

	if err := o.Append(progressEntry("cmd-1", 2, "fails")); err != nil {
		t.Fatalf("append buffered the record: %v", err)
	}
	if err := o.Flush(); err == nil {
		t.Fatal("flush must fail while the log directory is unwritable")
	}

	// Restore writability: recovery must succeed and the previously buffered
	// record must land on the log.
	if err := os.Chmod(o.Dir(), 0o700); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush after recovery: %v", err)
	}
	records, err := o.ReadRecords(10, 1<<20)
	if err != nil {
		t.Fatalf("read after recovery: %v", err)
	}
	// A second failure quarantines the directory aside and starts empty, so
	// records that were already on disk may be lost with it; what must hold is
	// that the buffered record lands on the recovered log and every read is
	// consistent.
	var order []string
	for _, r := range records {
		order = append(order, r.Entry.GetProgress().GetContent())
	}
	if len(order) < 1 || order[len(order)-1] != "fails" {
		t.Fatalf("buffered record lost across WAL error: %v", order)
	}
}

// TestCorruptLogQuarantines locks the corruption handling: a wholly unreadable
// log directory is quarantined aside while a fresh log continues.
func TestCorruptLogQuarantines(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "outbox")
	o, err := Open(walDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Corrupt the first segment's frame header: an impossible uvarint size.
	segment := filepath.Join(walDir, "00000000000000000001")
	broken, err := os.ReadFile(segment)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	broken[0] = 0xff
	broken[1] = 0xff
	broken[2] = 0xff
	if err := os.WriteFile(segment, broken, 0o600); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	if _, err := Open(walDir); err != nil {
		t.Fatalf("open corrupt log must recover via retry/quarantine, got %v", err)
	}
	// The quarantined directory must exist next to the fresh one.
	quarantined := false
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "outbox.quarantine-") {
			quarantined = true
		}
	}
	if !quarantined {
		t.Fatal("corrupt log must be quarantined aside")
	}
	// The fresh log must be usable.
	fresh, err := Open(walDir)
	if err != nil {
		t.Fatalf("fresh open: %v", err)
	}
	defer fresh.Close()
	if err := fresh.Append(progressEntry("cmd-2", 1, "fresh")); err != nil {
		t.Fatalf("append to fresh log: %v", err)
	}
	if err := fresh.Flush(); err != nil {
		t.Fatalf("flush fresh log: %v", err)
	}
}

// TestPoisonRecordReportedAndEvictable locks the read-path handling of an
// unparseable record inside a readable log: it surfaces as a nil-entry poison
// record that counts as settled, so eviction can move past it instead of
// stalling the queue.
func TestPoisonRecordReportedAndEvictable(t *testing.T) {
	dir := t.TempDir()
	walDir := filepath.Join(dir, "outbox")
	o, err := Open(walDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := o.Append(progressEntry("cmd-1", 1, "ok")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Inject a record whose bytes are not a valid proto message, directly via
	// the library, so the next read must report it as poison.
	_, last, err := readWALLastIndex(t, walDir)
	if err != nil {
		t.Fatalf("last index: %v", err)
	}
	raw, err := wal.Open(walDir, &wal.Options{AllowEmpty: true})
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if err := raw.Write(last+1, []byte{0xde, 0xad, 0xbe}); err != nil {
		t.Fatalf("raw write: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	o2, err := Open(walDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer o2.Close()
	records, err := o2.ReadRecords(10, 1<<20)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 2 || records[1].Entry != nil {
		t.Fatalf("poison record must surface with nil entry, got %+v", records)
	}
	if records[0].Entry.GetProgress().GetContent() != "ok" {
		t.Fatalf("healthy record must read intact, got %+v", records[0].Entry)
	}
	// Eviction through the poison boundary must not stall.
	if err := o2.EvictThrough(records[1].Index); err != nil {
		t.Fatalf("evict through poison: %v", err)
	}
	if empty, _ := o2.Empty(); !empty {
		t.Fatal("log must be empty after evicting through the poison record")
	}
}

// readWALLastIndex opens the WAL directly and reports its last index.
func readWALLastIndex(t *testing.T, dir string) (first, last uint64, err error) {
	t.Helper()
	log, err := wal.Open(dir, &wal.Options{AllowEmpty: true})
	if err != nil {
		return 0, 0, err
	}
	defer log.Close()
	first, err = log.FirstIndex()
	if err != nil {
		return 0, 0, err
	}
	last, err = log.LastIndex()
	return first, last, err
}

// TestEnforceCapDropsWholeLog locks the retention backstop: a WAL over the cap
// is dropped whole (one command's group under the barrier invariant).
func TestEnforceCapDropsWholeLog(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	if err := o.Append(progressEntry("cmd-1", 1, "keep-me-not")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// Simulate an oversized log with a filler file inside the WAL directory.
	filler := filepath.Join(o.Dir(), "filler.bin")
	if err := os.WriteFile(filler, make([]byte, MaxBytes+1), 0o600); err != nil {
		t.Fatalf("write filler: %v", err)
	}
	o.EnforceCap()
	empty, err := o.Empty()
	if err != nil || !empty {
		t.Fatalf("log must be dropped when over cap (empty=%v err=%v)", empty, err)
	}
}

// TestConcurrentAppendRead locks the single-writer discipline claim: appends
// (turn side) and reads/evictions (uploader side) may run on separate
// goroutines without data races or index desync.
func TestConcurrentAppendRead(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := int32(1); i <= 50; i++ {
			if err := o.Append(progressEntry("cmd-1", i, "concurrent")); err != nil {
				t.Errorf("append: %v", err)
				return
			}
		}
	})
	wg.Go(func() {
		for i := 0; i < 20; i++ {
			records, rErr := o.ReadRecords(ReadBatchMaxEntries, 1<<20)
			if rErr != nil {
				t.Errorf("read: %v", rErr)
				return
			}
			if len(records) > 0 {
				if err := o.EvictThrough(records[len(records)-1].Index); err != nil {
					t.Errorf("evict: %v", err)
					return
				}
			}
		}
	})
	wg.Wait()
}

// TestEnvelopeWireShape locks that the stored envelope round-trips as the wire
// UploadCommandDataEntry with its typed payload intact.
func TestEnvelopeWireShape(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	e := &Entry{
		CommandId: "cmd-1",
		Kind:      v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT,
		SeqNo:     7,
		Payload: &v1pb.UploadCommandDataEntry_Event{
			Event: &v1pb.CommandEvent{
				CommandId: "cmd-1",
				SeqNo:     7,
				Type:      v1pb.CommandEventType_TOOL_CALL_STARTED,
				Summary:   "tool",
			},
		},
	}
	if err := o.Append(e); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := o.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	records, err := o.ReadRecords(10, 1<<20)
	if err != nil || len(records) != 1 {
		t.Fatalf("read: %v records=%d", err, len(records))
	}
	got := records[0].Entry
	if got.GetEvent().GetType() != v1pb.CommandEventType_TOOL_CALL_STARTED ||
		got.GetKind() != v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT ||
		got.GetSeqNo() != 7 {
		t.Fatalf("envelope round-trip mismatch: %+v", got)
	}
	// The envelope marshals with the same wire encoding the manager receives.
	data, err := proto.Marshal(got)
	if err != nil || len(data) == 0 {
		t.Fatalf("marshal: %v", err)
	}
}

// TestClosedOutboxRefusesOperations locks the close semantics: operations on a
// closed Outbox return ErrClosed and never reopen the WAL behind the caller's
// back.
func TestClosedOutboxRefusesOperations(t *testing.T) {
	o, err := Open(filepath.Join(t.TempDir(), "outbox"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := o.Append(progressEntry("cmd-1", 1, "x")); err != ErrClosed {
		t.Fatalf("append on closed outbox = %v, want ErrClosed", err)
	}
	if err := o.Flush(); err != ErrClosed {
		t.Fatalf("flush on closed outbox = %v, want ErrClosed", err)
	}
	if _, err := o.ReadRecords(10, 1<<20); err != ErrClosed {
		t.Fatalf("read on closed outbox = %v, want ErrClosed", err)
	}
	if _, err := o.Empty(); err != ErrClosed {
		t.Fatalf("empty on closed outbox = %v, want ErrClosed", err)
	}
	if err := o.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
