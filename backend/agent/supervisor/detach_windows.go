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

// prepareWorker puts the worker in its own Windows process group so the
// supervisor can later send it a CTRL_BREAK console event (delivered to Go
// programs as os.Interrupt, which the worker handles for graceful shutdown).
func prepareWorker(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// signalWorker asks the worker to shut down gracefully. os.Process.Signal
// does not support SIGTERM on Windows, so we send CTRL_BREAK to the worker's
// process group instead. If the supervisor has no console (for example when
// it was started from a service), this returns an error and the caller falls
// back to a hard kill.
func signalWorker(cmd *exec.Cmd) error {
	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	proc, err := kernel32.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	r, _, e := proc.Call(syscall.CTRL_BREAK_EVENT, uintptr(cmd.Process.Pid))
	if r == 0 {
		return e
	}
	return nil
}
