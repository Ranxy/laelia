#!/usr/bin/env bash
# Build the laelia docker images: laelia/manager (frontend embedded) and
# laelia/machine (pi embedded).
#
# Usage:
#   scripts/build_laelia_docker.sh
#   LAELIA_BUILD_PROXY=http://host:port scripts/build_laelia_docker.sh
#   APT_MIRROR=http://mirrors.aliyun.com/debian scripts/build_laelia_docker.sh
#
# LAELIA_BUILD_PROXY is the single build proxy: it routes the Go module
# download and the pi GitHub download through the proxy. It is passed as a
# custom build arg only to the Go build stages, so the final images never
# contain proxy settings and the apt/apk steps of the runtime stages stay
# proxy-free. PI_PROXY, when set, overrides the proxy for the pi download
# only. APT_MIRROR swaps the machine image's Debian apt source for a faster
# local mirror.
#
# Note: do NOT export global HTTP_PROXY/HTTPS_PROXY to proxy docker builds;
# BuildKit auto-injects those standard args into every build stage, including
# the runtime images. LAELIA_BUILD_PROXY avoids that.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

echo "Building laelia docker images ${VERSION}..."

build_args=(
	--build-arg "VERSION=${VERSION}"
	--build-arg "GIT_COMMIT=${GIT_COMMIT}"
)
if [[ -n "${LAELIA_BUILD_PROXY:-}" ]]; then
	build_args+=(--build-arg "LAELIA_BUILD_PROXY=${LAELIA_BUILD_PROXY}")
fi
if [[ -n "${PI_PROXY:-}" ]]; then
	build_args+=(--build-arg "PI_PROXY=${PI_PROXY}")
fi
if [[ -n "${APT_MIRROR:-}" ]]; then
	build_args+=(--build-arg "APT_MIRROR=${APT_MIRROR}")
fi

docker build -f ./scripts/Dockerfile \
	"${build_args[@]}" \
	-t "laelia/manager:${VERSION}" \
	-t "laelia/manager:latest" \
	--target manager .

docker build -f ./scripts/Dockerfile \
	"${build_args[@]}" \
	-t "laelia/machine:${VERSION}" \
	-t "laelia/machine:latest" \
	--target machine .

echo ""
echo "Images:"
echo "  laelia/manager:${VERSION}  (run with LAELIA_PG_URL pointing at PostgreSQL)"
echo "  laelia/machine:${VERSION}  (run with LAELIA_MANAGER_URL and LAELIA_TOKEN)"
