package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
)

// failingWaitingReasons are container waiting states that do not recover on
// their own (bad image, crashing init/bootstrap, broken config); they map to
// the CR's Failed phase and a terminal FAILED on the manager side.
// CreateContainerConfigError is deliberately absent: it is retryable, and
// pods wait through it while the bootstrap Secret finishes being written.
var failingWaitingReasons = map[string]bool{
	"ErrImagePull":         true,
	"ImagePullBackOff":     true,
	"CrashLoopBackOff":     true,
	"InvalidImageName":     true,
	"RunContainerError":    true,
	"CreateContainerError": true,
}

// LaeliaMachineReconciler reconciles one LaeliaMachine CR into its workload
// objects and reports phase transitions as backend events.
type LaeliaMachineReconciler struct {
	client.Client
	// APIReader bypasses the manager's cache: PVCs are not watched (they are
	// only garbage-collected on CR deletion), so the finalizer must read them
	// from the apiserver to avoid racing a cold informer.
	APIReader client.Reader
	Scheme    *runtime.Scheme
	// Events receives one event per observed phase transition; the kubernetes
	// backend wires it to the manager stream. Nil in tests.
	Events func(backend.Event)
}

// +kubebuilder:rbac:groups=laelia.sh,resources=laeliamachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=laelia.sh,resources=laeliamachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=laelia.sh,resources=laeliamachines/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one CR to its desired workload and observed status.
func (r *LaeliaMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	m := &laeliav1.LaeliaMachine{}
	if err := r.Get(ctx, req.NamespacedName, m); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !m.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, m)
	}

	// Deletion must run PVC/Secret cleanup before the object disappears, so
	// the finalizer is added before any child is created.
	if !controllerutil.ContainsFinalizer(m, laeliav1.FinalizerName) {
		base := m.DeepCopy()
		controllerutil.AddFinalizer(m, laeliav1.FinalizerName)
		if err := r.Patch(ctx, m, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to add the LaeliaMachine finalizer")
		}
		return ctrl.Result{RequeueAfter: resyncDelay}, nil
	}

	if err := r.ensureService(ctx, m); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureStatefulSet(ctx, m); err != nil {
		return ctrl.Result{}, err
	}

	observed, err := r.observePod(ctx, m)
	if err != nil {
		return ctrl.Result{}, err
	}

	if _, err := r.updateStatus(ctx, m, observed); err != nil {
		return ctrl.Result{}, err
	}

	if observed.Phase == laeliav1.LaeliaMachinePhaseReady {
		return ctrl.Result{}, nil
	}
	// Non-ready workloads resync so lost watches and unobserved pod states
	// still converge (e.g. an STS that cannot schedule).
	return ctrl.Result{RequeueAfter: resyncDelay}, nil
}

// reconcileDelete runs under the finalizer: report DELETED, clean up the
// bootstrap Secret and (unless retained) the data PVCs, then release the CR
// to the apiserver (owner references cascade to Service/StatefulSet/pod).
func (r *LaeliaMachineReconciler) reconcileDelete(ctx context.Context, m *laeliav1.LaeliaMachine) (ctrl.Result, error) {
	if r.Events != nil {
		r.Events(backend.Event{
			MachineID:    m.Spec.MachineID,
			Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED,
			WorkloadName: workloadOf(m),
		})
	}

	if m.Status.Phase != laeliav1.LaeliaMachinePhaseDeleting {
		m.Status.Phase = laeliav1.LaeliaMachinePhaseDeleting
		m.Status.Conditions = readyConditions(m.Status.Conditions, m.Status.Phase)
		// Best effort: a conflicting update just means the next pass retries.
		_ = r.Status().Update(ctx, m)
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: m.Name, Namespace: m.Namespace}}
	if err := r.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to delete the bootstrap secret")
	}

	if !m.Spec.RetainData {
		pvcs := &corev1.PersistentVolumeClaimList{}
		if err := r.APIReader.List(ctx, pvcs,
			client.InNamespace(m.Namespace),
			client.MatchingLabels{laeliav1.MachineNameLabel: m.Name},
		); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to list the machine data PVCs")
		}
		for i := range pvcs.Items {
			if err := r.Delete(ctx, &pvcs.Items[i]); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, errors.Wrap(err, "failed to delete the machine data PVC")
			}
		}
	}

	base := m.DeepCopy()
	controllerutil.RemoveFinalizer(m, laeliav1.FinalizerName)
	if err := r.Patch(ctx, m, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to remove the LaeliaMachine finalizer")
	}
	return ctrl.Result{}, nil
}

// ensureService creates or updates the headless Service.
func (r *LaeliaMachineReconciler) ensureService(ctx context.Context, m *laeliav1.LaeliaMachine) error {
	desired := serviceFor(m)
	current := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: m.Name, Namespace: m.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.Labels = desired.Labels
		current.Spec = desired.Spec
		return controllerutil.SetControllerReference(m, current, r.Scheme)
	})
	if err != nil {
		return errors.Wrap(err, "failed to reconcile the headless service")
	}
	return nil
}

// ensureStatefulSet creates or updates the StatefulSet. volumeClaimTemplates
// and the selector are immutable, so updates preserve the stored ones.
func (r *LaeliaMachineReconciler) ensureStatefulSet(ctx context.Context, m *laeliav1.LaeliaMachine) error {
	desired := statefulSetFor(m)
	current := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: m.Name, Namespace: m.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		if current.CreationTimestamp.IsZero() {
			current.Labels = desired.Labels
			current.Spec = desired.Spec
		} else {
			desired.Spec.VolumeClaimTemplates = current.Spec.VolumeClaimTemplates
			desired.Spec.Selector = current.Spec.Selector
			current.Labels = desired.Labels
			current.Spec = desired.Spec
		}
		return controllerutil.SetControllerReference(m, current, r.Scheme)
	})
	if err != nil {
		return errors.Wrap(err, "failed to reconcile the statefulset")
	}
	return nil
}

// podObservation is the CR-level view of the machine pod.
type podObservation struct {
	Phase   laeliav1.LaeliaMachinePhase
	PodName string
	Error   string
}

// observePod reads the single StatefulSet pod and maps its status to a CR
// phase (design §8.2: Pending → Creating → Ready / Failed).
func (r *LaeliaMachineReconciler) observePod(ctx context.Context, m *laeliav1.LaeliaMachine) (podObservation, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Namespace: m.Namespace, Name: podNameOf(m)}, pod)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return podObservation{}, errors.Wrap(err, "failed to read the machine pod")
		}
		return podObservation{Phase: laeliav1.LaeliaMachinePhasePending}, nil
	}
	return observePodStatus(pod), nil
}

// observePodStatus is the pod-status → phase mapping, unit-testable without a
// cluster.
func observePodStatus(pod *corev1.Pod) podObservation {
	if pod == nil {
		return podObservation{Phase: laeliav1.LaeliaMachinePhasePending}
	}
	obs := podObservation{PodName: pod.Name}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		if msg, failed := failingMessage(pod); failed {
			obs.Phase, obs.Error = laeliav1.LaeliaMachinePhaseFailed, msg
			return obs
		}
		if podReady(pod) {
			obs.Phase = laeliav1.LaeliaMachinePhaseReady
			return obs
		}
		obs.Phase = laeliav1.LaeliaMachinePhaseCreating
		return obs

	case corev1.PodPending:
		if msg, failed := failingMessage(pod); failed {
			obs.Phase, obs.Error = laeliav1.LaeliaMachinePhaseFailed, msg
			return obs
		}
		obs.Phase = laeliav1.LaeliaMachinePhaseCreating
		return obs

	case corev1.PodFailed:
		msg := pod.Status.Message
		if msg == "" {
			msg = "machine pod failed"
		}
		obs.Phase, obs.Error = laeliav1.LaeliaMachinePhaseFailed, msg
		return obs

	case corev1.PodSucceeded:
		obs.Phase, obs.Error = laeliav1.LaeliaMachinePhaseFailed, "machine pod exited and will not restart"
		return obs

	default:
		obs.Phase = laeliav1.LaeliaMachinePhaseCreating
		return obs
	}
}

// failingMessage scans init and main container statuses for non-recoverable
// waiting reasons; the waiting message (or the reason itself) becomes the CR
// error text.
func failingMessage(pod *corev1.Pod) (string, bool) {
	statuses := pod.Status.InitContainerStatuses
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for i := range statuses {
		waiting := statuses[i].State.Waiting
		if waiting == nil || !failingWaitingReasons[waiting.Reason] {
			continue
		}
		if waiting.Message != "" {
			return waiting.Message, true
		}
		return waiting.Reason, true
	}
	return "", false
}

// podReady reports the standard Ready condition.
func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// updateStatus persists the observed phase when it changed and reports the
// transition as a backend event. It reports whether anything changed.
func (r *LaeliaMachineReconciler) updateStatus(ctx context.Context, m *laeliav1.LaeliaMachine, observed podObservation) (bool, error) {
	prev := m.Status.Phase
	if m.Status.Phase == observed.Phase &&
		m.Status.PodName == observed.PodName &&
		m.Status.Error == observed.Error &&
		m.Status.ObservedGeneration == m.Generation {
		return false, nil
	}

	base := m.DeepCopy()
	m.Status.Phase = observed.Phase
	m.Status.PodName = observed.PodName
	m.Status.Error = observed.Error
	m.Status.ObservedGeneration = m.Generation
	m.Status.Conditions = readyConditions(m.Status.Conditions, observed.Phase)
	if err := r.Status().Patch(ctx, m, client.MergeFrom(base)); err != nil {
		return false, errors.Wrap(err, "failed to update the LaeliaMachine status")
	}

	if r.Events != nil && observed.Phase != prev {
		r.Events(backend.Event{
			MachineID:    m.Spec.MachineID,
			Phase:        provisioningPhase(observed.Phase),
			Error:        observed.Error,
			WorkloadName: workloadOf(m),
		})
	}
	return true, nil
}

// provisioningPhase maps a CR phase to the manager-side provisioning phase.
// Pending and Creating both surface as PROVISIONING: the manager's PENDING is
// pre-ack, and the ack arrives with the first observed progress.
func provisioningPhase(phase laeliav1.LaeliaMachinePhase) storepb.ProvisioningPhase {
	switch phase {
	case laeliav1.LaeliaMachinePhaseReady:
		return storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED
	case laeliav1.LaeliaMachinePhaseFailed:
		return storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED
	default:
		return storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING
	}
}

// readyConditions maintains the standard Ready condition mirroring the phase.
func readyConditions(existing []metav1.Condition, phase laeliav1.LaeliaMachinePhase) []metav1.Condition {
	status := metav1.ConditionFalse
	reason := "Not" + string(phase)
	if phase == laeliav1.LaeliaMachinePhaseReady {
		status = metav1.ConditionTrue
		reason = string(phase)
	}
	meta.SetStatusCondition(&existing, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            string(phase),
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	return existing
}

// SetupWithManager wires the reconciler: CRs and their owned StatefulSets
// trigger reconciles; pods are mapped back via the laelia.sh/machine label
// (the pod's owner is the StatefulSet, not the CR).
func (r *LaeliaMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&laeliav1.LaeliaMachine{}).
		Owns(&appsv1.StatefulSet{}).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.machineForPod)).
		Complete(r)
}

// machineForPod maps a pod event to its owning LaeliaMachine reconcile.
func (*LaeliaMachineReconciler) machineForPod(_ context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.Labels[laeliav1.ManagedByLabel] != laeliav1.ManagedByValue {
		return nil
	}
	name := pod.Labels[laeliav1.MachineNameLabel]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: name}}}
}

// podNameOf is the single StatefulSet replica's pod name.
func podNameOf(m *laeliav1.LaeliaMachine) string { return m.Name + "-0" }

// workloadOf is the manager-facing workload locator.
func workloadOf(m *laeliav1.LaeliaMachine) string { return m.Namespace + "/" + m.Name }
