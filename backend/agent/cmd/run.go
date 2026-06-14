package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/client"
)

func init() {
	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Connect to the manager and start the agent",
	RunE: func(_ *cobra.Command, _ []string) error {
		return run()
	},
}

func run() error {
	slog.Info("laelia-agent starting", "manager", flags.managerURL)

	apiClient, err := client.New(flags.managerURL, flags.token, flags.insecure, flags.allowHTTP)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		slog.Info("shutdown signal received, stopping agent")
		cancel()
	}()

	return apiClient.Run(ctx)
}
