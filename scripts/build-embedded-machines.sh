#!/usr/bin/env bash
# build-embedded-machines.sh — cross-compile the per-platform laelia-machine
# binaries, gzip them, and write manifest.json into an embed directory.
#
# Usage:
#   scripts/build-embedded-machines.sh [output-dir]
#   scripts/build-embedded-machines.sh --no-pi [output-dir]
#   EMBED_PI=false scripts/build-embedded-machines.sh [output-dir]
#
# The output dir defaults to backend/manager/server/embedded_machine, which
# the manager embeds with `-tags embed_machine`.
#
# EMBED_PI=true (default) downloads and embeds the pi runtime into each
# machine binary. EMBED_PI=false (or --no-pi) builds machine binaries without
# the embedded pi runtime; builtin-pi agents are unavailable, but a
# user-installed pi on PATH is still detected and used.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

EMBED_DIR=""
EMBED_PI="${EMBED_PI:-true}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-pi)
      EMBED_PI=false
      shift
      ;;
    --embed-pi)
      EMBED_PI=true
      shift
      ;;
    --help|-h)
      echo "Usage: $0 [--no-pi|--embed-pi] [output-dir]"
      exit 0
      ;;
    *)
      if [[ -z "${EMBED_DIR}" ]]; then
        EMBED_DIR="$1"
      else
        echo "Unknown argument: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

case "${EMBED_PI}" in
  true|1|yes) EMBED_PI=true ;;
  false|0|no) EMBED_PI=false ;;
  *)
    echo "EMBED_PI must be true or false (got: ${EMBED_PI})" >&2
    exit 1
    ;;
esac

EMBED_DIR="${EMBED_DIR:-backend/manager/server/embedded_machine}"

# Prompt bundle hash: the content hash of the machine binary's embedded static
# prompt files (communication.md / agent_memory.md / reanchor.md plus the
# AgentFirstPromptBody constant in prompt.go). Any change to these files changes
# the hash, which is baked into the binary as version.PromptBundleVersion and
# written into the manager manifest as the expected prompt bundle version.
PROMPT_HASH="$({
  sha256sum "backend/agent/executor/prompt"/*.md
  sha256sum backend/agent/executor/prompt.go
} | sha256sum | cut -c1-16)"

# Target matrix: GOOS GOARCH target-name
TARGETS=(
  "linux amd64 linux-x64"
  "windows amd64 windows-x64"
  "darwin arm64 darwin-arm64"
)

if [[ "${EMBED_PI}" == "true" ]]; then
  echo "Building machine binaries for ${#TARGETS[@]} targets (pi embedded)..."
else
  echo "Building machine binaries for ${#TARGETS[@]} targets (pi not embedded)..."
fi
rm -rf "${EMBED_DIR}"
mkdir -p "${EMBED_DIR}"

manifest_file="${EMBED_DIR}/manifest.json"
cat > "${manifest_file}" <<JSON
{
  "version": "${VERSION}",
  "embed_pi": ${EMBED_PI},
  "prompt_bundle_version": "${PROMPT_HASH}",
  "targets": {
JSON

BUILD_TAGS="release"
if [[ "${EMBED_PI}" == "false" ]]; then
  BUILD_TAGS="${BUILD_TAGS} no_embed_pi"
fi

first=1
for entry in "${TARGETS[@]}"; do
  read -r goos goarch target <<< "${entry}"
  echo "  building ${target} (${goos}/${goarch})..."

  if [[ "${EMBED_PI}" == "true" ]]; then
    GOOS="${goos}" GOARCH="${goarch}" scripts/build-pi.sh
  fi

  bin_name="laelia-machine-${target}"
  if [[ "${EMBED_PI}" == "false" ]]; then
    bin_name="${bin_name}-no-pi"
  fi
  if [[ "${goos}" == "windows" ]]; then
    bin_name="${bin_name}.exe"
  fi
  gz_name="${bin_name}.gz"
  bin_path="${EMBED_DIR}/${bin_name}"
  gz_path="${EMBED_DIR}/${gz_name}"

  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" \
    -ldflags "-w -s -X github.com/Ranxy/laelia/backend/agent/version.Version=${VERSION} -X github.com/Ranxy/laelia/backend/agent/version.GitCommit=${GIT_COMMIT} -X github.com/Ranxy/laelia/backend/agent/version.BuildTime=${BUILD_TIME} -X github.com/Ranxy/laelia/backend/agent/version.PromptBundleVersion=${PROMPT_HASH}" -p=16 \
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
