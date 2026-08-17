package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/state"
	"github.com/Ranxy/laelia/backend/agent/supervisor"
	"github.com/Ranxy/laelia/backend/common/log"
)

func init() {
	rootCmd.AddCommand(daemonCmd)
	setupCmd.Flags().BoolVar(&flags.setupForeground, "foreground", false, "run in the foreground after setup instead of daemonizing (setup only)")
	daemonCmd.Flags().BoolVar(&flags.daemonForeground, "foreground", false, "run the supervisor in the foreground (container/PID-1 mode)")
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the machine supervisor in the background-managed mode",
	Long: `Run the machine supervisor: it spawns and watches the business process
(laelia-machine run), serves the local upgrade control endpoint, and performs
self-upgrades when the manager requests them. Normally started automatically
by 'laelia-machine setup'; --foreground runs it attached to the current
terminal (used by the docker entrypoint).`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runDaemon()
	},
}

// daemonArgs builds the argv (excluding the exe) that relaunches this
// supervisor with the same configuration.
func daemonArgs() []string {
	args := []string{"daemon", "--manager", flags.managerURL}
	if flags.insecure {
		args = append(args, "--insecure")
	}
	if flags.allowHTTP {
		args = append(args, "--allow-http")
	}
	if flags.debug {
		args = append(args, "--debug")
	}
	if flags.daemonForeground || flags.setupForeground {
		args = append(args, "--foreground")
	}
	return args
}

// workerArgs builds the argv (excluding the exe) for the business process.
func workerArgs() []string {
	args := []string{"run", "--manager", flags.managerURL}
	if flags.insecure {
		args = append(args, "--insecure")
	}
	if flags.allowHTTP {
		args = append(args, "--allow-http")
	}
	if flags.debug {
		args = append(args, "--debug")
	}
	return args
}

func runDaemon() error {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	log.SetSlog()

	st, err := state.Load()
	if err != nil {
		return errors.New("failed to read local machine state: " + err.Error())
	}
	if st == nil {
		return errors.New("not configured; run `laelia-machine setup` first")
	}

	exe, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "resolve executable")
	}

	// setup --foreground (container/PID-1 mode) and daemon --foreground both
	// mean the supervisor must stay attached to the current process/terminal.
	foreground := flags.daemonForeground || flags.setupForeground
	sup, err := supervisor.New(exe, workerArgs(), daemonArgs(), foreground)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		slog.Info("shutdown signal received, stopping supervisor")
		cancel()
	}()

	return sup.Run(ctx)
}

// startDaemonize spawns a detached supervisor process (`laelia-machine
// daemon`) and returns, leaving the machine running in the background.
func startDaemonize() error {
	exe, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "resolve executable")
	}
	cmd := exec.Command(exe, daemonArgs()...)
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	supervisor.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "start background supervisor")
	}
	_, _ = fmt.Println("laelia-machine is now running in the background (supervisor pid " + fmt.Sprint(cmd.Process.Pid) + ")")
	return nil
}

// daemonize replaces the foreground run after setup: in --foreground mode it
// runs the supervisor attached to the current terminal (container/PID-1
// mode); otherwise it spawns the detached supervisor and returns.
func daemonize() error {
	if flags.setupForeground {
		return runDaemon()
	}
	return startDaemonize()
}
