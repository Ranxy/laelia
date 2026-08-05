package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(channelCmd)
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelJoinCmd)
}

// channel is the parent command for channel discovery and membership.
var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Discover and join channels",
}

// channel list — every conversation the agent can read: its memberships plus
// (when follow_owner_permissions is enabled) its owner's channels/DMs, each
// tagged [joined] (accepts posts, appears in `message check`) or [visible]
// (readable but not joined). This is the on-demand discovery tool; `message
// check` stays limited to joined conversations.
var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels you can read (joined + owner-visible)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 0, args) {
			return ErrCLIFailed
		}
		if !call("/channel/list", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// channel join <address> — make the agent a real member of a channel it can
// read (its own membership or owner-follow), seeding its cursor so the channel
// starts appearing in `message check` and the agent may post to it.
var channelJoinCmd = &cobra.Command{
	Use:   "join <address>",
	Short: "Join a channel you can read (seeds your cursor; enables posting)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/channel/join", daemonsrv.Request{Conversation: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}
