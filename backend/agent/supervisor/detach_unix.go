//go:build unix

package supervisor

import (
	"os/exec"
	"syscall"
)

// setDetached detaches a child process from this process's session and
// terminal so it survives the parent exiting (used for the relaunched
// supervisor after an upgrade).
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// execInPlace replaces the current process image with the upgraded binary,
// keeping the pid, stdout, and signal handling (container/PID-1 upgrades).
func execInPlace(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}

// prepareWorker applies any platform-specific setup needed before starting
// the business process so that stopWorker can later deliver a graceful stop
// signal. On Unix the default SIGTERM works without extra setup.
func prepareWorker(_ *exec.Cmd) {}

// signalWorker asks the worker to shut down gracefully. On Unix this is the
// conventional SIGTERM, which the worker handles by cancelling its context.
func signalWorker(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
