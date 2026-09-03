package controller

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
)

// machine returns a CR with the common fields filled in.
func machine() *laeliav1.LaeliaMachine {
	return machineNamed("laelia-machine-a1b2c3d4")
}

func machineNamed(name string) *laeliav1.LaeliaMachine {
	return &laeliav1.LaeliaMachine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "laelia-machines"},
		Spec: laeliav1.LaeliaMachineSpec{
			MachineID:       "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
			ManagerURL:      "https://manager.test",
			Fingerprint:     "0123456789abcdef",
			RuntimeImage:    "laelia/machine-runtime:v1",
			BinaryTarget:    "linux-x64",
			BootstrapSecret: name,
			Storage:         laeliav1.LaeliaMachineStorageSpec{Size: resource.MustParse("5Gi")},
		},
	}
}

func TestStatefulSetPodContract(t *testing.T) {
	m := machine()
	sts := statefulSetFor(m)

	require.NotNil(t, sts.Spec.PersistentVolumeClaimRetentionPolicy)
	assert.Equal(t, appsv1.DeletePersistentVolumeClaimRetentionPolicyType, sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted)
	assert.Equal(t, appsv1.DeletePersistentVolumeClaimRetentionPolicyType, sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled)
	assert.Equal(t, int32(1), *sts.Spec.Replicas)
	assert.Equal(t, map[string]string{"kubernetes.io/arch": "amd64"}, sts.Spec.Template.Spec.NodeSelector)
	assert.Equal(t, m.Name, sts.Spec.ServiceName)
	assert.Equal(t, m.Name, sts.Name)
	assert.Equal(t, m.Namespace, sts.Namespace)

	// The init container runs the manager-rendered script off the read-only
	// secret mount; the main container runs the runtime image entrypoint.
	init := sts.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, []string{"sh", "/bootstrap/bootstrap.sh"}, init.Command)
	require.Len(t, init.VolumeMounts, 2)
	assert.Equal(t, "/data", init.VolumeMounts[0].MountPath)
	assert.False(t, init.VolumeMounts[0].ReadOnly)
	assert.Equal(t, "/bootstrap", init.VolumeMounts[1].MountPath)
	assert.True(t, init.VolumeMounts[1].ReadOnly)

	main := sts.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "laelia-machine", main.Name)
	assert.Equal(t, corev1.PullIfNotPresent, main.ImagePullPolicy)
	// The command is set explicitly (not the image entrypoint) so any user
	// runtime image works; it runs the machine binary via POSIX sh.
	require.Len(t, main.Command, 3)
	assert.Equal(t, "/bin/sh", main.Command[0])
	assert.Equal(t, "-c", main.Command[1])
	assert.Contains(t, main.Command[2], `exec "$BIN" "$@"`)
	assert.Contains(t, main.Command[2], `--provisioned`)
	assert.Contains(t, main.Command[2], `--manager "$LAELIA_MANAGER_URL"`)
	env := envByName(main.Env)
	assert.Equal(t, "/data/laelia", env["LAELIA_HOME"].Value)
	assert.Equal(t, "0123456789abcdef", env["LAELIA_FINGERPRINT"].Value)
	assert.Equal(t, "https://manager.test", env["LAELIA_MANAGER_URL"].Value)
	assert.Equal(t, "true", env["LAELIA_PROVISIONED"].Value)
	assert.Equal(t, "/data/bin/laelia-machine", env["LAELIA_MACHINE_BIN"].Value)
	assert.Equal(t, "/data/laelia/codex", env["CODEX_HOME"].Value)
	require.Len(t, main.VolumeMounts, 1)
	assert.Equal(t, "/data", main.VolumeMounts[0].MountPath)

	// The pod security context runs the workload as the image's non-root user
	// and lets the init container take group ownership of the fresh PVC.
	podSpec := sts.Spec.Template.Spec
	require.NotNil(t, podSpec.SecurityContext)
	assert.True(t, *podSpec.SecurityContext.RunAsNonRoot)
	assert.Equal(t, int64(1001), *podSpec.SecurityContext.RunAsUser)
	assert.Equal(t, int64(1001), *podSpec.SecurityContext.FSGroup)

	vct := sts.Spec.VolumeClaimTemplates[0]
	assert.Equal(t, "data", vct.Name)
	assert.Equal(t, resource.MustParse("5Gi"), vct.Spec.Resources.Requests[corev1.ResourceStorage])
	assert.Nil(t, vct.Spec.StorageClassName)

	// The bootstrap secret is mounted read-only into the pod.
	require.Len(t, podSpec.Volumes, 1)
	assert.Equal(t, m.Name, podSpec.Volumes[0].Secret.SecretName)
	require.NotNil(t, podSpec.Volumes[0].Secret.DefaultMode)
	assert.Equal(t, int32(0o444), *podSpec.Volumes[0].Secret.DefaultMode)
}

func TestStatefulSetRetainData(t *testing.T) {
	m := machine()
	m.Spec.RetainData = true
	sts := statefulSetFor(m)
	require.NotNil(t, sts.Spec.PersistentVolumeClaimRetentionPolicy)
	assert.Equal(t, appsv1.RetainPersistentVolumeClaimRetentionPolicyType, sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted)

	// The PVC labels land via the volumeClaimTemplate so the finalizer's
	// garbage collection finds them.
	assert.Equal(t, m.Name, sts.Spec.VolumeClaimTemplates[0].Labels[laeliav1.MachineNameLabel])
}

func TestContainerEnvOverride(t *testing.T) {
	m := machine()
	m.Spec.ExtraEnv = []corev1.EnvVar{
		{Name: "LAELIA_INSECURE", Value: "true"},
		// Overrides the operator default on purpose.
		{Name: "CODEX_HOME", Value: "/opt/codex"},
	}
	env := envByName(containerEnv(m))
	assert.Equal(t, "true", env["LAELIA_INSECURE"].Value)
	assert.Equal(t, "/opt/codex", env["CODEX_HOME"].Value)
	names := envNames(containerEnv(m))
	assert.True(t, slices.IsSorted(names), "env names must be sorted: %v", names)
	assert.ElementsMatch(t, []string{
		"CODEX_HOME", "LAELIA_FINGERPRINT", "LAELIA_HOME", "LAELIA_INSECURE",
		"LAELIA_MACHINE_BIN", "LAELIA_MANAGER_URL", "LAELIA_PROVISIONED",
	}, names)
}

func TestObservePodStatus(t *testing.T) {
	tests := []struct {
		name    string
		pod     *corev1.Pod
		phase   laeliav1.LaeliaMachinePhase
		wantErr string
	}{
		{
			name:  "no pod waits pending",
			pod:   nil,
			phase: laeliav1.LaeliaMachinePhasePending,
		},
		{
			name:  "pending pod without states is creating",
			pod:   podWith(corev1.PodPending, nil, nil),
			phase: laeliav1.LaeliaMachinePhaseCreating,
		},
		{
			name: "init image pull failure fails",
			pod: podWith(corev1.PodPending,
				waiting("ImagePullBackOff", "back-off pulling image"),
				nil),
			phase:   laeliav1.LaeliaMachinePhaseFailed,
			wantErr: "back-off pulling image",
		},
		{
			name: "config error is retryable and keeps creating",
			pod: podWith(corev1.PodPending,
				waiting("CreateContainerConfigError", "secret not found"),
				nil),
			phase: laeliav1.LaeliaMachinePhaseCreating,
		},
		{
			name: "ready running pod is ready",
			pod: podWith(corev1.PodRunning, nil,
				readyCondition(corev1.ConditionTrue)),
			phase: laeliav1.LaeliaMachinePhaseReady,
		},
		{
			name: "running but not ready is creating",
			pod: podWith(corev1.PodRunning, nil,
				readyCondition(corev1.ConditionFalse)),
			phase: laeliav1.LaeliaMachinePhaseCreating,
		},
		{
			name: "crash loop fails",
			pod: podWith(corev1.PodRunning,
				waiting("CrashLoopBackOff", "back-off 5m0s restarting failed container"),
				readyCondition(corev1.ConditionFalse)),
			phase:   laeliav1.LaeliaMachinePhaseFailed,
			wantErr: "back-off 5m0s restarting failed container",
		},
		{
			name:    "failed pod carries the pod message",
			pod:     failedPod("pod was evicted: node was under memory pressure"),
			phase:   laeliav1.LaeliaMachinePhaseFailed,
			wantErr: "pod was evicted: node was under memory pressure",
		},
		{
			name:    "succeeded pod is a failure for a restart-always workload",
			pod:     podWith(corev1.PodSucceeded, nil, nil),
			phase:   laeliav1.LaeliaMachinePhaseFailed,
			wantErr: "machine pod exited and will not restart",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs := observePodStatus(tt.pod)
			assert.Equal(t, tt.phase, obs.Phase)
			if tt.wantErr != "" {
				assert.Equal(t, tt.wantErr, obs.Error)
			} else {
				assert.Empty(t, obs.Error)
			}
		})
	}
}

func TestWorkloadLocatorHelpers(t *testing.T) {
	m := machine()
	assert.Equal(t, "laelia-machine-a1b2c3d4-0", podNameOf(m))
	assert.Equal(t, "laelia-machines/laelia-machine-a1b2c3d4", workloadOf(m))
}

// envByName indexes an env list by name.
func envByName(env []corev1.EnvVar) map[string]corev1.EnvVar {
	out := map[string]corev1.EnvVar{}
	for _, e := range env {
		out[e.Name] = e
	}
	return out
}

// envNames lists an env list's names in order.
func envNames(env []corev1.EnvVar) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		out = append(out, e.Name)
	}
	return out
}

// podWith builds a pod in the given phase with one main container status
// (waiting, when set) and the ready condition.
func podWith(phase corev1.PodPhase, waitingState *corev1.ContainerStateWaiting, ready *corev1.PodCondition) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "laelia-machine-a1b2c3d4-0", Namespace: "laelia-machines"},
		Status: corev1.PodStatus{
			Phase: phase,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "laelia-machine",
				State: corev1.ContainerState{},
			}},
		},
	}
	if waitingState != nil {
		pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: waitingState}
	}
	if ready != nil {
		pod.Status.Conditions = append(pod.Status.Conditions, *ready)
	}
	return pod
}

func waiting(reason, message string) *corev1.ContainerStateWaiting {
	return &corev1.ContainerStateWaiting{Reason: reason, Message: message}
}

func readyCondition(status corev1.ConditionStatus) *corev1.PodCondition {
	return &corev1.PodCondition{Type: corev1.PodReady, Status: status}
}

func failedPod(message string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "laelia-machine-a1b2c3d4-0", Namespace: "laelia-machines"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed, Message: message},
	}
}
