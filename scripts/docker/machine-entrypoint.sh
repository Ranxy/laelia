#!/usr/bin/env bash
# laelia-machine docker entrypoint: maps environment variables to CLI flags.
# The machine authenticates via the OAuth2 device-code flow: `setup` prints an
# approval URL to the container logs and waits for a logged-in user to approve
# it in a browser, then runs in the foreground. No token is ever baked into an
# image or command line.
set -euo pipefail

args=(setup --no-browser)
if [[ -n "${LAELIA_MANAGER_URL:-}" ]]; then
	args+=(--manager "${LAELIA_MANAGER_URL}")
	if [[ "${LAELIA_MANAGER_URL}" == http://* ]]; then
		args+=(--allow-http)
	fi
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
