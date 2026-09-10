package dispatcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// pendingControlDispatchTimeout bounds one machine's pending-control drain at
// (re)connect.
const pendingControlDispatchTimeout = 10 * time.Second

// deliverAgentControl sends one control interaction to the agent's machine
// control stream. When the machine is offline (or the send races a disconnect)
// the interaction is queued for delivery at the machine's next (re)connect and
// queued=true is reported (design §3.5: cancel/steer issued offline take
// effect only after the machine comes back).
func (d *Dispatcher) deliverAgentControl(
	ctx context.Context,
	agentID int,
	kind store.ControlKind,
	commandID uuid.UUID,
	text string,
) (queued bool, err error) {
	if d.store == nil {
		return false, errors.New("agent is not connected")
	}
	// The agent→machine binding comes from the store: the per-agent session
	// registry is retired, so there is no live handle to shortcut through.
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil {
		return false, errors.Wrap(err, "failed to load agent for control delivery")
	}
	if agent == nil || agent.Deleted {
		return false, errors.Errorf("agent %d not found", agentID)
	}
	if agent.MachineID == 0 {
		return false, errors.New("agent is not bound to a machine")
	}
	if err := d.sendAgentControl(agent, agentControlRequest(kind, commandID, text)); err == nil {
		return false, nil
	}
	// Offline (or the send raced a disconnect): enqueue for delivery at the
	// machine's next (re)connect.
	if _, eErr := d.store.EnqueueAgentControl(ctx, &store.AgentPendingControlMessage{
		MachineID: agent.MachineID,
		AgentID:   agentID,
		Kind:      string(kind),
		CommandID: commandID,
		Text:      text,
	}); eErr != nil {
		return false, errors.Wrap(eErr, "failed to enqueue pending agent control")
	}
	return true, nil
}

// agentControlRequest builds the wire control request for one interaction.
// The agent_name is filled by sendAgentControl.
func agentControlRequest(kind store.ControlKind, commandID uuid.UUID, text string) *v1pb.AgentControlRequest {
	// The agent_name is filled by sendAgentControl (the machine-side router
	// dispatches on it).
	req := &v1pb.AgentControlRequest{}
	switch kind {
	case store.ControlKindCancel:
		req.Control = &v1pb.AgentControlRequest_Cancel{
			Cancel: &v1pb.CancelMessage{CommandId: commandID.String()},
		}
	case store.ControlKindSteer:
		req.Control = &v1pb.AgentControlRequest_Steer{
			Steer: &v1pb.SteerMessage{CommandId: commandID.String(), Text: text},
		}
	default:
	}
	return req
}

// ResolveAgentByName resolves an agent resource name (agents/{agent}) arriving
// on the named machine's control stream to its agent id, verifying the machine
// hosts it. Used to route per-agent interactions (e.g. a prompt release
// notice ack) that carry no agent id of their own.
func (d *Dispatcher) ResolveAgentByName(machineID int, agentName string) (int, error) {
	resourceID, err := common.GetAgentResourceID(agentName)
	if err != nil {
		return 0, errors.Wrap(err, "invalid agent name")
	}
	agent, err := d.store.GetAgentByResourceID(d.lifecycleCtx, resourceID)
	if err != nil {
		return 0, errors.Wrap(err, "failed to load agent for machine-stream routing")
	}
	if agent == nil || agent.Deleted || agent.MachineID != machineID {
		return 0, errors.Errorf("machine %d does not host agent %s", machineID, agentName)
	}
	return agent.ID, nil
}

// CancelCommand delivers the cancel interaction to the agent's machine, or
// queues it for delivery at the machine's next (re)connect when the machine is
// offline. The command row is already CANCELED by the time this runs (the API
// marks it first: the cancelled state is the irreversible anchor), so a
// machine that never comes back is covered by the pending-control TTL and rule
// 3 covers any late terminal the turn reports.
func (d *Dispatcher) CancelCommand(ctx context.Context, agentID int, commandID string) error {
	commandUUID, err := uuid.Parse(commandID)
	if err != nil {
		return errors.Wrapf(err, "invalid command id %q", commandID)
	}
	_, err = d.deliverAgentControl(ctx, agentID, store.ControlKindCancel, commandUUID, "")
	return err
}

// QueueCancelCommands enqueues a cancel interaction for each just-cancelled
// command of a machine without attempting delivery. Used by
// ForceDisconnectMachine: the machine is being torn down by construction, so
// there is no stream to race — the rows surface when the machine reconnects.
func (d *Dispatcher) QueueCancelCommands(ctx context.Context, machineID int, cancelled []*store.CancelledCommand) error {
	for _, cc := range cancelled {
		if _, err := d.store.EnqueueAgentControl(ctx, &store.AgentPendingControlMessage{
			MachineID: machineID,
			AgentID:   cc.AgentID,
			Kind:      string(store.ControlKindCancel),
			CommandID: cc.ID,
		}); err != nil {
			return errors.Wrapf(err, "failed to enqueue cancel for command %s", cc.ID)
		}
	}
	return nil
}

// SteerCommand delivers the steer to the agent's machine, or queues it for
// delivery at the machine's next (re)connect; queued reports which one
// happened so the API can tell the caller the steer is pending.
func (d *Dispatcher) SteerCommand(ctx context.Context, agentID int, commandID, text string) (bool, error) {
	commandUUID, err := uuid.Parse(commandID)
	if err != nil {
		return false, errors.Wrapf(err, "invalid command id %q", commandID)
	}
	return d.deliverAgentControl(ctx, agentID, store.ControlKindSteer, commandUUID, text)
}

// DispatchPendingAgentControl drains one machine's queued control rows in id
// order at (re)connect (design §3.5). Each row is delivered over the machine's
// control stream, or dropped when it can no longer matter: a cancel does not
// chase a command the machine already closed (COMPLETED/FAILED — the user's
// CANCELED anchor is applied at enqueue time, so a delivered cancel stops a
// still-running turn), and a steer into a non-running command is meaningless.
// An agent that moved machines drops its stale row (the command's terminal
// state is already anchored; rule 3 covers any late terminal). Rows are
// deleted once delivered or dropped; a send failure leaves the row for the
// next connect.
func (d *Dispatcher) DispatchPendingAgentControl(machineID int) {
	ctx, cancel := context.WithTimeout(d.lifecycleCtx, pendingControlDispatchTimeout)
	defer cancel()

	rows, err := d.store.ListAgentPendingControl(ctx, machineID)
	if err != nil {
		slog.Warn("failed to list pending agent control", "machineID", machineID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	delivered := make([]int64, 0, len(rows))
	for _, row := range rows {
		cmd, err := d.store.GetCommand(ctx, row.CommandID)
		if err != nil || cmd == nil {
			slog.Warn("pending control for a missing command; dropping", "commandID", row.CommandID, "kind", row.Kind)
			delivered = append(delivered, row.ID)
			continue
		}
		agent, err := d.store.GetAgent(ctx, row.AgentID)
		if err != nil || agent == nil || agent.Deleted || agent.MachineID != machineID {
			// The agent is gone or moved machines; the row's machine binding is
			// stale, so delivery would not reach the runner.
			slog.Warn("pending control for a re-bound agent; dropping", "commandID", row.CommandID, "kind", row.Kind)
			delivered = append(delivered, row.ID)
			continue
		}
		switch row.Kind {
		case store.ControlKindCancel:
			if cmd.Status == store.CommandStatusCompleted || cmd.Status == store.CommandStatusFailed {
				// The machine closed the command while offline: nothing to stop.
				delivered = append(delivered, row.ID)
				continue
			}
		case store.ControlKindSteer:
			if cmd.Status != store.CommandStatusRunning {
				// A steer into a turn that no longer exists is meaningless.
				delivered = append(delivered, row.ID)
				continue
			}
		default:
			slog.Warn("unknown pending control kind; dropping", "kind", row.Kind)
			delivered = append(delivered, row.ID)
			continue
		}

		commandUUID, err := uuid.Parse(cmd.ID.String())
		if err != nil {
			continue
		}
		if err := d.sendAgentControl(agent, agentControlRequest(store.ControlKind(row.Kind), commandUUID, row.Text)); err != nil {
			slog.Warn("pending control delivery failed; keeping the row", "commandID", row.CommandID, "kind", row.Kind, "error", err)
			continue
		}
		delivered = append(delivered, row.ID)
	}
	if err := d.store.DeleteAgentControl(ctx, delivered); err != nil {
		slog.Warn("failed to delete dispatched pending control rows", "machineID", machineID, "error", err)
	}
}
