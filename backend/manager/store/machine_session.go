package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// MachineSessionMessage is the storage-layer representation of a live machine
// connection (liveness row). Mirrors AgentSessionMessage.
type MachineSessionMessage struct {
	ID                int
	SessionID         string
	MachineID         int
	MachineResourceID string
	TokenFamily       string
	State             string // ACTIVE, KICKED, TERMINATED
	SourceIP          string
	Fingerprint       string
	AgentVersion      string
	ConnectedAt       time.Time
	DisconnectedAt    time.Time
	LastHeartbeatAt   time.Time
	DisconnectReason  string
}

func (s *Store) CreateMachineSession(ctx context.Context, session *MachineSessionMessage) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO machine_session (
			session_id, machine_id, token_family, state, source_ip,
			fingerprint, agent_version, connected_at, last_heartbeat_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, session.SessionID, session.MachineID, session.TokenFamily, session.State, session.SourceIP,
		session.Fingerprint, session.AgentVersion, session.ConnectedAt, session.ConnectedAt)
	if err != nil {
		return errors.Wrapf(err, "failed to create machine session")
	}

	return tx.Commit()
}

func (s *Store) GetMachineSession(ctx context.Context, sessionID string) (*MachineSessionMessage, error) {
	query := `SELECT
			machine_session.id,
			machine_session.session_id,
			machine_session.machine_id,
			machine_session.token_family,
			machine_session.state,
			machine_session.source_ip,
			machine_session.fingerprint,
			machine_session.agent_version,
			machine_session.connected_at,
			machine_session.disconnected_at,
			machine_session.last_heartbeat_at,
			machine_session.disconnect_reason,
			machine.resource_id
		FROM machine_session
		JOIN machine ON machine.id = machine_session.machine_id
		WHERE machine_session.session_id = $1`

	var session MachineSessionMessage
	var disconnectedAt sql.NullTime
	var disconnectReason sql.NullString
	err := s.GetDB().QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID,
		&session.SessionID,
		&session.MachineID,
		&session.TokenFamily,
		&session.State,
		&session.SourceIP,
		&session.Fingerprint,
		&session.AgentVersion,
		&session.ConnectedAt,
		&disconnectedAt,
		&session.LastHeartbeatAt,
		&disconnectReason,
		&session.MachineResourceID,
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

func (s *Store) TouchMachineSession(ctx context.Context, sessionID string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_session SET last_heartbeat_at = now() WHERE session_id = $1
	`, sessionID)
	return err
}

func (s *Store) TerminateMachineSession(ctx context.Context, sessionID string, reason string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_session SET
			state = 'TERMINATED',
			disconnected_at = now(),
			disconnect_reason = $2
		WHERE session_id = $1 AND state = 'ACTIVE'
	`, sessionID, reason)
	return err
}

func (s *Store) TerminateAllMachineSessions(ctx context.Context, machineID int, reason string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_session SET
			state = 'KICKED',
			disconnected_at = now(),
			disconnect_reason = $2
		WHERE machine_id = $1 AND state = 'ACTIVE'
	`, machineID, reason)
	return err
}

func (s *Store) ListMachineSessions(ctx context.Context, machineID int, includeTerminated bool) ([]*MachineSessionMessage, error) {
	where := []string{"machine_id = $1"}
	args := []any{machineID}
	if !includeTerminated {
		where = append(where, "state IN ('ACTIVE', 'KICKED')")
	}

	query := fmt.Sprintf(`SELECT
			id, session_id, machine_id, token_family, state, source_ip,
			fingerprint, agent_version, connected_at, disconnected_at,
			last_heartbeat_at, disconnect_reason
		FROM machine_session
		WHERE %s
		ORDER BY connected_at DESC`, strings.Join(where, " AND "))

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*MachineSessionMessage
	for rows.Next() {
		var session MachineSessionMessage
		var disconnectedAt sql.NullTime
		var disconnectReason sql.NullString
		if err := rows.Scan(
			&session.ID, &session.SessionID, &session.MachineID,
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
