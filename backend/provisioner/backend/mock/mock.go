// Package mock provides the test-only Backend that fakes the full phase
// progression PROVISIONING → PROVISIONED → DELETED, so the provisioner
// skeleton and manager-side integration tests can run the complete
// provisioning journey before a real backend (the kubernetes operator)
// exists. Workloads are keyed by machine id, which is what makes replayed
// jobs idempotent: a second Provision for the same machine observes the same
// single workload instead of creating a duplicate.
package mock

import (
	"context"
	"sync"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// eventBuffer bounds how many events the mock may queue ahead of the client's
// event pump; full buffers drop events (the manager replays non-terminal
// jobs, so a drop is never fatal).
const eventBuffer = 64

// Backend is the fake workload backend. Its knobs (gate, failNext) make the
// phase progression deterministic for tests.
type Backend struct {
	namespace string

	mu                  sync.Mutex
	eventCh             chan<- backend.Event
	workloads           map[string]string            // machineID → workload name
	lastParams          map[string]map[string]string // machineID → last Provision's spec.Params
	provisionCalls      int
	deprovisionCalls    int
	shutdownCalls       int
	deprovisionKeepData map[string]bool
	gate                chan struct{} // when set, Provision blocks before PROVISIONED
	failNext            error         // one-shot: the next Provision returns it
}

// New builds the mock backend (registered under the "mock" name).
func New(cfg backend.Config) (backend.Backend, error) {
	return &Backend{
		namespace:           cfg.Namespace,
		workloads:           map[string]string{},
		deprovisionKeepData: map[string]bool{},
	}, nil
}

func init() {
	backend.Register("mock", New)
}

// Name matches the provisioner row's backend field for tests that register
// the mock.
func (*Backend) Name() string { return "mock" }

// MachineParams reports no parameters: the mock has no sizing concept.
func (*Backend) MachineParams() []*storepb.MachineParamSpec { return nil }

// Start records the event channel the client drains.
func (m *Backend) Start(_ context.Context, ch chan<- backend.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventCh = ch
	return nil
}

// Provision records the call, creates the workload once (idempotent), emits
// PROVISIONING, then — unless the test gate blocks it — PROVISIONED. A
// cancelled ctx unblocks the gate wait without emitting anything further, so
// a killed connection never reports a spurious FAILED.
func (m *Backend) Provision(ctx context.Context, spec backend.MachineSpec, _ string) error {
	m.mu.Lock()
	m.provisionCalls++
	if fail := m.failNext; fail != nil {
		m.failNext = nil
		m.mu.Unlock()
		return fail
	}
	workload, existed := m.workloads[spec.MachineID]
	if !existed {
		workload = backend.WorkloadName(m.namespace, spec.MachineID)
		m.workloads[spec.MachineID] = workload
	}
	if m.lastParams == nil {
		m.lastParams = map[string]map[string]string{}
	}
	m.lastParams[spec.MachineID] = spec.Params
	ch, gate := m.eventCh, m.gate
	m.mu.Unlock()

	m.emit(ch, backend.Event{
		MachineID:    spec.MachineID,
		Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING,
		WorkloadName: workload,
	})

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.emit(ch, backend.Event{
		MachineID:    spec.MachineID,
		Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED,
		WorkloadName: workload,
	})
	return nil
}

// Deprovision removes the workload and reports DELETED.
func (m *Backend) Deprovision(_ context.Context, machineID string, keepData bool) error {
	m.mu.Lock()
	m.deprovisionCalls++
	m.deprovisionKeepData[machineID] = keepData
	delete(m.workloads, machineID)
	ch := m.eventCh
	m.mu.Unlock()

	m.emit(ch, backend.Event{
		MachineID: machineID,
		Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED,
	})
	return nil
}

// Shutdown records the call; the mock has no hosting to tear down.
func (m *Backend) Shutdown(_ context.Context) error {
	m.mu.Lock()
	m.shutdownCalls++
	m.mu.Unlock()
	return nil
}

// emit delivers one event without ever blocking the caller.
func (*Backend) emit(ch chan<- backend.Event, e backend.Event) {
	if ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
	}
}

// ---- test observation knobs ----

// ProvisionCalls reports how many times Provision ran (replays included).
func (m *Backend) ProvisionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.provisionCalls
}

// DeprovisionCalls reports how many times Deprovision ran.
func (m *Backend) DeprovisionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deprovisionCalls
}

// ShutdownCalls reports how many times Shutdown ran.
func (m *Backend) ShutdownCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdownCalls
}

// DeprovisionKeepData reports the keepData flag recorded for one machine.
func (m *Backend) DeprovisionKeepData(machineID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deprovisionKeepData[machineID]
}

// WorkloadCount reports the number of live workloads (never duplicated per
// machine, no matter how often jobs replay).
func (m *Backend) WorkloadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.workloads)
}

// WorkloadFor returns the workload name recorded for one machine.
func (m *Backend) WorkloadFor(machineID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, ok := m.workloads[machineID]
	return name, ok
}

// ParamsFor returns the spec.Params of the last Provision call for one
// machine — the passthrough the client layer is responsible for.
func (m *Backend) ParamsFor(machineID string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastParams[machineID]
}

// SetGate installs the channel Provision waits on right before PROVISIONED —
// tests hold the workload mid-provisioning to exercise replay.
func (m *Backend) SetGate(gate chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = gate
}

// FailNext makes the next Provision return err (one shot), driving the
// manager-side FAILED path.
func (m *Backend) FailNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = err
}
