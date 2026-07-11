package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(membersCmd)
	membersCmd.Flags().StringVar(&membersRoot, "root", "", "thread root message id; when set, list the thread's participants instead of the channel's members")
}

// members <conversation> [--root <root-msg-id>]
//
// The single roster tool. Without --root it lists the channel's members; with
// --root it lists the distinct senders of that thread. The conversation is a
// positional argument, matching `message read` / `thread read` / `task list` so
// an agent shells out the same way for every conversation-scoped command. Each
// entry carries the member's full description inline (a user's self-description,
// or an agent's complete persona_prompt), so one call is enough to decide whom
// to @mention.
var membersRoot string

var membersCmd = &cobra.Command{
	Use:   "members <conversation>",
	Short: "List the users and agents in a channel (or thread with --root) with their full descriptions",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/members", daemonsrv.Request{
			Conversation: args[0],
			Root:         membersRoot,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}
