package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(channelCmd)
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelJoinCmd)
	channelCmd.AddCommand(channelLeaveCmd)
	channelCmd.AddCommand(channelAddMemberCmd)
	channelCmd.AddCommand(channelRemoveMemberCmd)
}

// channel is the parent command for channel discovery and membership.
var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Discover, join, leave, and manage channels",
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

// channel leave <address> — remove the agent from a channel it is a member of.
// The channel stops appearing in `message check` and the agent can no longer
// post; rejoin with `channel join` if it can still read the channel.
var channelLeaveCmd = &cobra.Command{
	Use:   "leave <address>",
	Short: "Leave a channel you are a member of",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/channel/leave", daemonsrv.Request{Conversation: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// channel add-member <address> <member>... — add members (users or agents) to a
// channel the agent manages. The manager enforces the same rules as a user
// adding members: the caller must be a channel Admin/Owner (or an agent whose
// owner is a channel Admin/Owner with can_manage_channel_members enabled), and a
// private agent (allow_add_to_channel=false) cannot be added by another agent.
var channelAddMemberCmd = &cobra.Command{
	Use:   "add-member <address> <member>...",
	Short: "Add members to a channel you manage (Admin/Owner)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireMinArgs(cmd, 2, args) {
			return ErrCLIFailed
		}
		if !call("/channel/add-member", daemonsrv.Request{Conversation: args[0], Members: args[1:]}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// channel remove-member <address> <member>... — remove members (users or agents)
// from a channel the agent manages, under the same conversations.manageMembers
// rule as adding.
var channelRemoveMemberCmd = &cobra.Command{
	Use:   "remove-member <address> <member>...",
	Short: "Remove members from a channel you manage (Admin/Owner)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireMinArgs(cmd, 2, args) {
			return ErrCLIFailed
		}
		if !call("/channel/remove-member", daemonsrv.Request{Conversation: args[0], Members: args[1:]}) {
			return ErrCLIFailed
		}
		return nil
	},
}
