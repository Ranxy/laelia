#!/usr/bin/env bash
# Build the laelia machine docker image (pi embedded).
#
# Usage:
#   scripts/build_laelia_machine_docker.sh
#   LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_machine_docker.sh
#   APT_MIRROR=http://mirrors.aliyun.com/debian scripts/build_laelia_machine_docker.sh
#
# LAELIA_BUILD_PROXY is the single build proxy: it routes the Go module
# download and the pi GitHub download through the proxy. It is passed as a
# custom build arg only to the Go build stages, so the final image never
# contains proxy settings and the apt steps of the runtime stage stay
# proxy-free. APT_MIRROR swaps the machine image's Debian apt source for a
# faster local mirror. CODEX_NPM_SPEC pins the codex CLI version installed in
# the machine image (default @openai/codex@0.146.0).
#
# Note: do NOT export global HTTP_PROXY/HTTPS_PROXY to proxy docker builds;
# BuildKit auto-injects those standard args into every build stage, including
# the runtime image. LAELIA_BUILD_PROXY avoids that.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_docker_common.sh
collect_common_build_args

if [[ -n "${APT_MIRROR:-}" ]]; then
	BUILD_ARGS+=(--build-arg "APT_MIRROR=${APT_MIRROR}")
fi
if [[ -n "${CODEX_NPM_SPEC:-}" ]]; then
	BUILD_ARGS+=(--build-arg "CODEX_NPM_SPEC=${CODEX_NPM_SPEC}")
fi

echo "Building laelia machine docker image ${VERSION}..."
docker build -f ./scripts/docker/Dockerfile.machine \
	"${BUILD_ARGS[@]}" \
	-t "laelia/machine:${VERSION}" \
	-t "laelia/machine:latest" \
	.

echo ""
echo "Image:"
echo "  laelia/machine:${VERSION}  (run with LAELIA_MANAGER_URL; approve the device login printed to the logs)"
