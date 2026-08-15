#!/usr/bin/env bash
# Build the laelia manager docker image (frontend + machine binaries embedded).
#
# Usage:
#   scripts/build_laelia_manager_docker.sh
#   LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_manager_docker.sh
#
# LAELIA_BUILD_PROXY is the single build proxy: it routes the Go module
# download through the proxy. It is passed as a custom build arg only to the Go
# build stage, so the final image never contains proxy settings.
#
# Note: do NOT export global HTTP_PROXY/HTTPS_PROXY to proxy docker builds;
# BuildKit auto-injects those standard args into every build stage, including
# the runtime image. LAELIA_BUILD_PROXY avoids that.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_docker_common.sh
collect_common_build_args

echo "Building laelia manager docker image ${VERSION}..."
docker build -f ./scripts/docker/Dockerfile.manager \
	"${BUILD_ARGS[@]}" \
	-t "laelia/manager:${VERSION}" \
	-t "laelia/manager:latest" \
	.

echo ""
echo "Image:"
echo "  laelia/manager:${VERSION}  (run with LAELIA_PG_URL pointing at PostgreSQL)"
