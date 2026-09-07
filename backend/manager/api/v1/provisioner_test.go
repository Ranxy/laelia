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

// TestValidateCustomImageRef locks the shape gate for user-provided runtime
// images: image-reference characters only, so nothing shell- or spec-hostile
// reaches a pod spec.
func TestValidateCustomImageRef(t *testing.T) {
	for _, valid := range []string{
		"ubuntu",
		"ubuntu:22.04",
		"my-registry.example.com:5000/team/app",
		"my-registry.example.com/team/app:v1.2.3",
		"my-registry.example.com/team/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if err := validateCustomImageRef(valid); err != nil {
			t.Errorf("validateCustomImageRef(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{
		"",
		"my registry/app",    // whitespace
		"$(_escape)/app",     // shell metacharacter
		"registry/app:tag;x", // semicolon
		"registry/app\t:tag", // control character
		"-leading-dash/app",  // must start alphanumerically
		"registry/app\nevil", // newline
	} {
		if err := validateCustomImageRef(invalid); err == nil {
			t.Errorf("validateCustomImageRef(%q) = nil, want error", invalid)
		}
	}
}

// TestNormalizeImageRef locks Docker's implicit-default canonicalization: a
// bare Docker Hub reference ("ubuntu") and its fully-qualified form
// ("docker.io/library/ubuntu:latest") are the same image and must normalize to
// one canonical string, so allowlist matching and persistence see one form.
func TestNormalizeImageRef(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		// Docker Hub official images: registry, namespace, and tag default.
		{"ubuntu", "docker.io/library/ubuntu:latest"},
		{"ubuntu:22.04", "docker.io/library/ubuntu:22.04"},
		{"myteam/app", "docker.io/myteam/app:latest"},
		{"library/ubuntu:22.04", "docker.io/library/ubuntu:22.04"},
		// Explicit forms round-trip (idempotence).
		{"docker.io/library/ubuntu", "docker.io/library/ubuntu:latest"},
		{"docker.io/library/ubuntu:22.04", "docker.io/library/ubuntu:22.04"},
		{"registry.example.com/team/app:v1", "registry.example.com/team/app:v1"},
		// Registries with a port and the localhost special case.
		{"localhost:5000/app", "localhost:5000/app:latest"},
		{"localhost/app", "localhost/app:latest"},
		// Digests never gain a :latest default.
		{"ubuntu@sha256:abc", "docker.io/library/ubuntu@sha256:abc"},
		{"ubuntu:v1@sha256:abc", "docker.io/library/ubuntu:v1@sha256:abc"},
		{"registry.example.com/app@sha256:abc", "registry.example.com/app@sha256:abc"},
		// Prefix patterns canonicalize their literal head; no :latest is
		// injected and the wildcard's position survives.
		{"ubuntu:*", "docker.io/library/ubuntu:*"},
		{"ubuntu*", "docker.io/library/ubuntu*"},
		{"*", "*"},
		{"docker.io/library/*", "docker.io/library/*"},
		{"registry.example.com/team/*", "registry.example.com/team/*"},
		{"registry.example.com/*", "registry.example.com/*"},
	} {
		if got := normalizeImageRef(tt.in); got != tt.want {
			t.Errorf("normalizeImageRef(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := normalizeImageRef(""); got != "" {
		t.Errorf(`normalizeImageRef("") = %q, want empty`, got)
	}
}

// TestCustomImageAllowed locks the custom-image allowlist semantics: exact
// entries, "*" suffix prefix patterns, the empty-list-disables rule, and
// canonicalization on both sides (Docker's implicit defaults must not split
// one image across differently-written entries).
func TestCustomImageAllowed(t *testing.T) {
	allowlist := []string{
		"registry.example.com/team/app:v1",
		"registry.example.com/team/*",
		"mirror.local/*",
	}
	for _, allowed := range []string{
		"registry.example.com/team/app:v1",     // exact entry
		"registry.example.com/team/other:beta", // prefix pattern
		"mirror.local/anything",                // prefix pattern
	} {
		if !customImageAllowed(allowed, allowlist) {
			t.Errorf("customImageAllowed(%q) = false, want true", allowed)
		}
	}
	for _, denied := range []string{
		"registry.example.com/other-team/app:v1", // different repository path
		"registry.example.com/teamx/app:v1",      // prefix must not match mid-path
		"evil.example.com/team/app:v1",           // different registry
	} {
		if customImageAllowed(denied, allowlist) {
			t.Errorf("customImageAllowed(%q) = true, want false", denied)
		}
	}
	// An empty (or blank-entry) allowlist disables custom images entirely.
	for _, empty := range [][]string{nil, {}, {"", "  "}} {
		if customImageAllowed("registry.example.com/team/app:v1", empty) {
			t.Errorf("customImageAllowed with empty allowlist %v = true, want false", empty)
		}
	}

	// Docker Hub official images: the caller may omit the registry prefix, so
	// matching canonicalizes both sides before comparing.
	hub := []string{"docker.io/library/*"}
	for _, allowed := range []string{
		"ubuntu:22.04",        // bare official image, explicit tag
		"ubuntu",              // everything implicit
		"library/ubuntu:22.4", // docker.io omitted, namespace kept
	} {
		if !customImageAllowed(allowed, hub) {
			t.Errorf("customImageAllowed(%q, docker.io/library/*) = false, want true", allowed)
		}
	}
	if customImageAllowed("evil/app", hub) {
		t.Error("a non-library docker.io namespace must not match docker.io/library/*")
	}
	// The allowlist entry may itself be written in bare form.
	if !customImageAllowed("ubuntu:22.04", []string{"ubuntu:*"}) {
		t.Error(`customImageAllowed("ubuntu:22.04", ["ubuntu:*"]) = false, want true`)
	}
	if !customImageAllowed("ubuntu", []string{"docker.io/library/ubuntu:latest"}) {
		t.Error("a bare official image must match its canonical exact entry")
	}
	if customImageAllowed("ubuntu:22.04", []string{"docker.io/library/ubuntu:latest"}) {
		t.Error("an exact entry pins the tag: 22.04 must not match :latest")
	}
}

// TestNormalizeCustomImageAllowlist locks the setting-side normalization:
// trimming, blank-entry dropping, canonicalization, and dedupe (a bare entry
// and its fully-qualified form collapse into one); empty normalizes to nil.
func TestNormalizeCustomImageAllowlist(t *testing.T) {
	got := normalizeCustomImageAllowlist([]string{" registry.example.com/team/* ", "", "a/b", "c/d*"})
	want := []string{"registry.example.com/team/*", "docker.io/a/b:latest", "docker.io/c/d*"}
	if len(got) != len(want) {
		t.Fatalf("normalizeCustomImageAllowlist = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeCustomImageAllowlist = %v, want %v", got, want)
		}
	}
	// A bare Docker Hub entry and its canonical form are duplicates.
	got = normalizeCustomImageAllowlist([]string{"ubuntu", "docker.io/library/ubuntu:latest"})
	if len(got) != 1 || got[0] != "docker.io/library/ubuntu:latest" {
		t.Fatalf("normalizeCustomImageAllowlist dedupe = %v, want [docker.io/library/ubuntu:latest]", got)
	}
	if out := normalizeCustomImageAllowlist([]string{"", "  "}); out != nil {
		t.Errorf("blank allowlist = %v, want nil", out)
	}
	if out := normalizeCustomImageAllowlist(nil); out != nil {
		t.Errorf("nil allowlist = %v, want nil", out)
	}
}

// TestResolveProvisionRuntimeImage locks the image preference: the machine's
// persisted custom image wins over the workspace default (so replayed jobs
// rebuild the same workload), and the workspace default applies otherwise.
func TestResolveProvisionRuntimeImage(t *testing.T) {
	const custom = "registry.example.com/team/app:v1"
	const workspace = "laelia/machine-runtime:test"

	if got := resolveProvisionRuntimeImage(custom, workspace); got != custom {
		t.Errorf("custom image must win, got %q", got)
	}
	if got := resolveProvisionRuntimeImage("", workspace); got != workspace {
		t.Errorf("empty machine image must fall back to the workspace default, got %q", got)
	}
	if got := resolveProvisionRuntimeImage("  ", workspace); got != workspace {
		t.Errorf("blank machine image must fall back to the workspace default, got %q", got)
	}
	if got := resolveProvisionRuntimeImage("", ""); got != "" {
		t.Errorf("no image anywhere must resolve empty, got %q", got)
	}
}
