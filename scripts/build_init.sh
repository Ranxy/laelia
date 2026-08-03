#!/usr/bin/env bash
# Shared variables for the laelia build scripts. Source from the repo root.
set -euo pipefail

VERSION="${VERSION:-local}"
GIT_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
