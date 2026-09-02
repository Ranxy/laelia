package dispatcher

import (
	"context"
	"log/slog"
	"time"
)

func (d *Dispatcher) RegisterAgent(_ context.Context, agentID int, machineID int, agentResourceID string, send SendFunc) *AgentSession {
	// The registry lock is held across the whole check-and-set below: the
	// previous session must be invalidated, any pending grace timer cancelled,
	// and the new session installed atomically. Otherwise a grace goroutine
	// could observe "no session" between cancelGraceForAgent and the map write
	// and mark the command FAILED out from under the reconnecting agent.
	d.registry.mu.Lock()
	defer d.registry.mu.Unlock()

	if old, ok := d.registry.sessions[agentID]; ok {
		slog.Info("replacing existing agent session", "agentID", agentID)
		// Invalidate the previous session's send so in-flight deliver calls
		// error out instead of writing to the torn-down stream. The atomic
		// store is race-free against concurrent deliver readers.
		old.send.Store(nil)
	}

	// The agent reconnected: cancel any pending grace-period "mark FAILED"
	// timers for its in-flight commands. The reconnect path (handleAgentReady)
	// reaps stale RUNNING commands itself, so a dangling 60s timer is redundant
	// and racy (it could mark a command FAILED out from under the new session).
	d.cancelGraceForAgent(agentID)

	sess := &AgentSession{
		agentID:         agentID,
		agentResourceID: agentResourceID,
		machineID:       machineID,
		connectedAt:     time.Now(),
		lastPingAt:      time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.registry.sessions[agentID] = sess
	slog.Info("agent registered for command dispatch", "agentID", agentID, "machineID", machineID)

	// The agent drives its own work via BeginSession; the manager no longer
	// pushes commands on connect. The agent sends AgentReady (handled in the
	// bidi loop) and then its drain loop calls BeginSession as needed.
	return sess
}

func (d *Dispatcher) UnregisterAgent(agentID int) {
	sess, ok := d.registry.deleteAgent(agentID)
	if !ok {
		return
	}
	d.teardownAgentSession(sess)
}

// UnregisterAgentIf tears down the agent session only if sess is still the one
// registered for agentID. The AgentChannel handler uses this for its deferred
// cleanup so that, when a reconnect has replaced the session in the map, the
// old stream's teardown does not delete the new (live) session nor arm a grace
// timer against its in-flight command.
func (d *Dispatcher) UnregisterAgentIf(agentID int, sess *AgentSession) {
	if !d.registry.deleteAgentIf(agentID, sess) {
		return
	}
	d.teardownAgentSession(sess)
}

func (d *Dispatcher) teardownAgentSession(sess *AgentSession) {
	sess.mu.Lock()
	cmdID := sess.currentCmdID
	sess.mu.Unlock()
	// Invalidate send so any concurrent deliver returns "agent session
	// invalidated" rather than writing to the closed stream.
	sess.send.Store(nil)

	slog.Info("agent unregistered from command dispatch", "agentID", sess.agentID)

	if cmdID != "" {
		d.startGracePeriod(sess.agentID, cmdID)
	}
}

func (d *Dispatcher) IsAgentConnected(agentID int) bool {
	_, ok := d.registry.getAgent(agentID)
	return ok
}

// RegisterMachine registers a machine's MachineChannel control stream. A
// machine authenticates once and holds this stream for its lifetime; each of
// its agents opens a separate AgentChannel (registered via RegisterAgent with
// the matching machineID). Returns the session so the stream handler can wire
// up its receive loop.
func (d *Dispatcher) RegisterMachine(machineID int, machineResourceID string, send MachineSendFunc) *MachineSession {
	// Same atomic check-and-set rationale as RegisterAgent: invalidate the old
	// session and install the new one under one critical section.
	d.registry.mu.Lock()
	defer d.registry.mu.Unlock()

	if old, ok := d.registry.machines[machineID]; ok {
		slog.Info("replacing existing machine session", "machineID", machineID)
		old.send.Store(nil)
	}

	sess := &MachineSession{
		machineID:         machineID,
		machineResourceID: machineResourceID,
		connectedAt:       time.Now(),
		lastPingAt:        time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.registry.machines[machineID] = sess
	// A (re)connect ends any previously reported upgrade: the machine either
	// just came back on the new version or never finished the old attempt.
	d.upgradeMu.Lock()
	delete(d.machineUpgrades, machineID)
	d.upgradeMu.Unlock()
	slog.Info("machine registered for control dispatch", "machineID", machineID)
	return sess
}

// UnregisterMachine tears down a machine's control stream AND every agent
// session owned by it. Each owned agent with an in-flight command gets a 60s
// grace period (→ FAILED if the agent does not reconnect). Machine reconnect
// re-registers every agent via RegisterAgent, which cancels each agent's grace
// timer — so no machine-scoped grace tracking is needed.
func (d *Dispatcher) UnregisterMachine(machineID int) {
	machine, owned, ok := d.registry.deleteMachineWithAgents(machineID)
	if !ok {
		return
	}
	d.teardownMachineSession(machine, owned)
}

// UnregisterMachineIf tears down the machine session only if sess is still the
// one registered for machineID. The MachineChannel handler uses this for its
// deferred cleanup so that, when a reconnect has replaced the session in the
// map, the old stream's teardown does not destroy the new (live) session and
// re-arming grace timers against its agents' in-flight commands.
func (d *Dispatcher) UnregisterMachineIf(machineID int, sess *MachineSession) {
	owned, ok := d.registry.deleteMachineIfWithAgents(machineID, sess)
	if !ok {
		return
	}
	d.teardownMachineSession(sess, owned)
}

func (d *Dispatcher) teardownMachineSession(machine *MachineSession, owned []*AgentSession) {
	machine.send.Store(nil)

	for _, sess := range owned {
		sess.mu.Lock()
		cmdID := sess.currentCmdID
		sess.mu.Unlock()
		sess.send.Store(nil)
		if cmdID != "" {
			d.startGracePeriod(sess.agentID, cmdID)
		}
	}
	slog.Info("machine unregistered from control dispatch", "machineID", machine.machineID, "agents", len(owned))
}

func (d *Dispatcher) IsMachineConnected(machineID int) bool {
	_, ok := d.registry.getMachine(machineID)
	return ok
}

// RegisterProvisioner registers a provisioner's ProvisionerChannel stream.
// Mirrors RegisterMachine: invalidate any previous session (a reconnect
// replaced it) and install the new one under one critical section.
func (d *Dispatcher) RegisterProvisioner(provisionerID int, provisionerResourceID string, send ProvisionerSendFunc) *ProvisionerSession {
	d.registry.mu.Lock()
	defer d.registry.mu.Unlock()

	if old, ok := d.registry.provisioners[provisionerID]; ok {
		slog.Info("replacing existing provisioner session", "provisionerID", provisionerID)
		old.send.Store(nil)
	}

	sess := &ProvisionerSession{
		provisionerID:         provisionerID,
		provisionerResourceID: provisionerResourceID,
		connectedAt:           time.Now(),
		lastPingAt:            time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.registry.provisioners[provisionerID] = sess
	slog.Info("provisioner registered for control dispatch", "provisionerID", provisionerID)
	return sess
}

// UnregisterProvisionerIf tears down the provisioner session only if sess is
// still the one registered for provisionerID. The ProvisionerChannel handler
// uses this for its deferred cleanup so that, when a reconnect has replaced
// the session in the map, the old stream's teardown does not destroy the new
// (live) session. Reports whether this call actually tore the session down.
func (d *Dispatcher) UnregisterProvisionerIf(provisionerID int, sess *ProvisionerSession) bool {
	if !d.registry.deleteProvisionerIf(provisionerID, sess) {
		return false
	}
	sess.send.Store(nil)
	slog.Info("provisioner unregistered from control dispatch", "provisionerID", provisionerID)
	return true
}

// UnregisterProvisioner tears down a provisioner's stream regardless of which
// stream instance is registered. Used by rotate/delete flows to close the old
// token's stream from outside the stream handler.
func (d *Dispatcher) UnregisterProvisioner(provisionerID int) {
	if sess, ok := d.registry.deleteProvisioner(provisionerID); ok {
		sess.send.Store(nil)
		slog.Info("provisioner unregistered from control dispatch", "provisionerID", provisionerID)
	}
}

// IsProvisionerConnected reports whether a ProvisionerChannel stream is live
// for the provisioner right now.
func (d *Dispatcher) IsProvisionerConnected(provisionerID int) bool {
	_, ok := d.registry.getProvisioner(provisionerID)
	return ok
}
