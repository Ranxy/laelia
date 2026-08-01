//go:build linux

package executor

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup makes cmd its own process-group leader so KillGroup can
// signal the whole tree, and (Linux only) requests SIGKILL on parent death so a
// manager or machine process killed with SIGKILL (OOM, kill -9) does not leave
// orphaned agent descendants reparented to init. Call once before cmd.Start.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
