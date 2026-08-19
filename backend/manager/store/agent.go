package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

var (
	agentDeleteTrue   = true
	agentEnabledTrue  = true
	agentEnabledFalse = false
)

type AgentMessage struct {
	ID         int
	ResourceID string
	Name       string
	// Description is the public agent intro shown to other users and agents
	// (what the agent is responsible for, its role). It is NOT injected into the
	// agent's own prompt — persona_prompt in Info.AcpConfig is the private self
	// prompt that defines the agent to itself.
	Description  string
	TokenVersion int
	CreatedAt    time.Time
	Deleted      bool
	// Enabled reports whether the agent is running. When false the agent has
	// been stopped (StopAgent): its machine runner is torn down and it
	// processes no session messages until StartAgent.
	Enabled            bool
	Info               *models.AgentInfo
	Status             *models.AgentStatus
	LastTokenRotatedAt time.Time
	// CreatedBy is the principal id of the user who created the agent (0 =
	// unknown/legacy). Display-only: it never authorizes anything (OwnerID
	// does). Immutable after creation.
	CreatedBy int
	// OwnerID is the principal id of the agent's owner (authorization
	// authority; 0 = unknown/legacy). Gates profile mutations and channel-adds
	// to the owner or a workspace admin, and defaults to the creator on
	// creation. Transferable via TransferAgentOwnership.
	OwnerID int
	// AllowAddToChannel reports whether other users may add this agent to a
	// channel. False (default) restricts adds to the agent's owner or a
	// workspace admin.
	AllowAddToChannel bool
	// FollowOwnerPermissions reports whether the agent inherits its owner's
	// channel read access (channels/DMs the owner can read). True (default):
	// the agent can read and proactively join anything its owner can see.
	FollowOwnerPermissions bool
	// CanManageChannelMembers reports whether the agent may add/remove members
	// in a channel where its owner is a channel Admin/Owner. True (default):
	// the agent acts on its owner's behalf for member management. Independent
	// of FollowOwnerPermissions (which controls read visibility).
	CanManageChannelMembers bool
	// AvatarS3Key is the S3 object key of the agent's uploaded avatar image,
	// empty when the agent has not uploaded one.
	AvatarS3Key string
	// MachineID is the id of the machine this agent is bound to. 0 = unbound
	// (legacy agents created before the machine refactor). Immutable after
	// creation; set by CreateAgent.
	MachineID int
	// MachineResourceID is the resource id (uuid) of the bound machine, populated
	// via a LEFT JOIN on read so converters can emit the machines/{machine} name
	// without an N+1 lookup. Empty for unbound/legacy agents.
	MachineResourceID string
}

// GetResourceID returns the agent's resource name, used to key context-derived
// identifiers such as per-agent rate-limit buckets (heartbeat and agent API).
func (m *AgentMessage) GetResourceID() string {
	return m.ResourceID
}

type FindAgentMessage struct {
	ID          *int
	ResourceID  *string
	MachineID   *int
	ShowDeleted bool
	Limit       *int
	Offset      *int
}

type UpdateAgentMessage struct {
	ResourceID              *string
	Name                    *string
	Description             *string
	Info                    *models.AgentInfo
	Status                  *models.AgentStatus
	TokenVersion            *int
	LastTokenRotatedAt      *time.Time
	Delete                  *bool
	AvatarS3Key             *string
	AllowAddToChannel       *bool
	FollowOwnerPermissions  *bool
	CanManageChannelMembers *bool
	OwnerID                 *int
	Enabled                 *bool
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
	if v := find.MachineID; v != nil {
		where, args = append(where, fmt.Sprintf("agent.machine_id = $%d", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("agent.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := `SELECT
		agent.id,
		agent.resource_id,
		agent.name,
		agent.description,
		agent.token_version,
		agent.created_at,
		agent.deleted,
		agent.info,
		agent.status,
		agent.last_token_rotated_at,
		agent.created_by,
		agent.owner_id,
		agent.allow_add_to_channel,
		agent.follow_owner_permissions,
		agent.can_manage_channel_members,
		agent.enabled,
		agent.avatar_s3_key,
		agent.machine_id,
		machine.resource_id
	FROM agent
	LEFT JOIN machine ON machine.id = agent.machine_id
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
		var lastTokenRotatedAt sql.NullTime
		var machineID sql.NullInt64
		var machineResourceID sql.NullString
		if err := rows.Scan(
			&agentMessage.ID,
			&agentMessage.ResourceID,
			&agentMessage.Name,
			&agentMessage.Description,
			&agentMessage.TokenVersion,
			&agentMessage.CreatedAt,
			&agentMessage.Deleted,
			&infoBytes,
			&statusBytes,
			&lastTokenRotatedAt,
			&agentMessage.CreatedBy,
			&agentMessage.OwnerID,
			&agentMessage.AllowAddToChannel,
			&agentMessage.FollowOwnerPermissions,
			&agentMessage.CanManageChannelMembers,
			&agentMessage.Enabled,
			&agentMessage.AvatarS3Key,
			&machineID,
			&machineResourceID,
		); err != nil {
			return nil, err
		}
		if lastTokenRotatedAt.Valid {
			agentMessage.LastTokenRotatedAt = lastTokenRotatedAt.Time
		}
		if machineID.Valid {
			agentMessage.MachineID = int(machineID.Int64)
		}
		if machineResourceID.Valid {
			agentMessage.MachineResourceID = machineResourceID.String
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

	// The agent's resource id IS its readable handle ("rei-agent-1"): slugified
	// name plus a per-slug counter. It doubles as the mention id, the DM
	// address suffix, and the agent's workspace directory name (under the
	// machine data root, default ~/.laelia/<machine>/<handle>/). Immutable
	// after creation.
	slug := common.SlugifyHandle(create.Name)
	if slug == "" {
		slug = "agent"
	}
	resourceID, err := s.reserveHandle(ctx, tx, "agent", "resource_id", slug, common.HandleKindAgent)
	if err != nil {
		return nil, err
	}

	// machine_id is a nullable FK; insert NULL when the agent is unbound (0)
	// so the FK constraint is satisfied for legacy/unbound agents.
	var machineIDArg any
	if create.MachineID > 0 {
		machineIDArg = create.MachineID
	}

	// follow_owner_permissions is deliberately omitted from the INSERT so the
	// column's DEFAULT TRUE governs: a fresh agent always starts with owner-follow
	// on (a proto3 bool cannot express "unset" vs false on CreateAgent); toggle it
	// via UpdateAgent.
	var agentID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO agent (
			resource_id, name, description, token_version, info, status, created_by, owner_id, allow_add_to_channel, machine_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (resource_id) DO NOTHING
		RETURNING id, created_at
	`,
		resourceID,
		create.Name,
		create.Description,
		create.TokenVersion,
		infoBytes,
		statusBytes,
		create.CreatedBy,
		create.OwnerID,
		create.AllowAddToChannel,
		machineIDArg,
	).Scan(&agentID, &create.CreatedAt)
	if err == sql.ErrNoRows {
		// A concurrent creation claimed the same handle; roll back and retry the
		// whole reservation+insert once (the fresh transaction sees the winner).
		if rbErr := tx.Rollback(); rbErr != nil {
			return nil, errors.Wrap(rbErr, "failed to rollback handle collision")
		}
		return s.CreateAgent(ctx, create)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	agent := &AgentMessage{
		ID:                      agentID,
		ResourceID:              resourceID,
		Name:                    create.Name,
		Description:             create.Description,
		TokenVersion:            create.TokenVersion,
		CreatedAt:               create.CreatedAt,
		Info:                    create.Info,
		Status:                  create.Status,
		CreatedBy:               create.CreatedBy,
		OwnerID:                 create.OwnerID,
		AllowAddToChannel:       create.AllowAddToChannel,
		FollowOwnerPermissions:  true,
		CanManageChannelMembers: true,
		MachineID:               create.MachineID,
	}
	s.agentIDCache.Add(agent.ID, agent)
	s.agentResourceIDCache.Add(agent.ResourceID, agent)
	return agent, nil
}

// TouchAgentHeartbeat records an agent heartbeat in one round trip: it touches
// the ACTIVE session row and patches the agent status' last_heartbeat_at via
// jsonb_set (instead of marshaling and rewriting the whole status JSONB from
// Go), then refreshes the in-memory cache so reads see the new heartbeat
// without a DB re-read. The HeartbeatBuffer calls it once per agent per flush
// window, so a steady heartbeat stream costs one UPDATE per flush instead of a
// full-row status rewrite per heartbeat.
func (s *Store) TouchAgentHeartbeat(ctx context.Context, agentID int, lastHeartbeatAt int64) error {
	// Single data-modifying CTE keeps both writes atomic in one round trip.
	if _, err := s.GetDB().ExecContext(ctx, `
		WITH touched AS (
			UPDATE agent_session SET last_heartbeat_at = to_timestamp($2)
			WHERE agent_id = $1 AND state = 'ACTIVE'
		)
		UPDATE agent SET status = jsonb_set(status, '{last_heartbeat_at}', to_jsonb($2))
		WHERE id = $1
	`, agentID, lastHeartbeatAt); err != nil {
		return err
	}
	s.refreshAgentHeartbeatCache(agentID, lastHeartbeatAt)
	return nil
}

// refreshAgentHeartbeatCache patches the cached status in memory (clone then
// re-Add, so readers never observe a concurrently-mutated pointer) keeping
// GetAgent/ListAgents fresh without a DB round trip per flush. The DB write
// above is the source of truth; a cache miss (disabled cache, eviction) simply
// skips the refresh and the next read falls back to the DB.
func (s *Store) refreshAgentHeartbeatCache(agentID int, lastHeartbeatAt int64) {
	cached, ok := s.agentIDCache.Peek(agentID)
	if !ok || cached == nil {
		return
	}
	clone := *cached
	if clone.Status == nil {
		clone.Status = &models.AgentStatus{LastHeartbeatAt: lastHeartbeatAt}
	} else {
		// proto.Clone: the generated type embeds a sync.Mutex, so a plain copy
		// would alias the message state. The type is fixed by construction.
		cloned, ok := proto.Clone(cached.Status).(*models.AgentStatus)
		if !ok {
			return
		}
		cloned.LastHeartbeatAt = lastHeartbeatAt
		clone.Status = cloned
	}
	s.agentIDCache.Remove(agentID)
	s.agentResourceIDCache.Remove(clone.ResourceID)
	s.agentIDCache.Add(agentID, &clone)
	s.agentResourceIDCache.Add(clone.ResourceID, &clone)
}

func (s *Store) UpdateAgent(ctx context.Context, current *AgentMessage, patch *UpdateAgentMessage) (*AgentMessage, error) {
	sets, args := []string{}, []any{}
	if v := patch.ResourceID; v != nil {
		sets, args = append(sets, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Name; v != nil {
		sets, args = append(sets, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Description; v != nil {
		sets, args = append(sets, fmt.Sprintf("description = $%d", len(args)+1)), append(args, *v)
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
	if v := patch.LastTokenRotatedAt; v != nil {
		sets, args = append(sets, fmt.Sprintf("last_token_rotated_at = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Delete; v != nil {
		sets, args = append(sets, fmt.Sprintf("deleted = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.AvatarS3Key; v != nil {
		sets, args = append(sets, fmt.Sprintf("avatar_s3_key = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.AllowAddToChannel; v != nil {
		sets, args = append(sets, fmt.Sprintf("allow_add_to_channel = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.FollowOwnerPermissions; v != nil {
		sets, args = append(sets, fmt.Sprintf("follow_owner_permissions = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.CanManageChannelMembers; v != nil {
		sets, args = append(sets, fmt.Sprintf("can_manage_channel_members = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.OwnerID; v != nil {
		sets, args = append(sets, fmt.Sprintf("owner_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Enabled; v != nil {
		sets, args = append(sets, fmt.Sprintf("enabled = $%d", len(args)+1)), append(args, *v)
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

// StopAgent disables an agent (enabled = false): its machine runner is torn
// down and it processes no session messages until StartAgent. Returns the
// updated agent.
func (s *Store) StopAgent(ctx context.Context, resourceID string) (*AgentMessage, error) {
	agent, err := s.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, errors.Errorf("agent %s not found", resourceID)
	}
	return s.UpdateAgent(ctx, agent, &UpdateAgentMessage{Enabled: &agentEnabledFalse})
}

// StartAgent re-enables a stopped agent (enabled = true) so it resumes
// processing session messages. Returns the updated agent.
func (s *Store) StartAgent(ctx context.Context, resourceID string) (*AgentMessage, error) {
	agent, err := s.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, errors.Errorf("agent %s not found", resourceID)
	}
	return s.UpdateAgent(ctx, agent, &UpdateAgentMessage{Enabled: &agentEnabledTrue})
}
