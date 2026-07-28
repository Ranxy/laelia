#!/usr/bin/env bash
# build-pi.sh — materialize the standalone pi binary that the release build
# embeds into laelia (see backend/agent/pi/binary_release.go's `//go:embed
# embedded/pi`). Run this BEFORE `go build -tags release` for a target platform.
#
# pi publishes prebuilt standalone binaries on its GitHub releases for
# linux/darwin/windows on x64+arm64. This script downloads the archive matching
# the target GOOS/GOARCH, verifies it against the release's SHA256SUMS, and
# extracts the `pi` binary to backend/agent/pi/embedded/pi.
#
# Usage:
#   scripts/build-pi.sh                       # current platform
#   GOOS=linux GOARCH=amd64 scripts/build-pi.sh
#   PI_VERSION=v0.82.1 scripts/build-pi.sh    # pin a version (default below)
#
# After it succeeds, build laelia for the same target:
#   GOOS=linux GOARCH=amd64 go build -tags release -o laelia ./backend/manager/bin/server/main.go
set -euo pipefail

PI_VERSION="${PI_VERSION:-v0.82.1}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/backend/agent/pi/embedded"
OUT_FILE="${OUT_DIR}/pi"

# Resolve target platform (defaults to the build host).
GOOS_TARGET="${GOOS:-$(go env GOOS)}"
GOARCH_TARGET="${GOARCH:-$(go env GOARCH)}"

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

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

echo "build-pi: downloading ${archive} for ${PI_VERSION}"
curl -fsSL "${archive_url}" -o "${workdir}/${archive}"
curl -fsSL "${sums_url}" -o "${workdir}/SHA256SUMS"

# Verify the archive against the published checksum (fail closed on mismatch).
expected="$(grep "  ${archive}\$" "${workdir}/SHA256SUMS" | awk '{print $1}')"
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

mkdir -p "${OUT_DIR}"
rm -f "${OUT_FILE}"

if [[ "${ext}" == "zip" ]]; then
  (cd "${workdir}" && unzip -o "${archive}" -d extracted)
else
  mkdir -p "${workdir}/extracted"
  tar -xzf "${workdir}/${archive}" -C "${workdir}/extracted"
fi

# The archive contains the standalone `pi` binary at its root (or under a
# single top directory). Find it and copy it in.
bin_path="$(find "${workdir}/extracted" -type f -name pi | head -n1)"
if [[ -z "${bin_path}" ]]; then
  echo "build-pi: pi binary not found in archive" >&2
  exit 1
fi
cp "${bin_path}" "${OUT_FILE}"
chmod 0700 "${OUT_FILE}"

echo "build-pi: wrote ${OUT_FILE} ($(stat -c%s "${OUT_FILE}") bytes)"
echo "build-pi: now run: GOOS=${GOOS_TARGET} GOARCH=${GOARCH_TARGET} go build -tags release -o laelia ./backend/manager/bin/server/main.go"