package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL string
	token      string
	insecure   bool
	allowHTTP  bool
	debug      bool
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
	rootCmd.PersistentFlags().StringVar(&flags.token, "token", "", "machine registration token (required for `run`)")
	rootCmd.PersistentFlags().BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	rootCmd.PersistentFlags().BoolVar(&flags.allowHTTP, "allow-http", false, "allow plain HTTP connections (insecure, dev only)")
	rootCmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "start in debug mode")

	// --token is validated only for the run command (in runMachine); the
	// LLM-facing `message` / `command` subcommands authenticate via env vars the
	// daemon injected, so it must not be a global required flag.

	// CLI subcommands render their own canonical Error:/Code: block to stderr,
	// and the run command surfaces real errors via main's logger. Silence
	// cobra's own usage/error printing in both cases.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
