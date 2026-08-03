#!/usr/bin/env bash
# Build laelia manager (with embedded frontend) and machine (with embedded pi)
# for the current platform. Usage: scripts/build_laelia.sh [output-dir]
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

OUTPUT_DIR="${1:-build}"
mkdir -p "${OUTPUT_DIR}"

echo "Building frontend..."
rm -rf backend/manager/server/dist
pnpm --dir frontend i --frozen-lockfile
pnpm --dir frontend build
cp -r frontend/dist backend/manager/server/dist

echo "Building manager (${VERSION})..."
CGO_ENABLED=0 go build -tags embed_frontend -ldflags "-w -s" -p=16 \
	-o "${OUTPUT_DIR}/laelia" ./backend/manager/bin/server/main.go

echo "Building machine with embedded pi..."
scripts/build-pi.sh
CGO_ENABLED=0 go build -tags release -ldflags "-w -s" -p=16 \
	-o "${OUTPUT_DIR}/laelia-machine" ./backend/agent/bin/agent/main.go

echo ""
echo "Build complete:"
echo "  ${OUTPUT_DIR}/laelia        (manager, frontend embedded)"
echo "  ${OUTPUT_DIR}/laelia-machine (machine, pi embedded)"
