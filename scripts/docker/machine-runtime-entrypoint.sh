#!/usr/bin/env bash
# laelia machine-runtime entrypoint: turns the pod's environment into CLI
# flags and execs the laelia-machine binary that the pod's init container
# downloaded onto the data volume (LAELIA_MACHINE_BIN, default
# /data/bin/laelia-machine). The image contains no laelia binary of its own —
# the binary is always manager-distributed (design §8.3/§8.4).
#
# Environment (all optional):
#   LAELIA_MANAGER_URL   manager base URL; http:// URLs auto-add --allow-http
#   LAELIA_INSECURE=true skip TLS certificate verification
#   LAELIA_PROVISIONED=true  run `setup --provisioned` (headless pod mode: a
#                        dead credential fails fast instead of the device-code
#                        login, which nobody can approve from a pod log)
#   LAELIA_DEBUG=true    debug logging
#   LAELIA_HOME          machine data root (the PVC: machine.json, workspaces)
#   LAELIA_MACHINE_BIN   binary path to exec (default /data/bin/laelia-machine)
#   LAELIA_CODEX_HOME    writable codex home, exported as CODEX_HOME
set -euo pipefail

BIN="${LAELIA_MACHINE_BIN:-/data/bin/laelia-machine}"
if [[ ! -x "${BIN}" ]]; then
	echo "machine-runtime: ${BIN} is missing or not executable; the init container must run the bootstrap script before this entrypoint" >&2
	exit 1
fi

args=(setup --no-browser --foreground)
if [[ "${LAELIA_PROVISIONED:-false}" == "true" ]]; then
	args+=(--provisioned)
fi
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
# carrying config.toml + auth/models.json (k8s default: LAELIA_HOME/codex on
# the PVC, so login state survives restarts).
if [[ -n "${LAELIA_CODEX_HOME:-}" ]]; then
	export CODEX_HOME="${LAELIA_CODEX_HOME}"
fi

exec "${BIN}" "${args[@]}" "$@"