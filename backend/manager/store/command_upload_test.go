package store

import (
	"strings"
	"testing"
)

// TestUploadAppendOutputSQL locks the retransmission dedup shape: the
// (command_id, seq_no) unique index plus ON CONFLICT DO NOTHING is what makes
// outbox retransmission idempotent. Dropping the conflict clause would turn a
// retransmission into a duplicate row.
func TestUploadAppendOutputSQL(t *testing.T) {
	if !strings.Contains(uploadAppendOutputSQL, "ON CONFLICT (command_id, seq_no) DO NOTHING") {
		t.Fatal("progress upload must dedup on (command_id, seq_no) so outbox retransmission is idempotent")
	}
}

// TestUploadAppendEventSQL locks the event dedup shape for the same reason.
func TestUploadAppendEventSQL(t *testing.T) {
	if !strings.Contains(uploadAppendEventSQL, "ON CONFLICT (command_id, seq_no) DO NOTHING") {
		t.Fatal("event upload must dedup on (command_id, seq_no) so outbox retransmission is idempotent")
	}
}

// TestUploadTerminalSQL locks the terminal status guard: only a PENDING or
// RUNNING command may transition to COMPLETED/FAILED from an upload batch. A
// late result must never overwrite the user's CANCELLED state or the reaper's
// FAILED state (the cancelled/failed state is the irreversible anchor).
func TestUploadTerminalSQL(t *testing.T) {
	if !strings.Contains(uploadTerminalSQL, "AND status IN ($7, $8)") {
		t.Fatal("terminal upload must be status-guarded so a late result cannot overwrite CANCELLED/FAILED")
	}
}

// TestUploadOwnershipSQL locks the ownership load shape: the batch resolves
// command ownership (machine_id) with one ANY() query, and COALESCE keeps a
// legacy NULL machine_id from matching a real machine.
func TestUploadOwnershipSQL(t *testing.T) {
	if !strings.Contains(uploadOwnershipSQL, "COALESCE(c.machine_id, 0)") {
		t.Fatal("ownership load must COALESCE machine_id so a legacy NULL never matches a machine")
	}
	if !strings.Contains(uploadOwnershipSQL, "c.id = ANY($1::uuid[])") {
		t.Fatal("ownership load must fetch the whole batch in one query")
	}
}

// TestUploadAckSeqSQL locks the cursor semantics: last_ack_seq only advances
// (GREATEST), so a partial or reordered retransmission cannot regress the
// persisted event cursor.
func TestUploadAckSeqSQL(t *testing.T) {
	if !strings.Contains(uploadAckSeqSQL, "GREATEST(last_ack_seq, $1)") {
		t.Fatal("ack seq must only advance; a retransmission must not regress the cursor")
	}
}
