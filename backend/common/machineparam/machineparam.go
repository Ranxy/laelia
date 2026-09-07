// Package machineparam names the well-known machine-parameter catalog keys
// shared by the manager (which owns the catalog and its value semantics) and
// the provisioner backends (which declare which keys they accept). The keys
// are stable contract strings: adding a key is additive for both sides.
package machineparam

// Catalog keys for user-customizable machine parameters. See
// docs/plan/provisioner-machine-params-design.md for the catalog semantics.
const (
	// CPU sizes the main container's CPU (k8s quantity; sets request+limit).
	CPU = "cpu"
	// Memory sizes the main container's memory (k8s quantity; sets
	// request+limit).
	Memory = "memory"
	// Disk sizes the machine's data volume (k8s quantity; the PVC size).
	Disk = "disk"
	// StorageClass names the data volume's storage class (DNS-1123 label).
	StorageClass = "storage_class"
)
