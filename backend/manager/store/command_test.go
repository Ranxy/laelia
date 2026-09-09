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
