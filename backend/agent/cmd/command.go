package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(commandCmd)
	commandCmd.AddCommand(commandContextCmd)
}

var commandCmd = &cobra.Command{
	Use:   "command",
	Short: "Inspect agent command execution context (LLM-facing)",
}

// command context [--command-id ID] — defaults to the session's command id.
var commandContextID string

var commandContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Get the execution context (instruction, reply, events) behind an agent reply",
	RunE: func(_ *cobra.Command, _ []string) error {
		if !call("/command/context", daemonsrv.Request{
			CommandID: commandContextID,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	commandContextCmd.Flags().StringVar(&commandContextID, "command-id", "", "command id to inspect (defaults to the current session's command)")
}
