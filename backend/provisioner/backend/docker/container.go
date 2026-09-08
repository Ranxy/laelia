// Object builders for the docker backend: the machine container, its data
// volume, the bootstrap payload copied into the created container, and the
// inspect-state → provisioning-phase mapping (design §2, §4).
package docker

import (
	"archive/tar"
	"bytes"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// Fixed runtime choices (design §2.2): the docker backend adds no config
// keys, so these are constants — an operator needing different values runs a
// different backend or files a follow-up.
const (
	// dataMountPoint is where the machine's named data volume mounts — the
	// whole LAELIA_HOME world (binary, machine.json, workspaces, logs).
	dataMountPoint = "/data"
	// bootstrapMount is where the credential payload is copied before the
	// container starts (the docker analog of the k8s bootstrap secret mount).
	bootstrapMount = "/bootstrap"
	// restartPolicy keeps machines alive across daemon/host restarts but
	// respects an explicit docker stop (maintenance).
	restartPolicy = container.RestartPolicyUnlessStopped
	// stopTimeoutSeconds is the SIGTERM grace before SIGKILL on teardown and
	// drift recreate.
	stopTimeoutSeconds = 15
	// transientExitGrace covers the window between a die and the restart
	// policy's next attempt: a freshly-exited container is transient
	// (PROVISIONING), a settled one means nothing will bring it back.
	transientExitGrace = 60 * time.Second
)

// Docker label conventions, mirroring the kubernetes backend's object labels.
const (
	managedByLabel  = "app.kubernetes.io/managed-by"
	managedByValue  = "laelia-provisioner"
	appNameLabel    = "app.kubernetes.io/name"
	appNameValue    = "laelia-machine"
	machineLabel    = "laelia.sh/machine"    // container-name stem (k8s MachineNameLabel parity)
	machineIDLabel  = "laelia.sh/machine-id" // full manager resource id; event routing depends on it
	specDigestLabel = "laelia.sh/spec-digest"
)

// Phase aliases keep the mapping readable.
const (
	phaseProvisioning = storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING
	phaseProvisioned  = storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED
	phaseFailed       = storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED
)

// dataVolumeName is the machine's persistent data volume.
func dataVolumeName(name string) string { return name + "-data" }

// bootstrapCommand builds the container command: run the manager-rendered
// bootstrap script first (idempotent: skips the download when the binary is
// on the volume, seeds machine.json only when absent), then run the shared
// machine startup (backend.MachineRunScript — the same script the kubernetes
// pod injects, so the image's own entrypoint is irrelevant). A failing stage
// exits the container, so the restart policy retries — the docker analog of a
// failing init container.
func bootstrapCommand() []string {
	return []string{bootstrapMount + "/bootstrap.sh || exit $?;\n" + backend.MachineRunScript}
}

// machineContainerConfig builds the machine container + host config (design
// §2). The env map is applied sorted for deterministic configs.
func machineContainerConfig(s specSnapshot) (*container.Config, *container.HostConfig) {
	name := backend.WorkloadStem(s.MachineID)

	labels := map[string]string{}
	for k, v := range s.Labels {
		labels[k] = v
	}
	labels[specDigestLabel] = s.digest()

	env := make([]string, 0, len(s.Env))
	for _, k := range slices.Sorted(maps.Keys(s.Env)) {
		env = append(env, k+"="+s.Env[k])
	}

	cfg := &container.Config{
		Hostname:   name,
		Image:      s.RuntimeImage,
		Entrypoint: []string{"/bin/sh", "-c"},
		Cmd:        bootstrapCommand(),
		Env:        env,
		Labels:     labels,
	}
	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: dataVolumeName(name),
			Target: dataMountPoint,
		}},
		RestartPolicy: container.RestartPolicy{Name: restartPolicy},
		Resources: container.Resources{
			// Zero stays zero — docker reads it as "no limit".
			NanoCPUs: int64(s.CPUMilli) * 1_000_000,
			Memory:   s.MemoryBytes,
		},
	}
	return cfg, hostCfg
}

// dataVolumeCreateRequest builds the volume-create request for one machine.
// Ownership is delegated to the image: docker initializes an empty named
// volume from the image's mount-point directory (content and uid/gid), and
// the runtime image contract ships a /data owned by the container user — the
// same job the kubernetes backend's fsGroup does.
func dataVolumeCreateRequest(name string) *volume.CreateOptions {
	return &volume.CreateOptions{
		Name: dataVolumeName(name),
		Labels: map[string]string{
			managedByLabel: managedByValue,
			appNameLabel:   appNameValue,
			machineLabel:   name,
		},
	}
}

// bootstrapTar packs the manager-rendered bootstrap script and — on the
// create path, where the caller holds the minted credential — the machine
// state file into a tar stream for CopyToContainer. Journal-based recreation
// passes a nil state: the volume's machine.json is authoritative and the
// bootstrap script only needs the /bootstrap copy when the volume lacks it.
// Files are world-readable (secret-mount parity, k8s defaultMode 0644) so
// the non-root container user can read them.
func bootstrapTar(script, stateJSON []byte) (*bytes.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "bootstrap/",
		Mode:     0o755,
		ModTime:  time.Unix(0, 0),
	}); err != nil {
		return nil, err
	}
	entries := []tar.Header{{Name: "bootstrap/bootstrap.sh", Mode: 0o755}}
	bodies := [][]byte{script}
	if stateJSON != nil {
		entries = append(entries, tar.Header{Name: "bootstrap/machine.json", Mode: 0o644})
		bodies = append(bodies, stateJSON)
	}
	for i := range entries {
		hdr := entries[i]
		hdr.Typeflag = tar.TypeReg
		hdr.Size = int64(len(bodies[i]))
		hdr.ModTime = time.Unix(0, 0)
		if err := tw.WriteHeader(&hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(bodies[i]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

// phaseFromState maps an inspect state to the reported phase (design §4).
// "restarting" is the restart scheduler's own state during crash-loop
// backoff; "exited" right after a die is transient (the policy owns the
// restart), while an exited container past the grace window means nothing
// will bring it back (manual stop or a dead policy) — report FAILED,
// truthfully, and let a later start flip it back.
func phaseFromState(st *container.State, now time.Time) (storepb.ProvisioningPhase, string) {
	if st == nil {
		return phaseProvisioning, ""
	}
	if st.Running && !st.Paused {
		return phaseProvisioned, ""
	}
	if st.Status == container.StateCreated || st.Status == container.StateRestarting ||
		st.Status == container.StateRemoving {
		return phaseProvisioning, ""
	}
	if st.Status == container.StateExited {
		if finishedAt, err := time.Parse(time.RFC3339Nano, st.FinishedAt); err == nil &&
			now.Sub(finishedAt) < transientExitGrace {
			return phaseProvisioning, ""
		}
	}
	msg := fmt.Sprintf("machine container %s and will not restart (exit code %d)", stateDescription(st), st.ExitCode)
	if st.OOMKilled {
		msg += ": killed by the OOM killer"
	}
	if st.Error != "" {
		msg += ": " + st.Error
	}
	return phaseFailed, msg
}

// stateDescription renders a container state for error messages.
func stateDescription(st *container.State) string {
	switch st.Status {
	case container.StatePaused:
		return "is paused"
	case container.StateDead:
		return "is dead"
	case container.StateRemoving:
		return "is being removed"
	default:
		return fmt.Sprintf("exited (status %q)", st.Status)
	}
}
