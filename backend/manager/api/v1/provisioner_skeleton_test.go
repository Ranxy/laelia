package v1

// Provisioner skeleton integration test — the Phase 3 milestone demo.
//
// The real laelia-provisioner client loop (backend/provisioner/client) runs
// against the real manager stack with the mock backend, covering the exit
// criteria: connect → receive a job → mock events update machine row phases
// in the DB → client killed mid-provisioning → reconnect replays the same
// job without duplicating the workload → backend failure drives FAILED →
// DeleteMachine drives deprovision → rotate delivers the disconnect notice
// and the client stops instead of retrying with a dead credential.
//
// Gated like the other provisioner tests: LAELIA_RUN_PROVISIONER_TESTS=1 +
// LAELIA_TEST_PG_URL.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	provisionbackend "github.com/Ranxy/laelia/backend/provisioner/backend"
	"github.com/Ranxy/laelia/backend/provisioner/backend/mock"
	provisionclient "github.com/Ranxy/laelia/backend/provisioner/client"
	provisionercmd "github.com/Ranxy/laelia/backend/provisioner/cmd"
)

// newSkeletonMock builds a started mock backend with the namespace the
// workload locator is derived from.
func newSkeletonMock(t *testing.T) *mock.Backend {
	t.Helper()
	be, err := mock.New(provisionbackend.Config{Namespace: "laelia-machines"})
	require.NoError(t, err)
	mb, ok := be.(*mock.Backend)
	require.True(t, ok, "mock.New must return the concrete mock backend")
	return mb
}

func TestProvisionerSkeletonClient(t *testing.T) {
	env := newProvisionerTestEnv(t)
	ctx := context.Background()
	adminClient := env.provisionerServiceClient(t, env.adminToken(t))
	memberClient := env.provisionerServiceClient(t, env.memberToken(t))

	// ---- 1. admin registers the provisioner; the client runs the mock ----
	created, err := adminClient.CreateProvisioner(ctx, connect.NewRequest(&v1pb.CreateProvisionerRequest{
		Provisioner: &v1pb.Provisioner{Title: "skel-cluster", Backend: "kubernetes"},
	}))
	require.NoError(t, err)
	token := created.Msg.GetToken()
	provName := created.Msg.GetProvisioner().GetName()

	mb := newSkeletonMock(t)
	// The Ready frame carries the config digest computed by the cmd layer.
	digest := (&provisionercmd.Config{Backend: "kubernetes", Namespace: "laelia-machines"}).Digest()

	// startClient launches one real client loop; the returned channel yields
	// Run's terminal error (nil = clean shutdown by cancellation).
	startClient := func(provToken string, be provisionbackend.Backend) (context.CancelFunc, <-chan error) {
		t.Helper()
		c, err := provisionclient.New(provisionclient.Config{
			ManagerURL:   env.server.URL,
			Token:        provToken,
			Backend:      "mock",
			ConfigDigest: digest,
			Insecure:     true, // httptest's self-signed certificate
		}, be)
		require.NoError(t, err)
		runCtx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- c.Run(runCtx) }()
		return cancel, done
	}

	// ---- 2. the client connects and is registered with its config digest ----
	cancel1, done1 := startClient(token, mb)
	t.Cleanup(cancel1)
	require.Eventually(t, func() bool {
		st := env.provisionerStatus(t, provName)
		return st != nil && st.Connected && st.ConfigDigest == digest
	}, 10*time.Second, 50*time.Millisecond, "the provisioner must come online and report its config digest")

	// ---- 3. provision machine A; the mock acks PROVISIONING then parks ----
	gate := make(chan struct{})
	mb.SetGate(gate)
	resp, err := memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: provName,
		Title:       "skeleton workload",
	}))
	require.NoError(t, err)
	machineA := resp.Msg
	idA := strings.TrimPrefix(machineA.GetName(), common.MachineNamePrefix)

	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, idA)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING
	}, 10*time.Second, 50*time.Millisecond, "the mock's PROVISIONING event must land in the machine row")
	require.Equal(t, 1, mb.ProvisionCalls())
	workload, ok := mb.WorkloadFor(idA)
	require.True(t, ok)
	require.Equal(t, "laelia-machines/laelia-machine-"+idA[:8], workload)
	require.Equal(t, workload, env.machineProvisioning(t, idA).WorkloadName)

	// ---- 4. kill -9: the process dies mid-provisioning ----
	cancel1()
	select {
	case err := <-done1:
		require.NoError(t, err, "a cancelled client must shut down cleanly")
	case <-time.After(10 * time.Second):
		t.Fatal("the provisioner client did not stop after cancellation")
	}
	require.Eventually(t, func() bool {
		st := env.provisionerStatus(t, provName)
		return st != nil && !st.Connected
	}, 10*time.Second, 50*time.Millisecond, "the dead stream must mark the provisioner offline")

	// ---- 5. reconnect: the in-flight job replays, with no duplicate workload ----
	cancel2, done2 := startClient(token, mb)
	t.Cleanup(cancel2)
	require.Eventually(t, func() bool {
		return mb.ProvisionCalls() == 2
	}, 10*time.Second, 50*time.Millisecond,
		"the replay must re-run Provision for the in-flight job")
	require.Equal(t, 1, mb.WorkloadCount(),
		"the replayed job must not duplicate the workload (idempotent backend upsert)")
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, idA)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING
	}, 5*time.Second, 50*time.Millisecond,
		"the replay's stale PROVISIONING ack must not regress the phase")

	// ---- 6. release the gate; the replayed job completes provisioning ----
	close(gate)
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, idA)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED && p.ProvisionedAt > 0
	}, 10*time.Second, 50*time.Millisecond)

	// ---- 7. a backend failure drives FAILED with the error text ----
	mb.FailNext(errors.New("quota exceeded"))
	respB, err := memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: provName,
		Title:       "doomed workload",
	}))
	require.NoError(t, err)
	idB := strings.TrimPrefix(respB.Msg.GetName(), common.MachineNamePrefix)
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, idB)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED &&
			strings.Contains(p.Error, "quota exceeded") && p.FailedAt > 0
	}, 10*time.Second, 50*time.Millisecond,
		"a backend provision error must fail the machine row with the reason")
	require.Equal(t, 1, mb.WorkloadCount(), "a failed provision must not leave a workload behind")

	// ---- 8. DeleteMachine: teardown job → DELETED on the soft-deleted row ----
	machineClient := env.machineServiceClient(t, env.adminToken(t))
	_, err = machineClient.DeleteMachine(ctx, connect.NewRequest(&v1pb.DeleteMachineRequest{Name: machineA.GetName()}))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, idA)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED
	}, 10*time.Second, 50*time.Millisecond,
		"the deprovision job must complete the teardown on the soft-deleted row")
	rowA, err := env.store.GetMachineByResourceID(ctx, idA)
	require.NoError(t, err)
	require.True(t, rowA.Deleted)
	require.Equal(t, 1, mb.DeprovisionCalls())
	require.False(t, mb.DeprovisionKeepData(idA), "keep_data defaults to false in the MVP")
	require.Equal(t, 0, mb.WorkloadCount(), "deprovision removes the workload")

	// ---- 9. rotate: the disconnect notice stops the client ----
	_, err = adminClient.RotateProvisionerToken(ctx, connect.NewRequest(&v1pb.RotateProvisionerTokenRequest{Name: provName}))
	require.NoError(t, err)
	select {
	case err := <-done2:
		require.ErrorIs(t, err, provisionclient.ErrShutdown,
			"a rotated token must stop the client instead of retrying forever")
	case <-time.After(15 * time.Second):
		t.Fatal("the provisioner did not stop after the disconnect notice")
	}

	// ---- 10. the old token is permanently rejected, not retried ----
	cancel3, done3 := startClient(token, newSkeletonMock(t))
	t.Cleanup(cancel3)
	select {
	case err := <-done3:
		require.Error(t, err)
		require.False(t, errors.Is(err, provisionclient.ErrShutdown))
		require.True(t, provisionclient.IsPermanentAuthFailure(err),
			"a rotated token is a permanent auth failure: exit instead of retrying")
	case <-time.After(15 * time.Second):
		t.Fatal("a rotated token must fail fast instead of retrying forever")
	}
}
