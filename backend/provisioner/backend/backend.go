// Package backend defines the provisioner's pluggable workload layer: the
// Backend interface is the extension point that lets one laelia-provisioner
// binary drive different virtualization stacks (kubernetes today, docker/VM
// later) without manager changes (design §7.1). The client layer is the only
// translator between the provisioning jobs on the control stream and these
// interfaces.
package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

// ErrUnsupportedBackend is returned for backend names with no implementation
// available in this binary (registry stubs report it too, e.g. docker until
// its backend lands).
var ErrUnsupportedBackend = errors.New("unsupported provisioner backend")

// MachineSpec is the backend-neutral description of one machine workload,
// derived from a ProvisionMachineJob. The manager and the client layer never
// learn backend specifics beyond the status events.
type MachineSpec struct {
	MachineID    string // manager resource id (stable workload identity)
	Title        string
	ManagerURL   string
	Fingerprint  string
	RuntimeImage string
	BinaryTarget string
	// BootstrapScript is the manager-rendered init-container script (design
	// §8.3). Backends that download the binary inside the workload (e.g. the
	// kubernetes init container) carry it verbatim; the runtime image itself
	// needs nothing laelia-specific.
	BootstrapScript string
	Labels          map[string]string
}

// Event is a workload status change reported back to the manager as a
// ProvisionJobProgress frame.
type Event struct {
	MachineID    string
	Phase        storepb.ProvisioningPhase // PROVISIONING / PROVISIONED / FAILED / DEPROVISIONING / DELETED
	Error        string
	WorkloadName string // human-readable locator, e.g. "namespace/name"
}

// Resources is the passthrough pod sizing for one machine workload. Values
// are k8s quantities ("500m", "2Gi"); backends without a sizing concept may
// ignore it.
type Resources struct {
	Requests map[string]string `yaml:"requests,omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty"`
}

// Config is the backend-neutral slice of the provisioner configuration,
// handed to every backend factory. Backend-specific validation (e.g. a
// required namespace) belongs to the backend implementation.
type Config struct {
	Namespace  string
	RetainData bool
	Resources  Resources
	ExtraEnv   map[string]string
}

// Backend provisions machine workloads in one virtualization stack. All
// methods must be idempotent: jobs are replayed on reconnect.
type Backend interface {
	// Name matches the provisioner row's backend field.
	Name() string
	// Start launches the backend's controllers/watchers and emits Events on ch
	// until ctx is done.
	Start(ctx context.Context, ch chan<- Event) error
	// Provision creates or updates the workload for spec (idempotent upsert).
	// Credential handoff is backend-owned: the backend stores the machine's
	// bootstrap credential wherever the workload can read it (k8s Secret).
	Provision(ctx context.Context, spec MachineSpec, refreshToken string) error
	// Deprovision removes the workload; keepData preserves machine data
	// volumes when the backend supports retention.
	Deprovision(ctx context.Context, machineID string, keepData bool) error
}

// Factory builds one backend from the backend-neutral configuration.
type Factory func(Config) (Backend, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register makes a backend constructible by name. Called from backend
// packages' init functions; later registrations of the same name win.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// New constructs the named backend. Unknown or unimplemented names fail with
// ErrUnsupportedBackend.
func New(name string, cfg Config) (Backend, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	known := Known()
	registryMu.RUnlock()
	if !ok {
		return nil, errors.Wrapf(ErrUnsupportedBackend, "unknown provisioner backend %q (known backends: %v)", name, known)
	}
	backend, err := factory(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize %q backend", name)
	}
	return backend, nil
}

// Known lists the registered backend names (sorted; for logs and errors).
func Known() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// WorkloadName is the canonical human-readable locator for a machine
// workload: "<namespace>/laelia-machine-<machine uuid first segment>", shared
// by backends that follow the manager's workload naming.
func WorkloadName(namespace, machineID string) string {
	first, _, _ := strings.Cut(machineID, "-")
	return fmt.Sprintf("%s/laelia-machine-%s", namespace, first)
}
