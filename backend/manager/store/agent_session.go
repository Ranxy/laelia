package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type AgentSessionMessage struct {
	ID               int
	SessionID        string
	AgentID          int
	AgentResourceID  string
	TokenFamily      string
	State            string // ACTIVE, KICKED, TERMINATED
	SourceIP         string
	Fingerprint      string
	AgentVersion     string
	ConnectedAt      time.Time
	DisconnectedAt   time.Time
	LastHeartbeatAt  time.Time
	DisconnectReason string
}

func (s *Store) CreateAgentSession(ctx context.Context, session *AgentSessionMessage) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_session (
			session_id, agent_id, token_family, state, source_ip,
			fingerprint, agent_version, connected_at, last_heartbeat_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, session.SessionID, session.AgentID, session.TokenFamily, session.State, session.SourceIP,
		session.Fingerprint, session.AgentVersion, session.ConnectedAt, session.ConnectedAt)
	if err != nil {
		return errors.Wrapf(err, "failed to create agent session")
	}

	return tx.Commit()
}

func (s *Store) GetAgentSession(ctx context.Context, sessionID string) (*AgentSessionMessage, error) {
	query := `SELECT
		agent_session.id,
		agent_session.session_id,
		agent_session.agent_id,
		agent_session.token_family,
		agent_session.state,
		agent_session.source_ip,
		agent_session.fingerprint,
		agent_session.agent_version,
		agent_session.connected_at,
		agent_session.disconnected_at,
		agent_session.last_heartbeat_at,
		agent_session.disconnect_reason,
		agent.resource_id
	FROM agent_session
	JOIN agent ON agent.id = agent_session.agent_id
	WHERE agent_session.session_id = $1`

	var session AgentSessionMessage
	var disconnectedAt sql.NullTime
	var disconnectReason sql.NullString
	err := s.GetDB().QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID,
		&session.SessionID,
		&session.AgentID,
		&session.TokenFamily,
		&session.State,
		&session.SourceIP,
		&session.Fingerprint,
		&session.AgentVersion,
		&session.ConnectedAt,
		&disconnectedAt,
		&session.LastHeartbeatAt,
		&disconnectReason,
		&session.AgentResourceID,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if disconnectedAt.Valid {
		session.DisconnectedAt = disconnectedAt.Time
	}
	if disconnectReason.Valid {
		session.DisconnectReason = disconnectReason.String
	}
	return &session, nil
}

func (s *Store) TouchAgentSession(ctx context.Context, sessionID string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_session SET last_heartbeat_at = now() WHERE session_id = $1
	`, sessionID)
	return err
}

func (s *Store) TouchAgentSessionForHeartbeat(ctx context.Context, agentID int, lastHeartbeatAt int64) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_session SET last_heartbeat_at = to_timestamp($2) WHERE agent_id = $1 AND state = 'ACTIVE'
	`, agentID, lastHeartbeatAt)
	return err
}

func (s *Store) TerminateAgentSession(ctx context.Context, sessionID string, reason string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_session SET
			state = 'TERMINATED',
			disconnected_at = now(),
			disconnect_reason = $2
		WHERE session_id = $1 AND state = 'ACTIVE'
	`, sessionID, reason)
	return err
}

func (s *Store) TerminateAllAgentSessions(ctx context.Context, agentID int, reason string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_session SET
			state = 'KICKED',
			disconnected_at = now(),
			disconnect_reason = $2
		WHERE agent_id = $1 AND state = 'ACTIVE'
	`, agentID, reason)
	return err
}

func (s *Store) ListAgentSessions(ctx context.Context, agentID int, includeTerminated bool) ([]*AgentSessionMessage, error) {
	where := []string{"agent_id = $1"}
	args := []any{agentID}
	if !includeTerminated {
		where = append(where, "state IN ('ACTIVE', 'KICKED')")
	}

	query := fmt.Sprintf(`SELECT
		id, session_id, agent_id, token_family, state, source_ip,
		fingerprint, agent_version, connected_at, disconnected_at,
		last_heartbeat_at, disconnect_reason
	FROM agent_session
	WHERE %s
	ORDER BY connected_at DESC`, strings.Join(where, " AND "))

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*AgentSessionMessage
	for rows.Next() {
		var session AgentSessionMessage
		var disconnectedAt sql.NullTime
		var disconnectReason sql.NullString
		if err := rows.Scan(
			&session.ID, &session.SessionID, &session.AgentID,
			&session.TokenFamily, &session.State, &session.SourceIP,
			&session.Fingerprint, &session.AgentVersion, &session.ConnectedAt,
			&disconnectedAt, &session.LastHeartbeatAt, &disconnectReason,
		); err != nil {
			return nil, err
		}
		if disconnectedAt.Valid {
			session.DisconnectedAt = disconnectedAt.Time
		}
		if disconnectReason.Valid {
			session.DisconnectReason = disconnectReason.String
		}
		sessions = append(sessions, &session)
	}
	return sessions, rows.Err()
}
