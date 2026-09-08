// Package controller implements the LaeliaMachine reconciler (design §8.2):
// each CR is turned into a headless Service plus a single-replica StatefulSet
// whose init container bootstraps the machine binary and credential onto the
// data PVC. The pod is watched to drive status.phase transitions, which are
// reported back to the manager as provisioning events. Deletion runs under a
// finalizer so the bootstrap Secret and the data PVCs are cleaned up (or kept
// when spec.retainData is set) before the CR disappears.
package controller

import (
	"maps"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Ranxy/laelia/backend/provisioner/backend"
	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
)

// resyncDelay requeues workloads that are not Ready, so lost watches or pods
// stuck outside the failure reasons still make progress. Ready workloads are
// not requeued — the pod watch fires on every relevant change.
const resyncDelay = 30 * time.Second

// dataVolumeName is the StatefulSet volumeClaimTemplate name; the PVC becomes
// data-<statefulset>-0.
const dataVolumeName = "data"

// bootstrapSecretMountPath is where the read-only bootstrap Secret is mounted
// in the init container; the manager-rendered script expects it there.
const bootstrapSecretMountPath = "/bootstrap"

// dataMountPath is the data PVC mount, the future LAELIA_HOME mount.
const dataMountPath = "/data"

// machineUser is the non-root uid the runtime image runs as; fsGroup lets the
// init container (same uid) write into the fresh PVC.
const machineUID int64 = 1001

// serviceFor builds the desired headless Service required by StatefulSet pod
// identity. It carries no ports: machine pods only use unix sockets and
// outbound connections.
func serviceFor(m *laeliav1.LaeliaMachine) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
			Labels:    childLabels(m),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  selectorLabels(m),
		},
	}
}

// statefulSetFor builds the desired StatefulSet: one replica of the runtime
// image with a bootstrap init container, the machine credential mounted
// read-only, and all persistent state on the data PVC (design §8.2).
func statefulSetFor(m *laeliav1.LaeliaMachine) *appsv1.StatefulSet {
	bootstrap := corev1.Container{
		Name:    "bootstrap",
		Image:   m.Spec.RuntimeImage,
		Command: []string{"sh", bootstrapSecretMountPath + "/bootstrap.sh"},
		Env:     containerEnv(m),
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath},
			{Name: "bootstrap-secret", MountPath: bootstrapSecretMountPath, ReadOnly: true},
		},
	}

	// Runtime images run the machine binary from the data volume (the init
	// container installs it); the command maps the env vars to flags. It is
	// set explicitly so the image needs no laelia-specific entrypoint.
	main := corev1.Container{
		Name:            "laelia-machine",
		Image:           m.Spec.RuntimeImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c", backend.MachineRunScript},
		Env:             containerEnv(m),
		Resources:       containerResources(m.Spec.Resources),
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath},
		},
	}

	// WhenScaled is irrelevant (replicas are fixed at 1); WhenDeleted follows
	// the provisioner's retention configuration (design decision #4).
	retention := appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	if m.Spec.RetainData {
		retention = appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
			Labels:    childLabels(m),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    new(int32(1)),
			ServiceName: m.Name,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels(m),
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name:   dataVolumeName,
					Labels: pvcLabels(m),
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					VolumeName:  "",
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: m.Spec.Storage.Size.DeepCopy(),
						},
					},
					StorageClassName: optionalString(m.Spec.Storage.StorageClassName),
				},
			}},
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenDeleted: retention,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels(m),
				},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:        new(true),
						RunAsUser:           new(machineUID),
						FSGroup:             new(machineUID),
						FSGroupChangePolicy: new(corev1.FSGroupChangeOnRootMismatch),
					},
					InitContainers: []corev1.Container{bootstrap},
					Containers:     []corev1.Container{main},
					Volumes: []corev1.Volume{{
						Name: "bootstrap-secret",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName:  m.Spec.BootstrapSecret,
								DefaultMode: new(int32(0o444)),
							},
						},
					}},
				},
			},
		},
	}
}

// containerEnv builds the pod container environment: the operator's base
// entries (the pod contract, design §8.2) merged with spec.extraEnv, where
// each extra entry overrides the same-named default. The result is sorted by
// name for deterministic manifests (k8s rejects duplicate env names).
func containerEnv(m *laeliav1.LaeliaMachine) []corev1.EnvVar {
	merged := map[string]corev1.EnvVar{
		"LAELIA_HOME":        {Name: "LAELIA_HOME", Value: dataMountPath + "/laelia"},
		"LAELIA_FINGERPRINT": {Name: "LAELIA_FINGERPRINT", Value: m.Spec.Fingerprint},
		"LAELIA_MANAGER_URL": {Name: "LAELIA_MANAGER_URL", Value: m.Spec.ManagerURL},
		"LAELIA_PROVISIONED": {Name: "LAELIA_PROVISIONED", Value: "true"},
		"LAELIA_MACHINE_BIN": {Name: "LAELIA_MACHINE_BIN", Value: dataMountPath + "/bin/laelia-machine"},
		"CODEX_HOME":         {Name: "CODEX_HOME", Value: dataMountPath + "/laelia/codex"},
	}
	for _, e := range m.Spec.ExtraEnv {
		merged[e.Name] = e
	}
	out := make([]corev1.EnvVar, 0, len(merged))
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		out = append(out, merged[name])
	}
	return out
}

// containerResources converts the spec passthrough; empty spec means "cluster
// defaults" (no requests/limits set).
func containerResources(res *corev1.ResourceRequirements) corev1.ResourceRequirements {
	if res == nil {
		return corev1.ResourceRequirements{}
	}
	out := corev1.ResourceRequirements{}
	if len(res.Requests) > 0 {
		out.Requests = res.Requests.DeepCopy()
	}
	if len(res.Limits) > 0 {
		out.Limits = res.Limits.DeepCopy()
	}
	return out
}

// selectorLabels is the StatefulSet's immutable pod selector.
func selectorLabels(m *laeliav1.LaeliaMachine) map[string]string {
	return map[string]string{
		laeliav1.AppNameLabel:     laeliav1.AppNameValue,
		laeliav1.MachineNameLabel: m.Name,
	}
}

// podLabels extends the selector with managed-by and passthrough labels.
func podLabels(m *laeliav1.LaeliaMachine) map[string]string {
	labels := selectorLabels(m)
	labels[laeliav1.ManagedByLabel] = laeliav1.ManagedByValue
	maps.Copy(labels, m.Spec.Labels)
	return labels
}

// childLabels labels the Service/StatefulSet objects.
func childLabels(m *laeliav1.LaeliaMachine) map[string]string {
	labels := map[string]string{
		laeliav1.AppNameLabel:     laeliav1.AppNameValue,
		laeliav1.ManagedByLabel:   laeliav1.ManagedByValue,
		laeliav1.MachineNameLabel: m.Name,
	}
	maps.Copy(labels, m.Spec.Labels)
	return labels
}

// pvcLabels land on the data PVC via the volumeClaimTemplate metadata so the
// finalizer's explicit garbage collection can find them.
func pvcLabels(m *laeliav1.LaeliaMachine) map[string]string {
	return map[string]string{
		laeliav1.AppNameLabel:     laeliav1.AppNameValue,
		laeliav1.ManagedByLabel:   laeliav1.ManagedByValue,
		laeliav1.MachineNameLabel: m.Name,
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
