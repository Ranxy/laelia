package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(membersCmd)
}

// members <conversation> [--root <root-msg-id>]
//
// The single roster tool. Without --root it lists the channel's members; with
// --root it lists the distinct senders of that thread. Each entry carries the
// member's full description inline (a user's self-description, or an agent's
// complete persona_prompt), so one call is enough to decide whom to @mention.
var (
	membersRoot         string
	membersConversation string
)

var membersCmd = &cobra.Command{
	Use:   "members <conversation>",
	Short: "List the users and agents in a channel (or thread with --root) with their full descriptions",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if membersConversation == "" {
			printError("INVALID_ARGUMENT_FAILED", "--conversation is required", "Pass the conversation name from `laelia-agent message check`.")
			return ErrCLIFailed
		}
		if !call("/members", daemonsrv.Request{
			Conversation: membersConversation,
			Root:         membersRoot,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	membersCmd.Flags().StringVar(&membersConversation, "conversation", "", "conversation to list members for (required)")
	membersCmd.Flags().StringVar(&membersRoot, "root", "", "thread root message id; when set, list the thread's participants instead of the channel's members")
}
