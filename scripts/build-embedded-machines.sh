#!/usr/bin/env bash
# build-embedded-machines.sh — cross-compile the per-platform laelia-machine
# binaries, gzip them, and write manifest.json into an embed directory.
#
# Usage:
#   scripts/build-embedded-machines.sh [output-dir]
#
# The output dir defaults to backend/manager/server/embedded_machine, which
# the manager embeds with `-tags embed_machine`.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

EMBED_DIR="${1:-backend/manager/server/embedded_machine}"

# Target matrix: GOOS GOARCH target-name
TARGETS=(
  "linux amd64 linux-x64"
  "windows amd64 windows-x64"
  "darwin arm64 darwin-arm64"
)

echo "Building machine binaries for ${#TARGETS[@]} targets..."
rm -rf "${EMBED_DIR}"
mkdir -p "${EMBED_DIR}"

manifest_file="${EMBED_DIR}/manifest.json"
cat > "${manifest_file}" <<JSON
{
  "version": "${VERSION}",
  "targets": {
JSON

first=1
for entry in "${TARGETS[@]}"; do
  read -r goos goarch target <<< "${entry}"
  echo "  building ${target} (${goos}/${goarch})..."

  GOOS="${goos}" GOARCH="${goarch}" scripts/build-pi.sh

  bin_name="laelia-machine-${target}"
  if [[ "${goos}" == "windows" ]]; then
    bin_name="${bin_name}.exe"
  fi
  gz_name="laelia-machine-${target}.gz"
  bin_path="${EMBED_DIR}/${bin_name}"
  gz_path="${EMBED_DIR}/${gz_name}"

  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go build -tags release \
    -ldflags "-w -s -X github.com/Ranxy/laelia/backend/agent/version.Version=${VERSION}" -p=16 \
    -o "${bin_path}" ./backend/agent/bin/agent/main.go

  gzip -9 -c "${bin_path}" > "${gz_path}"

  bin_sha="$(sha256sum "${bin_path}" | awk '{print $1}')"
  gz_sha="$(sha256sum "${gz_path}" | awk '{print $1}')"
  bin_size="$(wc -c < "${bin_path}")"
  gz_size="$(wc -c < "${gz_path}")"

  if [[ "${first}" -ne 1 ]]; then
    printf ',\n' >> "${manifest_file}"
  fi
  first=0
  cat >> "${manifest_file}" <<JSON
    "${target}": {
      "file": "${bin_name}",
      "sha256": "${bin_sha}",
      "size": ${bin_size},
      "gz": {
        "file": "${gz_name}",
        "sha256": "${gz_sha}",
        "size": ${gz_size}
      }
    }
JSON
done

cat >> "${manifest_file}" <<JSON
  }
}
JSON

echo "Wrote ${manifest_file}"
