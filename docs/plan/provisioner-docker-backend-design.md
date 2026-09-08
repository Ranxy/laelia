# Provisioner — Docker Backend — Reference

> Status: **implemented** (2026-09-08). Maintenance reference for the docker
> provisioner backend (`backend/provisioner/backend/docker/`), the second
> implementation of the `Backend` interface (`docs/plan/provisioner-design.md`
> §7.1). User-facing install docs: `docs/deploy.md` §8.7 (中文:
> `docs/deploy_zh.md` §8.7).

## Decisions (confirmed with the product owner)

| Question | Decision |
|---|---|
| Form | Docker Engine API via the Go SDK; one machine = one container + one named volume. No compose/swarm. |
| Daemon | Standard docker env resolution (local socket, `DOCKER_HOST`, `DOCKER_TLS_VERIFY`/`DOCKER_CERT_PATH`) + API version negotiation; remote-safe (named volumes only, no host bind mounts). |
| Credential + bootstrap | `docker cp` into the created-but-not-started container; the refresh token never appears in env or inspect metadata. |
| Machine params | `cpu` + `memory` only (named volumes have no size; `disk`/`storage_class` are undeclared). |
| Docker config keys | None — everything comes from the neutral `backend.Config` (see the mapping table below). |
| Shutdown | Best-effort stop of the provisioner's own container; no-op for bare processes. |
| Self-heal | Local spec journal rebuilds containers removed out-of-band (the docker analog of the CR as desired-state store). |

## Workload shape

All objects derive from the workload stem `laelia-machine-<M-uuid-first-segment>`
(`backend.WorkloadStem`, shared with the kubernetes backend).

```
named volume  laelia-machine-<p>-data      (local driver)
  mounted at /data  →  the whole LAELIA_HOME world:
      /data/bin/laelia-machine   (bootstrap-downloaded binary)
      /data/laelia/machine.json  (authoritative machine state)
      /data/laelia/...           (workspaces, logs, daemon socket, codex)

container  laelia-machine-<p>   (image: spec.RuntimeImage — provides only the
                                 environment; nothing laelia-specific)
  mounts     -v laelia-machine-<p>-data:/data
  env        LAELIA_HOME=/data/laelia
             LAELIA_FINGERPRINT=<spec.Fingerprint>
             LAELIA_MANAGER_URL=<spec.ManagerURL>          (override-aware)
             LAELIA_PROVISIONED=true
             CODEX_HOME=/data/laelia/codex
             <ExtraEnv passthrough, sorted by name>
  command    "/bootstrap/bootstrap.sh || exit $?;\n" + backend.MachineRunScript
             (overrides whatever the image's own ENTRYPOINT is)
  restart    unless-stopped
  resources  --cpus / --memory hard limits (k8s quantities converted, §7)
  labels     app.kubernetes.io/managed-by=laelia-provisioner
             app.kubernetes.io/name=laelia-machine
             laelia.sh/machine=laelia-machine-<p>
             laelia.sh/machine-id=<full manager resource id>   (event routing)
             laelia.sh/spec-digest=<spec digest, below>
             <spec.Labels passthrough — reserved labels win>
```

### Startup + credential injection

1. `ContainerCreate` (stopped; the data volume already mounted).
2. `docker cp` two files into the container's rw layer (not the volume):
   `/bootstrap/bootstrap.sh` ← `spec.BootstrapScript` (manager-rendered,
   provisioner-design §8.3) and `/bootstrap/machine.json` ← the machine state
   JSON (shared `backend.MachineStateJSON`).
3. `ContainerStart`. The command first runs the bootstrap script (idempotent:
   skips the download when the binary is on the volume, seeds machine.json
   only if absent), then runs `backend.MachineRunScript` — the same inline
   startup the kubernetes pod spec injects (env → CLI flags → `exec
   $LAELIA_MACHINE_BIN`). The image's own entrypoint is never consulted; any
   contract image works, even one without any entrypoint.

The wrapper re-runs on every container start (init-container lifecycle). The
volume's `machine.json` is authoritative after first boot, so recreating a
container whose volume survived needs no credential injection at all. Fixed
choices: restart policy `unless-stopped`; `/bin/sh -c` wrapper; volume
ownership comes from the image's `/data` initialization (the docker job k8s
does with `fsGroup`).

## Core mechanics

### Idempotent upsert + spec digest

The spec digest is sha256 over the backend-affecting projection of the job
(runtime image, manager URL, fingerprint, env incl. ExtraEnv, cpu/memory,
labels, startup-script digest) stored in the `laelia.sh/spec-digest` label.
Provision flow:

1. Ensure the data volume exists.
2. Inspect the container: **absent** → create → cp → start; **digest match** →
   strict no-op (replay), only re-emit the observed phase; **digest differs** →
   stop + remove the container (volume kept), create → cp → start.
3. Emit `PROVISIONING` (unconditional ack), then the current-truth phase.

The refresh token is deliberately excluded — replayed jobs re-mint it
(provisioner-design §4.3), so the digest must be credential-blind. The
startup-script digest is included so a provisioner upgrade that changes the
startup rolls replayed workloads (the kubernetes pod-template-hash analog).

### Watcher: events stream + reconcile poll

Two loops after a fail-fast daemon `Ping` in `Start` (construction is hermetic
— no dial):

1. Docker events stream (container events) → immediate inspect + phase frame;
   reconnects with backoff on stream death.
2. Reconcile ticker (30s): inspect every tracked machine — the correctness
   floor that makes the events stream a latency optimization. The initial pass
   also seeds the registry from `ContainerList` + the journal.

Phase mapping (mirrors the `LaeliaMachine` CR phases):

| Container state | Reported phase |
|---|---|
| `created` / `restarting` / `removing` | `PROVISIONING` |
| `running` | `PROVISIONED` |
| ≥3 `die` events within 10 min (crash-loop counter, reset on a running stretch) | `FAILED` with exit code |
| `exited` past the 60s transient grace / `paused` / `dead` | `FAILED` ("will not restart") |
| container absent, volume present, journal entry exists | recreate from the journal, then the rows above |
| container absent, volume absent | nothing (job replay / deprovision owns it) |

Note: the backend re-reports truthfully (recovery flips FAILED back to
PROVISIONED), but the manager's phase machine is terminal-sticky — FAILED only
advances to DEPROVISIONING — so flip-back frames are folded away there; the UI
remedy for a failed machine is delete + re-create.

### Spec journal (self-heal)

The manager replays provision jobs only for `PENDING`/`PROVISIONING` machines,
so an already-`PROVISIONED` machine whose container someone `docker rm`'d
would otherwise stay OFFLINE forever. One JSON file (`machineID → spec
snapshot`), located beside the config file (`docker-specs.json`) or under
`$XDG_STATE_HOME/laelia-provisioner/`; written atomically on every Provision,
removed on Deprovision. **It never stores the refresh token** — credentials
live on the data volume, so recovery works exactly when the volume survived;
a lost volume is the unrecoverable terminal case (crash-loop → FAILED → UI
delete + re-create).

### Deprovision / Shutdown

Deprovision is idempotent and replay-safe: stop + remove the container, remove
the volume unless `keep_data`, drop the journal entry, emit `DELETED` once
removals settle (missing objects are success — the manager replays teardown
until the workload reports gone). Shutdown (manager permanently deleted the
provisioner) stops the provisioner's own container, self-identified via
hostname → container-id prefix match; no-op for bare processes; best-effort.

## Config mapping + machine params

| `backend.Config` | docker backend use |
|---|---|
| `Namespace` | unused; the reported `WorkloadName` is the container name |
| `RetainData` | echoed in `ProvisionerReady.retain_data`; the manager returns it as `keep_data` |
| `Resources` | cpu/memory defaults (Limits, else Requests) for container limits + `MachineParams` |
| `Storage` | ignored (named volumes have no sizing) |
| `ExtraEnv` | container env passthrough |
| `ParamBounds` | reported with the cpu/memory specs |
| `StatePath` | spec journal location |

`MachineParams` reports `cpu` + `memory` only. Conversion: k8s quantities
(via `backend/common/quantity`) → `--cpus` (milli/1000; docker min 0.01) and
`--memory` bytes (docker minimums surface as `Provision` errors → FAILED).
Unknown `spec.Params` keys are ignored (machine-params design Appendix A F2).
The reported workloads' `MachineRunScript` injects the same flag mapping as
the kubernetes pod (`--manager`/`--allow-http`/`--provisioned`/`--insecure`/
`--debug`).

## Invariants (do not break when changing this code)

Derived from a design-time ablation pass; these are the couplings that
silently break the system:

- **Replay safety is credential-blind idempotency.** The digest must never
  include the refresh token, and an in-sync replay must be a strict no-op —
  otherwise every reconnect restarts machines and a flapping stream becomes a
  create/destroy loop. Any future upsert comparator must stay token-blind.
- **The reconcile poll is the correctness floor; events are latency.** Phase
  truth is push-only (the manager never re-checks workloads); removing the
  poll degrades correctness with no signal.
- **The named volume is the durability boundary; credentials live on it,
  never in backend state.** The journal holds the spec but can never re-seed
  a credential — recovery works exactly when the volume survived.
- **`unless-stopped` is load-bearing**: without it a host reboot is
  indistinguishable from a fleet-wide crash-loop and the ≥3-dies heuristic
  never fires.
- **Droppable with documented remedies**: the spec journal (remedy: UI delete
  + re-provision), the Shutdown self-stop (remedy: crash-loop noise), docker
  config keys (add on first real need).
- Forced by the platform: `cpu`+`memory` params only (no volume sizing);
  terminal-sticky FAILED manager-side (zero-manager-change constraint).

## Failure modes

| Scenario | Behavior |
|---|---|
| Daemon unreachable at `Start` | Ping fails fast → process exits (crash-loop visibility) |
| Daemon restarts mid-run | Events stream reconnects; the reconcile ticker covers the gap |
| Container crash-loops | `FAILED` with last exit code; recovery → `PROVISIONED` re-report (folded manager-side) |
| `docker rm` out-of-band | Reconcile recreates from the journal (volume survived); volume also gone → crash-loop `FAILED` → UI delete + re-create |
| Job replayed while container healthy | No-op upsert; the re-minted token is deliberately not pushed into the live workload |
| Image pull slow/failing | `ContainerCreate` pulls inline; failure → `Provision` error → `FAILED` frame |
| Manager URL unreachable from the container | Container runs (`PROVISIONED`) but the machine stays OFFLINE — same as k8s pod semantics |
| Spec journal lost | Only self-healing degrades; existing containers keep reconciling from docker state |

## Where things live

- `backend/provisioner/backend/docker/`: `docker.go` (Backend impl),
  `spec.go` (snapshot/digest/journal), `container.go` (builders, cp, phase
  mapping), `watch.go` (events + reconciler).
- Tests: `docker_test.go` (hermetic, no daemon), `docker_integration_test.go`
  (`LAELIA_RUN_DOCKER_TESTS=1` — full journey incl. replay no-op, journal
  recovery, keep_data). The fake runtime image is plain busybox — proving the
  injected startup carries the whole machine launch.
- Enablement: `knownProvisionerBackends` (`backend/manager/api/v1/
  provisioner.go`), `BACKEND_OPTIONS` (`frontend/src/pages/dashboard/
  settings-provisioners.tsx`); shared startup/helpers in
  `backend/provisioner/backend/backend.go` (`MachineRunScript`,
  `WorkloadStem`, `MachineStateJSON` — used by both backends).
- Dependency: `github.com/docker/docker` v28.x with
  `client.WithAPIVersionNegotiation()`; no image changes
  (`scripts/build_laelia_provisioner_docker.sh` unchanged).
- Non-goals (unchanged): compose/swarm, custom networks/restart policies,
  GPU passthrough, Windows/arm64, multiple docker hosts per provisioner.