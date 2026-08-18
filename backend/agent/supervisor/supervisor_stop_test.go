//go:build unix

package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ranxy/laelia/backend/agent/home"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgrepToken reports whether any running process has token in its command line.
func pgrepToken(t *testing.T, token string) bool {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", token).Output()
	if err == nil {
		return len(out) > 0
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false // pgrep exits 1 when no process matches
	}
	t.Fatalf("pgrep %q failed: %v", token, err)
	return false
}

// TestStopDoesNotOrphanWorker is a regression test for the shutdown race where
// `laelia-machine stop` could stop the supervisor (daemon) while watchWorker
// respawned a fresh `run` worker in the gap between stopWorker and
// close(stopWatcher). The respawned worker was then orphaned (reparented to
// init) and kept running under a new pid. After the fix, shutdown sets
// `stopping` and closes stopWatcher before killing the worker, so no respawn
// occurs and exactly zero worker processes remain.
//
// It runs several iterations because the original race was timing-dependent;
// the fix must leave no orphan on every iteration.
func TestStopDoesNotOrphanWorker(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available")
	}

	tmp := t.TempDir()
	t.Setenv(home.EnvDir, tmp) // isolate the supervisor addr file + logs

	// A uniquely-named symlink to sleep is the worker executable: its argv[0]
	// carries the token, so pgrep -f token matches only this supervisor's
	// worker and nothing else in the test process.
	token := "laelia-test-worker-" + filepath.Base(tmp)
	workerExe := filepath.Join(tmp, token)
	require.NoError(t, os.Symlink(sleepPath, workerExe))

	const iterations = 10
	for i := 0; i < iterations; i++ {
		sup, err := New(workerExe, []string{"300"}, []string{"daemon"}, false)
		require.NoError(t, err, "iter %d: New", i)

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- sup.Run(ctx) }()

		// Wait for the worker to come up.
		require.Eventually(t, func() bool { return pgrepToken(t, token) },
			5*time.Second, 30*time.Millisecond, "iter %d: worker never started", i)

		// Trigger the real shutdown path that `laelia-machine stop` uses.
		require.NoError(t, Stop(), "iter %d: Stop", i)

		select {
		case err := <-runDone:
			assert.NoError(t, err, "iter %d: Run returned error", i)
		case <-time.After(20 * time.Second):
			cancel()
			t.Fatalf("iter %d: supervisor did not stop within 20s", i)
		}
		cancel()

		// Give any racing respawn a moment to surface in the process table, then
		// assert no worker is left. On the buggy code a respawned worker survived
		// here under a new pid.
		time.Sleep(200 * time.Millisecond)
		if pgrepToken(t, token) {
			_ = exec.Command("pkill", "-f", token).Run()
			t.Fatalf("iter %d: orphan worker still running after stop (shutdown race regression)", i)
		}
	}
}
