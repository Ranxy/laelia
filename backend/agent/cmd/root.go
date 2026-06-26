package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL string
	token      string
	insecure   bool
	allowHTTP  bool
	agentName  string
}

var rootCmd = &cobra.Command{
	Use:   "laelia-agent",
	Short: "Laelia Agent - execute tasks from the manager",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.managerURL, "manager", "https://localhost:8181", "manager server URL")
	rootCmd.PersistentFlags().StringVar(&flags.token, "token", "", "agent connection token (required)")
	rootCmd.PersistentFlags().BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	rootCmd.PersistentFlags().BoolVar(&flags.allowHTTP, "allow-http", false, "allow plain HTTP connections (insecure, dev only)")
	rootCmd.PersistentFlags().StringVar(&flags.agentName, "agent-name", "", "agent resource name used for API routing")

	// --token is validated only for the daemon command (in runDaemon); the
	// LLM-facing `message` / `command` subcommands authenticate via env vars the
	// daemon injected, so it must not be a global required flag.

	// CLI subcommands render their own canonical Error:/Code: block to stderr,
	// and the daemon command surfaces real errors via main's logger. Silence
	// cobra's own usage/error printing in both cases.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
