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
	Instruction     string
	Profile         string
	AllowDiff       bool
	Status          int32
	ExitCode        sql.NullInt32
	DurationMs      sql.NullInt64
	CreatedAt       time.Time
	StartedAt       sql.NullTime
	CompletedAt     sql.NullTime
	ErrorMessage    string
	FinalSummary    string
	ResultJSON      string
	Env             string
	WorkingDir      string
	TimeoutSeconds  int32
	LastAckSeq      int32
	ConversationID  *uuid.UUID
}

type CommandOutputMessage struct {
	ID         int64
	CommandID  uuid.UUID
	SeqNo      int32
	StreamType int32
	Content    string
	CreatedAt  time.Time
}

type CommandEventMessage struct {
	ID          int64
	CommandID   uuid.UUID
	SeqNo       int32
	EventType   int32
	Summary     string
	PayloadJSON string
	CreatedAt   time.Time
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
			agent_id, principal_id, command, instruction, profile, allow_diff, status, env, working_dir, timeout_seconds, conversation_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at
	`,
		cmd.AgentID,
		cmd.PrincipalID,
		cmd.Command,
		cmd.Instruction,
		cmd.Profile,
		cmd.AllowDiff,
		cmd.Status,
		cmd.Env,
		cmd.WorkingDir,
		cmd.TimeoutSeconds,
		cmd.ConversationID,
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
		Instruction:    cmd.Instruction,
		Profile:        cmd.Profile,
		AllowDiff:      cmd.AllowDiff,
		Status:         cmd.Status,
		CreatedAt:      createdAt,
		Env:            cmd.Env,
		WorkingDir:     cmd.WorkingDir,
		TimeoutSeconds: cmd.TimeoutSeconds,
		ConversationID: cmd.ConversationID,
	}
	return created, nil
}

// SetCommandConversationID links a command to the conversation the agent ended
// up processing during its autonomous session. A session's command is created
// before the agent has chosen which channel to work on, so the link is filled
// in when the agent commits to a channel (via AckProcessedVersion).
func (s *Store) SetCommandConversationID(ctx context.Context, commandID, conversationID uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE command SET conversation_id = $1 WHERE id = $2
	`, conversationID, commandID)
	if err != nil {
		return errors.Wrapf(err, "failed to set command conversation id")
	}
	return nil
}

func (s *Store) GetCommand(ctx context.Context, id uuid.UUID) (*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
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
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
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
	var resultJSON string
	var conversationID sql.NullString

	if err := row.Scan(
		&cmd.ID, &cmd.AgentID, &cmd.PrincipalID, &cmd.Command, &cmd.Instruction, &cmd.Profile, &cmd.AllowDiff, &cmd.Status,
		&exitCode, &durationMs, &cmd.CreatedAt, &startedAt, &completedAt,
		&cmd.ErrorMessage, &cmd.FinalSummary, &resultJSON, &cmd.Env, &cmd.WorkingDir, &cmd.TimeoutSeconds, &cmd.LastAckSeq,
		&conversationID, &cmd.PrincipalName, &cmd.AgentResourceID,
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
	cmd.ResultJSON = resultJSON
	if conversationID.Valid {
		id, err := uuid.Parse(conversationID.String)
		if err == nil {
			cmd.ConversationID = &id
		}
	}
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
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
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
		var resultJSON string
		var conversationID sql.NullString

		if err := rows.Scan(
			&cmd.ID, &cmd.AgentID, &cmd.PrincipalID, &cmd.Command, &cmd.Instruction, &cmd.Profile, &cmd.AllowDiff, &cmd.Status,
			&exitCode, &durationMs, &cmd.CreatedAt, &startedAt, &completedAt,
			&cmd.ErrorMessage, &cmd.FinalSummary, &resultJSON, &cmd.Env, &cmd.WorkingDir, &cmd.TimeoutSeconds, &cmd.LastAckSeq,
			&conversationID, &cmd.PrincipalName, &cmd.AgentResourceID,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan command row")
		}

		cmd.ExitCode = exitCode
		cmd.DurationMs = durationMs
		cmd.StartedAt = startedAt
		cmd.CompletedAt = completedAt
		cmd.ResultJSON = resultJSON
		if conversationID.Valid {
			id, parseErr := uuid.Parse(conversationID.String)
			if parseErr == nil {
				cmd.ConversationID = &id
			}
		}
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

func (s *Store) UpdateCommandResultSummary(ctx context.Context, id uuid.UUID, finalSummary, resultJSON string) error {
	sets := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if finalSummary != "" {
		sets = append(sets, fmt.Sprintf("final_summary = $%d", len(args)+1))
		args = append(args, finalSummary)
	}
	if resultJSON != "" {
		sets = append(sets, fmt.Sprintf("result_json = $%d::jsonb", len(args)+1))
		args = append(args, resultJSON)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.GetDB().ExecContext(ctx, fmt.Sprintf(`
		UPDATE command SET %s WHERE id = $%d
	`, strings.Join(sets, ", "), len(args)), args...)
	if err != nil {
		return errors.Wrapf(err, "failed to update command result summary")
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

func (s *Store) AppendCommandEvent(ctx context.Context, event *CommandEventMessage) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO command_event (command_id, seq_no, event_type, summary, payload_json)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (command_id, seq_no) DO NOTHING
	`, event.CommandID, event.SeqNo, event.EventType, event.Summary, event.PayloadJSON)
	if err != nil {
		return errors.Wrapf(err, "failed to append command event")
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

func (s *Store) GetCommandEvents(ctx context.Context, cmdID uuid.UUID, afterSeq int32) ([]*CommandEventMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, command_id, seq_no, event_type, summary, payload_json::text, created_at
		FROM command_event
		WHERE command_id = $1 AND seq_no > $2
		ORDER BY seq_no ASC
	`, cmdID, afterSeq)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get command events")
	}
	defer rows.Close()

	var events []*CommandEventMessage
	for rows.Next() {
		var event CommandEventMessage
		if err := rows.Scan(&event.ID, &event.CommandID, &event.SeqNo, &event.EventType, &event.Summary, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan command event row")
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate command event rows")
	}

	return events, nil
}

func (s *Store) GetNextPendingCommand(ctx context.Context, agentID int) (*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
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
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.agent_id = $1 AND c.status = 2
	ORDER BY c.created_at DESC
	LIMIT 1`

	return scanCommand(s.GetDB().QueryRowContext(ctx, query, agentID))
}

// ListPendingCommandsByAgent returns all PENDING (status=1) commands for an
// agent ordered by created_at ASC. It drives the dispatcher's queue-less
// next-command dispatch (replacing the removed agent_inbox table).
func (s *Store) ListPendingCommandsByAgent(ctx context.Context, agentID int) ([]*CommandMessage, error) {
	query := `SELECT
		c.id, c.agent_id, c.principal_id, c.command, c.instruction, c.profile, c.allow_diff, c.status,
		c.exit_code, c.duration_ms, c.created_at, c.started_at, c.completed_at,
		c.error_message, c.final_summary, c.result_json::text, c.env, c.working_dir, c.timeout_seconds, c.last_ack_seq,
		c.conversation_id, COALESCE(p.name, ''), a.resource_id
	FROM command c
	JOIN agent a ON a.id = c.agent_id
	JOIN principal p ON p.id = c.principal_id
	WHERE c.agent_id = $1 AND c.status = 1
	ORDER BY c.created_at ASC`

	rows, err := s.GetDB().QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list pending commands")
	}
	defer rows.Close()

	var commands []*CommandMessage
	for rows.Next() {
		var cmd CommandMessage
		var exitCode sql.NullInt32
		var durationMs sql.NullInt64
		var startedAt sql.NullTime
		var completedAt sql.NullTime
		var resultJSON string
		var conversationID sql.NullString

		if err := rows.Scan(
			&cmd.ID, &cmd.AgentID, &cmd.PrincipalID, &cmd.Command, &cmd.Instruction, &cmd.Profile, &cmd.AllowDiff, &cmd.Status,
			&exitCode, &durationMs, &cmd.CreatedAt, &startedAt, &completedAt,
			&cmd.ErrorMessage, &cmd.FinalSummary, &resultJSON, &cmd.Env, &cmd.WorkingDir, &cmd.TimeoutSeconds, &cmd.LastAckSeq,
			&conversationID, &cmd.PrincipalName, &cmd.AgentResourceID,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan command row")
		}

		cmd.ExitCode = exitCode
		cmd.DurationMs = durationMs
		cmd.StartedAt = startedAt
		cmd.CompletedAt = completedAt
		cmd.ResultJSON = resultJSON
		if conversationID.Valid {
			id, parseErr := uuid.Parse(conversationID.String)
			if parseErr == nil {
				cmd.ConversationID = &id
			}
		}
		commands = append(commands, &cmd)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate pending command rows")
	}

	return commands, nil
}

// SearchChatHistory searches chat messages in a conversation by keyword,
// optional time range, with pagination support via offset.
// Results include sender identity (principal name, agent name, sender type)
// via JOINs to the principal and agent tables.
func (s *Store) SearchChatHistory(ctx context.Context, conversationID uuid.UUID, query string, since, until *time.Time, limit, offset int) ([]*ChatMessage, error) {
	args := []any{conversationID}
	where := `cm.conversation_id = $1`

	if query != "" {
		args = append(args, "%"+query+"%")
		where += fmt.Sprintf(` AND cm.content ILIKE $%d`, len(args))
	}
	if since != nil {
		args = append(args, since)
		where += fmt.Sprintf(` AND cm.created_at >= $%d`, len(args))
	}
	if until != nil {
		args = append(args, until)
		where += fmt.Sprintf(` AND cm.created_at <= $%d`, len(args))
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)

	rows, err := s.GetDB().QueryContext(ctx, fmt.Sprintf(`
		SELECT `+chatMessageColumns+`
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE %s
		ORDER BY cm.created_at DESC, cm.id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to search chat history")
	}
	defer rows.Close()

	var msgs []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat history rows")
	}

	return msgs, nil
}

// RunningCommandInfo holds the minimal data needed to derive agent execution
// status for a conversation activity feed. AgentID is the internal integer ID;
// CommandID is the UUID of the running command; EventType and Summary come from
// the latest command_event (both zero/nil when no event has been recorded yet).
type RunningCommandInfo struct {
	AgentID   int
	CommandID uuid.UUID
	EventType int32
	Summary   sql.NullString
}

// GetRunningCommandsForConversation returns the running commands (status=2)
// for a set of agents within a conversation, joined with their latest
// command_event. This is the data source for FetchConversationActivity.
func (s *Store) GetRunningCommandsForConversation(ctx context.Context, agentIDs []int, conversationID uuid.UUID) ([]*RunningCommandInfo, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}

	// Build the $1 array parameter as a PostgreSQL int[] literal.
	// Using pq.Array would require an extra dependency; constructing the
	// literal is safe because agentIDs are integers from the database.
	arr := make([]string, len(agentIDs))
	for i, id := range agentIDs {
		arr[i] = fmt.Sprintf("%d", id)
	}
	arrayLiteral := "{" + strings.Join(arr, ",") + "}"

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT c.agent_id, c.id, ce.event_type, ce.summary
		FROM command c
		LEFT JOIN LATERAL (
			SELECT event_type, summary FROM command_event
			WHERE command_id = c.id
			ORDER BY seq_no DESC
			LIMIT 1
		) ce ON true
		WHERE c.agent_id = ANY($1::int[])
		  AND c.conversation_id = $2
		  AND c.status = 2
	`, arrayLiteral, conversationID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query running commands")
	}
	defer rows.Close()

	var result []*RunningCommandInfo
	for rows.Next() {
		var rci RunningCommandInfo
		if scanErr := rows.Scan(&rci.AgentID, &rci.CommandID, &rci.EventType, &rci.Summary); scanErr != nil {
			return nil, errors.Wrapf(scanErr, "failed to scan running command row")
		}
		result = append(result, &rci)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate running commands")
	}
	return result, nil
}
