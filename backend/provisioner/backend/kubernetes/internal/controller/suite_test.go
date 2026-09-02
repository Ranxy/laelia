package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
)

// The envtest suite drives the reconciler against a real apiserver+etcd. The
// control-plane binaries are not bundled with the repo: fetch them once with
// setup-envtest into .gopath/envtest (or set KUBEBUILDER_ASSETS). Without
// them the suite skips — the real-cluster e2e journey covers the same
// reconciliation path end to end.
const envtestAssetsDir = "../../../../../../.gopath/envtest"

// crdPath points at the generated CRD manifest applied into the test env.
const crdPath = "../../deploy/laelia.sh_laeliamachines.yaml"

// One control plane + manager per test binary: controller names must be
// unique per process, and apiserver startup is slow. Tests isolate through
// their own namespaces and machine ids.
var (
	envtestAvailable bool
	sharedClient     client.Client
	sharedRawClient  client.Client // uncached, for debugging assertions
	sharedEvents     = &eventCollector{}
	machineSeq       atomic.Int64
)

func TestMain(m *testing.M) {
	assets := envtestAssets()
	if assets == "" {
		envtestAvailable = false
		os.Exit(m.Run()) // nolint:revive // TestMain convention
	}
	envtestAvailable = true

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: assets,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		fatal("failed to start the control plane: %v", err)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		fatal("failed to register the core scheme: %v", err)
	}
	if err := laeliav1.AddToScheme(scheme); err != nil {
		fatal("failed to register the laelia.sh scheme: %v", err)
	}
	sharedRawClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		fatal("failed to build the raw client: %v", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme,
		Metrics: server.Options{BindAddress: "0"},
	})
	if err != nil {
		fatal("failed to construct the manager: %v", err)
	}
	reconciler := &LaeliaMachineReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Scheme:    mgr.GetScheme(),
		Events:    sharedEvents.emit,
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		fatal("failed to set up the reconciler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = mgr.Start(ctx) }()
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		fatal("manager caches did not sync")
	}
	sharedClient = mgr.GetClient()

	code := m.Run()
	cancel()
	_ = testEnv.Stop()
	os.Exit(code) // nolint:revive // post-run cleanup happens before this
}

// fatal reports a broken harness (not a test failure) and stops the binary.
func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "envtest: "+format+"\n", args...)
	os.Exit(1) // nolint:revive // a broken harness is not a test failure
}

// requireEnvtest skips the test when the control-plane binaries are missing.
func requireEnvtest(t *testing.T) {
	t.Helper()
	if !envtestAvailable {
		t.Skip("no envtest control-plane binaries (fetch with setup-envtest or set KUBEBUILDER_ASSETS)")
	}
}

// eventCollector collects the events the reconciler reports.
type eventCollector struct {
	mu     sync.Mutex
	events []backend.Event
}

func (c *eventCollector) emit(e backend.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *eventCollector) snapshot() []backend.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]backend.Event{}, c.events...)
}

// sawEvent reports whether an event for the machine id and phase was seen.
func (c *eventCollector) sawEvent(machineID string, phase storepb.ProvisioningPhase) bool {
	for _, e := range c.snapshot() {
		if e.MachineID == machineID && e.Phase == phase {
			return true
		}
	}
	return false
}

// envtestAssets resolves the control-plane binaries directory: an explicit
// KUBEBUILDER_ASSETS wins over the setup-envtest layout under .gopath/envtest.
func envtestAssets() string {
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		return dir
	}
	matches, _ := filepath.Glob(filepath.Join(envtestAssetsDir, "k8s", "*", "kube-apiserver"))
	if len(matches) == 0 {
		return ""
	}
	return filepath.Dir(matches[0])
}

// waitFor polls until check reports true; NotFound keeps polling until the
// deadline.
func waitFor(t *testing.T, check func() (bool, error), msg string) {
	t.Helper()
	const timeout = 10 * time.Second
	deadline := time.Now().Add(timeout)
	for {
		ok, err := check()
		if ok {
			return
		}
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("%s: %v", msg, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out (last error: %v)", msg, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// namespace creates a unique test namespace and registers its deletion.
func namespace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "laelia-test-"}}
	require.NoError(t, sharedClient.Create(ctx, ns))
	t.Cleanup(func() { _ = sharedClient.Delete(ctx, ns) })
	return ns.Name
}

// newTestMachine builds a CR with a unique machine id (unique CR name and
// event identity, since the harness is shared across tests).
func newTestMachine(t *testing.T, ns string) *laeliav1.LaeliaMachine {
	t.Helper()
	id := fmt.Sprintf("t%04d-00000000-0000-0000-0000-000000000000", machineSeq.Add(1))
	name := "laelia-machine-" + id[:5]
	return &laeliav1.LaeliaMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: laeliav1.LaeliaMachineSpec{
			MachineID:       id,
			ManagerURL:      "https://manager.test",
			Fingerprint:     "0123456789abcdef",
			RuntimeImage:    "laelia/machine-runtime:v1",
			BinaryTarget:    "linux-x64",
			BootstrapSecret: name,
			Storage:         laeliav1.LaeliaMachineStorageSpec{Size: resource.MustParse("5Gi")},
		},
	}
}

func TestReconcileCreatesWorkloadChildren(t *testing.T) {
	requireEnvtest(t)
	ctx := context.Background()
	ns := namespace(t)
	m := newTestMachine(t, ns)
	require.NoError(t, sharedClient.Create(ctx, m))

	waitFor(t, func() (bool, error) {
		var got laeliav1.LaeliaMachine
		if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &got); err != nil {
			return false, err
		}
		for _, f := range got.Finalizers {
			if f == laeliav1.FinalizerName {
				return true, nil
			}
		}
		return false, nil
	}, "finalizer added")

	waitFor(t, func() (bool, error) {
		var svc corev1.Service
		if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &svc); err != nil {
			return false, err
		}
		return svc.Spec.ClusterIP == corev1.ClusterIPNone, nil
	}, "headless service created")

	var sts appsv1.StatefulSet
	waitFor(t, func() (bool, error) {
		err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &sts)
		return err == nil, err
	}, "statefulset created")
	assert.Equal(t, int32(1), *sts.Spec.Replicas)
	assert.Equal(t, appsv1.DeletePersistentVolumeClaimRetentionPolicyType, sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted)
	assert.Equal(t, m.Spec.RuntimeImage, sts.Spec.Template.Spec.Containers[0].Image)
	require.Len(t, sts.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, []string{"sh", "/bootstrap/bootstrap.sh"}, sts.Spec.Template.Spec.InitContainers[0].Command)

	// The status is Pending until the StatefulSet's pod appears (envtest has
	// no pod controller; pods are faked in the next test).
	waitFor(t, func() (bool, error) {
		var got laeliav1.LaeliaMachine
		if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &got); err != nil {
			return false, err
		}
		return got.Status.Phase == laeliav1.LaeliaMachinePhasePending, nil
	}, "status pending")
}

func TestPodStatusDrivesPhaseAndEvents(t *testing.T) {
	requireEnvtest(t)
	ctx := context.Background()
	ns := namespace(t)
	m := newTestMachine(t, ns)
	require.NoError(t, sharedClient.Create(ctx, m))

	// A ready pod flips the CR to Ready and reports PROVISIONED. The
	// apiserver resets pod status on create, so the ready state goes through
	// the status subresource, exactly like a real kubelet would report it.
	pod := readyPod(m)
	require.NoError(t, sharedClient.Create(ctx, pod))
	pod.Status = readyStatus()
	require.NoError(t, sharedClient.Status().Update(ctx, pod))
	waitFor(t, func() (bool, error) {
		var got laeliav1.LaeliaMachine
		if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &got); err != nil {
			return false, err
		}
		return got.Status.Phase == laeliav1.LaeliaMachinePhaseReady &&
			got.Status.PodName == pod.Name, nil
	}, "CR became Ready")
	waitFor(t, func() (bool, error) {
		return sharedEvents.sawEvent(m.Spec.MachineID, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED), nil
	}, "PROVISIONED event reported")

	// A crash-looping pod flips the CR to Failed and reports FAILED with the
	// pod message; the manager treats FAILED as terminal for the row.
	failed := pod.DeepCopy()
	failed.Status.Phase = corev1.PodRunning
	failed.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{
		Reason:  "CrashLoopBackOff",
		Message: "back-off restarting failed container",
	}
	failed.Status.Conditions = nil
	require.NoError(t, sharedClient.Status().Update(ctx, failed))
	waitFor(t, func() (bool, error) {
		var got laeliav1.LaeliaMachine
		if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &got); err != nil {
			return false, err
		}
		return got.Status.Phase == laeliav1.LaeliaMachinePhaseFailed &&
			got.Status.Error == "back-off restarting failed container", nil
	}, "CR became Failed")
	waitFor(t, func() (bool, error) {
		return sharedEvents.sawEvent(m.Spec.MachineID, storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED), nil
	}, "FAILED event reported")
}

func TestDeleteCleansUpAndHonorsRetainData(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retainData bool
		wantPVC    bool
	}{
		{name: "default deletes the PVC", retainData: false, wantPVC: false},
		{name: "retain_data keeps the PVC", retainData: true, wantPVC: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireEnvtest(t)
			ctx := context.Background()
			ns := namespace(t)
			m := newTestMachine(t, ns)
			m.Spec.RetainData = tc.retainData
			require.NoError(t, sharedClient.Create(ctx, m))
			waitFor(t, func() (bool, error) {
				var got laeliav1.LaeliaMachine
				if err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &got); err != nil {
					return false, err
				}
				return len(got.Finalizers) > 0, nil
			}, "finalizer added before delete")

			// Objects that would exist once the workload ran: the bootstrap
			// secret (owner reference) and the data PVC (template label).
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      m.Name,
					Namespace: ns,
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: laeliav1.GroupVersion.String(),
						Kind:       "LaeliaMachine",
						Name:       m.Name,
						UID:        m.UID,
					}},
				},
			}
			require.NoError(t, sharedClient.Create(ctx, secret))
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "data-" + m.Name + "-0",
					Namespace: ns,
					Labels: map[string]string{
						laeliav1.MachineNameLabel: m.Name,
						laeliav1.ManagedByLabel:   laeliav1.ManagedByValue,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					}},
				},
			}
			require.NoError(t, sharedClient.Create(ctx, pvc))

			require.NoError(t, sharedClient.Delete(ctx, m))
			waitFor(t, func() (bool, error) {
				err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: m.Name}, &laeliav1.LaeliaMachine{})
				if apierrors.IsNotFound(err) {
					return true, nil // the finalizer released the CR
				}
				return false, err
			}, "CR removed after finalizer cleanup")

			// The finalizer ran before the CR disappeared: the DELETED event
			// was emitted and the cleanup decided.
			waitFor(t, func() (bool, error) {
				for _, e := range sharedEvents.snapshot() {
					if e.MachineID == m.Spec.MachineID &&
						e.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED &&
						e.WorkloadName == ns+"/"+m.Name {
						return true, nil
					}
				}
				return false, nil
			}, "DELETED event reported")

			// Reads go through the manager's informer cache; poll until the
			// deletion events propagate instead of racing the cache.
			waitFor(t, func() (bool, error) {
				err := sharedClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: secret.Name}, &corev1.Secret{})
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}, "bootstrap secret deleted")

			// The reconciler's PVC cleanup is a delete call; envtest has no
			// volume controllers, so a deleted PVC may stay Terminating
			// forever instead of disappearing. Read uncached and accept
			// either NotFound or a set deletionTimestamp as "collected".
			waitFor(t, func() (bool, error) {
				var got corev1.PersistentVolumeClaim
				err := sharedRawClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: pvc.Name}, &got)
				switch {
				case tc.wantPVC:
					if err == nil && got.DeletionTimestamp == nil {
						return true, nil // retained PVC must survive untouched
					}
					return false, err
				case apierrors.IsNotFound(err):
					return true, nil // data PVC garbage collected
				case err == nil && got.DeletionTimestamp != nil:
					return true, nil // delete issued; envtest cannot finalize
				default:
					return false, err
				}
			}, "data PVC cleanup honored the retention policy")
		})
	}
}

// readyPod builds the fake StatefulSet pod body (status is applied
// separately via the status subresource, like a real kubelet would).
func readyPod(m *laeliav1.LaeliaMachine) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podNameOf(m),
			Namespace: m.Namespace,
			Labels:    podLabels(m),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "laelia-machine",
				Image: m.Spec.RuntimeImage,
			}},
		},
	}
}

// readyStatus is the kubelet-reported state of a healthy machine pod.
func readyStatus() corev1.PodStatus {
	return corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
		},
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "laelia-machine",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
				StartedAt: metav1.NewTime(time.Now()),
			}},
		}},
	}
}
