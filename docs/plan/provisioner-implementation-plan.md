# Provisioner — Phased Implementation Plan

Companion to [provisioner-design.md](./provisioner-design.md) (referenced below as
"design §N"). The design's §13 lists the work items; this document groups them
into **six phases**, each with a hard dependency boundary, file-level tasks, and
an exit criterion that is a runnable demonstration — not "code written".

## Phase overview

| Phase | Title | Depends on | Size | Milestone demo |
|---|---|---|---|---|
| 1 | Foundation: contracts, storage, auth, permissions | — | M | Provisioner token authenticates through the interceptor; migration applies; all generated types exist |
| 2 | Manager control plane | 1 | L | A fake provisioner (Go test client) drives the full job state machine: register → connect → job → progress → machine phases, incl. replay |
| 3 | Provisioner binary skeleton | 1 (parallel with 2) | M | `laelia-provisioner` connects to the real manager, receives/replays/acks jobs through a mock backend |
| 4 | Machine side & pod contract | 1 (parallel with 2–3) | M | A hand-built container (runtime image + seeded `machine.json` + downloaded binary) comes ONLINE and self-upgrades in place |
| 5 | Kubernetes operator backend | 2 + 3 + 4 | L | kind e2e: `ProvisionMachine` → pod Running → machine ONLINE → upgrade → delete → PVC gone |
| 6 | Frontend, docs, hardening | 2 (UI); 5 (docs/e2e) | M | The full user story runs from the UI: admin registers provisioner → user clicks create → machine ONLINE |

```
P1 ──► P2 ──────────────► P6 (UI parts)
 │  ╲
 │   ╲─► P3 ──┐
 │   ╲        ├──► P5 ──► P6 (docs/e2e)
 └────► P4 ──┘
```

Phases 3 and 4 only need Phase 1's generated types, so they proceed in parallel
with Phase 2. Phase 5 is the first phase that touches all three moving parts
(manager, provisioner, machine pod) and is where integration risk concentrates.

Suggested shipping: one PR per phase, conventional-commit messages. No phase
leaves the tree in a state that fails the AGENTS.md gates.

---

## Phase 1 — Foundation: contracts, storage, auth, permissions

Pure additions; no behavior change anywhere. Everything later imports the types
created here.

**Tasks**

1. **Store protos** — new `proto/store/store/provisioner.proto`:
   `ProvisionerStatus`, `ProvisioningPhase` (values `PROVISIONING_PHASE_*`),
   `ProvisioningStatus` (design §5.2).
2. **v1 protos** — new `proto/v1/v1/provisioner.proto`, following the layout of
   `machine.proto` (which also hosts `MachineStreamService`):
   - `ProvisionerService` + request/response messages (design §6.1);
   - `ProvisionerStreamService` with `ProvisionerChannel`, frame messages,
     `ProvisionMachineJob` / `DeprovisionMachineJob` / `ProvisionJobProgress`,
     `ProvisionerReady` / `DisconnectNotice` (design §4.3, §6.3);
   - `Machine` gains `provisioning = 18`, `provisioner = 19` (both
     `OUTPUT_ONLY`); mirror on `MachineSummary` if list badges need it.
3. **Setting wiring** — `ProvisioningSetting` in
   `proto/store/store/setting.proto`; add to the `SettingValue` oneof in
   `proto/v1/v1/setting.proto`.
4. **Generate** — `buf format -w proto && buf lint proto && (cd proto && buf generate)`.
5. **Migration** — `backend/manager/migration/migration/1.1/0029##provisioner.sql`
   with the SQL from design §5.1 (confirm 0029 is still the next free number).
6. **Store layer** — new `backend/manager/store/provisioner.go`
   (`CreateProvisioner`, `GetProvisionerByResourceID`, `GetProvisioner`,
   `ListProvisioners`, `UpdateProvisionerStatus`,
   `DeleteProvisioner`-soft) and `machine.go` extensions
   (`UpdateMachineProvisioning`, `ListReplayableProvisioningMachines` — driven by
   the `idx_machine_provisioning_active` partial index, `CountMachinesByProvisioner`).
7. **Auth** — `backend/manager/api/auth/auth.go`: fourth audience kind
   (`ll.provisioner.access.%s`), `provisionerClaimsMessage`,
   `authenticateProvisionerByClaims` (loads via `GetProvisionerByResourceID`,
   checks deleted + `token_version`), context injection via
   `common.ProvisionerContextKey`. Token mint/verify helpers parallel to the
   machine token (HS256, kid `v1`, no expiry, subject = provisioner resource id).
8. **Permissions** — the four `laelia.provisioners.*` entries in
   `backend/common/permission/permission.json` (regenerate), plus the
   predefined `machineProvisioner` role in
   `backend/manager/store/predefined_roles.go`. `workspaceMember` unchanged.

**Exit criteria**

- [ ] `buf lint` clean, generated code committed; `go build` + `golangci-lint` clean.
- [ ] Migration applies on the dev database (`psql … -c "\d provisioner"` shows the
      columns and the partial index).
- [ ] Interceptor unit test: a minted provisioner token resolves to the provisioner
      principal in the context; a user/machine token is rejected on provisioner RPCs
      and vice versa.
- [ ] Permission catalog contains the four entries; `machineProvisioner` role exists.
- [ ] Existing test suites pass unchanged (no behavior change).

## Phase 2 — Manager control plane

Everything the manager needs to register provisioners, stream jobs, and track
provisioning state. Testable end-to-end **without any cluster** using a fake
provisioner client.

**Tasks**

1. **Wiring** — `backend/manager/api/v1/provisioner.go` (+ server registration of
   `ProvisionerService` and `ProvisionerStreamService`).
2. **CRUD handlers** — `CreateProvisioner` (mint token, return once),
   `ListProvisioners`/`GetProvisioner` (with live `machine_count`),
   `RotateProvisionerToken` (bump version, close stream),
   `DeleteProvisioner` (`FailedPrecondition` while machines bound; bump; close).
3. **Dispatcher** — provisioner session registry parallel to the machine
   dispatcher (`backend/manager/api/v1/machine_command.go`): push jobs by
   provisioner id, emit disconnect notices on rotate/delete, mark status offline
   on stream death.
4. **`ProvisionerChannel` handler** — first frame `ProvisionerReady` → register
   session + stamp `provisioner.status`; inbound `job_progress` →
   `UpdateMachineProvisioning`; ping/pong; **connect replay** of every machine in
   a replayable phase (PENDING / PROVISIONING / DEPROVISIONING) for this
   provisioner.
5. **`ProvisionMachine`** (design §6.2) — validations (provisioner alive, backend
   known, `ProvisioningSetting.runtime_image` non-empty, embedded-binary guard,
   `owner` is caller or caller is workspaceAdmin); fingerprint
   `sha256("provisioner:"+provID+":"+machineUUID)[:16]`; `CreateMachineWithToken`;
   row at `PENDING`; compose `ProvisionMachineJob` (manager_url, runtime_image,
   binary_target, rendered bootstrap script) and push — or park at PENDING when
   the provisioner is offline.
6. **`DeleteMachine` hook** (`backend/manager/api/v1/machine.go`) — when
   `provisioner_id` is set: phase `DEPROVISIONING` + `DeprovisionMachineJob`;
   row deletion not blocked on teardown.
7. **Auto-upgrade loop** — ticker pushing existing `UpgradeRequest` for machines
   of `auto_upgrade` provisioners whose version lags `machinebuild.LatestVersion()`.
8. **Bootstrap template** — `server/provision_bootstrap.sh.tmpl` + render helper
   (unit-tested; consumed by the Phase 5 init container).

**Exit criteria**

- [ ] Unit tests for the phase-transition state machine and token mint/rotate
      paths (mirroring `device.go` tests).
- [ ] Integration test with a fake provisioner (real Connect client, minted
      provisioner token): create → connect → receive job → report
      PROVISIONING → PROVISIONED; kill the stream, reconnect → job replayed;
      `DeleteProvisioner` refused while a machine is bound.
- [ ] `ProvisionMachine` fails fast with clear codes: unknown backend, missing
      runtime image, dev build without embedded binaries, non-admin naming
      another owner.

## Phase 3 — Provisioner binary skeleton (parallel with Phase 2)

**Tasks**

1. **Package layout** — `backend/provisioner/` per design §7 (`bin/provisioner/main.go`,
   `cmd/`, `client/`, `backend/`).
2. **Config** — yaml + flags: `manager_url`, `token`, `backend`, `namespace`,
   `manager_url_override`, `retain_data`, resources/extra_env passthrough;
   version stamped via ldflags.
3. **Client loop** — connect (Bearer token) → `ProvisionerReady` → receive pump
   (`ProvisionMachineJob` → `Backend.Provision`, `DeprovisionMachineJob` →
   `Backend.Deprovision`) → ping ticker → exponential-backoff reconnect. No
   durable local job state (replay comes from the manager).
4. **Backend abstraction** — `Backend` interface + registry +
   `ErrUnsupportedBackend` (design §7.1); `docker/` stub.
5. **Mock backend** (test-only backend type) — fakes phase progression
   PROVISIONING → PROVISIONED → DELETED for manager-side integration tests
   before the operator exists.

**Exit criteria**

- [ ] `laelia-provisioner` builds for linux/amd64.
- [ ] Against the Phase 2 manager: connects, receives a job, mock backend emits
      events, machine row phases update in the DB; `kill -9` + reconnect replays
      the same job with no duplicate workload (idempotency observed via mock).

## Phase 4 — Machine side & pod contract (parallel with Phases 2–3)

The "pod contract": how a container becomes a laelia machine with zero human
interaction.

**Tasks**

1. **Fingerprint override** — `LAELIA_FINGERPRINT` env checked in
   `client.ComputeFingerprint` (`backend/agent/client/client.go`); used by
   `setup`'s probe and `client.Connect` alike. Self-hosted unaffected (unset).
2. **`setup --provisioned`** (`backend/agent/cmd/setup.go`) — with a valid saved
   credential: boot normally; with a dead credential: fail fast with a clear log
   line instead of starting the device-code login.
3. **Bootstrap script template** — `server/provision_bootstrap.sh.tmpl`
   rendered by the manager (created in Phase 2.8); verified here end-to-end.
   Image contract: POSIX `sh`, `curl`, `gzip`, `sha256sum`.
4. **Runtime image** — `scripts/docker/Dockerfile.machine-runtime` (today's
   machine image minus the Go build and binary) +
   `machine-runtime-entrypoint.sh`: maps `LAELIA_MANAGER_URL` / `LAELIA_INSECURE`
   / `LAELIA_PROVISIONED` (→ `--provisioned --no-browser --foreground`, auto
   `--allow-http` for `http://`) and execs `$LAELIA_MACHINE_BIN`
   (default `/data/bin/laelia-machine`).
5. **Manual verification** — `docker run` the runtime image with a local
   directory as the future PVC: run the bootstrap script against the dev manager
   (binary lands in `<vol>/bin/`), seed `machine.json`, boot → machine ONLINE;
   push an upgrade → supervisor swaps + execs in place on the "PVC".

**Exit criteria**

- [ ] Existing agent suites pass (self-hosted flow untouched).
- [ ] The manual container journey works: download → seed → ONLINE → in-place
      upgrade, all state under `LAELIA_HOME` on the volume.

## Phase 5 — Kubernetes operator backend

The first phase that runs all three components together. Highest integration
risk; budget time for the kind loop.

**Tasks**

1. **Scaffold** — `backend/provisioner/backend/kubernetes/` with kubebuilder-style
   `api/v1` types (`LaeliaMachine` + deepcopy) and a reconciler under
   `internal/controller`; add controller-runtime to go.mod (dependency-weight
   review before merging).
2. **CRD manifest** — `laeliamachines.laelia.sh` (group `laelia.sh/v1`,
   namespace-scoped), `status` subresource.
3. **Reconciler** — for each CR: Secret (machine.json, from `Provision`),
   headless Service, StatefulSet (replicas 1, `volumeClaimTemplates: [data]`,
   `persistentVolumeClaimRetentionPolicy` from provisioner config, nodeSelector
   `kubernetes.io/arch: amd64`, init container with the bootstrap script +
   read-only Secret mount at `/bootstrap`, main container env per design §8.2);
   watch pod → phase `Pending → Creating → Ready` / `Failed` (error text from
   pod status); ownerReferences on all children; explicit PVC GC fallback on CR
   delete.
4. **Backend bridge** (`kubernetes/backend.go`) — `Provision` = idempotent upsert
   of Secret + CR; `Deprovision` = delete CR (`keep_data` → `whenDeleted:
   Retain`); CR/pod watches → `Event` channel → client loop →
   `ProvisionJobProgress`.
5. **Deploy artifacts** — CRD + namespace-scoped RBAC + Deployment manifests;
   `scripts/docker/Dockerfile.provisioner`; build-script wiring for the
   provisioner image.
6. **envtest suite** — reconcile creates/updates children; delete cleans up;
   retention honored.
7. **kind e2e** — `scripts/test-server.sh` manager + locally built provisioner
   deployed to kind: create → pod Running → machine ONLINE → upgrade → delete →
   PVC gone (default) / retained (config); provisioner killed mid-provision →
   replay completes the workload.

**Exit criteria**

- [ ] envtest suite green in CI.
- [ ] kind e2e green, including the offline-provisioner replay case.
- [ ] `kubectl get laeliamachines` shows sane status; no token in any CR.

## Phase 6 — Frontend, docs, hardening

**Tasks**

1. **`/machines/new`** — "Provisioned" tab, visible with
   `laelia.provisioners.provision` (self-hosted content becomes the
   "Self-hosted" tab): provisioner picker (title, backend, connected badge),
   title field, Create → navigate to the machine profile; profile polls until
   ONLINE, showing the provisioning phase chip meanwhile.
2. **Machine profile** — "Provisioning" card (provisioner, backend,
   `workload_name`, phase timeline, last error); delete confirmation states the
   data-volume consequence per the provisioner's retention policy.
3. **Settings** — new Provisioners page (table + Add dialog ending in the
   copy-once token dialog + Rotate/Delete with the machines-bound refusal
   inline); runtime-image field in Settings → General with the image-contract
   help text.
4. **Frontend gates** — `pnpm --dir frontend biome:check && lint && type-check && test`.
5. **Docs** — `docs/deploy.md`: provisioner install (token, config, RBAC matrix,
   k8s ≥ 1.27 for PVC auto-delete), runtime image contract, troubleshooting.
6. **Hardening** — e2e checklist script (test-server + kind) committed;
   release-build wiring for the provisioner binary/image; final pass over the
   design doc's §11 failure-mode table against actual behavior.

**Exit criteria**

- [ ] The whole story runs from the UI: admin registers a provisioner and
      configures the runtime image → an entitled user creates a machine → it
      appears and goes ONLINE with no further action → upgrade → delete.
- [ ] Deploy docs merged; AGENTS.md gates pass repo-wide.

---

## Cross-cutting rules

- **Quality gates per phase** (AGENTS.md): `gofmt`, `golangci-lint run
  --allow-parallel-runners` until clean, `buf lint` for protos, `biome:check` +
  `type-check` + tests for frontend; conventional commits.
- **Contract-first**: Phases 3/4 start from Phase 1's generated types only —
  stream-frame shape changes after Phase 2 begins require updating this plan.
- **No silent scope creep**: anything from design §14 (bootstrap exchange,
  arm64, retry RPC, …) stays out of these six phases.