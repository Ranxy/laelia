package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Well-known labels applied to every child object the reconciler creates.
const (
	// MachineNameLabel carries the owning LaeliaMachine name on pods, PVCs,
	// and services; the pod watch uses it to map pods back to their CR.
	MachineNameLabel = "laelia.sh/machine"
	// ManagedByLabel marks objects created by the laelia provisioner.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	// ManagedByValue is the managed-by label value.
	ManagedByValue = "laelia-provisioner"
	// AppNameLabel names the application across pods, services, and PVCs.
	AppNameLabel = "app.kubernetes.io/name"
	// AppNameValue is the app-name label value.
	AppNameValue = "laelia-machine"
	// FinalizerName holds CR deletion until PVC cleanup ran.
	FinalizerName = "laelia.sh/finalizer"
)

// LaeliaMachinePhase is the coarse lifecycle phase of one machine workload,
// observed from its pod (design §8.2).
// +kubebuilder:validation:Enum=Pending;Creating;Ready;Failed;Deleting
type LaeliaMachinePhase string

const (
	// LaeliaMachinePhasePending is recorded from CR creation until the
	// StatefulSet produced a pod.
	LaeliaMachinePhasePending LaeliaMachinePhase = "Pending"
	// LaeliaMachinePhaseCreating covers a pod that exists but is not ready
	// (init container downloading the binary, image pulls, agent start).
	LaeliaMachinePhaseCreating LaeliaMachinePhase = "Creating"
	// LaeliaMachinePhaseReady means the pod is running and ready.
	LaeliaMachinePhaseReady LaeliaMachinePhase = "Ready"
	// LaeliaMachinePhaseFailed is terminal for the provision attempt; Error
	// carries the pod-reported reason.
	LaeliaMachinePhaseFailed LaeliaMachinePhase = "Failed"
	// LaeliaMachinePhaseDeleting marks a CR whose deletion is being cleaned
	// up (finalizer running).
	LaeliaMachinePhaseDeleting LaeliaMachinePhase = "Deleting"
)

// LaeliaMachineStorageSpec sizes the machine's data volume (design §8.1
// spec.storage); both values come from the provisioner configuration.
type LaeliaMachineStorageSpec struct {
	// Size is the data PVC size, a k8s quantity ("10Gi"). Required.
	Size resource.Quantity `json:"size"`
	// StorageClassName pins a storage class; empty uses the cluster default.
	StorageClassName string `json:"storageClass,omitempty"`
}

// LaeliaMachineSpec is the desired state of one provisioned machine workload.
// It carries no credentials: the machine's refresh token lives only in the
// bootstrap Secret referenced by BootstrapSecret.
type LaeliaMachineSpec struct {
	// MachineID is the manager machine resource id — the stable workload
	// identity that makes replayed provisioning jobs idempotent.
	MachineID string `json:"machineId"`
	// Title is the machine's display title.
	Title string `json:"title,omitempty"`
	// ManagerURL is the manager that pods download the binary from and
	// connect to (already resolved with the provisioner's URL override).
	ManagerURL string `json:"managerUrl"`
	// Fingerprint is injected verbatim as the pod's LAELIA_FINGERPRINT; the
	// machine's refresh token is bound to it manager-side.
	Fingerprint string `json:"fingerprint"`
	// RuntimeImage provides the agent runtime environment. It must NOT
	// contain the laelia-machine binary — the init container downloads it
	// from the manager onto the data volume.
	RuntimeImage string `json:"runtimeImage"`
	// BinaryTarget selects the manager-embedded machine binary (e.g.
	// linux-x64).
	BinaryTarget string `json:"binaryTarget"`
	// BootstrapSecret names the Secret carrying machine.json and the
	// rendered bootstrap script; the init container mounts it read-only at
	// /bootstrap.
	BootstrapSecret string `json:"bootstrapSecret"`
	// RetainData preserves the machine's data PVC when the workload is
	// deleted (StatefulSet whenDeleted=Retain, and the finalizer skips PVC
	// garbage collection).
	RetainData bool `json:"retainData,omitempty"`
	// Resources sizes the main container (provisioner-level defaults).
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Storage sizes the machine data PVC.
	Storage LaeliaMachineStorageSpec `json:"storage"`
	// ExtraEnv passes additional environment entries into the pod's
	// containers (e.g. LAELIA_INSECURE, LAELIA_FORCE_REDOWNLOAD). Each entry
	// overrides the operator's defaults for the same name.
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
	// Labels are passthrough machine labels, applied to the CR metadata and
	// the pod template.
	Labels map[string]string `json:"labels,omitempty"`
}

// LaeliaMachineStatus is the observed state reported by the reconciler.
type LaeliaMachineStatus struct {
	// Phase is the coarse workload lifecycle phase.
	Phase LaeliaMachinePhase `json:"phase,omitempty"`
	// PodName is the single StatefulSet pod (empty until it exists).
	PodName string `json:"podName,omitempty"`
	// Error carries the pod-reported failure message when Phase is Failed.
	Error string `json:"error,omitempty"`
	// ObservedGeneration is the metadata.generation the status reflects.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is the standard conditions list; a Ready condition mirrors
	// the phase for k8s tooling.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=laeliamachines,singular=laeliamachine,scope=Namespaced,categories=laelia
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// LaeliaMachine is one provisioned laelia machine workload: a Secret with the
// bootstrap credential, a headless Service, and a single-replica StatefulSet
// whose init container downloads the machine binary onto the data PVC.
type LaeliaMachine struct {
	metav1.TypeMeta   `json:",inline"` // nolint:revive // kubebuilder TypeMeta convention
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LaeliaMachineSpec   `json:"spec,omitempty"`
	Status LaeliaMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// LaeliaMachineList contains a list of LaeliaMachine objects.
type LaeliaMachineList struct {
	metav1.TypeMeta `json:",inline"` // nolint:revive // kubebuilder TypeMeta convention
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LaeliaMachine `json:"items"`
}
