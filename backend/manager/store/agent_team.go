package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// AgentTeamRole values mirror the laelia.v1.AgentTeamRole enum.
const (
	AgentTeamRoleLeader int16 = 1
	AgentTeamRoleMember int16 = 2
)

// AgentTeamMessage is the store representation of an agent team.
type AgentTeamMessage struct {
	ID            string
	ResourceID    string
	Title         string
	Description   string
	TeamPrompt    string
	LeaderAgentID int
	OwnerID       int
	CreatedBy     int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Deleted       bool
	Members       []*AgentTeamMemberMessage
}

// AgentTeamMemberMessage is one member row of an agent team.
type AgentTeamMemberMessage struct {
	TeamID          string
	AgentID         int
	Role            int16
	Responsibility  string
	JoinedAt        time.Time
	AgentResourceID string
}

// FindAgentTeamMessage is the message for finding agent teams.
type FindAgentTeamMessage struct {
	ID         *string
	ResourceID *string
	OwnerID    *int
	Limit      *int
	Offset     *int
}

// UpdateAgentTeamMessage is the message to update an agent team.
type UpdateAgentTeamMessage struct {
	Title         *string
	Description   *string
	TeamPrompt    *string
	LeaderAgentID *int
	Members       []*AgentTeamMemberMessage
}

// GetAgentTeamByID gets an agent team by its stable id.
func (s *Store) GetAgentTeamByID(ctx context.Context, id string) (*AgentTeamMessage, error) {
	teams, err := s.ListAgentTeams(ctx, &FindAgentTeamMessage{ID: &id})
	if err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return nil, nil
	}
	return teams[0], nil
}

// GetAgentTeamByResourceID gets an agent team by its resource id.
func (s *Store) GetAgentTeamByResourceID(ctx context.Context, resourceID string) (*AgentTeamMessage, error) {
	teams, err := s.ListAgentTeams(ctx, &FindAgentTeamMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return nil, nil
	}
	return teams[0], nil
}

// GetAgentTeamByName resolves an agent team by its resource name
// "agentTeams/{id}".
func (s *Store) GetAgentTeamByName(ctx context.Context, name string) (*AgentTeamMessage, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] != "agentTeams" || parts[1] == "" {
		return nil, errors.Errorf("invalid agent team name %q", name)
	}
	return s.GetAgentTeamByResourceID(ctx, parts[1])
}

// ListAgentTeams lists agent teams, optionally loading members.
func (s *Store) ListAgentTeams(ctx context.Context, find *FindAgentTeamMessage) ([]*AgentTeamMessage, error) {
	where, args := []string{"deleted = FALSE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.OwnerID; v != nil {
		where, args = append(where, fmt.Sprintf("owner_id = $%d", len(args)+1)), append(args, *v)
	}

	query := `SELECT
id, resource_id, name, description, team_prompt, leader_agent_id,
owner_id, created_by, created_at, updated_at, deleted
FROM agent_team WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at ASC`
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list agent teams")
	}
	defer rows.Close()

	var teams []*AgentTeamMessage
	for rows.Next() {
		var t AgentTeamMessage
		var leaderID sql.NullInt64
		if err := rows.Scan(
			&t.ID, &t.ResourceID, &t.Title, &t.Description, &t.TeamPrompt,
			&leaderID, &t.OwnerID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.Deleted,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan agent team")
		}
		if leaderID.Valid {
			t.LeaderAgentID = int(leaderID.Int64)
		}
		teams = append(teams, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate agent teams")
	}

	for _, t := range teams {
		members, err := s.ListAgentTeamMembers(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.Members = members
	}
	return teams, nil
}

// ListAgentTeamMembers lists the members of a team.
func (s *Store) ListAgentTeamMembers(ctx context.Context, teamID string) ([]*AgentTeamMemberMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
SELECT m.team_id, m.agent_id, a.resource_id, m.role, m.responsibility, m.joined_at
FROM agent_team_member m
JOIN agent a ON a.id = m.agent_id
WHERE m.team_id = $1 ORDER BY m.role, m.joined_at
`, teamID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list agent team members")
	}
	defer rows.Close()

	var members []*AgentTeamMemberMessage
	for rows.Next() {
		var m AgentTeamMemberMessage
		if err := rows.Scan(&m.TeamID, &m.AgentID, &m.AgentResourceID, &m.Role, &m.Responsibility, &m.JoinedAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan agent team member")
		}
		members = append(members, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate agent team members")
	}
	return members, nil
}

// GetAgentTeamByAgentID returns the team the given agent belongs to, or nil.
func (s *Store) GetAgentTeamByAgentID(ctx context.Context, agentID int) (*AgentTeamMessage, error) {
	var teamID string
	err := s.GetDB().QueryRowContext(ctx, `
SELECT team_id FROM agent_team_member WHERE agent_id = $1
`, agentID).Scan(&teamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to find agent team by agent")
	}
	return s.GetAgentTeamByID(ctx, teamID)
}

// CreateAgentTeam creates a team and its members in one transaction. The
// leader must be one of the members with role LEADER.
func (s *Store) CreateAgentTeam(ctx context.Context, team *AgentTeamMessage) (*AgentTeamMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin tx")
	}
	defer tx.Rollback()

	resourceID := team.ResourceID
	if resourceID == "" {
		resourceID = uuid.NewString()
	}
	if err := tx.QueryRowContext(ctx, `
INSERT INTO agent_team (resource_id, name, description, team_prompt, leader_agent_id, owner_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at, updated_at
`, resourceID, team.Title, team.Description, team.TeamPrompt, team.LeaderAgentID, team.OwnerID, team.CreatedBy).Scan(
		&team.ID, &team.CreatedAt, &team.UpdatedAt,
	); err != nil {
		return nil, errors.Wrap(err, "failed to create agent team")
	}
	team.ResourceID = resourceID

	if err := s.replaceTeamMembersTx(ctx, tx, team.ID, team.Members); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit")
	}
	team.Members, err = s.ListAgentTeamMembers(ctx, team.ID)
	if err != nil {
		return nil, err
	}
	return team, nil
}

// UpdateAgentTeam updates a team's scalar fields and optionally replaces its
// members.
func (s *Store) UpdateAgentTeam(ctx context.Context, id string, patch *UpdateAgentTeamMessage) (*AgentTeamMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin tx")
	}
	defer tx.Rollback()

	set, args := []string{}, []any{}
	if v := patch.Title; v != nil {
		set, args = append(set, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Description; v != nil {
		set, args = append(set, fmt.Sprintf("description = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.TeamPrompt; v != nil {
		set, args = append(set, fmt.Sprintf("team_prompt = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.LeaderAgentID; v != nil {
		set, args = append(set, fmt.Sprintf("leader_agent_id = $%d", len(args)+1)), append(args, *v)
	}
	if len(set) > 0 {
		args = append(args, id)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
UPDATE agent_team SET %s, updated_at = now() WHERE id = $%d
`, strings.Join(set, ", "), len(set)+1), args...); err != nil {
			return nil, errors.Wrap(err, "failed to update agent team")
		}
	}

	if patch.Members != nil {
		if err := s.replaceTeamMembersTx(ctx, tx, id, patch.Members); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit")
	}
	return s.GetAgentTeamByID(ctx, id)
}

// replaceTeamMembersTx deletes and re-inserts a team's members inside a
// transaction. It validates that exactly one leader is present and that no
// agent is already a member of another team.
func (s *Store) replaceTeamMembersTx(ctx context.Context, tx *sql.Tx, teamID string, members []*AgentTeamMemberMessage) error {
	if len(members) == 0 {
		return errors.New("agent team must have at least one member")
	}
	leaderCount := 0
	for _, m := range members {
		if m.Role == AgentTeamRoleLeader {
			leaderCount++
		}
	}
	if leaderCount != 1 {
		return errors.New("agent team must have exactly one leader")
	}

	// An agent can only belong to one team: reject any member already in
	// another team (excluding this team's existing rows).
	for _, m := range members {
		var existingTeamID string
		err := tx.QueryRowContext(ctx, `
SELECT team_id FROM agent_team_member
WHERE agent_id = $1 AND team_id <> $2
`, m.AgentID, teamID).Scan(&existingTeamID)
		if err == nil {
			return errors.Errorf("agent %d already belongs to another team", m.AgentID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return errors.Wrap(err, "failed to check agent team membership")
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_team_member WHERE team_id = $1`, teamID); err != nil {
		return errors.Wrap(err, "failed to clear agent team members")
	}
	for _, m := range members {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_team_member (team_id, agent_id, role, responsibility)
VALUES ($1, $2, $3, $4)
`, teamID, m.AgentID, m.Role, m.Responsibility); err != nil {
			return errors.Wrap(err, "failed to insert agent team member")
		}
	}
	return nil
}

// DeleteAgentTeam soft-deletes a team.
func (s *Store) DeleteAgentTeam(ctx context.Context, id string) error {
	res, err := s.GetDB().ExecContext(ctx, `
UPDATE agent_team SET deleted = TRUE, updated_at = now() WHERE id = $1 AND deleted = FALSE
`, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete agent team")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to read delete result")
	}
	if rows == 0 {
		return ErrAgentTeamNotFound
	}
	return nil
}

// TeamHasActiveTask reports whether the team is currently assigned to an
// active (TODO/IN_PROGRESS/IN_REVIEW) task.
func (s *Store) TeamHasActiveTask(ctx context.Context, teamID string) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
SELECT EXISTS (
SELECT 1 FROM task
WHERE assignee_team_id = $1 AND status IN (1,2,3)
)
`, teamID).Scan(&exists)
	if err != nil {
		return false, errors.Wrap(err, "failed to check team active task")
	}
	return exists, nil
}

// ErrAgentTeamNotFound is returned when an agent team does not exist.
var ErrAgentTeamNotFound = errors.New("agent team not found")
