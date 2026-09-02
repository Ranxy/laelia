package mock

// The mock backend's phase progression, idempotency, and test knobs.

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

const testMachineID = "1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e"

// newTestBackend builds a started mock draining events into a buffered channel.
func newTestBackend(t *testing.T) (*Backend, chan backend.Event) {
	t.Helper()
	be, err := New(backend.Config{Namespace: "laelia-machines"})
	require.NoError(t, err)
	m, ok := be.(*Backend)
	require.True(t, ok, "New must return the concrete mock backend")
	require.Equal(t, "mock", m.Name())
	ch := make(chan backend.Event, eventBuffer)
	require.NoError(t, m.Start(context.Background(), ch))
	return m, ch
}

func recvEvent(t *testing.T, ch chan backend.Event) backend.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a backend event")
		return backend.Event{}
	}
}

func assertNoEvent(t *testing.T, ch chan backend.Event) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("unexpected event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func testSpec() backend.MachineSpec {
	return backend.MachineSpec{
		MachineID:    testMachineID,
		Title:        "team workload",
		ManagerURL:   "https://manager.test",
		Fingerprint:  "0123456789abcdef0123456789abcdef",
		RuntimeImage: "laelia/machine-runtime:test",
		BinaryTarget: "linux-x64",
	}
}

func TestProvisionPhaseProgression(t *testing.T) {
	m, ch := newTestBackend(t)

	gate := make(chan struct{})
	m.SetGate(gate)
	done := make(chan error, 1)
	go func() { done <- m.Provision(context.Background(), testSpec(), "refresh-token") }()

	// First event: PROVISIONING with the workload locator.
	e1 := recvEvent(t, ch)
	assert.Equal(t, testMachineID, e1.MachineID)
	assert.Equal(t, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, e1.Phase)
	assert.Equal(t, "laelia-machines/laelia-machine-1f0a9c2d", e1.WorkloadName)
	assertNoEvent(t, ch)

	// Releasing the gate completes the progression to PROVISIONED.
	close(gate)
	e2 := recvEvent(t, ch)
	assert.Equal(t, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, e2.Phase)
	assert.Equal(t, e1.WorkloadName, e2.WorkloadName)
	require.NoError(t, <-done)

	name, ok := m.WorkloadFor(testMachineID)
	require.True(t, ok)
	assert.Equal(t, e1.WorkloadName, name)
	assert.Equal(t, 1, m.ProvisionCalls())
}

func TestProvisionIsIdempotentAcrossReplays(t *testing.T) {
	m, _ := newTestBackend(t)

	// A replayed job re-runs Provision but must never duplicate the workload
	// (the six phase events land in the buffered channel, non-blocking).
	for range 3 {
		require.NoError(t, m.Provision(context.Background(), testSpec(), "token"))
	}
	assert.Equal(t, 3, m.ProvisionCalls())
	assert.Equal(t, 1, m.WorkloadCount())
	name, ok := m.WorkloadFor(testMachineID)
	require.True(t, ok)
	assert.Equal(t, "laelia-machines/laelia-machine-1f0a9c2d", name)
}

func TestDeprovisionReportsDeletedAndDropsWorkload(t *testing.T) {
	m, ch := newTestBackend(t)
	require.NoError(t, m.Provision(context.Background(), testSpec(), "token"))
	recvEvent(t, ch)
	recvEvent(t, ch)

	require.NoError(t, m.Deprovision(context.Background(), testMachineID, false))
	e := recvEvent(t, ch)
	assert.Equal(t, testMachineID, e.MachineID)
	assert.Equal(t, storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED, e.Phase)
	assert.Equal(t, 1, m.DeprovisionCalls())
	assert.False(t, m.DeprovisionKeepData(testMachineID))
	assert.Equal(t, 0, m.WorkloadCount())
}

func TestFailNextDrivesFailedPath(t *testing.T) {
	m, _ := newTestBackend(t)
	m.FailNext(errors.New("quota exceeded"))
	err := m.Provision(context.Background(), testSpec(), "token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")

	// One shot: the next provision succeeds.
	require.NoError(t, m.Provision(context.Background(), testSpec(), "token"))
}

func TestCancelledGateWaitReportsNothing(t *testing.T) {
	m, ch := newTestBackend(t)
	gate := make(chan struct{})
	m.SetGate(gate)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- m.Provision(ctx, testSpec(), "token") }()

	recvEvent(t, ch) // PROVISIONING
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	assertNoEvent(t, ch)
}
