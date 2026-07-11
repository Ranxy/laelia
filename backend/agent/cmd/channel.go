package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(channelCmd)
	channelCmd.AddCommand(channelMembersCmd)
}

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Inspect a channel's members so you can decide whom to address (LLM-facing, used during drain sessions)",
}

// channel members <conversation>
var channelMembersCmd = &cobra.Command{
	Use:   "members <conversation>",
	Short: "List the users and agents in a channel with their short descriptions",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if channelMembersConversation == "" {
			printError("INVALID_ARGUMENT_FAILED", "--conversation is required", "Pass the conversation name from `laelia-agent message check`.")
			return ErrCLIFailed
		}
		if !call("/channel/members", daemonsrv.Request{
			Conversation: channelMembersConversation,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

var channelMembersConversation string

func init() {
	channelMembersCmd.Flags().StringVar(&channelMembersConversation, "conversation", "", "conversation to list members for (required)")
}
