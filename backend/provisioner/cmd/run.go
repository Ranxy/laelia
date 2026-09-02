package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
	"github.com/Ranxy/laelia/backend/provisioner/client"
	"github.com/Ranxy/laelia/backend/provisioner/version"
)

func init() {
	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Connect to the manager and drive machine provisioning jobs",
	RunE: func(_ *cobra.Command, _ []string) error {
		return runProvisioner()
	},
}

// runProvisioner resolves the configured backend, builds the manager client,
// and runs until the manager requests shutdown or the process is signalled.
func runProvisioner() error {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	log.SetSlog()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// The docker stub registers under the same name; constructing an
	// unimplemented backend fails fast with ErrUnsupportedBackend.
	be, err := backend.New(cfg.Backend, cfg.backendConfig())
	if err != nil {
		return err
	}

	c, err := client.New(client.Config{
		ManagerURL:         cfg.ManagerURL,
		Token:              cfg.Token,
		Backend:            cfg.Backend,
		AutoUpgrade:        cfg.AutoUpgrade,
		RetainData:         cfg.RetainData,
		ManagerURLOverride: cfg.ManagerURLOverride,
		ConfigDigest:       cfg.Digest(),
		Insecure:           flags.insecure,
		AllowHTTP:          flags.allowHTTP,
	}, be)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("laelia provisioner starting",
		"manager", cfg.ManagerURL,
		"backend", cfg.Backend,
		"namespace", cfg.Namespace,
		"configDigest", cfg.Digest(),
		"version", version.Version,
	)
	return c.Run(ctx)
}
