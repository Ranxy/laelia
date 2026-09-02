package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// GetMyTeamInput is empty: the team is resolved from the calling agent.
type GetMyTeamInput struct{}

// GetMyTeam returns the calling agent's current team (at most one per agent).
func GetMyTeam(ctx context.Context, d Deps, _ GetMyTeamInput) (string, error) {
	if d.AgentTeamClient == nil {
		return "", localError("MISSING_TEAM_CLIENT", "agent team client is not configured", "")
	}
	resp, err := d.AgentTeamClient.GetMyAgentTeam(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return formatTeam(resp.Msg), nil
}

// GetTeamByIDInput carries the team resource id (the "agentTeams/{id}" tail).
type GetTeamByIDInput struct {
	TeamID string
}

// GetTeamByID returns a team by its resource id so an agent can inspect any
// team it has learned about (e.g. from a task assignment message).
func GetTeamByID(ctx context.Context, d Deps, in GetTeamByIDInput) (string, error) {
	if d.AgentTeamClient == nil {
		return "", localError("MISSING_TEAM_CLIENT", "agent team client is not configured", "")
	}
	if strings.TrimSpace(in.TeamID) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "team id is required", "Pass the team id, e.g. `laelia-machine team show <id>`.")
	}
	resp, err := d.AgentTeamClient.GetAgentTeam(ctx, connect.NewRequest(&v1pb.GetAgentTeamRequest{
		Name: "agentTeams/" + in.TeamID,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return formatTeam(resp.Msg), nil
}

// formatTeam renders a team's members and roles for the agent.
func formatTeam(t *v1pb.AgentTeam) string {
	var b strings.Builder
	_, _ = b.WriteString("Team: " + t.GetTitle() + "\n")
	_, _ = b.WriteString("Team ID: " + t.GetName() + "\n")
	if d := t.GetDescription(); d != "" {
		_, _ = b.WriteString("Description: " + d + "\n")
	}
	if p := t.GetTeamPrompt(); p != "" {
		_, _ = b.WriteString("Team Prompt: " + p + "\n")
	}
	_, _ = b.WriteString("Leader: " + teamMemberName(t, t.GetLeaderAgent()) + "\n")
	_, _ = b.WriteString("Members:\n")
	for _, m := range t.GetMembers() {
		role := "member"
		if m.GetRole() == v1pb.AgentTeamRole_AGENT_TEAM_ROLE_LEADER {
			role = "leader"
		}
		line := fmt.Sprintf("- %s (%s)", teamMemberName(t, m.GetAgent()), role)
		if m.GetResponsibility() != "" {
			line += ": " + m.GetResponsibility()
		}
		_, _ = b.WriteString(line + "\n")
	}
	return b.String()
}

func teamMemberName(t *v1pb.AgentTeam, agentName string) string {
	for _, m := range t.GetMembers() {
		if m.GetAgent() == agentName {
			return agentName
		}
	}
	return agentName
}
