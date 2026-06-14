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

type CommandMessage struct {
	ID              uuid.UUID
	AgentID         int
	AgentResourceID string
	PrincipalID     int
	PrincipalName   string
	Command         string
	Status          int32
	ExitCode        sql.NullInt32
	DurationMs      sql.NullInt64
	CreatedAt       time.Time
	StartedAt       sql.NullTime
	CompletedAt     sql.NullTime
	ErrorMessage    string
	Env             string
	WorkingDir      string
	TimeoutSeconds  int32
	LastAckSeq      int32
}

type CommandOutputMessage struct {
	ID         int64
	CommandID  uuid.UUID
	SeqNo      int32
	StreamType int32
	Content    string
	CreatedAt  time.Time
}

type FindCommandMessage struct {
	AgentID *int
	Status  *int32
	Limit   *int
	Offset  *int
}

func (s *Store) CreateCommand(ctx context.Context, cmd *CommandMessage) (*CommandMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var commandID uuid.UUID
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO command (
			agent_id, principal_id, command, status, env, working_dir, timeout_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`,
		cmd.AgentID,
		cmd.PrincipalID,
		cmd.Command,
		cmd.Status,
		cmd.Env,
		cmd.WorkingDir,
		cmd.TimeoutSeconds,
	).Scan(&commandID, &createdAt); err != nil {
		return nil, errors.Wrapf(err, "failed to create command")
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	created := &CommandMessage{
		ID:             commandID,
		AgentID:        cmd.AgentID,
		PrincipalID:    cmd.PrincipalID,
		Command:        cmd.Command,
		Status:         cmd.Status,
		CreatedAt:      createdAt,
		Env:            cmd.Env,
		WorkingDir:     cmd.WorkingDir,
		TimeoutSeconds: cmd.TimeoutSeconds,
	}
	return created, nil
}

func (s *Store) GetCommand(ctx context.Context, id uuid.UUID) (*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.id = $1`

	cmd, err := scanCommand(s.GetDB().QueryRowContext(ctx, query, id))
	if err != nil {
		return nil, err
	}
	if cmd == nil {
		return nil, errors.Errorf("command %s not found", id)
	}
	return cmd, nil
}

func (s *Store) GetCommandByName(ctx context.Context, name string) (*CommandMessage, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "agents" || parts[2] != "commands" {
		return nil, errors.Errorf("invalid command name: %s", name)
	}
	agentResourceID := parts[1]
	commandIDStr := parts[3]

	commandID, err := uuid.Parse(commandIDStr)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid command ID: %s", commandIDStr)
	}

	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.id = $1 AND a.resource_id = $2`

	cmd, err := scanCommand(s.GetDB().QueryRowContext(ctx, query, commandID, agentResourceID))
	if err != nil {
		return nil, err
	}
	if cmd == nil {
		return nil, errors.Errorf("command %s not found", name)
	}
	return cmd, nil
}

func scanCommand(row *sql.Row) (*CommandMessage, error) {
	var cmd CommandMessage
	var exitCode sql.NullInt32
	var durationMs sql.NullInt64
	var startedAt sql.NullTime
	var completedAt sql.NullTime

	if err := row.Scan(
		&cmd.ID, &cmd.AgentID, &cmd.PrincipalID, &cmd.Command, &cmd.Status,
		&exitCode, &durationMs, &cmd.CreatedAt, &startedAt, &completedAt,
		&cmd.ErrorMessage, &cmd.Env, &cmd.WorkingDir, &cmd.TimeoutSeconds, &cmd.LastAckSeq,
		&cmd.PrincipalName, &cmd.AgentResourceID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to scan command")
	}

	cmd.ExitCode = exitCode
	cmd.DurationMs = durationMs
	cmd.StartedAt = startedAt
	cmd.CompletedAt = completedAt
	return &cmd, nil
}

func (s *Store) ListCommands(ctx context.Context, find *FindCommandMessage) ([]*CommandMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.AgentID; v != nil {
		where, args = append(where, fmt.Sprintf("c.agent_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Status; v != nil {
		where, args = append(where, fmt.Sprintf("c.status = $%d", len(args)+1)), append(args, *v)
	}

	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE ` + strings.Join(where, " AND ") + ` ORDER BY c.created_at DESC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list commands")
	}
	defer rows.Close()

	var commands []*CommandMessage
	for rows.Next() {
		var cmd CommandMessage
		var exitCode sql.NullInt32
		var durationMs sql.NullInt64
		var startedAt sql.NullTime
		var completedAt sql.NullTime

		if err := rows.Scan(
			&cmd.ID, &cmd.AgentID, &cmd.PrincipalID, &cmd.Command, &cmd.Status,
			&exitCode, &durationMs, &cmd.CreatedAt, &startedAt, &completedAt,
			&cmd.ErrorMessage, &cmd.Env, &cmd.WorkingDir, &cmd.TimeoutSeconds, &cmd.LastAckSeq,
			&cmd.PrincipalName, &cmd.AgentResourceID,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan command row")
		}

		cmd.ExitCode = exitCode
		cmd.DurationMs = durationMs
		cmd.StartedAt = startedAt
		cmd.CompletedAt = completedAt
		commands = append(commands, &cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate command rows")
	}

	return commands, nil
}

func (s *Store) UpdateCommandStatus(ctx context.Context, id uuid.UUID, status int32, startedAt, completedAt *time.Time, exitCode *int32, durationMs *int64, errorMsg string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sets, args := []string{fmt.Sprintf("status = $%d", 1)}, []any{status}
	if startedAt != nil {
		sets, args = append(sets, fmt.Sprintf("started_at = $%d", len(args)+1)), append(args, *startedAt)
	}
	if completedAt != nil {
		sets, args = append(sets, fmt.Sprintf("completed_at = $%d", len(args)+1)), append(args, *completedAt)
	}
	if exitCode != nil {
		sets, args = append(sets, fmt.Sprintf("exit_code = $%d", len(args)+1)), append(args, *exitCode)
	}
	if durationMs != nil {
		sets, args = append(sets, fmt.Sprintf("duration_ms = $%d", len(args)+1)), append(args, *durationMs)
	}
	if errorMsg != "" {
		sets, args = append(sets, fmt.Sprintf("error_message = $%d", len(args)+1)), append(args, errorMsg)
	}

	args = append(args, id)

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE command SET `+strings.Join(sets, ", ")+` WHERE id = $%d
	`, len(args)), args...); err != nil {
		return errors.Wrapf(err, "failed to update command status")
	}

	return tx.Commit()
}

func (s *Store) UpdateCommandAckSeq(ctx context.Context, id uuid.UUID, seq int32) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE command SET last_ack_seq = $1 WHERE id = $2
	`, seq, id)
	if err != nil {
		return errors.Wrapf(err, "failed to update command ack seq")
	}
	return nil
}

func (s *Store) AppendCommandOutput(ctx context.Context, cmdID uuid.UUID, seqNo int32, streamType int32, content string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO command_output (command_id, seq_no, stream_type, content)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (command_id, seq_no) DO NOTHING
	`, cmdID, seqNo, streamType, content)
	if err != nil {
		return errors.Wrapf(err, "failed to append command output")
	}
	return nil
}

func (s *Store) GetCommandOutput(ctx context.Context, cmdID uuid.UUID, afterSeq int32) ([]*CommandOutputMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, command_id, seq_no, stream_type, content, created_at
		FROM command_output
		WHERE command_id = $1 AND seq_no > $2
		ORDER BY seq_no ASC
	`, cmdID, afterSeq)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get command output")
	}
	defer rows.Close()

	var outputs []*CommandOutputMessage
	for rows.Next() {
		var o CommandOutputMessage
		if err := rows.Scan(&o.ID, &o.CommandID, &o.SeqNo, &o.StreamType, &o.Content, &o.CreatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan command output row")
		}
		outputs = append(outputs, &o)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate command output rows")
	}

	return outputs, nil
}

func (s *Store) GetNextPendingCommand(ctx context.Context, agentID int) (*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.agent_id = $1 AND c.status = 1
	ORDER BY c.created_at ASC
	LIMIT 1`

	return scanCommand(s.GetDB().QueryRowContext(ctx, query, agentID))
}

func (s *Store) GetRunningCommand(ctx context.Context, agentID int) (*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.agent_id = $1 AND c.status = 2
	ORDER BY c.created_at DESC
	LIMIT 1`

	return scanCommand(s.GetDB().QueryRowContext(ctx, query, agentID))
}
