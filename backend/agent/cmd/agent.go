package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentListCmd)
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Discover peer agents (LLM-facing, used to delegate work via dm:@<peer>)",
}

// agent list — the global peer-agent roster. It lists every other agent (the
// caller excluded) with its display name, agents/<id> handle, connection state,
// and full persona_prompt, so one call is enough to pick a peer to delegate to
// and understand how it works. Takes no argument: it spans every agent, not one
// conversation (use `members <conversation>` for a channel's roster).
var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every peer agent with display name, handle, connection state, and full persona",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if !call("/agent/list", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}
