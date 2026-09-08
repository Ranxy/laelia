package docker

// Hermetic tests: no daemon dial happens in construction or in any of the
// exercised paths (the factory defers the first contact to Start's Ping, so
// the whole package tests without docker). Daemon-dependent behavior lives in
// the env-gated integration file.

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common/quantity"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

func TestRegisteredAsDocker(t *testing.T) {
	// The registry lookup precedes the daemon contact, so construction with
	// any config succeeds (an unregistered name would fail with
	// ErrUnsupportedBackend instead) — the docker analog of the kubernetes
	// factory's hermetic construction contract.
	be, err := New(backend.Config{})
	require.NoError(t, err)
	assert.Equal(t, "docker", be.Name())
}

func TestWorkloadPrefixMatchesSharedStem(t *testing.T) {
	// The event pump filters container names on workloadPrefix; a drift
	// between the constant and backend.WorkloadStem would silently drop
	// every event.
	assert.Equal(t, backend.WorkloadStem("a1b2c3d4-e5f6-7890-abcd-ef0123456789"),
		workloadPrefix+"a1b2c3d4")
}

func TestMachineContainerConfig(t *testing.T) {
	snapshot, err := newSpecSnapshot(backend.MachineSpec{
		MachineID:       "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		Title:           "test machine",
		ManagerURL:      "https://manager.example.com",
		Fingerprint:     "fingerprint",
		RuntimeImage:    "laelia/machine-runtime:1",
		BootstrapScript: "#!/bin/sh\necho hi",
		Labels:          map[string]string{"team": "core"},
	}, backend.Config{})
	require.NoError(t, err)

	cfg, hostCfg := machineContainerConfig(snapshot)

	assert.Equal(t, "laelia-machine-a1b2c3d4", cfg.Hostname)
	assert.Equal(t, "laelia/machine-runtime:1", cfg.Image)
	assert.Equal(t, []string{"/bin/sh", "-c"}, []string(cfg.Entrypoint))
	// The command is the shared inline startup, prefixed by the bootstrap
	// stage — the image's own entrypoint is irrelevant (parity with the
	// kubernetes pod spec).
	assert.Equal(t, []string{
		bootstrapMount + "/bootstrap.sh || exit $?;\n" + backend.MachineRunScript,
	}, []string(cfg.Cmd))
	assert.Contains(t, cfg.Cmd[0], "exec \"$BIN\" \"$@\"")

	// Env is sorted and carries the pod contract (design §2); the image's own
	// env (HOME, LAELIA_MACHINE_BIN) stays in place.
	assert.Equal(t, []string{
		"CODEX_HOME=/data/laelia/codex",
		"LAELIA_FINGERPRINT=fingerprint",
		"LAELIA_HOME=/data/laelia",
		"LAELIA_MANAGER_URL=https://manager.example.com",
		"LAELIA_PROVISIONED=true",
	}, cfg.Env)

	// Labels: passthrough under the reserved set, plus the digest marker.
	assert.Equal(t, "laelia-provisioner", cfg.Labels[managedByLabel])
	assert.Equal(t, "laelia-machine", cfg.Labels[appNameLabel])
	assert.Equal(t, "laelia-machine-a1b2c3d4", cfg.Labels[machineLabel])
	assert.Equal(t, "a1b2c3d4-e5f6-7890-abcd-ef0123456789", cfg.Labels[machineIDLabel])
	assert.Equal(t, "core", cfg.Labels["team"])
	assert.Equal(t, snapshot.digest(), cfg.Labels[specDigestLabel])

	// Host config: named data volume at /data, unless-stopped, sizing.
	require.Len(t, hostCfg.Mounts, 1)
	assert.Equal(t, "laelia-machine-a1b2c3d4-data", hostCfg.Mounts[0].Source)
	assert.Equal(t, dataMountPoint, hostCfg.Mounts[0].Target)
	assert.Equal(t, container.RestartPolicyUnlessStopped, hostCfg.RestartPolicy.Name)
	assert.Zero(t, hostCfg.NanoCPUs) // unset sizing = no limit
	assert.Zero(t, hostCfg.Memory)
}

func TestMachineContainerConfigResources(t *testing.T) {
	snapshot, err := newSpecSnapshot(backend.MachineSpec{
		MachineID: "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		Params:    map[string]string{"cpu": "500m", "memory": "2Gi"},
	}, backend.Config{})
	require.NoError(t, err)
	_, hostCfg := machineContainerConfig(snapshot)
	assert.Equal(t, int64(500_000_000), hostCfg.NanoCPUs) // 0.5 CPU
	assert.Equal(t, int64(2<<30), hostCfg.Memory)
}

func TestSnapshotDigestStabilityAndCredentialBlindness(t *testing.T) {
	spec := backend.MachineSpec{
		MachineID:  "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		ManagerURL: "https://manager.example.com",
	}
	first, err := newSpecSnapshot(spec, backend.Config{})
	require.NoError(t, err)

	// A re-minted token never changes the snapshot: the digest must be
	// credential-blind for replayed jobs to stay no-ops (design §3/§6).
	second, err := newSpecSnapshot(spec, backend.Config{})
	require.NoError(t, err)
	assert.Equal(t, first.digest(), second.digest())

	// The token never appears anywhere in the persisted shape.
	data, jsonErr := json.Marshal(first)
	require.NoError(t, jsonErr)
	assert.NotContains(t, string(data), "token")

	// A backend-affecting change flips the digest.
	spec.ManagerURL = "https://other.example.com"
	third, err := newSpecSnapshot(spec, backend.Config{})
	require.NoError(t, err)
	assert.NotEqual(t, first.digest(), third.digest())
}

func TestResolveResource(t *testing.T) {
	cfg := backend.Config{Resources: backend.Resources{
		Requests: map[string]string{"cpu": "1", "memory": "1Gi"},
		Limits:   map[string]string{"cpu": "2", "memory": "4Gi"},
	}}

	// Params override the config defaults.
	cpu, err := resolveResource("cpu", cfg, map[string]string{"cpu": "500m"})
	require.NoError(t, err)
	assert.Equal(t, quantity.Milli(500), cpu)

	// No param: Limits win over Requests.
	memory, err := resolveResource("memory", cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, quantity.Milli(4000<<30), memory)

	// Unset everywhere resolves to zero (docker: no limit).
	disk, err := resolveResource("disk", backend.Config{}, nil)
	require.NoError(t, err)
	assert.Zero(t, disk)

	// Invalid values fail the provision (surfacing as FAILED).
	_, err = resolveResource("cpu", cfg, map[string]string{"cpu": "bogus"})
	require.Error(t, err)
	_, err = resolveResource("memory", cfg, map[string]string{"memory": "bogus"})
	require.Error(t, err)
}

func TestPhaseFromState(t *testing.T) {
	now := time.Now()
	fresh := now.Format(time.RFC3339Nano)
	stale := now.Add(-5 * time.Minute).Format(time.RFC3339Nano)

	tests := []struct {
		name      string
		state     *container.State
		wantPhase storepb.ProvisioningPhase
		wantErr   string
	}{
		{"nil state", nil, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, ""},
		{"running", &container.State{Running: true},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, ""},
		{"created", &container.State{Status: container.StateCreated},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, ""},
		{"restarting", &container.State{Status: container.StateRestarting},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, ""},
		{"removing", &container.State{Status: container.StateRemoving},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, ""},
		{"fresh exit", &container.State{Status: container.StateExited, ExitCode: 1, FinishedAt: fresh},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, ""},
		{"stale exit", &container.State{Status: container.StateExited, ExitCode: 137, FinishedAt: stale},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, "exit code 137"},
		{"oom", &container.State{Status: container.StateExited, ExitCode: 137, OOMKilled: true, FinishedAt: stale},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, "OOM killer"},
		{"paused", &container.State{Status: container.StatePaused, Running: true, Paused: true, FinishedAt: stale},
			storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, "paused"},
	}
	for _, tt := range tests {
		phase, msg := phaseFromState(tt.state, now)
		assert.Equal(t, tt.wantPhase, phase, tt.name)
		if tt.wantErr != "" {
			assert.Contains(t, msg, tt.wantErr, tt.name)
		}
	}
}

func TestCrashLoopCounter(t *testing.T) {
	tr := &machineTrack{machineID: "m"}
	now := time.Now()
	assert.False(t, tr.recordDie(now))
	assert.False(t, tr.recordDie(now.Add(time.Minute)), "two dies in the window are not a crash loop")
	assert.True(t, tr.recordDie(now.Add(2*time.Minute)), "the third die trips the window")

	// Old dies expire out of the window.
	tr2 := &machineTrack{machineID: "m"}
	tr2.recordDie(now)
	assert.False(t, tr2.recordDie(now.Add(crashLoopWindow+time.Minute)),
		"a die outside the window must not accumulate")
}

func TestSpecJournalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-specs.json")
	snapshot, err := newSpecSnapshot(backend.MachineSpec{
		MachineID:  "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		ManagerURL: "https://manager.example.com",
	}, backend.Config{})
	require.NoError(t, err)

	j := openSpecJournal(path)
	j.put(snapshot)

	// A fresh journal process reads the same shape (provisioner restart).
	reopened := openSpecJournal(path)
	got, ok := reopened.get(snapshot.MachineID)
	require.True(t, ok)
	assert.Equal(t, snapshot.digest(), got.digest())

	reopened.remove(snapshot.MachineID)
	assert.Empty(t, reopened.machineIDs())
	assert.Empty(t, openSpecJournal(path).machineIDs())
}

func TestSpecJournalCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-specs.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	j := openSpecJournal(path)
	assert.Empty(t, j.machineIDs(), "a corrupt journal starts empty instead of failing")
}

func TestBootstrapTar(t *testing.T) {
	credentialTar, err := bootstrapTar([]byte("script"), []byte(`{"machine_id":"m"}`))
	require.NoError(t, err)
	entries := tarEntries(t, credentialTar)
	require.Len(t, entries, 3)
	assert.EqualValues(t, tar.TypeDir, entries[0].Typeflag)
	assert.Equal(t, "bootstrap/", entries[0].Name)
	assert.EqualValues(t, tar.TypeReg, entries[1].Typeflag)
	assert.Equal(t, "bootstrap/bootstrap.sh", entries[1].Name)
	assert.EqualValues(t, tar.TypeReg, entries[2].Typeflag)
	assert.Equal(t, "bootstrap/machine.json", entries[2].Name)
	assert.Equal(t, int64(0o644), entries[2].Mode, "the credential file must stay readable by the container user")

	// Journal-based recreation carries no credential.
	recoveryTar, err := bootstrapTar([]byte("script"), nil)
	require.NoError(t, err)
	assert.Len(t, tarEntries(t, recoveryTar), 2)
}

func TestMachineParams(t *testing.T) {
	b := &Backend{cfg: backend.Config{
		Resources: backend.Resources{
			Limits:   map[string]string{"cpu": "4", "memory": "8Gi"},
			Requests: map[string]string{"cpu": "1", "memory": "1Gi"},
		},
		ParamBounds: map[string]backend.ParamBounds{
			"cpu": {Min: "100m", Max: "8"},
		},
	}}
	specs := b.MachineParams()
	require.Len(t, specs, 2)
	assert.Equal(t, "cpu", specs[0].Key)
	assert.Equal(t, "4", specs[0].DefaultValue, "docker defaults come from limits, not requests")
	assert.Equal(t, "100m", specs[0].MinValue)
	assert.Equal(t, "8", specs[0].MaxValue)
	assert.Equal(t, "memory", specs[1].Key)
	assert.Equal(t, "8Gi", specs[1].DefaultValue)
}

// tarEntries decodes the tar stream produced by bootstrapTar.
func tarEntries(t *testing.T, r *bytes.Reader) []tar.Header {
	t.Helper()
	tr := tar.NewReader(r)
	var entries []tar.Header
	for {
		hdr, err := tr.Next()
		if err != nil {
			require.ErrorIs(t, err, io.EOF)
			return entries
		}
		entries = append(entries, *hdr)
	}
}

func TestBootstrapCommandRunsSharedStartup(t *testing.T) {
	// The injected command runs the bootstrap stage and then the shared
	// machine startup — the image's own entrypoint is never consulted, which
	// is what lets any contract image run the machine.
	cmd := bootstrapCommand()
	assert.Contains(t, cmd[0], bootstrapMount+"/bootstrap.sh || exit $?;")
	assert.Contains(t, cmd[0], backend.MachineRunScript)
	assert.Contains(t, cmd[0], "exec \"$BIN\" \"$@\"")
}
