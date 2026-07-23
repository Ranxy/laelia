package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/client"
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

func runMachine() error {
	if flags.token == "" {
		return errors.New("--token is required (the machine registration token issued by CreateMachine)")
	}
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	log.SetSlog()

	slog.Info("laelia-machine starting", "manager", flags.managerURL)

	machine, err := client.New(flags.managerURL, flags.token, flags.insecure, flags.allowHTTP)
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
