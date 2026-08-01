//go:build !linux && !windows

package executor

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup makes cmd its own process-group leader so KillGroup can
// signal the whole tree. Pdeathsig is Linux-specific, so on these platforms
// only the group is set; the explicit KillGroup on cancel still handles the
// normal teardown path. Only a parent killed with SIGKILL leaves orphans here,
// which is an edge case the runtime does not guard on non-Linux platforms.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
