package dispatcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// staleCommandSweepInterval is how often the stale-command reaper scans
	// RUNNING command rows.
	staleCommandSweepInterval = 1 * time.Minute
	// staleCommandReapAfter is how long a RUNNING command may exist without
	// being its agent session's current in-flight command before the reaper
	// marks it FAILED. During an active turn the command is current, so a
	// legitimately long turn is never reaped no matter how silent it is.
	staleCommandReapAfter = 10 * time.Minute
	// beginSessionReapReason explains why a command was marked FAILED when the
	// agent opened a new drain session over it.
	beginSessionReapReason = "superseded by a newer agent session"
	// staleReapReason explains the reaper's mark on a command that can no
	// longer receive a result.
	staleReapReason = "stale running command reaped (no result received)"
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
			}
		}
	}()
}

// sweepStaleCommands marks RUNNING commands that can no longer receive a
// result as FAILED. The BeginSession reap (HandleBeginSession) covers the
// reconnect path; this sweep covers what no client message can report: a
// machine that never returns, a reconnected session that went idle before
// calling BeginSession, and result/status updates that failed mid-write.
// Every reaped row is a zombie: the machine clears its local last-command
// state when it abandons a turn, so no reconnect can ever resolve it.
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

	now := time.Now()
	for _, cmd := range cmds {
		if !d.shouldReapCommand(cmd, now) {
			continue
		}
		reaped, err := d.store.FailStaleRunningCommand(ctx, cmd.ID, now, staleReapReason)
		if err != nil {
			slog.Error("stale command reaper: failed to reap command", "commandID", cmd.ID, "agentID", cmd.AgentID, "error", err)
			continue
		}
		if !reaped {
			continue
		}
		slog.Warn("stale running command reaped", "commandID", cmd.ID, "agentID", cmd.AgentID, "age", now.Sub(cmd.CreatedAt).Round(time.Second))
		d.closeWatchers(cmd.ID.String())
		d.closeEventWatchers(cmd.ID.String())
	}
}

// shouldReapCommand reports whether a RUNNING command is a zombie row the
// drain loop can no longer resolve. The drain loop is strictly serial per
// agent: a command is alive only while the agent's session tracks it as the
// current in-flight command and it has not outlived the reap threshold. An
// empty currentCmdID covers the disconnected-session case, a session tracking
// a different command covers the reconnect-then-idle path.
func (d *Dispatcher) shouldReapCommand(cmd *store.CommandMessage, now time.Time) bool {
	if now.Sub(cmd.CreatedAt) < staleCommandReapAfter {
		return false
	}
	if sess, ok := d.registry.getAgent(cmd.AgentID); ok {
		sess.mu.Lock()
		current := sess.currentCmdID
		sess.mu.Unlock()
		if current == cmd.ID.String() {
			return false
		}
	}
	return true
}

// reapAgentRunningCommands marks every RUNNING command of an agent FAILED and
// closes their live watchers. Called from HandleBeginSession: BeginSession
// only arrives between turns (the drain loop is serial per agent), so any
// RUNNING command at that moment belongs to a turn whose result can never
// arrive — reaping it here keeps the agent at exactly one live RUNNING
// command regardless of how its previous stream died.
func (d *Dispatcher) reapAgentRunningCommands(ctx context.Context, agentID int) {
	reaped, err := d.store.FailRunningCommandsForAgent(ctx, agentID, time.Now(), beginSessionReapReason)
	if err != nil {
		slog.Error("failed to reap running commands on begin session", "agentID", agentID, "error", err)
		return
	}
	for _, id := range reaped {
		slog.Warn("reaped stale running command on begin session", "commandID", id, "agentID", agentID)
		d.closeWatchers(id.String())
		d.closeEventWatchers(id.String())
	}
}
