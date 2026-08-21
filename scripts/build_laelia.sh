#!/usr/bin/env bash
# Build laelia manager (with embedded frontend; machine binaries are embedded
# by default and can be disabled).
#
# Usage:
#   scripts/build_laelia.sh [output-dir] [embed-machine]
#   scripts/build_laelia.sh --no-embed-machine [output-dir]
#   scripts/build_laelia.sh --release [output-dir]
#
# embed-machine is "true" (default) or "false". You can also pass
# --no-embed-machine / --embed-machine, or set EMBED_MACHINE=true/false.
# When false, the script skips cross-compiling/embedding the per-platform
# machine binaries and builds the manager with only embed_frontend.
#
# release is "false" (default) or "true". You can also pass --release / --dev
# or set RELEASE=true/false. Release builds add the `release` build tag so the
# manager runs in release mode; daily builds default to dev mode.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

OUTPUT_DIR="build"
OUTPUT_DIR_SET=0
EMBED_MACHINE="${EMBED_MACHINE:-true}"
RELEASE="${RELEASE:-false}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-embed-machine)
      EMBED_MACHINE=false
      shift
      ;;
    --embed-machine)
      EMBED_MACHINE=true
      shift
      ;;
    --release)
      RELEASE=true
      shift
      ;;
    --dev)
      RELEASE=false
      shift
      ;;
    --help|-h)
      echo "Usage: $0 [output-dir] [embed-machine] [--no-embed-machine] [--release|--dev]"
      exit 0
      ;;
    true|1|yes)
      EMBED_MACHINE=true
      shift
      ;;
    false|0|no)
      EMBED_MACHINE=false
      shift
      ;;
    *)
      if [[ "${OUTPUT_DIR_SET}" -eq 0 ]]; then
        OUTPUT_DIR="$1"
        OUTPUT_DIR_SET=1
      else
        echo "Unknown argument: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

case "${EMBED_MACHINE}" in
  true|1|yes)
    EMBED_MACHINE=true
    ;;
  false|0|no)
    EMBED_MACHINE=false
    ;;
  *)
    echo "EMBED_MACHINE must be true or false (got: ${EMBED_MACHINE})" >&2
    exit 1
    ;;
esac

case "${RELEASE}" in
  true|1|yes)
    RELEASE=true
    ;;
  false|0|no)
    RELEASE=false
    ;;
  *)
    echo "RELEASE must be true or false (got: ${RELEASE})" >&2
    exit 1
    ;;
esac

EMBED_DIR="backend/manager/server/embedded_machine"
mkdir -p "${OUTPUT_DIR}"

echo "Building frontend..."
rm -rf backend/manager/server/dist
pnpm --dir frontend i --frozen-lockfile
pnpm --dir frontend build
cp -r frontend/dist backend/manager/server/dist

BUILD_TAGS="embed_frontend"
if [[ "${RELEASE}" == "true" ]]; then
  BUILD_TAGS="${BUILD_TAGS} release"
fi
if [[ "${EMBED_MACHINE}" == "true" ]]; then
  scripts/build-embedded-machines.sh "${EMBED_DIR}"
  BUILD_TAGS="${BUILD_TAGS} embed_machine"
fi

BUILD_MODE="dev"
if [[ "${RELEASE}" == "true" ]]; then
  BUILD_MODE="release"
fi
echo "Building manager (${VERSION}, ${BUILD_MODE} mode)..."
CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -ldflags "-w -s -X github.com/Ranxy/laelia/backend/manager/version.Version=${VERSION} -X github.com/Ranxy/laelia/backend/manager/version.GitCommit=${GIT_COMMIT} -X github.com/Ranxy/laelia/backend/manager/version.BuildTime=${BUILD_TIME}" -p=16 \
	-o "${OUTPUT_DIR}/laelia" ./backend/manager/bin/server/main.go

echo ""
echo "Build complete:"
if [[ "${EMBED_MACHINE}" == "true" ]]; then
  echo "  ${OUTPUT_DIR}/laelia        (manager, ${BUILD_MODE} mode, frontend + machine binaries embedded)"
  echo "  ${EMBED_DIR}/               (per-platform machine binaries + manifest)"
else
  echo "  ${OUTPUT_DIR}/laelia        (manager, ${BUILD_MODE} mode, frontend embedded, machine binaries not embedded)"
fi
