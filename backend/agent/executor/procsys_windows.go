//go:build windows

package executor

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup is a no-op on Windows: process groups use a different API
// (CREATE_NEW_PROCESS_GROUP / job objects) the agent runtime does not wire up.
// The agent runtime is Unix-focused in practice; this stub keeps the build
// green.
func SetProcessGroup(_ *exec.Cmd) {}

// KillGroup terminates the direct process on Windows (best effort). Negative-PID
// process-group kill is not available; descendants are not collected. The
// signal argument is ignored — Windows os.Process supports only os.Kill.
func KillGroup(cmd *exec.Cmd, _ syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
