// Package outbox is the machine-side durable buffer between turn execution and
// command data reporting. The turn loop appends progress/event/result records
// here (process-local, fsync-backed); a per-agent uploader reads them back,
// uploads them through the batched UploadCommandData RPC, and evicts settled
// records. Records survive disconnects and machine restarts, so a proxy that
// kills long-lived streams can no longer lose or interrupt anything a turn
// produced.
//
// Storage is github.com/tidwall/wal: one WAL per agent under
// <data>/<machineID>/<agentID>/outbox/, segmented and rotated by the library.
// The WAL index is an internal log position (eviction cursor only, never
// leaked); the idempotency key the manager dedups on is the envelope's
// per-kind seq. AllowEmpty is mandatory: after a full eviction the log reopens
// as empty (FirstIndex = LastIndex+1) instead of failing with ErrEmptyLog.
package outbox

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tidwall/wal"
	"google.golang.org/protobuf/proto"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	// flushThresholdBytes flushes the append buffer as soon as this much
	// unwritten envelope data accumulates (the uploader's tick flushes the
	// rest on its ~200ms window).
	flushThresholdBytes = 64 * 1024
	// MaxBytes is the per-agent retention backstop: when the WAL exceeds it,
	// the whole log is dropped (the turn-start barrier keeps at most one
	// command in flight, so the log is one command's unacked data). The
	// manager reaps any command whose terminal is lost this way. The design
	// targets this value as a machine-level cap; per-agent is the
	// conservative reading and keeps the backstop self-contained.
	MaxBytes = 512 << 20
	// ReadBatchMaxEntries bounds one upload batch's entry count.
	ReadBatchMaxEntries = 4096
)

// segmentSize keeps the library default (20MB segments, rotated for us). A
// variable only so tests can force segment cycling.
var segmentSize = 20 << 20

// ErrClosed is returned by operations on a closed Outbox.
var ErrClosed = errors.New("outbox closed")

// Entry is the durable envelope: the wire UploadCommandDataEntry. One format
// on disk, in upload requests, and in memory keeps the outbox ↔ upload path
// conversion-free.
type Entry = v1pb.UploadCommandDataEntry

// Record pairs a WAL index with its envelope so the uploader can fold acks
// and rejections into an eviction boundary by index. A nil Entry is a poison
// record: torn data inside a readable frame, counted as settled so eviction
// can move past it instead of stalling.
type Record struct {
	Index uint64
	Entry *Entry
}

// Outbox is one agent's durable command data log. Append is the turn's only
// network-free action; ReadRecords/EvictThrough belong to the uploader. All
// operations are serialized on mu: the WAL instance itself is goroutine-safe,
// but the buffered-write state, index bookkeeping, and quarantine swaps are
// not.
type Outbox struct {
	dir string

	mu     sync.Mutex
	log    *wal.Log
	closed bool
	// buffered holds records accepted but not yet handed to the WAL; the
	// turn loop appends here and flushes on the byte threshold.
	buffered      []*Entry
	bufferedBytes int
}

// Open opens (or creates) the agent's outbox WAL at dir. A corrupt log gets
// one recovery attempt (the library documents Close+Open as the recovery path
// for its torn-truncation bookkeeping), then the directory is quarantined
// (renamed aside with an alert) and a fresh log starts — the loss is bounded
// to this agent's unreported tail, which the manager's reaper covers.
func Open(dir string) (*Outbox, error) {
	log, err := openWAL(dir)
	if err != nil {
		if !errors.Is(err, wal.ErrCorrupt) {
			return nil, err
		}
		slog.Warn("outbox wal corrupt; retrying open", "dir", dir, "error", err)
		log, err = openWAL(dir)
		if err != nil {
			slog.Warn("outbox wal corrupt after retry; quarantining", "dir", dir, "error", err)
			if qErr := quarantine(dir); qErr != nil {
				slog.Error("failed to quarantine corrupt outbox", "dir", dir, "error", qErr)
			}
			log, err = openWAL(dir)
			if err != nil {
				return nil, err
			}
		}
	}
	return &Outbox{dir: dir, log: log}, nil
}

// openWAL opens the WAL with the outbox's fixed options.
func openWAL(dir string) (*wal.Log, error) {
	return wal.Open(dir, &wal.Options{
		NoSync:      false, // fsync every batch: a crash loses at most the open batch
		SegmentSize: segmentSize,
		LogFormat:   wal.Binary,
		AllowEmpty:  true, // eviction empties the log; reopening must not fail
		DirPerms:    0o700,
		FilePerms:   0o600,
	})
}

// quarantine renames a corrupt WAL directory aside so a fresh one can be
// created. Best-effort: a failed rename leaves the directory for a human.
func quarantine(dir string) error {
	target := dir + ".quarantine-" + fmt.Sprint(time.Now().UnixNano())
	return os.Rename(dir, target)
}

// Append accepts one record into the memory buffer. It flushes through to the
// WAL when the byte threshold is reached; the uploader's tick flushes the
// remainder. The returned error is a local disk failure — the caller (turn
// loop) treats it as the machine-local fault it is and fails the turn.
func (o *Outbox) Append(entry *Entry) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}
	o.buffered = append(o.buffered, entry)
	o.bufferedBytes += proto.Size(entry)
	if o.bufferedBytes >= flushThresholdBytes {
		return o.flushLocked()
	}
	return nil
}

// Flush writes buffered records to the WAL in one fsync'd batch. Idempotent
// and cheap when nothing is buffered.
func (o *Outbox) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}
	return o.flushLocked()
}

// flushLocked must hold mu.
func (o *Outbox) flushLocked() error {
	if len(o.buffered) == 0 {
		return nil
	}
	if err := o.writeLocked(o.buffered); err != nil {
		return err
	}
	o.buffered = o.buffered[:0]
	o.bufferedBytes = 0
	return nil
}

// writeLocked writes entries as one WAL batch, isolating on any WAL error:
// after a failed write the library's in-memory entry buffer is ahead of its
// index bookkeeping, so continuing to append would fork memory from disk.
// Recovery is Close+reopen (a partial batch may already be on disk; the
// retransmission dedup on the manager makes a re-write harmless); a second
// failure quarantines the directory and starts empty — the manager's reaper
// covers lost commands.
func (o *Outbox) writeLocked(entries []*Entry) error {
	if err := o.appendBatchLocked(entries); err == nil {
		return nil
	}
	if err := o.reopenLocked(); err != nil {
		return err
	}
	if err := o.appendBatchLocked(entries); err != nil {
		slog.Error("outbox wal failed after reopen; quarantining", "dir", o.dir, "error", err)
		if o.log != nil {
			_ = o.log.Close()
			o.log = nil
		}
		if qErr := quarantine(o.dir); qErr != nil {
			slog.Error("failed to quarantine failing outbox", "dir", o.dir, "error", qErr)
		}
		fresh, openErr := openWAL(o.dir)
		if openErr != nil {
			return errors.Join(err, openErr)
		}
		o.log = fresh
		return err
	}
	return nil
}

// walRange returns the WAL's current [first, last] index range.
func walRange(log *wal.Log) (first, last uint64, err error) {
	first, err = log.FirstIndex()
	if err != nil {
		return 0, 0, err
	}
	last, err = log.LastIndex()
	if err != nil {
		return 0, 0, err
	}
	return first, last, nil
}

// appendBatchLocked appends entries to the WAL in one fsync'd batch at
// lastIndex+1.. (the library enforces the no-gap invariant itself).
func (o *Outbox) appendBatchLocked(entries []*Entry) error {
	if o.log == nil {
		return ErrClosed
	}
	_, last, err := walRange(o.log)
	if err != nil {
		return err
	}
	batch := &wal.Batch{}
	next := last + 1
	for _, e := range entries {
		batch.Write(next, marshalEnvelope(e))
		next++
	}
	return o.log.WriteBatch(batch)
}

// marshalEnvelope serializes an envelope. Marshalling our own in-memory
// protos cannot fail; an impossible failure degrades to an empty record that
// the read path reports as poison instead of desyncing the WAL index.
func marshalEnvelope(e *Entry) []byte {
	data, err := proto.Marshal(e)
	if err != nil {
		slog.Error("outbox envelope marshal failed", "commandID", e.GetCommandId(), "error", err)
		return nil
	}
	return data
}

// ensureLogLocked reopens the WAL after an isolation swap. A closed Outbox
// never reopens; a quarantined instance retries on every operation so a
// transient fault (e.g. a read-only mount) self-heals once the fault clears.
func (o *Outbox) ensureLogLocked() error {
	if o.log != nil {
		return nil
	}
	if o.closed {
		return ErrClosed
	}
	log, err := openWAL(o.dir)
	if err != nil {
		return err
	}
	o.log = log
	return nil
}

// reopenLocked recovers from a WAL error by closing and reopening once.
func (o *Outbox) reopenLocked() error {
	if o.log != nil {
		_ = o.log.Close()
		o.log = nil
	}
	log, err := openWAL(o.dir)
	if err != nil {
		return err
	}
	o.log = log
	return nil
}

// ReadRecords returns up to maxRecords records starting at the log's first
// index, capped at maxBytes of raw payload, in log order. Unparseable records
// are torn data inside a readable frame: they are reported as poison records
// (Entry == nil) so the uploader still counts them as settled and can evict
// through them instead of stalling on them. The returned envelopes are
// freshly unmarshalled (caller-owned).
func (o *Outbox) ReadRecords(maxRecords int, maxBytes int64) (records []Record, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, ErrClosed
	}
	if err := o.ensureLogLocked(); err != nil {
		return nil, err
	}
	// Flush first so the newest records join the batch (the turn path only
	// flushes inline at the byte threshold).
	if err := o.flushLocked(); err != nil {
		return nil, err
	}
	first, last, err := walRange(o.log)
	if err != nil {
		return nil, err
	}
	for len(records) < maxRecords {
		if last < first {
			break // empty
		}
		data, rErr := o.log.Read(first)
		if errors.Is(rErr, wal.ErrNotFound) {
			// The log was truncated concurrently; resume from the new range.
			first, last, err = walRange(o.log)
			if err != nil {
				return nil, err
			}
			if last < first {
				break
			}
			continue
		}
		if rErr != nil {
			// A read error other than "out of range" is a local WAL fault:
			// isolate (Close+Open) and stop; the next batch retries from disk.
			if oErr := o.reopenLocked(); oErr != nil {
				return nil, errors.Join(rErr, oErr)
			}
			return nil, rErr
		}
		var e Entry
		if uErr := proto.Unmarshal(data, &e); uErr != nil {
			slog.Warn("outbox record failed to parse; reporting as poison", "dir", o.dir, "index", first, "error", uErr)
			records = append(records, Record{Index: first})
		} else {
			records = append(records, Record{Index: first, Entry: &e})
		}
		maxBytes -= int64(len(data))
		if maxBytes <= 0 {
			break
		}
		first++
	}
	return records, nil
}

// EvictThrough truncates every record at or before index (the caller folds
// acks/rejections into this settled boundary). TruncateFront(index+1) makes
// index+1 the first record; truncating the final record empties the log,
// which AllowEmpty permits.
func (o *Outbox) EvictThrough(index uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}
	if err := o.ensureLogLocked(); err != nil {
		return err
	}
	first, last, err := walRange(o.log)
	if err != nil {
		return err
	}
	if last < first || index < first {
		return nil // already empty / nothing settled inside this log
	}
	if index > last {
		index = last
	}
	if err := o.log.TruncateFront(index + 1); err != nil {
		if errors.Is(err, wal.ErrOutOfRange) {
			// The range moved under us (another eviction landed first); the
			// next round re-reads and re-folds.
			return nil
		}
		// Any other truncate error is a WAL fault: isolate before continuing.
		if rErr := o.reopenLocked(); rErr != nil {
			return errors.Join(err, rErr)
		}
		return err
	}
	return nil
}

// Empty reports whether the WAL holds no records.
func (o *Outbox) Empty() (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return true, ErrClosed
	}
	if err := o.ensureLogLocked(); err != nil {
		return true, err
	}
	first, last, err := walRange(o.log)
	if err != nil {
		return true, err
	}
	return last < first, nil
}

// EnforceCap drops the log's entire contents when its size breaches the
// retention backstop, so a long manager outage cannot fill the disk. With the
// turn-start barrier the log holds at most one command's records, so "the
// oldest command group" here is the whole log; the dropped command's fate is
// the manager's reaper.
func (o *Outbox) EnforceCap() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.log == nil {
		return
	}
	size, err := dirSize(o.dir)
	if err != nil || size < MaxBytes {
		return
	}
	first, last, err := walRange(o.log)
	if err != nil || last < first {
		return
	}
	slog.Warn("outbox over retention cap; dropping whole command group",
		"dir", o.dir, "bytes", size, "cap", MaxBytes)
	if err := o.log.TruncateFront(last + 1); err != nil {
		if rErr := o.reopenLocked(); rErr != nil {
			slog.Error("outbox cap enforcement failed to recover", "dir", o.dir, "error", errors.Join(err, rErr))
		}
	}
}

// dirSize sums the sizes of the WAL's segment files.
func dirSize(dir string) (int64, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var total int64
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if info, infoErr := e.Info(); infoErr == nil {
			total += info.Size()
		}
	}
	return total, nil
}

// Close flushes and closes the WAL. Safe to call twice; operations on a
// closed Outbox return ErrClosed (no auto-reopen).
func (o *Outbox) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if o.log == nil {
		return nil
	}
	err := o.log.Close()
	o.log = nil
	return err
}

// Dir returns the outbox directory (exported for tests and workspace reuse).
func (o *Outbox) Dir() string {
	return o.dir
}

// AgentOutboxDir is the canonical WAL directory for one agent.
func AgentOutboxDir(dataRoot, machineID, agentID string) string {
	return filepath.Join(dataRoot, machineID, agentID, "outbox")
}
