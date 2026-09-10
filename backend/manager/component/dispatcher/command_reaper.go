package dispatcher

import (
	"context"
	"log/slog"
	"time"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// staleCommandSweepInterval is how often the stale-command reaper scans
	// RUNNING command rows.
	staleCommandSweepInterval = 1 * time.Minute
	// reaperGrace is how long a machine must be lost — its MachineChannel
	// unregistered AND its persisted heartbeat expired — before the reaper
	// marks its RUNNING commands FAILED. Hard constraint: grace ≥ 2× the
	// reconnect backoff ceiling, so a flapping-but-alive machine (e.g. a proxy
	// killing long streams every 60s) never has its commands reaped while it
	// cycles through reconnects.
	reaperGrace = 10 * time.Minute
	// machineUnreachableReapReason explains the reaper's mark on a command
	// whose machine is gone: paired with failure_kind=machine_unreachable so a
	// late terminal can re-grade it (design §3.6 rule 2).
	machineUnreachableReapReason = "machine unreachable; command reaped after the disconnect grace"
)

// StartStaleCommandReaper launches the periodic RUNNING-command sweeper. It
// runs until Stop cancels the dispatcher's lifecycle context, and is tracked
// on the dispatcher's WaitGroup so shutdown joins it.
func (d *Dispatcher) StartStaleCommandReaper() {
	d.wgMu.Lock()
	d.wg.Add(1)
	d.wgMu.Unlock()
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(staleCommandSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-d.lifecycleCtx.Done():
				return
			case <-ticker.C:
				d.sweepStaleCommands()
				d.sweepExpiredPendingControl()
			}
		}
	}()
}

// sweepStaleCommands marks RUNNING commands that can no longer receive a
// result as FAILED(machine_unreachable). A command's fate is bound to its
// machine, not to a stream or a session: a turn survives stream death, so the
// only signal that its result will never arrive is machine loss — the dual
// signal (MachineChannel unregistered AND the persisted heartbeat expired)
// held for longer than the grace period. Either signal alone is not enough:
// the stream can drop for minutes while the machine keeps heartbeating, and
// the heartbeat (or the registry) can lag a manager restart.
func (d *Dispatcher) sweepStaleCommands() {
	if d.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.lifecycleCtx, graceDBTimeout)
	defer cancel()

	running := store.CommandStatusRunning
	cmds, err := d.store.ListCommands(ctx, &store.FindCommandMessage{Status: &running})
	if err != nil {
		slog.Warn("stale command reaper: failed to list running commands", "error", err)
		return
	}

	// Group the running commands by machine so each machine's liveness is
	// checked once per sweep.
	byMachine := make(map[int][]*store.CommandMessage)
	for _, cmd := range cmds {
		byMachine[cmd.MachineID] = append(byMachine[cmd.MachineID], cmd)
	}

	now := time.Now()
	for machineID, group := range byMachine {
		if !d.machineLost(ctx, machineID, now) {
			continue
		}
		for _, cmd := range group {
			reaped, err := d.store.ReapRunningCommand(ctx, cmd.ID, now,
				machineUnreachableReapReason, store.CommandFailureKindMachineUnreachable)
			if err != nil {
				slog.Error("stale command reaper: failed to reap command", "commandID", cmd.ID, "agentID", cmd.AgentID, "error", err)
				continue
			}
			if !reaped {
				continue
			}
			slog.Warn("running command reaped after machine loss",
				"commandID", cmd.ID, "agentID", cmd.AgentID, "machineID", machineID)
			d.closeWatchers(cmd.ID.String())
			d.closeEventWatchers(cmd.ID.String())
		}
	}
}

// machineLost reports whether a machine is unreachable per the dual signal:
// its MachineChannel is not registered in this process AND its persisted
// heartbeat (machine.status.last_heartbeat_at, refreshed by MachineHeartbeat
// every 30s) expired more than the grace period ago. An unknown machine row
// counts as lost. A machine with no binding (id 0) is never "lost" here —
// commands without a machine binding cannot have an in-flight turn.
func (d *Dispatcher) machineLost(ctx context.Context, machineID int, now time.Time) bool {
	if machineID == 0 {
		return false
	}
	if d.IsMachineConnected(machineID) {
		return false
	}
	machine, err := d.store.GetMachine(ctx, machineID)
	if err != nil {
		slog.Warn("stale command reaper: failed to load machine", "machineID", machineID, "error", err)
		return false
	}
	return machineLost(now, machine)
}

// machineLost is the pure dual-signal judgment over a loaded machine row: the
// machine must be OFFLINE (deleted, missing status, or a non-ONLINE state) or
// its heartbeat must be older than the grace period.
func machineLost(now time.Time, machine *store.MachineMessage) bool {
	if machine == nil || machine.Deleted {
		return true
	}
	if machine.Status == nil {
		return true
	}
	return machine.Status.GetState() != storepb.MachineStatus_ONLINE ||
		now.Unix()-machine.Status.GetLastHeartbeatAt() >= int64(reaperGrace.Seconds())
}

// sweepExpiredPendingControl reclaims queued control interactions older than
// the retention backstop: a machine that never returns must not leave rows in
// the queue forever (store.PendingControlTTL).
func (d *Dispatcher) sweepExpiredPendingControl() {
	ctx, cancel := context.WithTimeout(d.lifecycleCtx, graceDBTimeout)
	defer cancel()
	if err := d.store.DeleteExpiredAgentControl(ctx, time.Now().Add(-store.PendingControlTTL)); err != nil {
		slog.Warn("pending control TTL sweep failed", "error", err)
	}
}
