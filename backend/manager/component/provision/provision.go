// Package provision owns the manager-side pieces of the provisioning job that
// have no home in a service: the rendered init-container bootstrap script, the
// deterministic machine fingerprint binding, and the workload naming rule. It
// is consumed by api/v1 (ProvisionMachine + ProvisionerChannel replay) and, in
// later phases, by the kubernetes backend documentation.
package provision

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"text/template"

	"github.com/pkg/errors"
)

//go:embed provision_bootstrap.sh.tmpl
var templates embed.FS

// bootstrapTemplate is the init-container script template. Rendered values are
// manager-controlled strings (URL, target, checksums) — escaped with
// template.JSEscapeString semantics via the js escaper would corrupt shell
// metacharacters differently, so the template interpolates them raw and the
// renderer rejects control characters and quotes instead.
var bootstrapTemplate = template.Must(template.ParseFS(templates, "provision_bootstrap.sh.tmpl"))

// BootstrapParams are the render inputs of the bootstrap script. They all come
// from manager state: the workspace external URL, the configured binary target,
// and the embedded manifest checksums for that target.
type BootstrapParams struct {
	// ManagerURL is the scheme://host[:port] base URL pods download binaries
	// from and connect to (no trailing slash).
	ManagerURL string
	// BinaryTarget is the embedded manifest target, e.g. "linux-x64".
	BinaryTarget string
	// GzSha256 is the sha256 of the gzipped binary (from the embedded
	// manifest), verified before decompression.
	GzSha256 string
	// Sha256 is the sha256 of the decompressed binary, verified after gunzip.
	Sha256 string
}

// RenderBootstrapScript renders the init-container bootstrap script carried in
// every ProvisionMachineJob. The runtime image only needs POSIX sh, curl,
// gzip, and sha256sum — the script itself knows everything else.
func RenderBootstrapScript(params BootstrapParams) (string, error) {
	if params.ManagerURL == "" {
		return "", errors.New("manager url is required to render the bootstrap script")
	}
	if params.BinaryTarget == "" {
		return "", errors.New("binary target is required to render the bootstrap script")
	}
	if params.GzSha256 == "" || params.Sha256 == "" {
		return "", errors.New("embedded manifest checksums are required to render the bootstrap script")
	}
	for _, v := range []string{params.ManagerURL, params.BinaryTarget} {
		if strings.ContainsAny(v, "\"'`$\\\n\r") {
			return "", errors.Errorf("value %q contains characters the bootstrap script cannot quote", v)
		}
	}

	var buf strings.Builder
	if err := bootstrapTemplate.Execute(&buf, params); err != nil {
		return "", errors.Wrap(err, "failed to render bootstrap script")
	}
	return buf.String(), nil
}

// MachineFingerprint derives the connection fingerprint bound to a provisioned
// machine's refresh token: sha256("provisioner:" + provisionerResourceID + ":"
// + machineResourceID)[:16]. It is opaque, stable across pod restarts and
// reschedules, and independent of host properties — the pod receives it via
// env (LAELIA_FINGERPRINT) and never computes it. The manager recomputes the
// same value on job replay to re-mint the machine's refresh token.
func MachineFingerprint(provisionerResourceID, machineResourceID string) string {
	h := sha256.Sum256([]byte("provisioner:" + provisionerResourceID + ":" + machineResourceID))
	return hex.EncodeToString(h[:])[:16]
}

// MachineWorkloadName returns the backend-agnostic workload name for a
// provisioned machine: "laelia-machine-<uuid-prefix>". Used as the machine's
// seeded hostname and as the k8s resource name stem (design §8.1).
func MachineWorkloadName(machineResourceID string) string {
	id := machineResourceID
	if i := strings.IndexByte(id, '-'); i > 0 {
		id = id[:i]
	}
	return "laelia-machine-" + id
}
