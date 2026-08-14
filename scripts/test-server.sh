#!/usr/bin/env bash
# One-click laelia test server launcher.
#
# Builds the frontend + backend (embed_frontend) on first use, then starts an
# isolated instance: embedded PostgreSQL + the laelia manager on a random port,
# seeded with preset test users. All runtime state lives in --workdir; delete
# that directory to clean up.
#
# Usage:
#   scripts/test-server.sh run    --workdir <dir> [options]
#   scripts/test-server.sh stop   --workdir <dir>
#   scripts/test-server.sh status --workdir <dir>
#
# Options are forwarded to the launcher; see testserver run --help.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

CACHE_DIR="${LAELIA_TEST_CACHE:-$HOME/.cache/laelia-test}"
mkdir -p "$CACHE_DIR"

# Build the launcher binary once into the shared cache.
LAUNCHER="$CACHE_DIR/testserver"
if [[ ! -x "$LAUNCHER" ]]; then
  echo "Building testserver launcher..."
  (cd tools/testserver && go build -o "$LAUNCHER" .)
fi

# Forward to the launcher. Only the run subcommand needs the repo root (so it
# can build the manager); stop/status would reject the unknown -repo flag.
if [[ "${1:-}" == "run" ]]; then
  exec "$LAUNCHER" "$@" --repo "$(pwd)"
else
  exec "$LAUNCHER" "$@"
fi
