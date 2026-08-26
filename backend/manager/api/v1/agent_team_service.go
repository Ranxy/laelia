package v1

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// AgentTeamService implements the agent team service. A team belongs to its
// creator (owner) and is visible to other users; only the owner or a workspace
// admin may manage it.
type AgentTeamService struct {
	v1connect.UnimplementedAgentTeamServiceHandler
	store      *store.Store
	iam        *iam.Manager
	dispatcher *dispatcher.Dispatcher
}

// NewAgentTeamService returns a new AgentTeamService.
func NewAgentTeamService(s *store.Store, iamManager *iam.Manager, d *dispatcher.Dispatcher) *AgentTeamService {
	return &AgentTeamService{store: s, iam: iamManager, dispatcher: d}
}

// GetAgentTeam gets a team.
func (s *AgentTeamService) GetAgentTeam(ctx context.Context, req *connect.Request[v1pb.GetAgentTeamRequest]) (*connect.Response[v1pb.AgentTeam], error) {
	team, err := s.store.GetAgentTeamByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent team"))
	}
	if team == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent team %q not found", req.Msg.Name))
	}
	canManage, err := s.callerCanManage(ctx, team)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.convertToV1AgentTeam(ctx, team, canManage)), nil
}

// GetMyAgentTeam returns the calling agent's current team, or NotFound when
// the agent is not a member of any team.
func (s *AgentTeamService) GetMyAgentTeam(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.AgentTeam], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}
	team, err := s.store.GetAgentTeamByAgentID(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent team"))
	}
	if team == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("agent is not a member of any team"))
	}
	return connect.NewResponse(s.convertToV1AgentTeam(ctx, team, false)), nil
}

// ListAgentTeams lists all teams (visible to all authenticated users).
func (s *AgentTeamService) ListAgentTeams(ctx context.Context, req *connect.Request[v1pb.ListAgentTeamsRequest]) (*connect.Response[v1pb.ListAgentTeamsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	teams, err := s.store.ListAgentTeams(ctx, &store.FindAgentTeamMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list agent teams"))
	}

	nextPageToken := ""
	if len(teams) == limitPlusOne {
		teams = teams[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, err
		}
	}

	response := &v1pb.ListAgentTeamsResponse{NextPageToken: nextPageToken}
	for _, team := range teams {
		canManage, err := s.callerCanManage(ctx, team)
		if err != nil {
			return nil, err
		}
		response.AgentTeams = append(response.AgentTeams, s.convertToV1AgentTeam(ctx, team, canManage))
	}
	return connect.NewResponse(response), nil
}

// CreateAgentTeam creates a team. The caller becomes the owner. Only agents
// whose owner is the caller (or a workspace admin) may be added.
func (s *AgentTeamService) CreateAgentTeam(ctx context.Context, req *connect.Request[v1pb.CreateAgentTeamRequest]) (*connect.Response[v1pb.AgentTeam], error) {
	if req.Msg.AgentTeam == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent_team is required"))
	}
	if strings.TrimSpace(req.Msg.AgentTeam.Title) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent team title is required"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	members, err := s.validateAndConvertMembers(ctx, user, req.Msg.AgentTeam.Members)
	if err != nil {
		return nil, err
	}
	if err := s.validateLeaderInMembers(ctx, req.Msg.AgentTeam.LeaderAgent, members); err != nil {
		return nil, err
	}

	team, err := s.store.CreateAgentTeam(ctx, &store.AgentTeamMessage{
		Title:         req.Msg.AgentTeam.Title,
		Description:   req.Msg.AgentTeam.Description,
		TeamPrompt:    req.Msg.AgentTeam.TeamPrompt,
		LeaderAgentID: leaderAgentID(members),
		OwnerID:       user.ID,
		CreatedBy:     user.ID,
		Members:       members,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create agent team"))
	}
	return connect.NewResponse(s.convertToV1AgentTeam(ctx, team, true)), nil
}

// UpdateAgentTeam updates a team. The team owner or a workspace admin may
// update.
func (s *AgentTeamService) UpdateAgentTeam(ctx context.Context, req *connect.Request[v1pb.UpdateAgentTeamRequest]) (*connect.Response[v1pb.AgentTeam], error) {
	if req.Msg.AgentTeam == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent_team is required"))
	}
	team, err := s.store.GetAgentTeamByName(ctx, req.Msg.AgentTeam.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent team"))
	}
	if team == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent team %q not found", req.Msg.AgentTeam.Name))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	canManage, err := s.canManageTeam(ctx, user, team)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check agent team permission"))
	}
	if !canManage {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the team owner or a workspace admin can update this team"))
	}

	mask := req.Msg.UpdateMask
	if mask == nil || len(mask.Paths) == 0 {
		mask = &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "team_prompt", "leader_agent", "members"}}
	}
	patch := &store.UpdateAgentTeamMessage{}
	for _, path := range mask.Paths {
		switch path {
		case "title":
			if strings.TrimSpace(req.Msg.AgentTeam.Title) == "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent team title must not be empty"))
			}
			patch.Title = &req.Msg.AgentTeam.Title
		case "description":
			patch.Description = &req.Msg.AgentTeam.Description
		case "team_prompt":
			patch.TeamPrompt = &req.Msg.AgentTeam.TeamPrompt
		case "leader_agent", "members":
			members, err := s.validateAndConvertMembers(ctx, user, req.Msg.AgentTeam.Members)
			if err != nil {
				return nil, err
			}
			if err := s.validateLeaderInMembers(ctx, req.Msg.AgentTeam.LeaderAgent, members); err != nil {
				return nil, err
			}
			patch.Members = members
			leaderID := leaderAgentID(members)
			patch.LeaderAgentID = &leaderID
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}

	updated, err := s.store.UpdateAgentTeam(ctx, team.ID, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update agent team"))
	}
	// A team_prompt change is part of the agent's system prompt, so push a
	// release notice to every member so running agents perceive it on the
	// current or next turn.
	if patch.TeamPrompt != nil && s.dispatcher != nil {
		for _, member := range updated.Members {
			if member == nil {
				continue
			}
			if pushErr := s.dispatcher.PushPromptReleaseNotice(ctx, member.AgentID); pushErr != nil {
				slog.Info("best-effort prompt release notice push skipped for team member", "agentID", member.AgentID, "error", pushErr)
			}
		}
	}
	canManage, err = s.callerCanManage(ctx, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.convertToV1AgentTeam(ctx, updated, canManage)), nil
}

// DeleteAgentTeam deletes a team. The team owner or a workspace admin may
// delete.
func (s *AgentTeamService) DeleteAgentTeam(ctx context.Context, req *connect.Request[v1pb.DeleteAgentTeamRequest]) (*connect.Response[emptypb.Empty], error) {
	team, err := s.store.GetAgentTeamByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent team"))
	}
	if team == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent team %q not found", req.Msg.Name))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	canManage, err := s.canManageTeam(ctx, user, team)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check agent team permission"))
	}
	if !canManage {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the team owner or a workspace admin can delete this team"))
	}
	if err := s.store.DeleteAgentTeam(ctx, team.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete agent team"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// canManageTeam reports whether the caller may manage the team: the team owner
// or a workspace admin.
func (s *AgentTeamService) canManageTeam(ctx context.Context, user *store.UserMessage, team *store.AgentTeamMessage) (bool, error) {
	if user == nil {
		return false, nil
	}
	if team.OwnerID == user.ID {
		return true, nil
	}
	return isUserWorkspaceAdmin(ctx, s.store, user)
}

// callerCanManage resolves whether the current caller may manage the team, for
// the OUTPUT_ONLY can_manage field.
func (s *AgentTeamService) callerCanManage(ctx context.Context, team *store.AgentTeamMessage) (bool, error) {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return false, nil
	}
	return s.canManageTeam(ctx, user, team)
}

// validateAndConvertMembers checks that every member is an existing, non-deleted
// agent owned by the caller (or a workspace admin), and converts to store rows.
func (s *AgentTeamService) validateAndConvertMembers(ctx context.Context, user *store.UserMessage, members []*v1pb.AgentTeamMember) ([]*store.AgentTeamMemberMessage, error) {
	if len(members) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent team must have at least one member"))
	}
	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin"))
	}
	seen := make(map[string]bool, len(members))
	var out []*store.AgentTeamMemberMessage
	for _, m := range members {
		if m.GetAgent() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent team member must not be empty"))
		}
		agentResID, err := common.GetAgentResourceID(m.GetAgent())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid agent team member %q", m.GetAgent()))
		}
		if seen[agentResID] {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("duplicate agent team member %q", m.GetAgent()))
		}
		seen[agentResID] = true
		agent, err := s.store.GetAgentByResourceID(ctx, agentResID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up agent team member %q", m.GetAgent()))
		}
		if agent == nil || agent.Deleted {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("agent team member %q does not exist or is deleted", m.GetAgent()))
		}
		if !isAdmin && agent.OwnerID != user.ID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only the owner of agent %q can add it to a team", m.GetAgent()))
		}
		role := store.AgentTeamRoleMember
		switch m.GetRole() {
		case v1pb.AgentTeamRole_AGENT_TEAM_ROLE_LEADER:
			role = store.AgentTeamRoleLeader
		case v1pb.AgentTeamRole_AGENT_TEAM_ROLE_MEMBER:
			role = store.AgentTeamRoleMember
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported agent team member role %v", m.GetRole()))
		}
		out = append(out, &store.AgentTeamMemberMessage{
			AgentID:        agent.ID,
			Role:           role,
			Responsibility: m.GetResponsibility(),
		})
	}
	return out, nil
}

// validateLeaderInMembers ensures the leader_agent field names one of the
// members and that exactly one member has role LEADER.
func (s *AgentTeamService) validateLeaderInMembers(ctx context.Context, leaderAgent string, members []*store.AgentTeamMemberMessage) error {
	if leaderAgent == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("agent team must have a leader"))
	}
	leaderResID, err := common.GetAgentResourceID(leaderAgent)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid leader_agent"))
	}
	leaderCount := 0
	for _, m := range members {
		if m.Role == store.AgentTeamRoleLeader {
			leaderCount++
		}
	}
	if leaderCount != 1 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("agent team must have exactly one leader"))
	}
	// The leader_agent field must match the member marked LEADER.
	for _, m := range members {
		if m.Role == store.AgentTeamRoleLeader {
			agent, err := s.store.GetAgent(ctx, m.AgentID)
			if err != nil || agent == nil {
				return connect.NewError(connect.CodeInternal, errors.New("failed to resolve leader agent"))
			}
			if agent.ResourceID != leaderResID {
				return connect.NewError(connect.CodeInvalidArgument, errors.New("leader_agent must match the member with LEADER role"))
			}
		}
	}
	return nil
}

// leaderAgentID returns the agent id of the LEADER member.
func leaderAgentID(members []*store.AgentTeamMemberMessage) int {
	for _, m := range members {
		if m.Role == store.AgentTeamRoleLeader {
			return m.AgentID
		}
	}
	return 0
}

// convertToV1AgentTeam maps a store team to the v1 API shape.
func (s *AgentTeamService) convertToV1AgentTeam(ctx context.Context, team *store.AgentTeamMessage, canManage bool) *v1pb.AgentTeam {
	out := &v1pb.AgentTeam{
		Name:        common.FormatAgentTeamName(team.ResourceID),
		Title:       team.Title,
		Description: team.Description,
		TeamPrompt:  team.TeamPrompt,
		CanManage:   canManage,
		Owner:       resolveUserResource(ctx, s.store, team.OwnerID),
	}
	for _, m := range team.Members {
		role := v1pb.AgentTeamRole_AGENT_TEAM_ROLE_UNSPECIFIED
		switch m.Role {
		case store.AgentTeamRoleLeader:
			role = v1pb.AgentTeamRole_AGENT_TEAM_ROLE_LEADER
		case store.AgentTeamRoleMember:
			role = v1pb.AgentTeamRole_AGENT_TEAM_ROLE_MEMBER
		}
		agentName := common.FormatAgentUID(m.AgentResourceID)
		out.Members = append(out.Members, &v1pb.AgentTeamMember{
			Agent:          agentName,
			Role:           role,
			Responsibility: m.Responsibility,
		})
		if m.Role == store.AgentTeamRoleLeader {
			out.LeaderAgent = agentName
		}
	}
	return out
}
