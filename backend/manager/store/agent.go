package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

var agentDeleteTrue = true

type AgentMessage struct {
	ID           int
	ResourceID   string
	Name         string
	TokenVersion int
	CreatedAt    time.Time
	Deleted      bool
	Info         *models.AgentInfo
	Status       *models.AgentStatus
}

type FindAgentMessage struct {
	ID          *int
	ResourceID  *string
	ShowDeleted bool
	Limit       *int
	Offset      *int
}

type UpdateAgentMessage struct {
	ResourceID   *string
	Name         *string
	Info         *models.AgentInfo
	Status       *models.AgentStatus
	TokenVersion *int
	Delete       *bool
}

func (s *Store) GetAgent(ctx context.Context, id int) (*AgentMessage, error) {
	if v, ok := s.agentIDCache.Get(id); ok && s.enableCache {
		return v, nil
	}

	if err := s.listAndCacheAllAgents(ctx); err != nil {
		return nil, err
	}

	agent, _ := s.agentIDCache.Get(id)
	return agent, nil
}

func (s *Store) GetAgentByResourceID(ctx context.Context, resourceID string) (*AgentMessage, error) {
	if v, ok := s.agentResourceIDCache.Get(resourceID); ok && s.enableCache {
		return v, nil
	}

	if err := s.listAndCacheAllAgents(ctx); err != nil {
		return nil, err
	}

	agent, _ := s.agentResourceIDCache.Get(resourceID)
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context, find *FindAgentMessage) ([]*AgentMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	agents, err := listAgentImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, agent := range agents {
		s.agentIDCache.Add(agent.ID, agent)
		s.agentResourceIDCache.Add(agent.ResourceID, agent)
	}
	return agents, nil
}

func (s *Store) listAndCacheAllAgents(ctx context.Context) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	agents, err := listAgentImpl(ctx, tx, &FindAgentMessage{ShowDeleted: true})
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	for _, agent := range agents {
		s.agentIDCache.Add(agent.ID, agent)
		s.agentResourceIDCache.Add(agent.ResourceID, agent)
	}
	return nil
}

func listAgentImpl(ctx context.Context, txn *sql.Tx, find *FindAgentMessage) ([]*AgentMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("agent.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("agent.resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("agent.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := `SELECT
		agent.id,
		agent.resource_id,
		agent.name,
		agent.token_version,
		agent.created_at,
		agent.deleted,
		agent.info,
		agent.status
	FROM agent
	WHERE ` + strings.Join(where, " AND ") + ` ORDER BY agent.created_at ASC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var agentMessages []*AgentMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var agentMessage AgentMessage
		var infoBytes []byte
		var statusBytes []byte
		if err := rows.Scan(
			&agentMessage.ID,
			&agentMessage.ResourceID,
			&agentMessage.Name,
			&agentMessage.TokenVersion,
			&agentMessage.CreatedAt,
			&agentMessage.Deleted,
			&infoBytes,
			&statusBytes,
		); err != nil {
			return nil, err
		}

		info := &models.AgentInfo{}
		if err := json.Unmarshal(infoBytes, info); err != nil {
			return nil, err
		}
		agentMessage.Info = info

		status := &models.AgentStatus{}
		if err := json.Unmarshal(statusBytes, status); err != nil {
			return nil, err
		}
		agentMessage.Status = status

		agentMessages = append(agentMessages, &agentMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return agentMessages, nil
}

func (s *Store) CreateAgent(ctx context.Context, create *AgentMessage) (*AgentMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if create.Info == nil {
		create.Info = &models.AgentInfo{}
	}
	infoBytes, err := json.Marshal(create.Info)
	if err != nil {
		return nil, err
	}

	if create.Status == nil {
		create.Status = &models.AgentStatus{}
	}
	statusBytes, err := json.Marshal(create.Status)
	if err != nil {
		return nil, err
	}

	resourceID := uuid.New().String()

	var agentID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO agent (
			resource_id, name, token_version, info, status
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`,
		resourceID,
		create.Name,
		create.TokenVersion,
		infoBytes,
		statusBytes,
	).Scan(&agentID, &create.CreatedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	agent := &AgentMessage{
		ID:           agentID,
		ResourceID:   resourceID,
		Name:         create.Name,
		TokenVersion: create.TokenVersion,
		CreatedAt:    create.CreatedAt,
		Info:         create.Info,
		Status:       create.Status,
	}
	s.agentIDCache.Add(agent.ID, agent)
	s.agentResourceIDCache.Add(agent.ResourceID, agent)
	return agent, nil
}

func (s *Store) UpdateAgent(ctx context.Context, current *AgentMessage, patch *UpdateAgentMessage) (*AgentMessage, error) {
	sets, args := []string{}, []any{}
	if v := patch.ResourceID; v != nil {
		sets, args = append(sets, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Name; v != nil {
		sets, args = append(sets, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Info; v != nil {
		infoBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("info = $%d", len(args)+1)), append(args, infoBytes)
	}
	if v := patch.Status; v != nil {
		statusBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("status = $%d", len(args)+1)), append(args, statusBytes)
	}
	if v := patch.TokenVersion; v != nil {
		sets, args = append(sets, fmt.Sprintf("token_version = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Delete; v != nil {
		sets, args = append(sets, fmt.Sprintf("deleted = $%d", len(args)+1)), append(args, *v)
	}

	if len(sets) == 0 {
		return current, nil
	}

	args = append(args, current.ID)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE agent
		SET `+strings.Join(sets, ", ")+`
		WHERE id = $%d
	`, len(args)),
		args...,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.agentIDCache.Remove(current.ID)
	s.agentResourceIDCache.Remove(current.ResourceID)
	agent, err := s.GetAgent(ctx, current.ID)
	if err != nil {
		return nil, err
	}

	s.agentIDCache.Add(agent.ID, agent)
	s.agentResourceIDCache.Add(agent.ResourceID, agent)
	return agent, nil
}

func (s *Store) DeleteAgent(ctx context.Context, resourceID string) error {
	agent, err := s.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return err
	}
	if agent == nil {
		return errors.Errorf("agent %s not found", resourceID)
	}

	if _, err := s.UpdateAgent(ctx, agent, &UpdateAgentMessage{Delete: &agentDeleteTrue}); err != nil {
		return err
	}
	return nil
}
