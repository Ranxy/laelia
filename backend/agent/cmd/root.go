package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL       string
	insecure         bool
	allowHTTP        bool
	debug            bool
	force            bool
	noBrowser        bool
	setupForeground  bool
	daemonForeground bool
}

var rootCmd = &cobra.Command{
	Use:   "laelia-machine",
	Short: "Laelia Machine - host one or more agents and run their drain loops",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.managerURL, "manager", "https://localhost:8181", "manager server URL")
	rootCmd.PersistentFlags().BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	rootCmd.PersistentFlags().BoolVar(&flags.allowHTTP, "allow-http", false, "allow plain HTTP connections (insecure, dev only)")
	rootCmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "start in debug mode")
	rootCmd.PersistentFlags().BoolVar(&flags.force, "force", false, "wipe local machine state and register a brand-new machine (setup only)")
	rootCmd.PersistentFlags().BoolVar(&flags.noBrowser, "no-browser", false, "do not auto-open the approval URL in a browser (setup only)")

	// CLI subcommands render their own canonical Error:/Code: block to stderr,
	// and the run command surfaces real errors via main's logger. Silence
	// cobra's own usage/error printing in both cases.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
