package store

import (
	"strings"
	"testing"
)

// TestEnqueueAgentControlSQL locks the queue's FIFO dispatch shape: the serial
// id is the per-machine dispatch order, so a machine's (re)connect drains rows
// oldest-first (design §3.5: control interactions issued offline are delivered
// in issue order).
func TestEnqueueAgentControlSQL(t *testing.T) {
	if !strings.Contains(listAgentPendingControlSQL, "ORDER BY id") {
		t.Fatal("pending control must dispatch in id order so queued cancels/steers replay in issue order")
	}
	if !strings.Contains(listAgentPendingControlSQL, "WHERE machine_id = $1") {
		t.Fatal("pending control is dispatched per machine, not per agent")
	}
}

// TestDeleteAgentControlSQL locks the retention shape: delivered or
// terminal-judged rows are deleted by explicit id (never a blanket delete), so
// rows enqueued between the list and the delete survive for the next connect.
func TestDeleteAgentControlSQL(t *testing.T) {
	if !strings.Contains(deleteAgentControlSQL, "WHERE id = ANY($1::int[])") {
		t.Fatal("dispatched rows must be deleted by id so rows enqueued mid-dispatch survive")
	}
}

// TestDeleteExpiredAgentControlSQL locks the TTL backstop.
func TestDeleteExpiredAgentControlSQL(t *testing.T) {
	if !strings.Contains(deleteExpiredAgentControlSQL, "WHERE created_at < $1") {
		t.Fatal("the retention backstop must sweep by created_at")
	}
}

// TestAgentControlKinds locks the control-kind vocabulary: only cancel/steer
// are queued. Wake and prompt-notice are best-effort pushes (a dropped wake is
// recovered by the next BeginSession's cursor comparison; a notice by the
// prompt-version comparison), so they must not enter the queue.
func TestAgentControlKinds(t *testing.T) {
	if ControlKindCancel != "cancel" || ControlKindSteer != "steer" {
		t.Fatal("control kinds must match the dispatcher's payload routing")
	}
}
