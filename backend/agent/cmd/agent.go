package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentDetailCmd)
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Inspect a co-member agent's profile (LLM-facing, used during drain sessions)",
}

// agent detail --conversation C --agent agents/<id>
var (
	agentDetailConversation string
	agentDetailAgent        string
)

var agentDetailCmd = &cobra.Command{
	Use:   "detail",
	Short: "Fetch a co-member agent's full profile (title, status, persona_prompt)",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if agentDetailConversation == "" {
			printError("INVALID_ARGUMENT_FAILED", "--conversation is required", "Pass the conversation name from `laelia-agent message check`.")
			return ErrCLIFailed
		}
		if agentDetailAgent == "" {
			printError("INVALID_ARGUMENT_FAILED", "--agent is required", "Pass the agents/<id> handle from `channel members` / `thread participants`.")
			return ErrCLIFailed
		}
		if !call("/agent/profile", daemonsrv.Request{
			Conversation: agentDetailConversation,
			TargetAgent:  agentDetailAgent,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	agentDetailCmd.Flags().StringVar(&agentDetailConversation, "conversation", "", "conversation the target agent is a member of (required)")
	agentDetailCmd.Flags().StringVar(&agentDetailAgent, "agent", "", "target agent's agents/<id> handle (required, from `channel members` / `thread participants`)")
}
