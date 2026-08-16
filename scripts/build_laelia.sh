#!/usr/bin/env bash
# Build laelia manager (with embedded frontend + embedded machine binaries)
# and the current-platform machine binary. Usage: scripts/build_laelia.sh [output-dir]
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

OUTPUT_DIR="${1:-build}"
EMBED_DIR="backend/manager/server/embedded_machine"
mkdir -p "${OUTPUT_DIR}"

echo "Building frontend..."
rm -rf backend/manager/server/dist
pnpm --dir frontend i --frozen-lockfile
pnpm --dir frontend build
cp -r frontend/dist backend/manager/server/dist

scripts/build-embedded-machines.sh "${EMBED_DIR}"

echo "Building manager (${VERSION})..."
CGO_ENABLED=0 go build -tags "embed_frontend embed_machine" -ldflags "-w -s" -p=16 \
	-o "${OUTPUT_DIR}/laelia" ./backend/manager/bin/server/main.go

echo ""
echo "Build complete:"
echo "  ${OUTPUT_DIR}/laelia        (manager, frontend + machine binaries embedded)"
echo "  ${EMBED_DIR}/               (per-platform machine binaries + manifest)"
