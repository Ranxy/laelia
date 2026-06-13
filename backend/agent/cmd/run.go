package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/pkg/errors"
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

	info := collectAgentInfo()
	slog.Info("agent info collected",
		"hostname", info.Hostname,
		"os", info.Os,
		"arch", info.Arch,
		"version", info.Version,
		"ip", info.IP,
	)

	apiClient := client.New(flags.managerURL, flags.token)

	if err := apiClient.Connect(info); err != nil {
		return errors.Errorf("failed to connect to manager: %v", err)
	}
	slog.Info("connected to manager")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		slog.Info(fmt.Sprintf("%s received, shutting down", sig.String()))
		cancel()
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopped")
			return nil
		case <-ticker.C:
			if err := apiClient.Heartbeat(); err != nil {
				slog.Error("heartbeat failed", "error", err)
			} else {
				slog.Debug("heartbeat sent")
			}
		}
	}
}

func collectAgentInfo() *client.AgentInfo {
	hostname, _ := os.Hostname()
	return &client.AgentInfo{
		Hostname: hostname,
		Os:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  "0.1.0",
		IP:       getOutboundIP(),
	}
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return localAddr.IP.String()
}
