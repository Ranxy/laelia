package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/Ranxy/laelia/backend/agent/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// CLI subcommands have already printed the canonical Error:/Code: block
		// to stderr; only log unexpected (daemon) failures.
		if !errors.Is(err, cmd.ErrCLIFailed) {
			slog.Error("agent command failed", "error", err)
		}
		os.Exit(1)
	}
}
