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

// TestFailRunningCommandsForAgentSQL locks in the BeginSession reap shape: the
// update must scope to RUNNING rows only, so a command that completed
// concurrently (its result raced the agent's next BeginSession) is never
// overwritten, and must RETURN the reaped ids so the dispatcher can close
// their live watchers.
func TestFailRunningCommandsForAgentSQL(t *testing.T) {
	if !strings.Contains(failRunningCommandsForAgentSQL, "AND status = $5") {
		t.Fatal("the reap must be guarded by the RUNNING status so a concurrent result is never overwritten")
	}
	if !strings.Contains(failRunningCommandsForAgentSQL, "agent_id = $4") {
		t.Fatal("the reap must be scoped to a single agent")
	}
	if !strings.Contains(failRunningCommandsForAgentSQL, "RETURNING id") {
		t.Fatal("the reap must return the reaped command ids so callers can close their watchers")
	}
}

// TestFailStaleRunningCommandSQL locks in the stale-command reaper's guard:
// the per-command reap is a compare-and-set on the RUNNING status, so a result
// that landed between the reaper's list scan and its update (marking the
// command terminal) is never flipped back to FAILED.
func TestFailStaleRunningCommandSQL(t *testing.T) {
	if !strings.Contains(failStaleRunningCommandSQL, "WHERE id = $4 AND status = $5") {
		t.Fatal("the stale reap must be a status-guarded compare-and-set on the RUNNING row")
	}
	if !strings.Contains(failStaleRunningCommandSQL, "completed_at = $2") {
		t.Fatal("the stale reap must record when the command was closed")
	}
}
