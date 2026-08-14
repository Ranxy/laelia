package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/client"
	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/state"
	"github.com/Ranxy/laelia/backend/common/log"
)

func init() {
	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Connect to the manager and host this machine's agents",
	RunE: func(_ *cobra.Command, _ []string) error {
		return runMachine()
	},
}

// runMachine loads the local machine state and runs the machine in the
// foreground. It is shared by `run` (which requires existing state) and
// `setup` (which configures/validates the login first, then runs).
func runMachine() error {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	log.SetSlog()

	if alreadyRunning() {
		_, _ = fmt.Println("laelia-machine is already running on this computer")
		return nil
	}

	st, err := state.Load()
	if err != nil {
		return errors.New("failed to read local machine state: " + err.Error())
	}
	if st == nil {
		return errors.New("not configured; run `laelia-machine setup` first")
	}
	managerURL := strings.TrimRight(flags.managerURL, "/")
	if st.ManagerURL != managerURL {
		return errors.Errorf("this machine is configured for %s, not %s; run `laelia-machine --manager %s setup` to re-authenticate", st.ManagerURL, managerURL, st.ManagerURL)
	}
	if st.MachineID == "" || st.RefreshToken == "" {
		return errors.New("local machine state is incomplete; run `laelia-machine setup` to re-authenticate")
	}

	slog.Info("laelia-machine starting", "manager", managerURL, "machineID", st.MachineID, "dataDir", home.Dir())

	machine, err := client.New(managerURL, st.MachineID, st.RefreshToken, flags.insecure, flags.allowHTTP, func(token string) {
		st.RefreshToken = token
		if err := state.Save(st); err != nil {
			slog.Error("failed to persist renewed refresh token", "error", err)
		}
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		slog.Info("shutdown signal received, stopping machine")
		cancel()
	}()

	return machine.Run(ctx)
}

// alreadyRunning probes the well-known daemon socket. A live socket means
// another laelia-machine process is already running on this computer; the
// caller reports it and exits 0 (nothing to do). A stale socket file with no
// listener fails the dial, so a crashed process does not block a restart.
func alreadyRunning() bool {
	conn, err := net.DialTimeout("unix", daemonsrv.DefaultSocketPath(), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}
