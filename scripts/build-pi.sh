#!/usr/bin/env bash
# build-pi.sh — materialize the standalone pi distribution that the release
# build embeds into laelia (see backend/agent/pi/binary_release.go's
# `//go:embed embedded/dist-<goos>-<goarch>`). Run this BEFORE
# `go build -tags release` for a target platform.
#
# pi publishes prebuilt standalone binaries on its GitHub releases for
# linux/darwin/windows on x64+arm64. This script downloads the archive matching
# the target GOOS/GOARCH, verifies it against the release's SHA256SUMS, and
# extracts the whole distribution (binary + theme/, node_modules/, wasm, etc.)
# to backend/agent/pi/embedded/dist-<goos>-<goarch>/. pi resolves its runtime
# assets relative to its own executable, so embedding only the binary crashes
# at startup (missing theme/dark.json).
#
# Usage:
#   scripts/build-pi.sh                       # current platform
#   GOOS=linux GOARCH=amd64 scripts/build-pi.sh
#   PI_VERSION=v0.82.1 scripts/build-pi.sh    # pin a version (default below)
#   LAELIA_BUILD_PROXY=http://host:port scripts/build-pi.sh  # route this download through a proxy
#
# After it succeeds, build laelia for the same target:
#   GOOS=linux GOARCH=amd64 go build -tags release -o laelia ./backend/manager/bin/server/main.go
set -euo pipefail

PI_VERSION="${PI_VERSION:-v0.82.1}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/backend/agent/pi/embedded"

# Resolve target platform (defaults to the build host).
GOOS_TARGET="${GOOS:-$(go env GOOS)}"
GOARCH_TARGET="${GOARCH:-$(go env GOARCH)}"
DIST_DIR="${OUT_DIR}/dist-${GOOS_TARGET}-${GOARCH_TARGET}"
OUT_FILE="${DIST_DIR}/pi"
META_FILE="${DIST_DIR}/pi.meta"

# pi's release asset naming: pi-{os}-{arch}.tar.gz (windows uses .zip).
# Go's amd64 maps to pi's x64; arm64 is unchanged.
arch_name="${GOARCH_TARGET}"
if [[ "${arch_name}" == "amd64" ]]; then arch_name="x64"; fi
os_name="${GOOS_TARGET}"
ext="tar.gz"
if [[ "${os_name}" == "windows" ]]; then ext="zip"; fi

archive="pi-${os_name}-${arch_name}.${ext}"
base_url="https://github.com/earendil-works/pi/releases/download/${PI_VERSION}"
archive_url="${base_url}/${archive}"
sums_url="${base_url}/SHA256SUMS"

# Skip the download when the embedded pi already matches the requested
# version and target platform (recorded in pi.meta). Docker builds always
# download because the binary is excluded from the build context.
if [[ -z "${PI_FORCE:-}" && -s "${OUT_FILE}" && -f "${META_FILE}" ]] \
  && [[ "$(cat "${META_FILE}")" == "${PI_VERSION} ${GOOS_TARGET} ${GOARCH_TARGET}" ]]; then
  echo "build-pi: ${OUT_FILE} already matches ${PI_VERSION} ${GOOS_TARGET}/${GOARCH_TARGET}; skipping download (PI_FORCE=1 to override)"
  exit 0
fi

# LAELIA_BUILD_PROXY is the single build proxy, so restricted networks can
# accelerate GitHub without exporting a global HTTPS_PROXY that would also be
# picked up by docker builds and other tools.
proxy_url="${LAELIA_BUILD_PROXY:-}"
curl_opts=(-fsSL)
if [[ -n "${proxy_url}" ]]; then
  curl_opts+=(--proxy "${proxy_url}")
fi

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

echo "build-pi: downloading ${archive} for ${PI_VERSION}"
curl "${curl_opts[@]}" "${archive_url}" -o "${workdir}/${archive}"
curl "${curl_opts[@]}" "${sums_url}" -o "${workdir}/SHA256SUMS"

# Verify the archive against the published checksum (fail closed on mismatch).
expected="$(awk -v a="${archive}" '$2 == a {print $1}' "${workdir}/SHA256SUMS" | head -n1)"
if [[ -z "${expected}" ]]; then
  echo "build-pi: no checksum found for ${archive} in SHA256SUMS" >&2
  exit 1
fi
actual="$(sha256sum "${workdir}/${archive}" | awk '{print $1}')"
if [[ "${expected}" != "${actual}" ]]; then
  echo "build-pi: checksum mismatch for ${archive}: expected ${expected}, got ${actual}" >&2
  exit 1
fi
echo "build-pi: checksum OK (${actual})"

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

if [[ "${ext}" == "zip" ]]; then
  (cd "${workdir}" && unzip -o "${archive}" -d extracted)
else
  mkdir -p "${workdir}/extracted"
  tar -xzf "${workdir}/${archive}" -C "${workdir}/extracted"
fi

# The archive contains the standalone `pi` binary at its root (or under a
# single top directory). Locate the binary and copy the whole distribution
# root (the directory containing it) into DIST_DIR.
bin_path="$(find "${workdir}/extracted" -type f \( -name pi -o -name pi.exe \) | head -n1)"
if [[ -z "${bin_path}" ]]; then
  echo "build-pi: pi binary not found in archive" >&2
  exit 1
fi
OUT_FILE="${DIST_DIR}/$(basename "${bin_path}")"
# Copy the *contents* of the distribution root so DIST_DIR/pi (or pi.exe) is
# the binary file itself, alongside theme/, node_modules/, etc.
cp -a "$(dirname "${bin_path}")/." "${DIST_DIR}/"
chmod 0700 "${OUT_FILE}"
echo "${PI_VERSION} ${GOOS_TARGET} ${GOARCH_TARGET}" > "${META_FILE}"

echo "build-pi: wrote ${OUT_FILE} ($(wc -c < "${OUT_FILE}") bytes)"
echo "build-pi: now run: GOOS=${GOOS_TARGET} GOARCH=${GOARCH_TARGET} go build -tags release -o laelia ./backend/manager/bin/server/main.go"
