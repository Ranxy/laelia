#!/usr/bin/env bash
# Build the laelia-provisioner docker image (the kubernetes operator backend).
#
# Usage:
#   scripts/build_laelia_provisioner_docker.sh
#   LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_provisioner_docker.sh
#
# The image carries only the provisioner binary and CA certificates; the
# deploy manifests live in backend/provisioner/backend/kubernetes/deploy/.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_docker_common.sh
collect_common_build_args

echo "Building laelia provisioner docker image ${VERSION}..."
docker build -f ./scripts/docker/Dockerfile.provisioner \
	"${BUILD_ARGS[@]}" \
	-t "laelia/provisioner:${VERSION}" \
	-t "laelia/provisioner:latest" \
	.

echo ""
echo "Image:"
echo "  laelia/provisioner:${VERSION}  (run with the config in backend/provisioner/backend/kubernetes/deploy/)"