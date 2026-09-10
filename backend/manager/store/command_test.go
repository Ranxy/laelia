package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCoerceEnvJSON guards the JSONB NOT NULL command.env column against the
// empty-string bug that silently killed agent BeginSession sessions: an empty
// env must become valid JSON, while a real env value is passed through.
func TestCoerceEnvJSON(t *testing.T) {
	assert.Equal(t, "{}", coerceEnvJSON(""))
	assert.Equal(t, `{"FOO":"bar"}`, coerceEnvJSON(`{"FOO":"bar"}`))
}

// TestReapRunningCommandSQL locks in the stale-command reaper's guard: the
// per-command reap is a compare-and-set on the RUNNING status, so a result
// that landed between the reaper's list scan and its update (marking the
// command terminal) is never flipped back to FAILED — and the reap records the
// machine_unreachable provenance so a late terminal can re-grade the row.
func TestReapRunningCommandSQL(t *testing.T) {
	if !strings.Contains(reapRunningCommandSQL, "WHERE id = $5 AND status = $6") {
		t.Fatal("the stale reap must be a status-guarded compare-and-set on the RUNNING row")
	}
	if !strings.Contains(reapRunningCommandSQL, "completed_at = $2") {
		t.Fatal("the stale reap must record when the command was closed")
	}
	if !strings.Contains(reapRunningCommandSQL, "failure_kind = $4") {
		t.Fatal("the stale reap must record the machine_unreachable provenance")
	}
}

// TestCancelRunningForMachineSQL locks the force-disconnect anchor: only
// PENDING/RUNNING rows flip to CANCELED (terminal states and existing
// CANCELED anchors are untouched), scoped to the machine's commands.
func TestCancelRunningForMachineSQL(t *testing.T) {
	if !strings.Contains(cancelRunningForMachineSQL, "WHERE COALESCE(machine_id, 0) = $2 AND status IN ($3, $4)") {
		t.Fatal("the force-cancel must be a status-guarded update scoped to the machine")
	}
}

// TestUploadTerminalGuards lock the late-result re-grade SQL shapes: the
// normal terminal only transitions PENDING/RUNNING rows (rule 3: a CANCELED
// command is never re-graded), and both re-grade directions re-check the
// machine_unreachable provenance inside the UPDATE so a concurrent state
// change or a retransmission is a no-op.
func TestUploadTerminalGuards(t *testing.T) {
	if !strings.Contains(uploadTerminalSQL, "WHERE id = $7 AND status IN ($8, $9)") {
		t.Fatal("the terminal apply must be a status-guarded transition of pending/running rows")
	}
	if !strings.Contains(uploadTerminalSQL, "failure_kind = $6") {
		t.Fatal("the terminal apply must record the agent_failed provenance for FAILED rows")
	}
	if !strings.Contains(uploadRegradeSQL, "WHERE id = $5 AND status = $6 AND failure_kind = $7") {
		t.Fatal("the re-grade must re-check the unreachable reap provenance (compare-and-set)")
	}
	if !strings.Contains(uploadRegradeSQL, "failure_kind = NULL") {
		t.Fatal("a re-graded row must lose its failure kind (it completed)")
	}
	if !strings.Contains(uploadLateFailureSQL, "WHERE id = $3 AND status = $4 AND failure_kind = $5") {
		t.Fatal("the late-failure re-attribution must re-check the unreachable provenance")
	}
	if !strings.Contains(insertRegradeEventSQL, "event_type") {
		t.Fatal("the re-grade explanation must be recorded as a command event")
	}
}
