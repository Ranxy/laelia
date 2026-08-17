//go:build windows

package supervisor

import (
	"errors"
	"os/exec"
	"syscall"
)

// setDetached detaches a child process from this process's console so it
// survives the parent exiting (used for the relaunched supervisor after an
// upgrade).
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// execInPlace is unsupported on Windows; the upgrade path falls back to
// spawning a new supervisor process.
func execInPlace(_ string, _, _ []string) error {
	return errors.New("in-place exec is not supported on windows")
}
