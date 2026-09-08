// Package docker implements the provisioner Backend interface against one
// Docker Engine daemon (docs/plan/provisioner-docker-backend-design.md): a
// machine workload is one named data volume plus one container whose
// entrypoint runs the manager-rendered bootstrap script and then execs the
// runtime image's entrypoint, watched by a dual watcher (daemon events +
// reconcile poll) that drives phase events back to the manager. The refresh
// token is copied into the created-but-not-started container — it never
// appears in env or inspect metadata.
package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/machineparam"
	"github.com/Ranxy/laelia/backend/common/quantity"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// specSnapshot is the backend-affecting projection of one ProvisionMachineJob:
// the digest input and the shape the spec journal persists. The refresh token
// is deliberately excluded — replayed jobs re-mint it (provisioner-design
// §4.3), so the digest must be credential-blind for replays to stay no-ops
// (design Appendix A #2/#3).
type specSnapshot struct {
	MachineID    string `json:"machine_id"`
	RuntimeImage string `json:"runtime_image"`
	ManagerURL   string `json:"manager_url"`
	// StartupScriptDigest pins the injected startup script version: a
	// provisioner upgrade that changes the startup rolls replayed workloads
	// (the digest flips → drift recreate), mirroring the kubernetes pod
	// template hash behavior.
	StartupScriptDigest string            `json:"startup_script_digest"`
	Fingerprint         string            `json:"fingerprint"`
	BootstrapScript     string            `json:"bootstrap_script"`
	Env                 map[string]string `json:"env"`
	Labels              map[string]string `json:"labels"`
	CPUMilli            quantity.Milli    `json:"cpu_milli"`
	MemoryBytes         int64             `json:"memory_bytes"`
}

// newSpecSnapshot projects the spec onto the container shape, resolving the
// cpu/memory parameters over the config defaults (design §7) and merging the
// job labels under the reserved management labels. Reserved labels win: the
// watcher routes events by machine-id, so the job passthrough must not
// override them.
func newSpecSnapshot(spec backend.MachineSpec, cfg backend.Config) (specSnapshot, error) {
	name := backend.WorkloadStem(spec.MachineID)

	env := map[string]string{
		"LAELIA_HOME":        dataMountPoint + "/laelia",
		"LAELIA_FINGERPRINT": spec.Fingerprint,
		"LAELIA_MANAGER_URL": spec.ManagerURL,
		"LAELIA_PROVISIONED": "true",
		"CODEX_HOME":         dataMountPoint + "/laelia/codex",
	}
	for k, v := range cfg.ExtraEnv {
		env[k] = v
	}

	labels := map[string]string{}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels[managedByLabel] = managedByValue
	labels[appNameLabel] = appNameValue
	labels[machineLabel] = name
	labels[machineIDLabel] = spec.MachineID

	cpu, err := resolveResource(machineparam.CPU, cfg, spec.Params)
	if err != nil {
		return specSnapshot{}, err
	}
	memory, err := resolveResource(machineparam.Memory, cfg, spec.Params)
	if err != nil {
		return specSnapshot{}, err
	}

	return specSnapshot{
		MachineID:           spec.MachineID,
		RuntimeImage:        spec.RuntimeImage,
		ManagerURL:          spec.ManagerURL,
		StartupScriptDigest: startupScriptDigest(),
		Fingerprint:         spec.Fingerprint,
		BootstrapScript:     spec.BootstrapScript,
		Env:                 env,
		Labels:              labels,
		// NanoCPUs is CPUs·1e9 and Milli is CPUs·1e3, so one milli-unit is
		// 1e6 NanoCPUs; memory is stored directly in bytes (docker's unit).
		CPUMilli:    cpu,
		MemoryBytes: int64(memory) / 1000,
	}, nil
}

// resolveResource merges the job's parameter override over the config
// defaults (Limits, else Requests — the same precedence MachineParams
// reports). Unknown params keys are ignored so a key persisted by a newer
// manager never rejects an older provisioner (machine-params design
// Appendix A, F2). An unset key resolves to zero, which docker reads as
// "no limit"; daemon-side minimums (e.g. --cpus ≥ 0.01) surface as FAILED.
func resolveResource(key string, cfg backend.Config, params map[string]string) (quantity.Milli, error) {
	raw := params[key]
	if raw == "" {
		raw = cfg.Resources.Limits[key]
		if raw == "" {
			raw = cfg.Resources.Requests[key]
		}
	}
	if raw == "" {
		return 0, nil
	}
	value, err := quantity.Parse(raw)
	if err != nil {
		return 0, errors.Wrapf(err, "machine sizing %q value %q", key, raw)
	}
	return value, nil
}

// startupScriptDigest pins the shared machine startup script version.
func startupScriptDigest() string {
	sum := sha256.Sum256([]byte(backend.MachineRunScript))
	return hex.EncodeToString(sum[:])[:16]
}

// digest is the spec-drift comparator stored on the container as a label:
// matching digests make a replayed Provision a strict no-op, differing ones
// trigger an in-place recreate (data volume kept).
func (s specSnapshot) digest() string {
	data, _ := json.Marshal(s) // map keys are sorted; struct order is fixed
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// ---- spec journal ----

// specJournal is the docker analog of the kubernetes CR as a desired-state
// store (design §5): machineID → spec snapshot, persisted atomically so the
// reconcile loop can rebuild a container that was removed out-of-band. It
// never stores the refresh token — credentials live on the data volume, so
// recovery works exactly when the volume survived; a lost volume is the
// unrecoverable terminal case (crash-loop → FAILED → UI delete + re-create).
type specJournal struct {
	mu      sync.Mutex
	path    string // empty = disabled (self-healing degrades, nothing else)
	entries map[string]specSnapshot
}

// openSpecJournal loads the journal at path, degrading to an in-memory-only
// journal when the path cannot be used: self-healing is a droppable feature
// (design Appendix A #1), never a reason to fail construction.
func openSpecJournal(path string) *specJournal {
	j := &specJournal{path: path, entries: map[string]specSnapshot{}}
	if path == "" {
		return j
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("failed to create the docker backend state directory; out-of-band container recovery is disabled",
			"path", path, "error", err)
		j.path = ""
		return j
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read the docker backend spec journal; starting empty", "path", path, "error", err)
		}
		return j
	}
	if err := json.Unmarshal(data, &j.entries); err != nil {
		slog.Warn("the docker backend spec journal is corrupt; starting empty", "path", path, "error", err)
		j.entries = map[string]specSnapshot{}
	}
	return j
}

// put atomically records (or replaces) one machine's snapshot. The write is
// best-effort durable (temp file + rename; no fsync — crash tolerance is
// self-healing support, not a durability contract).
func (j *specJournal) put(s specSnapshot) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.entries == nil {
		j.entries = map[string]specSnapshot{}
	}
	j.entries[s.MachineID] = s
	j.flushLocked()
}

// remove drops one machine's snapshot (deprovision).
func (j *specJournal) remove(machineID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.entries, machineID)
	j.flushLocked()
}

// get returns one machine's journaled snapshot.
func (j *specJournal) get(machineID string) (specSnapshot, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s, ok := j.entries[machineID]
	return s, ok
}

// machineIDs lists the journaled machine ids in stable order.
func (j *specJournal) machineIDs() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	ids := make([]string, 0, len(j.entries))
	for id := range j.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// flushLocked writes the journal atomically; failures only log — the next
// mutation retries and a lost entry merely degrades recovery.
func (j *specJournal) flushLocked() {
	if j.path == "" {
		return
	}
	data, err := json.Marshal(j.entries)
	if err != nil {
		slog.Warn("failed to encode the docker backend spec journal", "error", err)
		return
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("failed to write the docker backend spec journal", "path", j.path, "error", err)
		return
	}
	if err := os.Rename(tmp, j.path); err != nil {
		slog.Warn("failed to swap the docker backend spec journal into place", "path", j.path, "error", err)
	}
}

// workloadPrefix is the container-name prefix every machine workload shares
// ("laelia-machine-"); the event pump filters on it before doing any work.
// Guarded against WorkloadStem drift by a test.
const workloadPrefix = "laelia-machine-"
