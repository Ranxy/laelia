package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

const (
	InboxStatePending  int32 = 1
	InboxStateSelected int32 = 2
	InboxStateDeferred int32 = 3

	WorkingStateIdle     int32 = 1
	WorkingStateDeciding int32 = 2
	WorkingStateWorking  int32 = 3
)

type InboxItemMessage struct {
	ID             uuid.UUID
	AgentID        int
	CommandID      uuid.UUID
	Priority       int32
	ContextSummary string
	State          int32
	DeferredUntil  sql.NullTime
	DeferCount     int32
	CreatedAt      time.Time
	SelectedAt     sql.NullTime
}

type WorkingStateMessage struct {
	AgentID     int
	SessionID   string
	InboxItemID uuid.NullUUID
	State       int32
	UpdatedAt   time.Time
}

func (s *Store) CreateInboxItem(ctx context.Context, agentID int, commandID uuid.UUID, priority int32, summary string) (*InboxItemMessage, error) {
	var item InboxItemMessage
	err := s.GetDB().QueryRowContext(ctx, `
		INSERT INTO agent_inbox (agent_id, command_id, priority, context_summary)
		VALUES ($1, $2, $3, $4)
		RETURNING id, agent_id, command_id, priority, context_summary, state, defer_count, created_at
	`, agentID, commandID, priority, summary).Scan(
		&item.ID, &item.AgentID, &item.CommandID, &item.Priority,
		&item.ContextSummary, &item.State, &item.DeferCount, &item.CreatedAt,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create inbox item")
	}
	return &item, nil
}

func (s *Store) GetInboxItems(ctx context.Context, agentID int) ([]*InboxItemMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, agent_id, command_id, priority, context_summary, state, deferred_until, defer_count, created_at, selected_at
		FROM agent_inbox
		WHERE agent_id = $1 AND state = $2 AND (deferred_until IS NULL OR deferred_until <= now())
		ORDER BY priority DESC, created_at ASC
	`, agentID, InboxStatePending)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get inbox items")
	}
	defer rows.Close()

	var items []*InboxItemMessage
	for rows.Next() {
		var item InboxItemMessage
		if err := rows.Scan(
			&item.ID, &item.AgentID, &item.CommandID, &item.Priority, &item.ContextSummary,
			&item.State, &item.DeferredUntil, &item.DeferCount, &item.CreatedAt, &item.SelectedAt,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan inbox item")
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate inbox items")
	}
	return items, nil
}

func (s *Store) SelectInboxItem(ctx context.Context, itemID uuid.UUID) error {
	now := time.Now()
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_inbox SET state = $1, selected_at = $2 WHERE id = $3 AND state = $4
	`, InboxStateSelected, now, itemID, InboxStatePending)
	if err != nil {
		return errors.Wrapf(err, "failed to select inbox item")
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.Errorf("inbox item %s not found or not in pending state", itemID)
	}
	return nil
}

func (s *Store) DeferInboxItem(ctx context.Context, itemID uuid.UUID, deferredUntil time.Time) error {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_inbox SET state = $1, deferred_until = $2, defer_count = defer_count + 1
		WHERE id = $3 AND state = $4
	`, InboxStateDeferred, deferredUntil, itemID, InboxStateSelected)
	if err != nil {
		return errors.Wrapf(err, "failed to defer inbox item")
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.Errorf("inbox item %s not found or not in selected state", itemID)
	}
	return nil
}

func (s *Store) ReleaseSelectedItems(ctx context.Context, agentID int) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_inbox SET state = $1, selected_at = NULL
		WHERE agent_id = $2 AND state = $3
	`, InboxStatePending, agentID, InboxStateSelected)
	if err != nil {
		return errors.Wrapf(err, "failed to release selected inbox items")
	}
	return nil
}

func (s *Store) DeleteInboxItemByCommandID(ctx context.Context, commandID uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM agent_inbox WHERE command_id = $1
	`, commandID)
	if err != nil {
		return errors.Wrapf(err, "failed to delete inbox item by command")
	}
	return nil
}

type InboxItemWithCommand struct {
	InboxItemID  uuid.UUID
	CommandID    uuid.UUID
	Priority     int32
	CreatedAt    time.Time

	Command     string
	Instruction string
	ExecutorKind int32
	Profile     string
	AllowDiff   bool
	SourceType  int32
	Env         string
	WorkingDir  string
	TimeoutSecs int32
	PrincipalID int
}

func (s *Store) GetInboxItemWithCommand(ctx context.Context, itemID uuid.UUID) (*InboxItemWithCommand, error) {
	var item InboxItemWithCommand
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT
			ai.id, ai.command_id, ai.priority, ai.created_at,
			c.command, c.instruction, c.executor_kind, c.profile, c.allow_diff,
			c.source_type, c.env, c.working_dir, c.timeout_seconds, c.principal_id
		FROM agent_inbox ai
		JOIN command c ON c.id = ai.command_id
		WHERE ai.id = $1 AND ai.state = $2
	`, itemID, InboxStateSelected).Scan(
		&item.InboxItemID, &item.CommandID, &item.Priority, &item.CreatedAt,
		&item.Command, &item.Instruction, &item.ExecutorKind, &item.Profile, &item.AllowDiff,
		&item.SourceType, &item.Env, &item.WorkingDir, &item.TimeoutSecs, &item.PrincipalID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("inbox item %s not found or not selected", itemID)
		}
		return nil, errors.Wrapf(err, "failed to get inbox item with command")
	}
	return &item, nil
}

func (s *Store) GetWorkingState(ctx context.Context, agentID int) (*WorkingStateMessage, error) {
	var state WorkingStateMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT agent_id, session_id, inbox_item_id, state, updated_at
		FROM agent_working_state
		WHERE agent_id = $1
	`, agentID).Scan(&state.AgentID, &state.SessionID, &state.InboxItemID, &state.State, &state.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to get working state")
	}
	return &state, nil
}

func (s *Store) UpsertWorkingState(ctx context.Context, state *WorkingStateMessage) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO agent_working_state (agent_id, session_id, inbox_item_id, state, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (agent_id) DO UPDATE SET
			session_id = EXCLUDED.session_id,
			inbox_item_id = EXCLUDED.inbox_item_id,
			state = EXCLUDED.state,
			updated_at = now()
	`, state.AgentID, state.SessionID, state.InboxItemID, state.State)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert working state")
	}
	return nil
}
