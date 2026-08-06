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
)

// McpServerMessage is the storage-layer representation of an MCP server.
// OwnerID 0 means workspace-global (admin-managed); OwnerID > 0 means the
// server is private to that user. Header values are plaintext-at-rest
// (consistent with api_provider keys); the service layer masks them before
// they cross the API.
type McpServerMessage struct {
	ID            int64
	ResourceID    string
	Title         string
	Description   string
	TransportType string // "http" | "sse"
	URL           string
	Headers       map[string]string
	ConfigVersion int64
	CreatedBy     int
	OwnerID       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Members       []string
}

// AgentMcpMessage is one enabled MCP server on an agent. ServerResourceID is
// the mcpServers/{id} token (uuid); AssignmentVersion is the per-row version
// bumped on every replace of the agent's selection.
type AgentMcpMessage struct {
	ServerResourceID  string
	AssignmentVersion int64
}

// GetMcpServerByResourceID returns an MCP server (with members) by resource id,
// or nil when not found.
func (s *Store) GetMcpServerByResourceID(ctx context.Context, resourceID string) (*McpServerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	servers, err := listMcpServerRows(ctx, tx, "resource_id = $1", []any{resourceID})
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, nil
	}
	if err := loadMcpServerDetail(ctx, tx, servers); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return servers[0], nil
}

// ListMcpServers returns every workspace-global MCP server (with members),
// ordered by creation time. Workspace-level config is small, so no pagination
// is applied; callers filter by member access in the service layer.
func (s *Store) ListMcpServers(ctx context.Context) ([]*McpServerMessage, error) {
	return s.listMcpServersWhere(ctx, "owner_id = 0")
}

// ListMyMcpServers returns the personal MCP servers owned by the given user.
func (s *Store) ListMyMcpServers(ctx context.Context, ownerID int) ([]*McpServerMessage, error) {
	return s.listMcpServersWhere(ctx, "owner_id = $1", ownerID)
}

// ListUserMcpServers returns every personal MCP server (with members, which
// are always empty for personal servers), ordered by creation time. It backs
// the admin read-only view.
func (s *Store) ListUserMcpServers(ctx context.Context) ([]*McpServerMessage, error) {
	return s.listMcpServersWhere(ctx, "owner_id <> 0")
}

func (s *Store) listMcpServersWhere(ctx context.Context, where string, args ...any) ([]*McpServerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	servers, err := listMcpServerRows(ctx, tx, where, args)
	if err != nil {
		return nil, err
	}
	if err := loadMcpServerDetail(ctx, tx, servers); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return servers, nil
}

// CreateMcpServer inserts an MCP server together with its members. A nil
// Members is treated as empty.
func (s *Store) CreateMcpServer(ctx context.Context, create *McpServerMessage) (*McpServerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	headers, err := json.Marshal(create.Headers)
	if err != nil {
		return nil, err
	}
	resourceID := uuid.New().String()
	var id int64
	var createdAt, updatedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO mcp_server (resource_id, title, description, transport_type, url, headers, created_by, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, resourceID, create.Title, create.Description, create.TransportType, create.URL, headers, create.CreatedBy, create.OwnerID,
	).Scan(&id, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	server := &McpServerMessage{
		ID:            id,
		ResourceID:    resourceID,
		Title:         create.Title,
		Description:   create.Description,
		TransportType: create.TransportType,
		URL:           create.URL,
		Headers:       cloneHeaderMap(create.Headers),
		ConfigVersion: 1,
		CreatedBy:     create.CreatedBy,
		OwnerID:       create.OwnerID,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
	if err := replaceMcpServerMembers(ctx, tx, id, create.Members); err != nil {
		return nil, err
	}
	server.Members = append([]string(nil), create.Members...)

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return server, nil
}

// UpdateMcpServer replaces the server's mutable fields plus its members (full
// replace) and increments config_version. Returns the updated server.
func (s *Store) UpdateMcpServer(ctx context.Context, current *McpServerMessage, patch *McpServerMessage) (*McpServerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	headers, err := json.Marshal(patch.Headers)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mcp_server
		SET title = $1, description = $2, transport_type = $3, url = $4,
		    headers = $5, config_version = $6, updated_at = now()
		WHERE id = $7
	`, patch.Title, patch.Description, patch.TransportType, patch.URL, headers, current.ConfigVersion+1, current.ID); err != nil {
		return nil, err
	}
	if err := replaceMcpServerMembers(ctx, tx, current.ID, patch.Members); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	updated := &McpServerMessage{
		ID:            current.ID,
		ResourceID:    current.ResourceID,
		Title:         patch.Title,
		Description:   patch.Description,
		TransportType: patch.TransportType,
		URL:           patch.URL,
		Headers:       cloneHeaderMap(patch.Headers),
		ConfigVersion: current.ConfigVersion + 1,
		CreatedBy:     current.CreatedBy,
		OwnerID:       current.OwnerID,
		CreatedAt:     current.CreatedAt,
		UpdatedAt:     time.Now(),
		Members:       append([]string(nil), patch.Members...),
	}
	return updated, nil
}

// DeleteMcpServer hard-deletes an MCP server (cascade removes its members).
// Callers must ensure no agent references the server first.
func (s *Store) DeleteMcpServer(ctx context.Context, resourceID string) error {
	_, err := s.GetDB().ExecContext(ctx, `DELETE FROM mcp_server WHERE resource_id = $1`, resourceID)
	return err
}

// CountAgentsReferencingMcpServer returns the number of live agents that have
// the given MCP server enabled.
func (s *Store) CountAgentsReferencingMcpServer(ctx context.Context, resourceID string) (int, error) {
	var count int
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM agent_mcp am
		JOIN agent a ON a.id = am.agent_id
		JOIN mcp_server ms ON ms.id = am.mcp_server_id
		WHERE ms.resource_id = $1 AND a.deleted = FALSE
	`, resourceID).Scan(&count)
	return count, err
}

// ListAgentMcpServers returns the MCP servers enabled on an agent, ordered by
// the server's creation time.
func (s *Store) ListAgentMcpServers(ctx context.Context, agentID int) ([]AgentMcpMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT ms.resource_id, am.assignment_version
		FROM agent_mcp am
		JOIN mcp_server ms ON ms.id = am.mcp_server_id
		WHERE am.agent_id = $1
		ORDER BY ms.created_at ASC
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentMcpMessage
	for rows.Next() {
		var m AgentMcpMessage
		if err := rows.Scan(&m.ServerResourceID, &m.AssignmentVersion); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplaceAgentMcpServers replaces an agent's enabled MCP server set. Removed
// rows are deleted; kept rows get assignment_version + 1; new rows are inserted
// with version 1. Unknown server resource ids are an error (service layer
// validates membership before calling).
func (s *Store) ReplaceAgentMcpServers(ctx context.Context, agentID int, serverResourceIDs []string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT ms.id, ms.resource_id, am.assignment_version
		FROM agent_mcp am
		JOIN mcp_server ms ON ms.id = am.mcp_server_id
		WHERE am.agent_id = $1
	`, agentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]struct {
		id      int64
		version int64
	}{}
	for rows.Next() {
		var id int64
		var resourceID string
		var version int64
		if err := rows.Scan(&id, &resourceID, &version); err != nil {
			return err
		}
		existing[resourceID] = struct {
			id      int64
			version int64
		}{id: id, version: version}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	wanted := map[string]bool{}
	for _, rid := range serverResourceIDs {
		wanted[rid] = true
		if prev, ok := existing[rid]; ok {
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_mcp SET assignment_version = $1 WHERE agent_id = $2 AND mcp_server_id = $3
			`, prev.version+1, agentID, prev.id); err != nil {
				return err
			}
			continue
		}
		var serverID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM mcp_server WHERE resource_id = $1`, rid).Scan(&serverID); err != nil {
			if err == sql.ErrNoRows {
				return errors.Errorf("mcp server %q not found", rid)
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_mcp (agent_id, mcp_server_id, assignment_version)
			VALUES ($1, $2, 1)
		`, agentID, serverID); err != nil {
			return err
		}
	}
	for rid, prev := range existing {
		if wanted[rid] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM agent_mcp WHERE agent_id = $1 AND mcp_server_id = $2`, agentID, prev.id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func listMcpServerRows(ctx context.Context, txn *sql.Tx, where string, args []any) ([]*McpServerMessage, error) {
	rows, err := txn.QueryContext(ctx, `
		SELECT id, resource_id, title, description, transport_type, url, headers, config_version,
		       created_by, owner_id, created_at, updated_at
		FROM mcp_server
		WHERE `+where+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []*McpServerMessage
	for rows.Next() {
		var s McpServerMessage
		var headers []byte
		if err := rows.Scan(&s.ID, &s.ResourceID, &s.Title, &s.Description, &s.TransportType, &s.URL,
			&headers, &s.ConfigVersion, &s.CreatedBy, &s.OwnerID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(headers, &s.Headers); err != nil {
			return nil, err
		}
		servers = append(servers, &s)
	}
	return servers, rows.Err()
}

func loadMcpServerDetail(ctx context.Context, txn *sql.Tx, servers []*McpServerMessage) error {
	if len(servers) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.ID)
	}
	members, err := listMcpServerMembers(ctx, txn, ids)
	if err != nil {
		return err
	}
	for _, s := range servers {
		s.Members = members[s.ID]
	}
	return nil
}

func listMcpServerMembers(ctx context.Context, txn *sql.Tx, serverIDs []int64) (map[int64][]string, error) {
	byID := make(map[int64][]string, len(serverIDs))
	if len(serverIDs) == 0 {
		return byID, nil
	}
	args := make([]any, 0, len(serverIDs))
	placeholders := make([]string, 0, len(serverIDs))
	for i, id := range serverIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := txn.QueryContext(ctx, `
		SELECT server_id, member
		FROM mcp_server_member
		WHERE server_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY member ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var serverID int64
		var member string
		if err := rows.Scan(&serverID, &member); err != nil {
			return nil, err
		}
		byID[serverID] = append(byID[serverID], member)
	}
	return byID, rows.Err()
}

func replaceMcpServerMembers(ctx context.Context, txn *sql.Tx, serverID int64, members []string) error {
	if _, err := txn.ExecContext(ctx, `DELETE FROM mcp_server_member WHERE server_id = $1`, serverID); err != nil {
		return err
	}
	for _, member := range members {
		if _, err := txn.ExecContext(ctx, `
			INSERT INTO mcp_server_member (server_id, member)
			VALUES ($1, $2)
		`, serverID, member); err != nil {
			return err
		}
	}
	return nil
}

func cloneHeaderMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
