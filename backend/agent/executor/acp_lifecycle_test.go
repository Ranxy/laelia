//go:build !windows

package executor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestKillGroup_ReapsDescendants (T7, group-reap half): KillGroup must signal
// the whole process tree led by the spawned subprocess, so a descendant the
// subprocess orphaned to init is reaped too — not left running. This is the
// primitive Stop / idle-evict rely on to avoid leaking pi's tool subprocesses.
// Before Phase 0 (no process group), killing only the direct child left
// descendants reparented to init and still running.
func TestKillGroup_ReapsDescendants(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	dir := t.TempDir()
	childPIDFile := filepath.Join(dir, "child.pid")
	// Background a long-lived sleep, record its PID, then block on `wait` so the
	// shell stays alive until the test kills the group.
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", childPIDFile)
	cmd := exec.Command("sh", "-c", script)
	SetProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	// Wait for the background descendant to start and record its PID.
	var childPID int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(childPIDFile)
		if err != nil {
			return false
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return false
		}
		childPID = pid
		return true
	}, 3*time.Second, 10*time.Millisecond, "descendant must record its PID")

	// The descendant is alive before the group kill.
	require.NoError(t, syscall.Kill(childPID, 0), "descendant must be alive before KillGroup")

	// Kill the whole group; the descendant (orphaned to init once the shell
	// dies, but still in the shell's group) must be reaped, not left behind.
	require.NoError(t, KillGroup(cmd, syscall.SIGKILL))
	_ = cmd.Wait()

	require.Eventually(t, func() bool {
		return syscall.Kill(childPID, 0) != nil // ESRCH once reaped
	}, 3*time.Second, 10*time.Millisecond, "descendant must be reaped by KillGroup, not orphaned")
}

// pdeathsigHelperArg re-execs the test binary as the PDEATHSIG helper parent.
// Detected in init() before flag.Parse so the child never runs the test suite.
const pdeathsigHelperArg = "--executor-pdeathsig-helper"

func init() {
	if isPdeathsigHelper() {
		os.Exit(runPdeathsigHelper())
	}
}

func isPdeathsigHelper() bool {
	return slices.Contains(os.Args[1:], pdeathsigHelperArg)
}

// runPdeathsigHelper is the re-exec'd parent: it spawns a long-lived sleep with
// SetProcessGroup (Pdeathsig=SIGKILL on Linux, relative to THIS helper), records
// the sleep's PID, then blocks so the test can SIGKILL it and observe whether
// Pdeathsig fires on the sleep. It returns an exit code for init's os.Exit so
// the call stays in init (revive deep-exit); the blocking loop never returns.
func runPdeathsigHelper() int {
	dir := os.Getenv("LAELIA_PDEATHSIG_DIR")
	pidFile := filepath.Join(dir, "child.pid")
	readyFile := filepath.Join(dir, "ready")
	cmd := exec.Command("sleep", "30")
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return 1
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	_ = os.WriteFile(readyFile, []byte("1"), 0o600)
	// Block until the test SIGKILLs us; on our death Pdeathsig fires on the
	// child. A sleep loop (rather than <-make(chan struct{})) keeps linters
	// happy while blocking indefinitely.
	for {
		time.Sleep(time.Hour)
	}
}

// TestSetProcessGroup_PdeathsigOnParentDeath (T7, PDEATHSIG half, Linux only):
// when a process spawned with SetProcessGroup has its parent killed with
// SIGKILL (an OOM or `kill -9` of the machine/manager), the kernel must SIGKILL
// the child via Pdeathsig so no agent descendants are orphaned to init. Linux is
// the only platform where SetProcessGroup sets Pdeathsig.
func TestSetProcessGroup_PdeathsigOnParentDeath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Pdeathsig is Linux-only")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], pdeathsigHelperArg)
	cmd.Env = append(os.Environ(), "LAELIA_PDEATHSIG_DIR="+dir)
	require.NoError(t, cmd.Start())

	// Wait for the helper to spawn its child and record the PID.
	var childPID int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "child.pid"))
		if err != nil {
			return false
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return false
		}
		childPID = pid
		return true
	}, 3*time.Second, 10*time.Millisecond, "helper must spawn its child and record the PID")

	require.NoError(t, syscall.Kill(childPID, 0), "child must be alive before the parent is killed")

	// SIGKILL the parent (the helper). Pdeathsig must SIGKILL the child too —
	// this is what prevents orphaned agent descendants when the machine or
	// manager is OOM-killed.
	require.NoError(t, syscall.Kill(cmd.Process.Pid, syscall.SIGKILL))
	_ = cmd.Wait()

	require.Eventually(t, func() bool {
		return syscall.Kill(childPID, 0) != nil
	}, 3*time.Second, 10*time.Millisecond, "Pdeathsig must kill the child when its parent is SIGKILLed")
}
