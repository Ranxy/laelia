package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// ControlKind classifies one queued manager→agent control interaction.
type ControlKind = string

// ControlKind values.
const (
	// ControlKindCancel stops the agent's in-flight turn.
	ControlKindCancel = "cancel"
	// ControlKindSteer injects a follow-up message into the in-flight turn.
	ControlKindSteer = "steer"
)

// PendingControlTTL is the queued-interaction retention backstop: a row older
// than this is swept, so a permanently retired machine's unconsumed backlog
// cannot accumulate forever.
const PendingControlTTL = 7 * 24 * time.Hour

// AgentPendingControlMessage is one queued control interaction awaiting
// delivery through its machine's control stream.
type AgentPendingControlMessage struct {
	ID      int64
	AgentID int
	// MachineID is the machine whose control stream delivers the row.
	MachineID int
	// Kind is one of the ControlKind values.
	Kind      string
	CommandID uuid.UUID
	// Text is the steer payload; empty for cancel.
	Text      string
	CreatedAt time.Time
}

// enqueueAgentControlSQL appends one interaction to the machine's pending
// queue; the serial id is the per-machine FIFO dispatch order.
const enqueueAgentControlSQL = `
	INSERT INTO agent_pending_control (machine_id, agent_id, kind, command_id, text)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING id, created_at
`

// listAgentPendingControlSQL reads one machine's queue in dispatch order.
const listAgentPendingControlSQL = `
	SELECT id, machine_id, agent_id, kind, command_id, text, created_at
	FROM agent_pending_control
	WHERE machine_id = $1
	ORDER BY id
`

// deleteAgentControlSQL removes dispatched (or dropped) rows by id. Dispatch
// resolution and deletion are two statements, not one: a row must leave the
// queue exactly once, whether it was delivered or judged terminal.
const deleteAgentControlSQL = `
	DELETE FROM agent_pending_control
	WHERE id = ANY($1::int[])
`

// deleteExpiredAgentControlSQL is the retention backstop.
const deleteExpiredAgentControlSQL = `
	DELETE FROM agent_pending_control
	WHERE created_at < $1
`

// EnqueueAgentControl appends one control interaction to its machine's queue.
func (s *Store) EnqueueAgentControl(ctx context.Context, msg *AgentPendingControlMessage) (*AgentPendingControlMessage, error) {
	row := s.GetDB().QueryRowContext(ctx, enqueueAgentControlSQL,
		msg.MachineID, msg.AgentID, msg.Kind, msg.CommandID, msg.Text)
	if err := row.Scan(&msg.ID, &msg.CreatedAt); err != nil {
		return nil, errors.Wrap(err, "failed to enqueue pending agent control")
	}
	return msg, nil
}

// ListAgentPendingControl returns the machine's queued interactions in FIFO
// dispatch order.
func (s *Store) ListAgentPendingControl(ctx context.Context, machineID int) ([]*AgentPendingControlMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, listAgentPendingControlSQL, machineID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list pending agent control")
	}
	defer rows.Close()

	out := make([]*AgentPendingControlMessage, 0)
	for rows.Next() {
		var msg AgentPendingControlMessage
		if err := rows.Scan(&msg.ID, &msg.MachineID, &msg.AgentID, &msg.Kind, &msg.CommandID, &msg.Text, &msg.CreatedAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan pending agent control row")
		}
		out = append(out, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate pending agent control rows")
	}
	return out, nil
}

// DeleteAgentControl removes delivered or dropped rows by id.
func (s *Store) DeleteAgentControl(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := s.GetDB().ExecContext(ctx, deleteAgentControlSQL, ids); err != nil {
		return errors.Wrap(err, "failed to delete pending agent control rows")
	}
	return nil
}

// DeleteExpiredAgentControl sweeps rows older than the retention window.
func (s *Store) DeleteExpiredAgentControl(ctx context.Context, before time.Time) error {
	if _, err := s.GetDB().ExecContext(ctx, deleteExpiredAgentControlSQL, before); err != nil {
		return errors.Wrap(err, "failed to sweep expired pending agent control rows")
	}
	return nil
}
