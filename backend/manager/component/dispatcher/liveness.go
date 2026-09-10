package dispatcher

import (
	"log/slog"
	"time"

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

// Stop cancels the dispatcher's lifecycle context (ping monitor and any other
// lifecycle goroutines) and waits for them to exit. Idempotent.
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
	machines := d.registry.snapshotMachines()
	provisioners := d.registry.snapshotProvisioners()

	now := time.Now()

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
			// Reconnect guard: only tear down the exact session that timed
			// out (a reconnect may have replaced it since the snapshot).
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
			// Same reconnect guard as for machines: only tear down the exact
			// session that timed out.
			d.UnregisterProvisionerIf(provisionerID, p)
		}
	}
}

func (d *Dispatcher) closeWatchers(commandID string) {
	d.bus.closeOutput(commandID)
}

func (d *Dispatcher) closeEventWatchers(commandID string) {
	d.bus.closeEvents(commandID)
}
