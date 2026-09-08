// The Backend implementation: jobs → idempotent container upserts, daemon
// state → phase events (design §3, §6, §8, §9).
package docker

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/machineparam"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

func init() {
	backend.Register("docker", New)
}

const (
	// reconcileInterval is the reconcile poll period — the correctness floor
	// that makes the events stream a latency optimization (Appendix A #4).
	reconcileInterval = 30 * time.Second
	// pingTimeout bounds the fail-fast daemon check in Start.
	pingTimeout = 10 * time.Second
)

// Backend implements the provisioner Backend interface against one Docker
// Engine daemon: one machine = one named data volume + one container. The
// client resolves the daemon endpoint (socket, DOCKER_HOST, TLS) — the
// backend is remote-safe and never touches the host filesystem.
type Backend struct {
	cfg     backend.Config
	api     *client.Client
	journal *specJournal

	mu sync.Mutex
	// eventCh is the client's event pump sink; nil until Start.
	eventCh chan<- backend.Event
	// tracked is the observed machine set (journal + daemon containers),
	// keyed by machine id; busy marks machines mid-Provision/Deprovision so
	// the observer never races its own mutations.
	tracked map[string]*machineTrack
	busy    map[string]bool
}

// New builds the backend. The docker client resolves its endpoint from the
// environment (DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH, or the
// default socket) and negotiates the API version on first use — construction
// is hermetic (no daemon dial), so unit tests build it freely; the daemon
// contact is the fail-fast Ping in Start.
func New(cfg backend.Config) (backend.Backend, error) {
	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, errors.Wrap(err, "failed to build the docker client (DOCKER_HOST, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH)")
	}
	return &Backend{
		cfg:     cfg,
		api:     api,
		journal: openSpecJournal(cfg.StatePath),
		tracked: map[string]*machineTrack{},
		busy:    map[string]bool{},
	}, nil
}

// Name matches the provisioner row's backend field.
func (*Backend) Name() string { return "docker" }

// MachineParams declares cpu + memory only: docker named volumes have no
// size concept, so disk/storage_class are not offered (design §7). Defaults
// come from the config's resource limits, falling back to requests — the
// same precedence the container build resolves.
func (b *Backend) MachineParams() []*storepb.MachineParamSpec {
	spec := func(key string) *storepb.MachineParamSpec {
		s := &storepb.MachineParamSpec{Key: key, DefaultValue: b.defaultResource(key)}
		if bounds, ok := b.cfg.ParamBounds[key]; ok {
			s.MinValue, s.MaxValue = bounds.Min, bounds.Max
		}
		return s
	}
	return []*storepb.MachineParamSpec{
		spec(machineparam.CPU),
		spec(machineparam.Memory),
	}
}

// defaultResource is the reported default for one sizing key.
func (b *Backend) defaultResource(key string) string {
	if v := b.cfg.Resources.Limits[key]; v != "" {
		return v
	}
	return b.cfg.Resources.Requests[key]
}

// Start verifies daemon reachability (fail fast with an actionable error —
// the process exits into a crash-loop exactly like a backend that cannot
// resolve its kubernetes rest config), seeds the tracked machines from the
// daemon and the spec journal, and launches the dual watcher. Events flow
// through ch until ctx is done.
func (b *Backend) Start(ctx context.Context, ch chan<- backend.Event) error {
	b.mu.Lock()
	b.eventCh = ch
	b.mu.Unlock()

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if _, err := b.api.Ping(pingCtx); err != nil {
		return errors.Wrap(err, "docker daemon is unreachable (check socket access or DOCKER_HOST)")
	}

	if err := b.seed(ctx); err != nil {
		return err
	}
	go b.watchEvents(ctx)
	go b.reconcileLoop(ctx)
	slog.Info("docker backend started", "daemon", b.api.DaemonHost())
	return nil
}

// seed rebuilds the tracked machines from both state sources: the daemon's
// managed containers (authoritative for what exists) and the spec journal
// (whose entries may have lost their container out-of-band).
func (b *Backend) seed(ctx context.Context) error {
	summaries, err := b.api.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", managedByLabel+"="+managedByValue)),
	})
	if err != nil {
		return errors.Wrap(err, "failed to list the managed machine containers")
	}
	for i := range summaries {
		s := &summaries[i]
		machineID := s.Labels[machineIDLabel]
		if machineID == "" || len(s.Names) == 0 {
			continue
		}
		b.track(machineID)
	}
	for _, machineID := range b.journal.machineIDs() {
		b.track(machineID)
	}
	return nil
}

// Provision is the idempotent upsert (design §3): ensure volume → container
// (create / no-op on digest match / recreate on drift) → cp the bootstrap
// payload → start. Replayed jobs converge; a replay of an in-sync workload
// never restarts anything.
func (b *Backend) Provision(ctx context.Context, spec backend.MachineSpec, refreshToken string) error {
	snapshot, err := newSpecSnapshot(spec, b.cfg)
	if err != nil {
		return err
	}
	name := backend.WorkloadStem(spec.MachineID)

	b.reportAck(spec.MachineID, name)

	// Journal first: a crash mid-provision leaves a recoverable state.
	b.journal.put(snapshot)

	b.setBusy(spec.MachineID, true)
	defer b.setBusy(spec.MachineID, false)

	existing, err := b.api.ContainerInspect(ctx, name)
	inSync := err == nil && existing.Config != nil &&
		existing.Config.Labels[specDigestLabel] == snapshot.digest()
	switch {
	case cerrdefs.IsNotFound(err):
		if err := b.createAndStart(ctx, snapshot, refreshToken); err != nil {
			return err
		}
	case err != nil:
		return errors.Wrap(err, "failed to inspect the machine container")
	case inSync:
		// In-sync replay: a strict no-op — restarting a healthy workload
		// here would churn on every reconnect (Appendix A #2/#3).
	default:
		// Drift (image, env, sizing, labels changed): recreate in place; the
		// data volume keeps machine state and the credential.
		slog.Info("machine container drifted from the job spec; recreating", "machine", spec.MachineID)
		if err := b.removeContainer(ctx, name); err != nil {
			return err
		}
		if err := b.createAndStart(ctx, snapshot, refreshToken); err != nil {
			return err
		}
	}

	// Re-report the truth so replayed jobs land the manager on reality even
	// when a phase frame was lost (the kubernetes Provision re-report).
	if insp, err := b.api.ContainerInspect(ctx, name); err == nil {
		phase, msg := phaseFromState(insp.State, time.Now())
		b.report(spec.MachineID, name, phase, msg)
	}
	return nil
}

// createAndStart ensures the data volume, creates the container (pulling the
// image inline), copies the bootstrap payload into the created container, and
// starts it. A create name-conflict means a concurrent replay won the race;
// its winner owns the cp/start.
func (b *Backend) createAndStart(ctx context.Context, snapshot specSnapshot, refreshToken string) error {
	name := backend.WorkloadStem(snapshot.MachineID)
	if err := b.ensureDataVolume(ctx, name); err != nil {
		return err
	}

	cfg, hostCfg := machineContainerConfig(snapshot)
	created, err := b.api.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if cerrdefs.IsConflict(err) {
		slog.Info("another create won the machine container name race; treating it as in-sync",
			"machine", snapshot.MachineID)
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "failed to create the machine container")
	}

	var stateJSON []byte
	if refreshToken != "" {
		stateJSON = backend.MachineStateJSON(backend.MachineSpec{
			MachineID:  snapshot.MachineID,
			ManagerURL: snapshot.ManagerURL,
		}, refreshToken)
	}
	tar, err := bootstrapTar([]byte(snapshot.BootstrapScript), stateJSON)
	if err != nil {
		return errors.Wrap(err, "failed to pack the bootstrap payload")
	}
	if err := b.api.CopyToContainer(ctx, created.ID, "/", tar, container.CopyToContainerOptions{}); err != nil {
		return errors.Wrap(err, "failed to inject the bootstrap payload into the created container")
	}
	if err := b.api.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return errors.Wrap(err, "failed to start the machine container")
	}
	return nil
}

// ensureDataVolume creates the machine's named data volume when absent. A
// concurrent create (replayed jobs) just means it exists.
func (b *Backend) ensureDataVolume(ctx context.Context, name string) error {
	vol := dataVolumeName(name)
	if _, err := b.api.VolumeInspect(ctx, vol); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return errors.Wrap(err, "failed to inspect the machine data volume")
	}
	if _, err := b.api.VolumeCreate(ctx, *dataVolumeCreateRequest(name)); err != nil && !cerrdefs.IsConflict(err) {
		// A lost race reports conflict; anything else is real. Re-inspect to
		// confirm rather than failing a benign race.
		if _, err2 := b.api.VolumeInspect(ctx, vol); err2 != nil {
			return errors.Wrap(err, "failed to create the machine data volume")
		}
	}
	return nil
}

// removeContainer tears a container down with a SIGTERM grace, then a forced
// removal; both steps ignore an already-gone container.
func (b *Backend) removeContainer(ctx context.Context, name string) error {
	timeout := stopTimeoutSeconds
	if err := b.api.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout}); err != nil && !cerrdefs.IsNotFound(err) {
		slog.Warn("failed to gracefully stop the machine container; forcing removal", "container", name, "error", err)
	}
	if err := b.api.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return errors.Wrap(err, "failed to remove the machine container")
	}
	return nil
}

// Deprovision removes the workload idempotently (design §6): container gone,
// volume removed unless the manager asked for retention, journal entry
// dropped, DELETED reported once removals settle. Missing objects are
// success — the manager replays teardown until the workload reports gone.
func (b *Backend) Deprovision(ctx context.Context, machineID string, keepData bool) error {
	name := backend.WorkloadStem(machineID)

	b.setBusy(machineID, true)
	defer b.setBusy(machineID, false)

	if err := b.removeContainer(ctx, name); err != nil {
		return err
	}
	if !keepData {
		if err := b.api.VolumeRemove(ctx, dataVolumeName(name), false); err != nil && !cerrdefs.IsNotFound(err) {
			return errors.Wrap(err, "failed to remove the machine data volume")
		}
	}
	b.journal.remove(machineID)
	b.untrack(machineID)

	b.mu.Lock()
	ch := b.eventCh
	b.mu.Unlock()
	emitEvent(ch, backend.Event{
		MachineID:    machineID,
		Phase:        phaseDeleted,
		WorkloadName: name,
	})
	return nil
}

// Shutdown runs when the manager permanently deletes this provisioner: if
// the provisioner itself runs in the docker host it manages (docker defaults
// a container's hostname to its own id), stop that container so a restart
// policy does not bring it back crash-looping with a dead credential — the
// docker analog of the kubernetes operator scaling its own Deployment to 0.
// Best-effort: failures only log.
func (b *Backend) Shutdown(ctx context.Context) error {
	self, err := os.Hostname()
	if err != nil || self == "" {
		return errors.Wrap(err, "failed to read the process hostname")
	}
	summaries, err := b.api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return errors.Wrap(err, "failed to list containers to locate the provisioner's own container")
	}
	for i := range summaries {
		c := &summaries[i]
		if !strings.HasPrefix(c.ID, self) || isMachineContainerName(c.Names) {
			continue
		}
		if err := b.api.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil && !cerrdefs.IsNotFound(err) {
			return errors.Wrap(err, "failed to stop the provisioner's own container")
		}
		slog.Info("stopped the provisioner's own container after provisioner deletion", "container", firstContainerName(c.Names))
		return nil
	}
	slog.Info("the provisioner is not running in the managed docker host; nothing to stop")
	return nil
}

// isMachineContainerName guards Shutdown against matching one of our own
// machine workloads (belt-and-braces: container ids are unique, so the
// hostname prefix match cannot hit a machine container in practice).
func isMachineContainerName(names []string) bool {
	return slices.ContainsFunc(names, func(n string) bool {
		return strings.HasPrefix(strings.TrimPrefix(n, "/"), workloadPrefix)
	})
}

// firstContainerName returns the container's primary name, slash-trimmed.
func firstContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// emit delivers one event without ever blocking the caller; the client's
// event pump drains the channel and dropped events are recovered by replay.
func emitEvent(ch chan<- backend.Event, e backend.Event) {
	if ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
	}
}

// deletedPhase alias for the teardown terminal phase.
const phaseDeleted = storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED
