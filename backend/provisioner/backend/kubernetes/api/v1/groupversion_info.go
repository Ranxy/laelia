// Package v1 contains the API schema definitions for the laelia.sh/v1 API
// group: the LaeliaMachine custom resource, one per provisioned machine
// workload (design §8.1). The CR is desired state; the reconciler in
// internal/controller drives the Secret/Service/StatefulSet children and
// reports pod health through status.phase. Machine credentials never appear
// in a CR — they live in the bootstrap Secret.
// +kubebuilder:object:generate=true
// +groupName=laelia.sh
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "laelia.sh", Version: "v1"}

	// SchemeBuilder registers this group-version's types with a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes registers the LaeliaMachine types.
func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &LaeliaMachine{}, &LaeliaMachineList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
