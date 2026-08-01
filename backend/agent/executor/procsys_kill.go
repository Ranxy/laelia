//go:build !windows

package executor

import (
	"os/exec"
	"syscall"
)

// KillGroup sends sig to the process group led by cmd — the agent subprocess
// and every descendant it spawned. A nil cmd or a cmd whose process is gone is
// a no-op. Call after a successful Start. The negative PID addresses the whole
// group; an already-reaped group returns ESRCH, which callers may ignore.
func KillGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}
