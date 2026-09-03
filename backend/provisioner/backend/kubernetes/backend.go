// Package kubernetes implements the provisioner Backend interface as a
// controller-runtime operator (design §8): Provision is an idempotent upsert
// of a bootstrap Secret and a LaeliaMachine CR, the reconciler drives the
// Secret/headless-Service/StatefulSet workload and watches its pod, and CR
// status transitions flow back to the manager as backend events. The machine
// refresh token only ever lives in the Secret — never in a CR.
package kubernetes

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	clog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
	"github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/internal/controller"
)

// defaultStorageSize is the data PVC size when the config leaves it empty.
const defaultStorageSize = "10Gi"

// serviceAccountNamespacePath is where k8s mounts the pod's namespace.
const serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

func init() {
	backend.Register("kubernetes", New)
}

// Backend is the kubernetes operator backend: one controller-runtime manager
// hosting the LaeliaMachine reconciler, plus the job-facing upsert methods.
type Backend struct {
	cfg backend.Config
	mgr ctrl.Manager

	// namespace is the workload namespace (design §8.1: all machines land in
	// the provisioner's configured namespace).
	namespace string
	// storageSize is the parsed PVC size (default applied).
	storageSize resource.Quantity

	mu      sync.Mutex
	eventCh chan<- backend.Event
}

// New builds the backend: resolves the k8s rest config (in-cluster service
// account or KUBECONFIG), constructs the manager and the reconciler. The
// manager only starts serving caches in Start.
func New(cfg backend.Config) (backend.Backend, error) {
	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve the kubernetes rest config (in-cluster or KUBECONFIG)")
	}

	ns := cfg.Namespace
	if strings.TrimSpace(ns) == "" {
		var nsErr error
		ns, nsErr = inClusterNamespace()
		if nsErr != nil {
			return nil, errors.Wrap(nsErr, "namespace is required (config file, or the in-cluster service account namespace)")
		}
	}

	size := cfg.Storage.Size
	if size == "" {
		size = defaultStorageSize
	}
	storageSize, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid storage size %q", size)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, errors.Wrap(err, "failed to register the core scheme")
	}
	if err := laeliav1.AddToScheme(scheme); err != nil {
		return nil, errors.Wrap(err, "failed to register the laelia.sh scheme")
	}

	// Cache scoped to the workload namespace; metrics/pprof servers are off —
	// the provisioner is a single-purpose worker, not a monitored service.
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:  scheme,
		Cache:   cache.Options{DefaultNamespaces: map[string]cache.Config{ns: {}}},
		Metrics: server.Options{BindAddress: "0"},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct the controller manager")
	}

	b := &Backend{
		cfg:         cfg,
		mgr:         mgr,
		namespace:   ns,
		storageSize: storageSize,
	}
	if err := (&controller.LaeliaMachineReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Scheme:    mgr.GetScheme(),
		Events:    b.emit,
	}).SetupWithManager(mgr); err != nil {
		return nil, errors.Wrap(err, "failed to set up the LaeliaMachine reconciler")
	}

	// Route controller-runtime logs through the provisioner's slog handler.
	clog.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))
	return b, nil
}

// Name matches the provisioner row's backend field.
func (*Backend) Name() string { return "kubernetes" }

// Start launches the controller-runtime manager (caches, watches, workers)
// and returns once the caches are synced — or when the manager fails to
// start. Events flow through ch until ctx is done.
func (b *Backend) Start(ctx context.Context, ch chan<- backend.Event) error {
	b.mu.Lock()
	b.eventCh = ch
	b.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := b.mgr.Start(ctx); err != nil {
			errCh <- err
		}
	}()

	// A cancelled context is the normal shutdown path.
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !b.mgr.GetCache().WaitForCacheSync(syncCtx) {
		select {
		case err := <-errCh:
			return errors.Wrap(err, "kubernetes controller manager failed to start")
		default:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("kubernetes caches did not sync")
	}
	return nil
}

// Provision is an idempotent upsert of the bootstrap Secret (machine.json +
// the manager-rendered script) and the LaeliaMachine CR. Replayed jobs
// converge onto the same objects; the CR's reconciler owns the rest. It
// reports PROVISIONING (the ack) and re-reports the current CR phase, so a
// replayed job after a dropped event still lands the manager on the truth.
func (b *Backend) Provision(ctx context.Context, spec backend.MachineSpec, refreshToken string) error {
	ns := b.namespace
	name := workloadStem(spec.MachineID)

	cr := &laeliav1.LaeliaMachine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	err := b.upsert(ctx, cr, func() error {
		cr.Labels = crLabels(spec)
		cr.Spec = laeliav1.LaeliaMachineSpec{
			MachineID:       spec.MachineID,
			Title:           spec.Title,
			ManagerURL:      spec.ManagerURL,
			Fingerprint:     spec.Fingerprint,
			RuntimeImage:    spec.RuntimeImage,
			BinaryTarget:    spec.BinaryTarget,
			BootstrapSecret: name,
			RetainData:      b.cfg.RetainData,
			Resources:       resourcesFor(b.cfg.Resources),
			Storage: laeliav1.LaeliaMachineStorageSpec{
				Size:             b.storageSize.DeepCopy(),
				StorageClassName: b.cfg.Storage.StorageClassName,
			},
			ExtraEnv: envVarsFor(b.cfg.ExtraEnv),
			Labels:   spec.Labels,
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "failed to upsert the LaeliaMachine resource")
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	err = b.upsert(ctx, secret, func() error {
		secret.Labels = secretLabels(spec)
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			"machine.json": machineStateJSON(spec, refreshToken),
			"bootstrap.sh": []byte(spec.BootstrapScript),
		}
		return controllerutil.SetControllerReference(cr, secret, b.mgr.GetScheme())
	})
	if err != nil {
		return errors.Wrap(err, "failed to upsert the bootstrap secret")
	}

	workload := ns + "/" + name
	b.emit(backend.Event{
		MachineID:    spec.MachineID,
		Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING,
		WorkloadName: workload,
	})
	switch cr.Status.Phase {
	case laeliav1.LaeliaMachinePhaseReady:

		b.emit(backend.Event{
			MachineID:    spec.MachineID,
			Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED,
			WorkloadName: workload,
		})
	case laeliav1.LaeliaMachinePhaseFailed:
		b.emit(backend.Event{
			MachineID:    spec.MachineID,
			Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED,
			Error:        cr.Status.Error,
			WorkloadName: workload,
		})
	default:
		// Pending/Creating/Deleting: the PROVISIONING ack above is current.
	}
	return nil
}

// Deprovision removes the workload: the CR is deleted (its finalizer cleans
// the Secret and — unless data is retained — the PVCs, while owner references
// cascade to Service/StatefulSet/pod). Idempotent: a missing CR re-runs the
// explicit PVC garbage collection and reports DELETED directly, because the
// manager replays teardown jobs until the workload reports gone.
func (b *Backend) Deprovision(ctx context.Context, machineID string, keepData bool) error {
	ns := b.namespace
	name := workloadStem(machineID)

	cr := &laeliav1.LaeliaMachine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	err := b.mgr.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, cr)
	switch {
	case apierrors.IsNotFound(err):

		// Nothing (left) to tear down through the CR; PVCs may still linger
		// from an out-of-band CR deletion.
		if !keepData {
			if err := b.deleteDataPVCs(ctx, ns, name); err != nil {
				return err
			}
		}
		b.emit(backend.Event{
			MachineID:    machineID,
			Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED,
			WorkloadName: ns + "/" + name,
		})
		return nil
	case err != nil:
		return errors.Wrap(err, "failed to read the LaeliaMachine resource")
	default:
		// CR present: fall through to the delete below.
	}

	if cr.Spec.RetainData != keepData {
		patch := cr.DeepCopy()
		cr.Spec.RetainData = keepData
		if err := b.mgr.GetClient().Patch(ctx, cr, client.MergeFrom(patch)); err != nil {
			return errors.Wrap(err, "failed to update the LaeliaMachine retention")
		}
	}
	if err := b.mgr.GetClient().Delete(ctx, cr); client.IgnoreNotFound(err) != nil {
		return errors.Wrap(err, "failed to delete the LaeliaMachine resource")
	}
	// The DELETED event comes from the reconciler's finalizer path once
	// cleanup ran; keepData==true there leaves the PVCs in place.
	return nil
}

// Shutdown is called when the manager permanently deletes this provisioner.
// The operator scales its own Deployment to 0 so it stops crash-looping with
// a dead credential; the Deployment/CRD/RBAC/namespace remain for the user to
// clean up manually (see the cleanup guide). Best-effort: a failure only logs.
func (b *Backend) Shutdown(ctx context.Context) error {
	podName := os.Getenv("LAELIA_POD_NAME")
	if podName == "" {
		return errors.New("LAELIA_POD_NAME is not set; cannot locate the operator deployment")
	}
	ns := b.namespace

	pod := &corev1.Pod{}
	if err := b.mgr.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: ns, Name: podName}, pod); err != nil {
		return errors.Wrap(err, "failed to read the operator pod")
	}
	rsName := ownerRefName(pod, "ReplicaSet")
	if rsName == "" {
		return errors.New("operator pod has no owning ReplicaSet; nothing to scale down")
	}
	rs := &appsv1.ReplicaSet{}
	if err := b.mgr.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: ns, Name: rsName}, rs); err != nil {
		return errors.Wrap(err, "failed to read the operator ReplicaSet")
	}
	depName := ownerRefName(rs, "Deployment")
	if depName == "" {
		return errors.New("operator ReplicaSet has no owning Deployment; nothing to scale down")
	}

	dep := &appsv1.Deployment{}
	if err := b.mgr.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: ns, Name: depName}, dep); err != nil {
		return errors.Wrap(err, "failed to read the operator deployment")
	}
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
		return nil // already scaled down
	}
	zero := int32(0)
	patch := dep.DeepCopy()
	patch.Spec.Replicas = &zero
	if err := b.mgr.GetClient().Patch(ctx, dep, client.MergeFrom(patch)); err != nil {
		return errors.Wrap(err, "failed to scale the operator deployment to 0")
	}
	slog.Info("scaled the operator deployment to 0 after provisioner deletion", "namespace", ns, "deployment", depName)
	return nil
}

// ownerRefName returns the name of the first owner reference of the given
// kind, or "" when absent.
func ownerRefName(obj metav1.Object, kind string) string {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == kind {
			return ref.Name
		}
	}
	return ""
}

// deleteDataPVCs removes PVCs labeled for one machine workload.
func (b *Backend) deleteDataPVCs(ctx context.Context, ns, name string) error {
	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := b.mgr.GetAPIReader().List(ctx, pvcs,
		client.InNamespace(ns),
		client.MatchingLabels{laeliav1.MachineNameLabel: name},
	); err != nil {
		return errors.Wrap(err, "failed to list the machine data PVCs")
	}
	for i := range pvcs.Items {
		if err := b.mgr.GetClient().Delete(ctx, &pvcs.Items[i]); client.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "failed to delete the machine data PVC")
		}
	}
	return nil
}

// upsert applies mutate to obj, creating it when absent. Reads go through
// the uncached APIReader: existence decisions must not trust the informer
// cache — Secrets are not watched, and an object deleted out-of-band lingers
// in the cache for a while, which would turn the create into a failing
// update on replayed jobs.
func (b *Backend) upsert(ctx context.Context, obj client.Object, mutate func() error) error {
	err := b.mgr.GetAPIReader().Get(ctx, client.ObjectKeyFromObject(obj), obj)
	switch {
	case apierrors.IsNotFound(err):
		if err := mutate(); err != nil {
			return err
		}
		return client.IgnoreAlreadyExists(b.mgr.GetClient().Create(ctx, obj))
	case err != nil:
		return err
	default:
		if err := mutate(); err != nil {
			return err
		}
		return b.mgr.GetClient().Update(ctx, obj)
	}
}

// emit delivers one event without ever blocking the caller; the client's
// event pump drains the channel and dropped events are recovered by replay.
func (b *Backend) emit(e backend.Event) {
	b.mu.Lock()
	ch := b.eventCh
	b.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- e:
	default:
	}
}

// machineStateJSON renders the pod's machine.json — exactly the agent's
// state.State shape ({manager_url, machine_id, refresh_token, hostname,
// created_at}): with a valid state file the pod's setup skips the device
// flow entirely. mirrored here to keep the provisioner decoupled from the
// agent's internal packages.
func machineStateJSON(spec backend.MachineSpec, refreshToken string) []byte {
	// The state fields are stable; marshaling cannot fail on strings.
	data, _ := json.Marshal(struct {
		ManagerURL   string    `json:"manager_url"`
		MachineID    string    `json:"machine_id"`
		RefreshToken string    `json:"refresh_token"`
		Hostname     string    `json:"hostname"`
		CreatedAt    time.Time `json:"created_at"`
	}{
		ManagerURL:   spec.ManagerURL,
		MachineID:    spec.MachineID,
		RefreshToken: refreshToken,
		Hostname:     workloadStem(spec.MachineID),
		CreatedAt:    time.Now(),
	})
	return data
}

// workloadStem is the object-name stem shared with the manager:
// "laelia-machine-<machine uuid first segment>" (backend.WorkloadName minus
// its namespace prefix).
func workloadStem(machineID string) string {
	_, name, _ := strings.Cut(backend.WorkloadName("", machineID), "/")
	return name
}

// resourcesFor converts the provisioner-level requests/limits passthrough.
func resourcesFor(res backend.Resources) *corev1.ResourceRequirements {
	req, lim := parseQuantities(res.Requests), parseQuantities(res.Limits)
	if len(req) == 0 && len(lim) == 0 {
		return nil
	}
	out := &corev1.ResourceRequirements{}
	if len(req) > 0 {
		out.Requests = req
	}
	if len(lim) > 0 {
		out.Limits = lim
	}
	return out
}

func parseQuantities(entries map[string]string) corev1.ResourceList {
	if len(entries) == 0 {
		return nil
	}
	out := corev1.ResourceList{}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		q, err := resource.ParseQuantity(entries[name])
		if err != nil {
			// Factory-level validation is not possible for free-form keys;
			// invalid quantities surface as apiserver errors on the CR write.
			continue
		}
		out[corev1.ResourceName(name)] = q
	}
	return out
}

// envVarsFor converts the extra-env passthrough, sorted by name.
func envVarsFor(entries map[string]string) []corev1.EnvVar {
	if len(entries) == 0 {
		return nil
	}
	out := make([]corev1.EnvVar, 0, len(entries))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		out = append(out, corev1.EnvVar{Name: name, Value: entries[name]})
	}
	return out
}

// crLabels marks the CR and carries the machine labels passthrough. The
// laelia.sh/machine label equals the CR name — the reconciler's pod and PVC
// lookups rely on it.
func crLabels(spec backend.MachineSpec) map[string]string {
	labels := map[string]string{
		laeliav1.ManagedByLabel:   laeliav1.ManagedByValue,
		laeliav1.AppNameLabel:     laeliav1.AppNameValue,
		laeliav1.MachineNameLabel: workloadStem(spec.MachineID),
	}
	maps.Copy(labels, spec.Labels)
	return labels
}

// secretLabels labels the bootstrap Secret.
func secretLabels(spec backend.MachineSpec) map[string]string {
	return map[string]string{
		laeliav1.ManagedByLabel:   laeliav1.ManagedByValue,
		laeliav1.AppNameLabel:     laeliav1.AppNameValue,
		laeliav1.MachineNameLabel: workloadStem(spec.MachineID),
	}
}

// inClusterNamespace reads the pod's namespace from the service account
// mount; it fails outside a cluster.
func inClusterNamespace() (string, error) {
	data, err := os.ReadFile(serviceAccountNamespacePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the in-cluster namespace")
	}
	ns := strings.TrimSpace(string(data))
	if ns == "" {
		return "", errors.New("empty in-cluster namespace")
	}
	return ns, nil
}
