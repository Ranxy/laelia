// The docker backend's observer (design §4): a daemon events stream for
// immediacy plus a reconcile poll as the correctness floor — the manager
// never polls workloads itself, so phase truth is push-only from here. The
// reconcile loop also owns out-of-band container recovery from the spec
// journal (design §5).
package docker

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// eventsRetryWait spaces events-stream reconnects; the reconcile poll covers
// every gap, so aggressive retries are pointless.
const eventsRetryWait = 5 * time.Second

// crashLoop detection: die events inside the window (design §4). Docker keeps
// a crash-looping container in "restarting" — a state the phase mapping
// reports as PROVISIONING — so the counter is what turns repeated crashes
// into FAILED.
const (
	crashLoopWindow    = 10 * time.Minute
	crashLoopThreshold = 3
)

// machineTrack is one machine's observed state: the emission dedup baseline
// and the crash-loop window. Container name is derived (the stem) and never
// stored.
type machineTrack struct {
	machineID string
	// lastPhase is the last reported phase. Terminal phases are sticky
	// manager-side (applyProvisionProgress), so PROVISIONING frames after a
	// FAILED are suppressed here — the manager would ignore them and the
	// stream stays quiet; a real recovery re-opens the phase via PROVISIONED.
	lastPhase storepb.ProvisioningPhase
	dies      []time.Time // die events inside the crash-loop window
}

// recordDie adds a die event to the crash-loop window and reports whether
// the container is now considered crash-looping (Appendix A: the counter is
// the crash-loop detector; docker keeps such containers in "restarting", a
// state the phase mapping alone reports as PROVISIONING).
func (t *machineTrack) recordDie(now time.Time) bool {
	t.dies = append(t.dies, now)
	t.dies = slices.DeleteFunc(t.dies, func(ts time.Time) bool {
		return now.Sub(ts) > crashLoopWindow
	})
	return len(t.dies) >= crashLoopThreshold
}

// watchEvents subscribes to the daemon's container-event stream until ctx is
// done, reconnecting with a fixed wait on stream death; the reconcile poll
// covers the gaps.
func (b *Backend) watchEvents(ctx context.Context) {
	for ctx.Err() == nil {
		if err := b.watchEventsOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("docker events stream lost; reconnecting", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(eventsRetryWait):
		}
	}
}

// watchEventsOnce lives for one events stream. The daemon closes both
// channels when the stream ends — a received (non-nil) error explains it.
func (b *Backend) watchEventsOnce(ctx context.Context) error {
	msgs, errs := b.api.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(filters.Arg("type", string(events.ContainerEventType))),
	})
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if ok && err != nil {
				return err
			}
			return nil
		case msg, ok := <-msgs:
			if !ok {
				// The stream closed: drain the error channel for the reason
				// (it closes alongside, or a healthy close yields zero).
				err, ok2 := <-errs
				if ok2 && err != nil {
					return err
				}
				return nil
			}
			b.handleEvent(ctx, msg)
		}
	}
}

// handleEvent routes one container event to the phase machine. Only machine
// workloads are interesting: the name filter is the cheap first gate.
func (b *Backend) handleEvent(ctx context.Context, msg events.Message) {
	name := strings.TrimPrefix(msg.Actor.Attributes["name"], "/")
	if !strings.HasPrefix(name, workloadPrefix) {
		return
	}
	machineID := b.machineIDFor(ctx, name)
	if machineID == "" {
		return
	}

	switch msg.Action {
	case events.ActionStart, events.ActionRestart:
		b.resetCrash(machineID) // the container ran again; crash-loop debt cleared
		b.reportFromInspect(ctx, machineID, name)

	case events.ActionDie:
		exitCode := msg.Actor.Attributes["exitCode"]
		if b.recordDie(machineID) {
			b.reportFailed(machineID, name,
				"machine container keeps crashing and restarting (last exit code: "+exitCode+")")
			return
		}
		b.reportFromInspect(ctx, machineID, name)

	case events.ActionDestroy:
		// Removed out-of-band (our own teardown marks the machine busy):
		// heal immediately rather than waiting for the next poll.
		if !b.isBusy(machineID) {
			go b.reconcileMachine(ctx, machineID)
		}

	default:
		// create/stop/pause/... carry no phase information.
	}
}

// machineIDFor resolves the machine id behind a container name: tracked
// first, then — after a provisioner restart with a lost journal — recovered
// from the container's own machine-id label.
func (b *Backend) machineIDFor(ctx context.Context, name string) string {
	if machineID := b.trackKeyFor(name); machineID != "" {
		return machineID
	}
	insp, err := b.api.ContainerInspect(ctx, name)
	if err != nil {
		return ""
	}
	machineID := ""
	if insp.Config != nil {
		machineID = insp.Config.Labels[machineIDLabel]
	}
	if machineID != "" {
		b.track(machineID)
	}
	return machineID
}

// reportFromInspect inspects and reports one machine's current truth.
func (b *Backend) reportFromInspect(ctx context.Context, machineID, name string) {
	insp, err := b.api.ContainerInspect(ctx, name)
	if cerrdefs.IsNotFound(err) {
		return // gone: the reconcile loop owns recreation
	}
	if err != nil {
		slog.Warn("failed to inspect the machine container", "machine", machineID, "error", err)
		return
	}
	phase, msg := phaseFromState(insp.State, time.Now())
	b.report(machineID, name, phase, msg)
}

// reconcileLoop is the correctness floor: periodically re-derive every
// tracked machine's phase, healing whatever the event pump missed (dropped
// streams, daemon restarts, provisioner restarts).
func (b *Backend) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	b.reconcileOnce(ctx) // truth immediately at startup, then per tick
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.reconcileOnce(ctx)
		}
	}
}

// reconcileOnce inspects every tracked machine in stable order.
func (b *Backend) reconcileOnce(ctx context.Context) {
	for _, machineID := range b.trackedIDs() {
		b.reconcileMachine(ctx, machineID)
	}
}

// reconcileMachine inspects one machine and either reports its truth or
// rebuilds it: a container that vanished out-of-band with the spec journal
// and data volume intact is recreated (design §5). Machines mid-mutation
// (Provision/Deprovision) are skipped — their owner reports the truth.
func (b *Backend) reconcileMachine(ctx context.Context, machineID string) {
	if b.isBusy(machineID) {
		return
	}
	tr := b.trackRef(machineID)
	if tr == nil {
		return
	}
	name := backend.WorkloadStem(machineID)

	insp, err := b.api.ContainerInspect(ctx, name)
	switch {
	case cerrdefs.IsNotFound(err):
		if !b.recoverMachine(ctx, machineID, name) {
			return
		}
		b.reportFromInspect(ctx, machineID, name)
	case err != nil:
		slog.Warn("failed to inspect the machine container", "machine", machineID, "error", err)
	default:
		phase, msg := phaseFromState(insp.State, time.Now())
		b.report(machineID, name, phase, msg)
	}
}

// recoverMachine recreates a container that was removed out-of-band: the
// journal supplies the spec (minus the credential — the data volume's
// machine.json is authoritative), and the data volume must still exist. It
// reports whether a recovery attempt was made.
func (b *Backend) recoverMachine(ctx context.Context, machineID, name string) bool {
	snapshot, ok := b.journal.get(machineID)
	if !ok {
		return false
	}
	if _, err := b.api.VolumeInspect(ctx, dataVolumeName(name)); err != nil {
		// The volume is gone too: nothing to recover into (the credential is
		// unrecoverable by design) — the UI remedy owns it.
		slog.Warn("machine container and data volume are both gone; recovery impossible",
			"machine", machineID, "volume", dataVolumeName(name))
		return false
	}
	slog.Info("recreating a machine container that was removed out-of-band", "machine", machineID)
	if err := b.createAndStart(ctx, snapshot, ""); err != nil {
		slog.Error("failed to recreate the machine container", "machine", machineID, "error", err)
		return false
	}
	return true
}

// ---- tracked state (all guarded by Backend.mu) ----

// track registers a machine for observation; unknown machines start
// UNSPECIFIED so the first truth report is emitted.
func (b *Backend) track(machineID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.tracked[machineID]; !ok {
		b.tracked[machineID] = &machineTrack{machineID: machineID}
	}
}

// trackRef returns the tracked entry for one machine, if any.
func (b *Backend) trackRef(machineID string) *machineTrack {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tracked[machineID]
}

// trackKeyFor finds the machine id whose container has the given name.
func (b *Backend) trackKeyFor(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, tr := range b.tracked {
		if tr.machineID != "" && backend.WorkloadStem(tr.machineID) == name {
			return tr.machineID
		}
	}
	return ""
}

// trackedIDs lists the tracked machines in stable order.
func (b *Backend) trackedIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.tracked))
	for id := range b.tracked {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// untrack forgets one machine (deprovision).
func (b *Backend) untrack(machineID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tracked, machineID)
}

// recordDie adds a die event to the machine's crash-loop window and reports
// whether the container is now considered crash-looping.
func (b *Backend) recordDie(machineID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	tr, ok := b.tracked[machineID]
	if !ok {
		return false
	}
	return tr.recordDie(time.Now())
}

// resetCrash clears the machine's crash-loop debt (the container ran again).
func (b *Backend) resetCrash(machineID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if tr, ok := b.tracked[machineID]; ok {
		tr.dies = nil
	}
}

// isBusy reports whether a machine is mid-Provision/Deprovision; the
// observer must not recreate or race those paths.
func (b *Backend) isBusy(machineID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.busy[machineID]
}

// setBusy marks the mutation window.
func (b *Backend) setBusy(machineID string, busy bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if busy {
		b.busy[machineID] = true
	} else {
		delete(b.busy, machineID)
	}
}

// report folds one observed phase into the dedup gate and emits on change.
func (b *Backend) report(machineID, name string, phase storepb.ProvisioningPhase, errMsg string) {
	b.mu.Lock()
	tr, ok := b.tracked[machineID]
	if !ok {
		tr = &machineTrack{machineID: machineID}
		b.tracked[machineID] = tr
	}
	if tr.lastPhase == phase || (tr.lastPhase == phaseFailed && phase == phaseProvisioning) {
		b.mu.Unlock()
		return
	}
	tr.lastPhase = phase
	if phase == phaseProvisioned {
		tr.dies = nil
	}
	ch := b.eventCh
	b.mu.Unlock()

	emitEvent(ch, backend.Event{
		MachineID:    machineID,
		Phase:        phase,
		Error:        errMsg,
		WorkloadName: name,
	})
}

// reportAck emits the unconditional PROVISIONING ack (the kubernetes
// backend's shape): every Provision acks the work even when the workload is
// already in sync — the manager folds stale frames away.
func (b *Backend) reportAck(machineID, name string) {
	b.mu.Lock()
	tr, ok := b.tracked[machineID]
	if !ok {
		tr = &machineTrack{machineID: machineID}
		b.tracked[machineID] = tr
	}
	tr.lastPhase = phaseProvisioning
	ch := b.eventCh
	b.mu.Unlock()

	emitEvent(ch, backend.Event{
		MachineID:    machineID,
		Phase:        phaseProvisioning,
		WorkloadName: name,
	})
}

// reportFailed emits FAILED for a crash-looping container (event-driven; the
// phase mapping's FAILED path covers the reconcile side).
func (b *Backend) reportFailed(machineID, name, errMsg string) {
	b.mu.Lock()
	tr, ok := b.tracked[machineID]
	if !ok {
		tr = &machineTrack{machineID: machineID}
		b.tracked[machineID] = tr
	}
	if tr.lastPhase == phaseFailed {
		b.mu.Unlock()
		return
	}
	tr.lastPhase = phaseFailed
	ch := b.eventCh
	b.mu.Unlock()

	emitEvent(ch, backend.Event{
		MachineID:    machineID,
		Phase:        phaseFailed,
		Error:        errMsg,
		WorkloadName: name,
	})
}
