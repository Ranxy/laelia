package v1

import (
	"testing"
	"time"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

func phaseOf(p *storepb.ProvisioningStatus) storepb.ProvisioningPhase {
	if p == nil {
		return storepb.ProvisioningPhase_PROVISIONING_PHASE_UNSPECIFIED
	}
	return p.Phase
}

// TestApplyProvisionProgressStateMachine locks in the frame-driven phase
// transitions: allowed advances stamp the matching timestamp, terminal phases
// never move, stale/out-of-order frames are ignored, and error/workload data is
// recorded on every accepted transition.
func TestApplyProvisionProgress(t *testing.T) {
	pending := func() *storepb.ProvisioningStatus {
		return &storepb.ProvisioningStatus{
			Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_PENDING,
			PendingAt: time.Now().Unix(),
		}
	}

	t.Run("pending to provisioning", func(t *testing.T) {
		next := applyProvisionProgress(pending(), storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", "ns/wl")
		if phaseOf(next) != storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING {
			t.Fatalf("phase = %v", next.Phase)
		}
		if next.WorkloadName != "ns/wl" {
			t.Errorf("workload name = %q", next.WorkloadName)
		}
		if next.PendingAt == 0 {
			t.Error("pending_at must survive the transition")
		}
	})

	t.Run("pending direct to provisioned", func(t *testing.T) {
		next := applyProvisionProgress(pending(), storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, "", "")
		if phaseOf(next) != storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED {
			t.Fatalf("phase = %v", next.Phase)
		}
		if next.ProvisionedAt == 0 {
			t.Error("provisioned_at must be stamped")
		}
	})

	t.Run("provisioning to failed records error and timestamp", func(t *testing.T) {
		cur := &storepb.ProvisioningStatus{
			Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING,
			PendingAt: 1,
		}
		next := applyProvisionProgress(cur, storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, "image pull backoff", "ns/wl")
		if phaseOf(next) != storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED {
			t.Fatalf("phase = %v", next.Phase)
		}
		if next.Error != "image pull backoff" {
			t.Errorf("error = %q", next.Error)
		}
		if next.FailedAt == 0 {
			t.Error("failed_at must be stamped")
		}
	})

	t.Run("failed machine can still be deprovisioned", func(t *testing.T) {
		cur := &storepb.ProvisioningStatus{Phase: storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, FailedAt: 1}
		next := applyProvisionProgress(cur, storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING, "", "")
		if phaseOf(next) != storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING {
			t.Fatalf("phase = %v", next.Phase)
		}
		// The failure reason survives the teardown transition.
		if next.FailedAt != 1 {
			t.Error("failed_at must survive")
		}
	})

	t.Run("deprovisioning to deleted is terminal", func(t *testing.T) {
		cur := &storepb.ProvisioningStatus{Phase: storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING}
		next := applyProvisionProgress(cur, storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED, "", "")
		if phaseOf(next) != storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED {
			t.Fatalf("phase = %v", next.Phase)
		}
		if again := applyProvisionProgress(next, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", ""); phaseOf(again) != storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED {
			t.Fatalf("DELETED must be terminal, got %v", again.Phase)
		}
	})

	t.Run("stale and out-of-order frames are ignored", func(t *testing.T) {
		provisioned := &storepb.ProvisioningStatus{
			Phase:         storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED,
			ProvisionedAt: 42,
		}
		// A late PROVISIONING ack must not demote the machine.
		if next := applyProvisionProgress(provisioned, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", ""); next != provisioned {
			t.Fatalf("stale frame must return the unchanged status, got phase %v", next.Phase)
		}
		// FAILED cannot resurrect to PROVISIONING (retry is delete + re-create).
		failed := &storepb.ProvisioningStatus{Phase: storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED}
		if next := applyProvisionProgress(failed, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", ""); next != failed {
			t.Fatalf("FAILED must be terminal for provisioning frames, got %v", next.Phase)
		}
		// UNSPECIFIED progress carries no information.
		if next := applyProvisionProgress(provisioned, storepb.ProvisioningPhase_PROVISIONING_PHASE_UNSPECIFIED, "boom", ""); next != provisioned {
			t.Fatal("unspecified progress must be ignored")
		}
	})

	t.Run("nil status is never started by a frame", func(t *testing.T) {
		if next := applyProvisionProgress(nil, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", ""); next != nil {
			t.Fatal("progress for a machine without provisioning state must be ignored")
		}
	})

	t.Run("input row is never mutated in place", func(t *testing.T) {
		cur := &storepb.ProvisioningStatus{
			Phase:          storepb.ProvisioningPhase_PROVISIONING_PHASE_PENDING,
			WorkloadLabels: map[string]string{"a": "1"},
		}
		next := applyProvisionProgress(cur, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", "ns/wl")
		if cur.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING {
			t.Fatal("applyProvisionProgress mutated the caller's status in place")
		}
		if next.WorkloadName != "ns/wl" {
			t.Errorf("workload name = %q", next.WorkloadName)
		}
		next.Error = "mutated"
		if cur.Error != "" {
			t.Fatal("clone shares memory with the caller's status")
		}
	})
}

// TestKnownProvisionerBackends locks the ProvisionMachine backend gate: only
// implemented backends may provision; the registry itself is permissive.
func TestKnownProvisionerBackends(t *testing.T) {
	if !knownProvisionerBackends["kubernetes"] {
		t.Error("kubernetes must be a known backend")
	}
	for _, unknown := range []string{"", "docker", "k8s", "Kubernetes"} {
		if knownProvisionerBackends[unknown] {
			t.Errorf("backend %q must not be known", unknown)
		}
	}
}
