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

exec laelia-machine "${args[@]}" "$@"
