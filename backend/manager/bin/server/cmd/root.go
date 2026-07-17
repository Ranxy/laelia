package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgconn"
	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/manager/server"
)

// -----------------------------------Global constant BEGIN----------------------------------------
const (
	greetingBanner = `
___________________________________________________________________________________________

██╗      █████╗ ███████╗██╗     ██╗ █████╗ 
██║     ██╔══██╗██╔════╝██║     ██║██╔══██╗
██║     ███████║█████╗  ██║     ██║███████║
██║     ██╔══██║██╔══╝  ██║     ██║██╔══██║
███████╗██║  ██║███████╗███████╗██║██║  ██║
╚══════╝╚═╝  ╚═╝╚══════╝╚══════╝╚═╝╚═╝  ╚═╝
                                                               
%s
___________________________________________________________________________________________

`
)

// -----------------------------------Command Line Config BEGIN------------------------------------.
var (
	flags struct {
		// Used for command line config
		port        int
		externalURL string
		dataDir     string
		ha          bool
		saas        bool
		// TLS flags
		tlsCertDir string
		tlsHost    string
		tlsDomain  string
		// output logs in json format
		enableJSONLogging bool
		// demo mode.
		demo  bool
		debug bool
		// memoryProfileThreshold is the threshold of memory usage in bytes to trigger a memory profile.
		memoryProfileThreshold uint64
		// trustProxy trusts X-Forwarded-For / X-Real-IP as the source IP (only
		// safe behind a trusted reverse proxy). Default false.
		trustProxy bool
		// pprofAddr is the bind address for the standalone pprof server, e.g.
		// "127.0.0.1:6060". Empty disables pprof. Only effective with --debug.
		pprofAddr string
	}

	rootCmd = &cobra.Command{
		Use:   "database management server",
		Short: "database management server",
		Run: func(_ *cobra.Command, _ []string) {
			start()
		},
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().IntVar(&flags.port, "port", 8181, "port where server runs. Default to 8181")
	rootCmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "whether to enable debug level logging")
	rootCmd.PersistentFlags().StringVar(&flags.tlsCertDir, "tls-cert-dir", "", "TLS certificate directory (enables TLS with self-signed cert if empty)")
	rootCmd.PersistentFlags().StringVar(&flags.tlsHost, "tls-host", "", "TLS server hostname (comma-separated for multiple hosts)")
	rootCmd.PersistentFlags().StringVar(&flags.tlsDomain, "tls-domain", "", "TLS public domain (enables ACME/Let's Encrypt auto-cert)")
	rootCmd.PersistentFlags().BoolVar(&flags.trustProxy, "trust-proxy", false, "trust X-Forwarded-For/X-Real-IP as the source IP (enable only behind a trusted reverse proxy)")
	rootCmd.PersistentFlags().StringVar(&flags.pprofAddr, "pprof-addr", "", "bind address for the standalone pprof server (e.g. 127.0.0.1:6060); empty disables pprof. Only effective with --debug; never exposed on the public port")
}

func start() {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}

	log.SetSlog()

	profile := activeProfile(flags.dataDir)

	if profile.PgURL == "" {
		slog.Error("must set PG_URL environment variable")
		return
	}

	var s *server.Server
	var err error
	// Setup signal handlers.
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	// Trigger graceful shutdown on SIGINT or SIGTERM.
	// The default signal sent by the `kill` command is SIGTERM,
	// which is taken as the graceful shutdown signal for many systems, eg., Kubernetes, Gunicorn.
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		slog.Info(fmt.Sprintf("%s received.", sig.String()))
		if s != nil {
			_ = s.Shutdown(ctx)
		}
		cancel()
	}()

	s, err = server.NewServer(ctx, profile)
	if err != nil {
		if pge, ok := errors.AsType[*pgconn.PgError](err); ok {
			slog.Error("Cannot new server", log.WithError(err), "detail", pge.Detail, "hint", pge.Hint)
			return
		}
		slog.Error("Cannot new server", log.WithError(err))
		return
	}

	fmt.Printf(greetingBanner, fmt.Sprintf("Server has started on port %d 🚀", flags.port))

	// Execute program.
	if err := s.Run(ctx, flags.port); err != nil {
		if err != http.ErrServerClosed {
			slog.Error(err.Error())
			_ = s.Shutdown(ctx)
			cancel()
		}
	}

	// Wait for CTRL-C.
	<-ctx.Done()
}
