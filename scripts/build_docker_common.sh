#!/usr/bin/env bash
# Shared helpers for the laelia docker build scripts. Source from the repo root.
set -euo pipefail

. ./scripts/build_init.sh

# Populate the global BUILD_ARGS array with the build args common to all laelia
# docker images (VERSION, GIT_COMMIT, LAELIA_BUILD_PROXY).
collect_common_build_args() {
	BUILD_ARGS=(
		--build-arg "VERSION=${VERSION}"
		--build-arg "GIT_COMMIT=${GIT_COMMIT}"
	)
	if [[ -n "${LAELIA_BUILD_PROXY:-}" ]]; then
		BUILD_ARGS+=(--build-arg "LAELIA_BUILD_PROXY=${LAELIA_BUILD_PROXY}")
	fi
}
