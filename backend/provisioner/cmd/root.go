// Package cmd implements the laelia-provisioner command line: flag parsing,
// yaml config load, and the run command that drives the manager client loop.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/provisioner/version"
)

var flags struct {
	manager   string
	token     string
	backend   string
	config    string
	insecure  bool
	allowHTTP bool
	debug     bool
	version   bool
}

var rootCmd = &cobra.Command{
	Use:   "laelia-provisioner",
	Short: "Laelia Provisioner - creates machine workloads in one virtualization backend on demand",
	Run: func(cmd *cobra.Command, _ []string) {
		if flags.version {
			printVersion()
			return
		}
		_ = cmd.Help()
	},
}

// Execute runs the root command; subcommand errors carry their own message.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flags.manager, "manager", "", "manager server URL (overrides the config file)")
	rootCmd.PersistentFlags().StringVar(&flags.token, "token", "", "provisioner token (overrides the config file)")
	rootCmd.PersistentFlags().StringVar(&flags.backend, "backend", "", "workload backend name (overrides the config file)")
	rootCmd.PersistentFlags().StringVar(&flags.config, "config", "", "path to the provisioner config file (yaml)")
	rootCmd.PersistentFlags().BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	rootCmd.PersistentFlags().BoolVar(&flags.allowHTTP, "allow-http", false, "allow plain HTTP connections (insecure, dev only)")
	rootCmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "log debug output")
	rootCmd.PersistentFlags().BoolVar(&flags.version, "version", false, "print version, git commit, and build time")

	// The run command surfaces real errors via main's logger; silence cobra's
	// own usage/error printing.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}

func printVersion() {
	fmt.Printf("version: %s\n", version.Version)
	fmt.Printf("git commit: %s\n", version.GitCommit)
	fmt.Printf("build time: %s\n", version.BuildTime)
}
