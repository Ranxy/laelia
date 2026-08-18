package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Ranxy/laelia/backend/agent/version"
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
	version          bool
}

var rootCmd = &cobra.Command{
	Use:   "laelia-machine",
	Short: "Laelia Machine - host one or more agents and run their drain loops",
	Run: func(cmd *cobra.Command, _ []string) {
		if flags.version {
			printVersion()
			return
		}
		_ = cmd.Help()
	},
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
	rootCmd.PersistentFlags().BoolVar(&flags.version, "version", false, "print version, git commit, and build time")

	// CLI subcommands render their own canonical Error:/Code: block to stderr,
	// and the run command surfaces real errors via main's logger. Silence
	// cobra's own usage/error printing in both cases.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}

func printVersion() {
	fmt.Printf("version: %s\n", version.Version)
	fmt.Printf("git commit: %s\n", version.GitCommit)
	fmt.Printf("build time: %s\n", version.BuildTime)
}
