package main

import (
	"log/slog"
	"os"

	"github.com/Ranxy/laelia/backend/agent/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		slog.Error("agent command failed", "error", err)
		os.Exit(1)
	}
}
