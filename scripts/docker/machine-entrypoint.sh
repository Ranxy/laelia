#!/usr/bin/env bash
# laelia-machine docker entrypoint: maps environment variables to CLI flags so
# the token never has to be baked into an image or command line.
set -euo pipefail

args=(run)
if [[ -n "${LAELIA_MANAGER_URL:-}" ]]; then
	args+=(--manager "${LAELIA_MANAGER_URL}")
	if [[ "${LAELIA_MANAGER_URL}" == http://* ]]; then
		args+=(--allow-http)
	fi
fi
if [[ -n "${LAELIA_TOKEN:-}" ]]; then
	args+=(--token "${LAELIA_TOKEN}")
fi
if [[ "${LAELIA_INSECURE:-false}" == "true" ]]; then
	args+=(--insecure)
fi
if [[ "${LAELIA_DEBUG:-false}" == "true" ]]; then
	args+=(--debug)
fi
# Codex provider login/config: point CODEX_HOME at a mounted writable volume
# carrying config.toml + auth/models.json. Without it codex falls back to
# ~/.codex under the container home (writable, but loses login state on
# container recreation).
if [[ -n "${LAELIA_CODEX_HOME:-}" ]]; then
	export CODEX_HOME="${LAELIA_CODEX_HOME}"
fi

exec laelia-machine "${args[@]}" "$@"
