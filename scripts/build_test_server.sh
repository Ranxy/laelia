#!/usr/bin/env bash
# Build the laelia manager binary (frontend embedded) into the shared test
# cache. Only the manager is built — the machine/pi build is not needed for a
# test server. Safe to run concurrently: a flock serializes the actual build
# and the git stamp lets repeat invocations skip it.
#
# Usage: scripts/build_test_server.sh [--force]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

CACHE_DIR="${LAELIA_TEST_CACHE:-$HOME/.cache/laelia-test}"
BIN="$CACHE_DIR/laelia"
STAMP="$CACHE_DIR/build.stamp"
mkdir -p "$CACHE_DIR"

# Serialize concurrent builds; waiters reuse the artifact produced by the first.
exec 9>"$CACHE_DIR/.build.lock"
flock 9

if [[ -f "$BIN" && -f "$STAMP" && "$(cat "$STAMP")" == "$GIT_COMMIT" && "${1:-}" != "--force" ]]; then
  echo "laelia already built ($GIT_COMMIT); skipping."
  exit 0
fi

echo "Building frontend..."
rm -rf backend/manager/server/dist
pnpm --dir frontend i --frozen-lockfile
pnpm --dir frontend build
cp -r frontend/dist backend/manager/server/dist

echo "Building manager (embed_frontend)..."
CGO_ENABLED=0 go build -tags embed_frontend -ldflags "-w -s" -p=16 -o "$BIN" ./backend/manager/bin/server/main.go

echo "$GIT_COMMIT" > "$STAMP"
echo "Build complete: $BIN"
