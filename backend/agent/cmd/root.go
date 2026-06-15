package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL string
	token      string
	insecure   bool
	allowHTTP  bool
	acpConfig  string
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
	_ = rootCmd.MarkPersistentFlagRequired("token")
}
