#!/usr/bin/env bash
# Build the laelia-provisioner binary (the kubernetes operator backend's
# manager client). For container installs use
# scripts/build_laelia_provisioner_docker.sh instead; this script targets bare
# processes that run outside a container (systemd, etc.).
#
# Usage:
#   scripts/build_laelia_provisioner.sh                      # host platform -> build/laelia-provisioner
#   GOOS=linux GOARCH=amd64 scripts/build_laelia_provisioner.sh
#   VERSION=v1.2.3 scripts/build_laelia_provisioner.sh       # release-style version stamp
#
# VERSION/GIT_COMMIT/BUILD_TIME mirror the ldflags the docker build injects.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

VERSION="${VERSION:-local}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

mkdir -p build
go build -trimpath \
	-ldflags "-w -s \
	-X github.com/Ranxy/laelia/backend/provisioner/version.Version=${VERSION} \
	-X github.com/Ranxy/laelia/backend/provisioner/version.GitCommit=${GIT_COMMIT} \
	-X github.com/Ranxy/laelia/backend/provisioner/version.BuildTime=${BUILD_TIME}" \
	-o build/laelia-provisioner ./backend/provisioner/bin/provisioner

echo "Built build/laelia-provisioner (${VERSION} ${GIT_COMMIT} ${BUILD_TIME})"
echo "Run with: ./build/laelia-provisioner run --config <provisioner.yaml>"