package dispatcher

import (
	"context"
	"log/slog"
	"time"
)

// RegisterMachine registers a machine's MachineChannel control stream. A
// machine authenticates once and holds this stream for its lifetime.
// Returns the session so the stream handler can wire up its receive loop.
func (d *Dispatcher) RegisterMachine(machineID int, machineResourceID string, send MachineSendFunc) *MachineSession {
	// The registry lock is held across the whole check-and-set: the previous
	// session must be invalidated and the new one installed atomically, so a
	// racing send never writes to the torn-down stream.
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

// UnregisterMachine tears down a machine's control stream. Command turns do
// not depend on the stream anymore: a RUNNING command left behind is reaped by
// the stale-command sweep once the machine's dual-signal loss (stream gone +
// heartbeat expired) outlasts the grace period.
func (d *Dispatcher) UnregisterMachine(machineID int) {
	sess, ok := d.registry.deleteMachine(machineID)
	if !ok {
		return
	}
	d.teardownMachineSession(sess)
}

// UnregisterMachineIf tears down the machine session only if sess is still the
// one registered for machineID. The MachineChannel handler uses this for its
// deferred cleanup so that, when a reconnect has replaced the session in the
// map, the old stream's teardown does not destroy the new (live) session.
func (d *Dispatcher) UnregisterMachineIf(machineID int, sess *MachineSession) {
	if !d.registry.deleteMachineIf(machineID, sess) {
		return
	}
	d.teardownMachineSession(sess)
}

func (*Dispatcher) teardownMachineSession(machine *MachineSession) {
	machine.send.Store(nil)
	slog.Info("machine unregistered from control dispatch", "machineID", machine.machineID)
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

// IsAgentOnline reports whether the agent's hosting machine has a live
// MachineChannel. The per-agent stream is retired: an agent is online exactly
// when its machine is. Used by callers that only hold an agent id (scheduler,
// activity); callers that already know the machine should use
// IsMachineConnected directly. Returns false for unknown agents.
func (d *Dispatcher) IsAgentOnline(ctx context.Context, agentID int) bool {
	if d.store == nil {
		return false
	}
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil || agent.Deleted || agent.MachineID == 0 {
		return false
	}
	return d.IsMachineConnected(agent.MachineID)
}
