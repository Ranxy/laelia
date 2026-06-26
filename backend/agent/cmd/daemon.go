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
)

func init() {
	rootCmd.AddCommand(daemonCmd)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Connect to the manager and run the agent drain loop",
	RunE: func(_ *cobra.Command, _ []string) error {
		return runDaemon()
	},
}

func runDaemon() error {
	if flags.token == "" {
		return errors.New("--token is required (the bootstrap token issued by the manager)")
	}
	slog.Info("laelia-agent starting", "manager", flags.managerURL)

	apiClient, err := client.New(flags.managerURL, flags.token, flags.insecure, flags.allowHTTP, flags.agentName)
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
