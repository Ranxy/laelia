#!/usr/bin/env bash
# Regenerate the provisioner operator's generated artifacts:
#   - backend/provisioner/backend/kubernetes/api/v1/zz_generated.deepcopy.go
#   - backend/provisioner/backend/kubernetes/deploy/laelia.sh_laeliamachines.yaml (CRD)
#   - charts/provisioner/crds/laelia.sh_laeliamachines.yaml (same CRD, for the
#     Helm chart; kept in sync so `helm install` ships a matching schema)
#
# controller-tools is pinned so the generated manifests are reproducible; bump
# the version here (and commit the regenerated output) together.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

CRD=backend/provisioner/backend/kubernetes/deploy/laelia.sh_laeliamachines.yaml

CONTROLLER_TOOLS_VERSION=v0.21.0

go run "sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_TOOLS_VERSION}" \
	object:headerFile="backend/provisioner/backend/kubernetes/boilerplate.go.txt" \
	paths="./backend/provisioner/backend/kubernetes/api/..."

go run "sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_TOOLS_VERSION}" \
	crd \
	paths="./backend/provisioner/backend/kubernetes/api/..." \
	output:crd:artifacts:config=backend/provisioner/backend/kubernetes/deploy

# Mirror the CRD into the Helm chart's crds/ directory so both consume the
# same schema (Helm applies crds/ before the charts' templates).
cp "$CRD" charts/provisioner/crds/

echo "Generated:"
echo "  backend/provisioner/backend/kubernetes/api/v1/zz_generated.deepcopy.go"
echo "  $CRD"
echo "  charts/provisioner/crds/laelia.sh_laeliamachines.yaml (copy)"