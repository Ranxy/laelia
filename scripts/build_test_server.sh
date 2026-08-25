#!/usr/bin/env bash
# Build the laelia manager binary (frontend embedded) into the shared test
# cache. Only the manager is built — the machine/pi build is not needed for a
# test server. Safe to run concurrently: a flock serializes the actual build
# and the git stamp lets repeat invocations skip it.
#
# Usage: scripts/build_test_server.sh [--force] [--release|--dev]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. ./scripts/build_init.sh

CACHE_DIR="${LAELIA_TEST_CACHE:-$HOME/.cache/laelia-test}"
# Per-worktree artifacts: the binary and stamp live under
# $CACHE_DIR/worktrees/<worktree-id>/ so multiple git worktrees never share or
# clobber each other's builds.
WORKTREE_ID="$(worktree_id)"
WORKTREE_CACHE="$CACHE_DIR/worktrees/$WORKTREE_ID"
BIN="$WORKTREE_CACHE/laelia"
STAMP="$WORKTREE_CACHE/build.stamp"
RELEASE="${RELEASE:-false}"
FORCE=0
QUIET=0
mkdir -p "$CACHE_DIR" "$WORKTREE_CACHE"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force)
      FORCE=1
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
    --quiet)
      QUIET=1
      shift
      ;;
    --help|-h)
      echo "Usage: $0 [--force] [--release|--dev] [--quiet]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

case "${RELEASE}" in
  true|1|yes) RELEASE=true ;;
  false|0|no) RELEASE=false ;;
  *)
    echo "RELEASE must be true or false (got: ${RELEASE})" >&2
    exit 1
    ;;
esac

# Serialize concurrent builds; waiters reuse the artifact produced by the first.
exec 9>"$CACHE_DIR/.build.lock"
flock 9

# build-info-v3 invalidates caches produced before the manager binary started
# embedding version/git commit/build time; the release flag is part of the
# stamp so dev and release artifacts never share a cache entry. The worktree
# fingerprint additionally captures uncommitted/untracked source changes so a
# per-worktree cache is only reused when the current code is truly unchanged.
BUILD_STAMP="${GIT_COMMIT}|${VERSION}|${RELEASE}|$(worktree_fingerprint)|build-info-v3"
if [[ -f "$BIN" && -f "$STAMP" && "$(cat "$STAMP")" == "$BUILD_STAMP" && "${FORCE}" -ne 1 ]]; then
  if [[ "${RELEASE}" == "true" ]]; then
    MODE="release"
  else
    MODE="dev"
  fi
  echo "laelia already built and matches current worktree source (git commit ${GIT_COMMIT}, ${MODE} mode, including any uncommitted changes); skipping."
  exit 0
fi

if [[ "${RELEASE}" == "true" ]]; then
  MODE="release"
else
  MODE="dev"
fi
if [[ "${QUIET}" -eq 1 ]]; then
  BUILD_LOG="$WORKTREE_CACHE/build.log"
  echo "Building laelia from current worktree source (git commit ${GIT_COMMIT}, ${MODE} mode, including any uncommitted changes)..."
  rm -rf backend/manager/server/dist
  pnpm --dir frontend i --frozen-lockfile >"$BUILD_LOG" 2>&1
  pnpm --dir frontend build >>"$BUILD_LOG" 2>&1
  cp -r frontend/dist backend/manager/server/dist
  echo "frontend build complete"
  BUILD_TAGS="embed_frontend"
  if [[ "${RELEASE}" == "true" ]]; then
    BUILD_TAGS="${BUILD_TAGS} release"
  fi
  BUILD_MODE="dev"
  if [[ "${RELEASE}" == "true" ]]; then
    BUILD_MODE="release"
  fi
  CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -ldflags "-w -s -X github.com/Ranxy/laelia/backend/manager/version.Version=${VERSION} -X github.com/Ranxy/laelia/backend/manager/version.GitCommit=${GIT_COMMIT} -X github.com/Ranxy/laelia/backend/manager/version.BuildTime=${BUILD_TIME}" -p=16 -o "$BIN" ./backend/manager/bin/server/main.go >>"$BUILD_LOG" 2>&1
  echo "backend build complete"
else
  echo "Building laelia from current worktree source (git commit ${GIT_COMMIT}, ${MODE} mode, including any uncommitted changes)..."
  echo "Building frontend..."
  rm -rf backend/manager/server/dist
  pnpm --dir frontend i --frozen-lockfile
  pnpm --dir frontend build
  cp -r frontend/dist backend/manager/server/dist

  BUILD_TAGS="embed_frontend"
  if [[ "${RELEASE}" == "true" ]]; then
    BUILD_TAGS="${BUILD_TAGS} release"
  fi
  BUILD_MODE="dev"
  if [[ "${RELEASE}" == "true" ]]; then
    BUILD_MODE="release"
  fi
  echo "Building manager (embed_frontend, ${BUILD_MODE} mode)..."
  CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -ldflags "-w -s -X github.com/Ranxy/laelia/backend/manager/version.Version=${VERSION} -X github.com/Ranxy/laelia/backend/manager/version.GitCommit=${GIT_COMMIT} -X github.com/Ranxy/laelia/backend/manager/version.BuildTime=${BUILD_TIME}" -p=16 -o "$BIN" ./backend/manager/bin/server/main.go
fi

echo "$BUILD_STAMP" > "$STAMP"
echo "Build complete: $BIN"
