package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(teamCmd)
	teamCmd.AddCommand(teamGetCmd, teamShowCmd)
}

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Inspect your agent team and its members (LLM-facing, used during drain sessions)",
}

// team get — the calling agent's current team (at most one per agent).
var teamGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the team you belong to, with all members, roles and responsibilities",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 0, args) {
			return ErrCLIFailed
		}
		if !call("/team/get", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// team show <team-id> — inspect any team by its resource id (from a task
// assignment message, e.g. "agentTeams/abc").
var teamShowCmd = &cobra.Command{
	Use:   "show <team-id>",
	Short: "Show a team by id, with members, roles and responsibilities",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/team/show", daemonsrv.Request{TeamID: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}
