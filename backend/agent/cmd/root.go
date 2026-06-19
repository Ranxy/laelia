package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL      string
	token           string
	insecure        bool
	allowHTTP       bool
	acpConfig       string
	acpConfigServer bool
	agentName       string
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
	rootCmd.PersistentFlags().StringVar(&flags.acpConfig, "acp-config", "", "path to ACP profile config file (defaults to ~/.laelia/acp.yaml if present)")
	rootCmd.PersistentFlags().BoolVar(&flags.acpConfigServer, "acp-config-server", false, "use server-provided ACP config instead of local file (takes precedence over --acp-config)")
	rootCmd.PersistentFlags().StringVar(&flags.agentName, "agent-name", "", "agent resource name used for API routing")
	_ = rootCmd.MarkPersistentFlagRequired("token")
}
