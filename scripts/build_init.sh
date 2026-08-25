#!/usr/bin/env bash
# Shared variables for the laelia build scripts. Source from the repo root.
set -euo pipefail

VERSION="${VERSION:-local}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

# worktree_id returns a stable per-worktree cache id derived from the absolute
# repo root path. Different git worktrees therefore never share or clobber each
# other's build artifacts.
worktree_id() {
  printf '%s' "$(pwd)" | sha256sum | cut -c1-16
}

# worktree_fingerprint hashes the current worktree source state: HEAD commit,
# staged/unstaged diffs, and untracked non-ignored files. It is what makes a
# per-worktree cache safe to reuse: any source change (including uncommitted
# edits and new untracked files) changes the fingerprint and forces a rebuild.
# When the directory is not inside a git worktree, it falls back to hashing the
# actual source files so changes are still detected.
worktree_fingerprint() {
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    {
      git rev-parse HEAD 2>/dev/null || echo unknown
      git status --porcelain=v1 2>/dev/null
      git diff --binary 2>/dev/null
      git diff --cached --binary 2>/dev/null
      git ls-files --others --exclude-standard 2>/dev/null | sort | while IFS= read -r f; do
        if [[ -f "$f" ]]; then
          sha256sum "$f"
        fi
      done
    } | sha256sum | cut -c1-16
  else
    find . -type f \
      -not -path './.git/*' \
      -not -path './frontend/node_modules/*' \
      -not -path './node_modules/*' \
      -not -path './temp/*' \
      -not -path './build/*' \
      -not -path '*/dist/*' \
      -print0 2>/dev/null | sort -z | xargs -0 sha256sum 2>/dev/null | sha256sum | cut -c1-16
  fi
}

# dir_fingerprint is like worktree_fingerprint but scoped to a single path, so
# a small tool (e.g. the testserver launcher) can be cached against only its
# own source changes instead of the whole repository.
dir_fingerprint() {
  local dir="$1"
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    {
      git rev-parse HEAD 2>/dev/null || echo unknown
      git status --porcelain=v1 -- "$dir" 2>/dev/null
      git diff --binary -- "$dir" 2>/dev/null
      git diff --cached --binary -- "$dir" 2>/dev/null
      git ls-files --others --exclude-standard -- "$dir" 2>/dev/null | sort | while IFS= read -r f; do
        if [[ -f "$f" ]]; then
          sha256sum "$f"
        fi
      done
    } | sha256sum | cut -c1-16
  else
    find "$dir" -type f -print0 2>/dev/null | sort -z | xargs -0 sha256sum 2>/dev/null | sha256sum | cut -c1-16
  fi
}
