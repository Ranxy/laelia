package dispatcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// HandleMachinePing records a machine heartbeat ping.
func (d *Dispatcher) HandleMachinePing(machineID int, _ *v1pb.Ping) {
	sess, ok := d.registry.getMachine(machineID)
	if ok {
		sess.mu.Lock()
		sess.lastPingAt = time.Now()
		sess.mu.Unlock()
	}
}

// HandlePing records an agent heartbeat ping.
func (d *Dispatcher) HandlePing(agentID int, _ *v1pb.Ping) {
	sess, ok := d.registry.getAgent(agentID)
	if ok {
		sess.mu.Lock()
		sess.lastPingAt = time.Now()
		sess.mu.Unlock()
	}
}

// StartPingMonitor launches the liveness ticker. It runs until Stop cancels
// the dispatcher's lifecycle context, and is tracked on the dispatcher's
// WaitGroup so shutdown joins it.
func (d *Dispatcher) StartPingMonitor() {
	d.wgMu.Lock()
	d.wg.Add(1)
	d.wgMu.Unlock()
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(d.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.lifecycleCtx.Done():
				return
			case <-ticker.C:
				d.checkSessionLiveness()
			}
		}
	}()
}

// Stop cancels the dispatcher's lifecycle context (ping monitor + any
// in-flight grace goroutines) and waits for them to exit. Idempotent.
func (d *Dispatcher) Stop() {
	d.lifecycleCancel()
	d.wgMu.Lock()
	d.wg.Wait()
	d.wgMu.Unlock()
}

// HandleProvisionerPing records a provisioner heartbeat ping.
func (*Dispatcher) HandleProvisionerPing(sess *ProvisionerSession, _ *v1pb.Ping) {
	if sess != nil {
		sess.TouchPing()
	}
}

func (d *Dispatcher) checkSessionLiveness() {
	sessions := d.registry.snapshotAgents()
	machines := d.registry.snapshotMachines()
	provisioners := d.registry.snapshotProvisioners()

	now := time.Now()
	for _, sess := range sessions {
		sess.mu.Lock()
		idle := now.Sub(sess.lastPingAt)
		agentID := sess.agentID
		sess.mu.Unlock()

		// Skip invalidated sessions (send is an atomic pointer now, not
		// guarded by mu).
		if sess.send.Load() == nil {
			continue
		}

		if idle > d.pingTimeout {
			slog.Warn("agent ping timeout, unregistering",
				"agentID", agentID,
				"idle", idle,
				"timeout", d.pingTimeout)
			// Unregister only if this exact session is still current: the
			// agent may have reconnected between the snapshot above and this
			// check, and UnregisterAgent(agentID) would tear down the new
			// session instead of the timed-out one.
			d.UnregisterAgentIf(agentID, sess)
		}
	}

	for _, m := range machines {
		m.mu.Lock()
		idle := now.Sub(m.lastPingAt)
		machineID := m.machineID
		m.mu.Unlock()

		if m.send.Load() == nil {
			continue
		}

		if idle > d.pingTimeout {
			slog.Warn("machine ping timeout, unregistering",
				"machineID", machineID,
				"idle", idle,
				"timeout", d.pingTimeout)
			// Same reconnect guard as for agents: only tear down the exact
			// session that timed out.
			d.UnregisterMachineIf(machineID, m)
		}
	}

	for _, p := range provisioners {
		p.mu.Lock()
		idle := now.Sub(p.lastPingAt)
		provisionerID := p.provisionerID
		p.mu.Unlock()

		if p.send.Load() == nil {
			continue
		}

		if idle > d.pingTimeout {
			slog.Warn("provisioner ping timeout, unregistering",
				"provisionerID", provisionerID,
				"idle", idle,
				"timeout", d.pingTimeout)
			// Same reconnect guard as for agents: only tear down the exact
			// session that timed out.
			d.UnregisterProvisionerIf(provisionerID, p)
		}
	}
}

// startGracePeriod arms a cancellable 60s timer that, if it fires, marks the
// given command FAILED. The timer is tracked in d.grace so a reconnect can
// cancel it (the reconnect path reaps stale commands itself). The goroutine
// is tracked on the dispatcher's WaitGroup so Stop joins it.
func (d *Dispatcher) startGracePeriod(agentID int, commandID string) {
	ctx, cancel := context.WithCancel(d.lifecycleCtx)

	d.graceMu.Lock()
	cmds := d.grace[agentID]
	if cmds == nil {
		cmds = make(map[string]context.CancelFunc)
		d.grace[agentID] = cmds
	}
	cmds[commandID] = cancel
	d.graceMu.Unlock()

	d.wgMu.Lock()
	d.wg.Add(1)
	d.wgMu.Unlock()
	go d.handleCommandGracePeriod(ctx, agentID, commandID)
}

// cancelGraceForAgent cancels every pending grace timer for an agent. Called
// on reconnect so a dangling 60s "mark FAILED" does not race the new session.
func (d *Dispatcher) cancelGraceForAgent(agentID int) {
	d.graceMu.Lock()
	cmds := d.grace[agentID]
	delete(d.grace, agentID)
	d.graceMu.Unlock()
	for _, cancel := range cmds {
		cancel()
	}
}

// finishGrace removes a grace timer's entry once its goroutine exits.
func (d *Dispatcher) finishGrace(agentID int, commandID string) {
	d.graceMu.Lock()
	defer d.graceMu.Unlock()
	if cmds := d.grace[agentID]; cmds != nil {
		delete(cmds, commandID)
		if len(cmds) == 0 {
			delete(d.grace, agentID)
		}
	}
}

func (d *Dispatcher) handleCommandGracePeriod(ctx context.Context, agentID int, commandID string) {
	defer d.wg.Done()
	defer d.finishGrace(agentID, commandID)

	cmdUUID, err := uuid.Parse(commandID)
	if err != nil {
		return
	}

	// A cancellable timer instead of a bare time.Sleep: a reconnect cancels
	// this context via cancelGraceForAgent, so the timer does not mark a
	// command FAILED out from under the new session.
	select {
	case <-ctx.Done():
		return
	case <-time.After(gracePeriod):
	}

	// Belt-and-suspenders: if the agent reconnected between the timer firing
	// and here, the reconnect path reaps the stale command — leave it alone.
	_, reconnected := d.registry.getAgent(agentID)
	if reconnected {
		return
	}

	// Bound the DB call so a hung Postgres does not accumulate blocked grace
	// goroutines. Previously this used a bare context.Background() with no
	// deadline.
	dbCtx, cancel := context.WithTimeout(d.lifecycleCtx, graceDBTimeout)
	defer cancel()
	status := int32(v1pb.CommandStatus_FAILED)
	now := time.Now()
	if err := d.store.UpdateCommandStatus(dbCtx, cmdUUID, status, nil, &now, nil, nil, "agent disconnected during execution"); err != nil {
		slog.Error("failed to mark command as failed after grace period", "commandID", commandID, "error", err)
	}

	d.closeWatchers(commandID)
	slog.Warn("command marked as FAILED after grace period", "commandID", commandID, "agentID", agentID)
}

func (d *Dispatcher) closeWatchers(commandID string) {
	d.bus.closeOutput(commandID)
}

func (d *Dispatcher) closeEventWatchers(commandID string) {
	d.bus.closeEvents(commandID)
}
