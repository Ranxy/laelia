package cmd

import (
	"github.com/spf13/cobra"
)

var flags struct {
	managerURL string
	token      string
	insecure   bool
}

var rootCmd = &cobra.Command{
	Use:   "laelia-agent",
	Short: "Laelia Agent - execute tasks from the manager",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.managerURL, "manager", "http://localhost:8111", "manager server URL")
	rootCmd.PersistentFlags().StringVar(&flags.token, "token", "", "agent connection token (required)")
	rootCmd.PersistentFlags().BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	_ = rootCmd.MarkPersistentFlagRequired("token")
}
