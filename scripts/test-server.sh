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

# Source shared helpers (worktree_id / worktree_fingerprint).
. ./scripts/build_init.sh

# Per-worktree cache: each git worktree gets its own launcher and server
# artifacts so multiple agents/worktrees never share or clobber each other.
WORKTREE_ID="$(worktree_id)"
WORKTREE_CACHE="$CACHE_DIR/worktrees/$WORKTREE_ID"
mkdir -p "$WORKTREE_CACHE"

# Build the launcher binary into the per-worktree cache. The stamp includes a
# fingerprint of tools/testserver so launcher changes in this worktree are
# picked up without rebuilding it for unrelated source changes.
LAUNCHER="$WORKTREE_CACHE/testserver"
LAUNCHER_STAMP="$WORKTREE_CACHE/testserver.stamp"
LAUNCHER_STAMP_VALUE="${GIT_COMMIT}|$(dir_fingerprint tools/testserver)|testserver-v1"
if [[ ! -x "$LAUNCHER" || ! -f "$LAUNCHER_STAMP" || "$(cat "$LAUNCHER_STAMP")" != "$LAUNCHER_STAMP_VALUE" ]]; then
  echo "Building testserver launcher..."
  (cd tools/testserver && go build -o "$LAUNCHER" .)
  echo "$LAUNCHER_STAMP_VALUE" > "$LAUNCHER_STAMP"
fi

# Forward to the launcher. Only the run subcommand needs the repo root (so it
# can build the manager); stop/status would reject the unknown -repo flag.
if [[ "${1:-}" == "run" ]]; then
  exec "$LAUNCHER" "$@" --repo "$(pwd)"
else
  exec "$LAUNCHER" "$@"
fi
