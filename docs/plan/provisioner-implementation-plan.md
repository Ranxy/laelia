# Provisioner — Phased Implementation Plan

> Status: verified and updated against the current code on 2026-09-06. Main changes: all six phases are implemented — this plan now marks per-phase completion and records the shipped deviations (no provisioning partial index, provisioner session registry under `component/dispatcher`, bootstrap template under `component/provision/`, offline-provisioner fail-fast, Helm charts for deploy, no kind e2e script).

Companion to [provisioner-design.md](./provisioner-design.md) (referenced below as
"design §N"). The design's §13 lists the work items; this document groups them
into **six phases**, each with a hard dependency boundary, file-level tasks, and
an exit criterion that is a runnable demonstration — not "code written".

**Status (2026-09-06): all six phases are implemented.** The sections below keep
the original task/criteria lists and mark, per phase, what shipped and where the
implementation deviated from the plan. Recent follow-on work beyond this plan:
per-provisioner IAM policies (`roles/provisionerMachineCreator`), a cleanup
guide page after provisioner deletion, and Helm charts for both the manager and
the provisioner (`charts/`).

## Phase overview

| Phase | Title | Depends on | Size | Milestone demo | Status (2026-09-06) |
|---|---|---|---|---|---|
| 1 | Foundation: contracts, storage, auth, permissions | — | M | Provisioner token authenticates through the interceptor; migration applies; all generated types exist | done |
| 2 | Manager control plane | 1 | L | A fake provisioner (Go test client) drives the full job state machine: register → connect → job → progress → machine phases, incl. replay | done |
| 3 | Provisioner binary skeleton | 1 (parallel with 2) | M | `laelia-provisioner` connects to the real manager, receives/replays/acks jobs through a mock backend | done |
| 4 | Machine side & pod contract | 1 (parallel with 2–3) | M | A hand-built container (runtime image + seeded `machine.json` + downloaded binary) comes ONLINE and self-upgrades in place | done |
| 5 | Kubernetes operator backend | 2 + 3 + 4 | L | kind e2e: `ProvisionMachine` → pod Running → machine ONLINE → upgrade → delete → PVC gone | done (operator, manifests, envtest); kind e2e script never committed |
| 6 | Frontend, docs, hardening | 2 (UI); 5 (docs/e2e) | M | The full user story runs from the UI: admin registers provisioner → user clicks create → machine ONLINE | done (UI + docs); e2e checklist script not committed |

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

**Status (2026-09-06): implemented.** `proto/store/store/provisioner.proto`,
`proto/v1/v1/provisioner.proto`, `ProvisioningSetting` (setting oneof field 7),
migration `backend/manager/migration/migration/1.1/0029##provisioner.sql`,
`backend/manager/store/provisioner.go`, the provisioner token audience in
`backend/manager/api/auth/auth.go`, the four `laelia.provisioners.*` catalog
entries, and the predefined `machineProvisioner` role all exist as planned.

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
     `OUTPUT_ONLY`); mirrored on `MachineSummary` as `provisioning = 13`
     (list-badge use; `MachineSummary` carries no `provisioner` field).
3. **Setting wiring** — `ProvisioningSetting` in
   `proto/store/store/setting.proto`; add to the `SettingValue` oneof in
   `proto/v1/v1/setting.proto`.
4. **Generate** — `buf format -w proto && buf lint proto && (cd proto && buf generate)`.
5. **Migration** — `backend/manager/migration/migration/1.1/0029##provisioner.sql`
   with the SQL from design §5.1 (landed as 0029, the last file in `1.1/`).
6. **Store layer** — new `backend/manager/store/provisioner.go`
   (`CreateProvisioner`, `GetProvisionerByResourceID`, `GetProvisioner`,
   `ListProvisioners`, `UpdateProvisionerStatus`,
   `DeleteProvisioner`-soft) and `machine.go` extensions
   (`UpdateMachineProvisioning`, `ListReplayableProvisioningMachines`,
   `CountMachinesByProvisioner`). Shipped deviation: **no
   `idx_machine_provisioning_active` partial index** — migration 0029 records
   that machines per provisioner are few and phases are filtered in Go, so
   `ListReplayableProvisioningMachines` filters replayable phases in Go on a
   `provisioner_id`-bounded lookup instead.
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

- [x] `buf lint` clean, generated code committed; `go build` + `golangci-lint` clean.
- [x] Migration applies (table + indexes are in `0029##provisioner.sql` and
      mirrored idempotently in `migration/LATEST.sql`; no partial provisioning
      index by design, see task 6).
- [x] Interceptor behavior covered: `backend/manager/api/auth/auth_interceptor_test.go`
      exercises the four audience kinds incl. provisioner; the API-level
      provisioner suites cover token rotation killing access and IAM policy
      (`provisioner_integration_test.go`, env-gated).
- [x] Permission catalog contains the four entries; `machineProvisioner` role exists.
- [x] Existing test suites pass unchanged (no behavior change).

## Phase 2 — Manager control plane

**Status (2026-09-06): implemented.** `backend/manager/api/v1/provisioner.go`
+ `provisioner_stream.go` (+ registration in
`backend/manager/server/grpc_routes.go`), the provisioner session registry in
`backend/manager/component/dispatcher` (`provisioner_dispatch.go`,
`liveness.go`), `provisioner_auto_upgrade.go`, and
`backend/manager/component/provision/` (bootstrap template + fingerprint
helper) all exist. Deviations from the plan below.

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
3. **Dispatcher** — provisioner session registry parallel to the machine one,
   shipped in `backend/manager/component/dispatcher/` (`provisioner_dispatch.go`
   is the send path, `liveness.go` the ping/timeout sweep): push jobs by
   provisioner id, emit disconnect notices on rotate/delete, mark status offline
   on stream death.
4. **`ProvisionerChannel` handler** — first frame `ProvisionerReady` → register
   session + stamp `provisioner.status`; inbound `job_progress` →
   `UpdateMachineProvisioning`; ping/pong; **connect replay** of every machine in
   a replayable phase (PENDING / PROVISIONING / DEPROVISIONING) for this
   provisioner (shipped in `provisioner_stream.go`; replay also re-mints the
   pod's refresh token and replays deprovision jobs for soft-deleted machines).
5. **`ProvisionMachine`** (design §6.2) — validations (provisioner alive, backend
   known, `ProvisioningSetting.runtime_image` non-empty, embedded-binary guard,
   `owner` is caller or caller is workspaceAdmin); fingerprint
   `sha256("provisioner:"+provResourceID+":"+machineResourceID)[:16]`
   (`provision.MachineFingerprint`); `CreateMachineWithToken`;
   row at `PENDING`; compose `ProvisionMachineJob` (manager_url, runtime_image,
   binary_target, rendered bootstrap script) and push. Shipped deviation:
   `ProvisionMachine` **fails fast when the provisioner is offline** instead of
   parking the job at PENDING — a job whose stream dies after the push is still
   replayed from its phase on the next connect.
6. **`DeleteMachine` hook** (`backend/manager/api/v1/machine.go`) — when
   `provisioner_id` is set: phase `DEPROVISIONING` + `DeprovisionMachineJob`;
   row deletion not blocked on teardown.
7. **Auto-upgrade loop** — `StartProvisionerAutoUpgradeLoop` (5 min ticker,
   2 min first delay) pushing existing `UpgradeRequest` for connected machines
   of `auto_upgrade` provisioners whose reported version lags the embedded
   latest version (`machinebuild`).
8. **Bootstrap template** —
   `backend/manager/component/provision/provision_bootstrap.sh.tmpl` + render
   helper (unit-tested; consumed by the Phase 5 init container).

**Exit criteria**

- [x] Phase-transition and token tests exist: `provisioner_test.go`
      (`TestApplyProvisionProgress`, `TestKnownProvisionerBackends`) plus the
      env-gated `TestProvisionerTokenVersionKillsAccess` and
      `TestProvisionerIamPolicy` in `provisioner_integration_test.go`.
- [x] Integration test with a fake provisioner: `provisioner_skeleton_test.go`
      drives the real `backend/provisioner/client` loop against the real manager
      with the mock backend (connect → job → DB phases → kill/reconnect replay →
      FAILED → deprovision → rotate notice), and
      `TestProvisionerControlPlane` covers the CRUD/state-machine flow. Both
      are gated by `LAELIA_RUN_PROVISIONER_TESTS=1` + `LAELIA_TEST_PG_URL`.
- [x] `ProvisionMachine` fails fast with clear codes: unknown backend, missing
      runtime image, dev build without embedded binaries, offline provisioner,
      non-admin naming another owner (`TestProvisionMachineFailsFast`).

## Phase 3 — Provisioner binary skeleton (parallel with Phase 2)

**Status (2026-09-06): implemented.** `backend/provisioner/` matches the layout
below (`bin/provisioner`, `cmd/`, `client/`, `backend/` with `docker/` stub,
`kubernetes/` operator, `mock/` test backend); the build output is
`build/laelia-provisioner` via `scripts/build_laelia_provisioner.sh` (and a
Docker image via `scripts/build_laelia_provisioner_docker.sh`).

**Tasks**

1. **Package layout** — `backend/provisioner/` per design §7 (`bin/provisioner/main.go`,
   `cmd/`, `client/`, `backend/`).
2. **Config** — yaml + flags: `manager_url`, `token`, `backend`, `namespace`,
   `manager_url_override`, `retain_data`, `auto_upgrade`, `storage`,
   resources/extra_env passthrough; version stamped via ldflags.
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

- [x] `laelia-provisioner` builds for linux/amd64 (`scripts/build_laelia_provisioner.sh`,
      `GOOS=linux GOARCH=amd64`).
- [x] `TestProvisionerSkeletonClient` (env-gated) drives exactly this: connect,
      receive a job, mock backend events update machine row phases in the DB;
      client killed mid-provision → reconnect replays the same job with no
      duplicate workload.

## Phase 4 — Machine side & pod contract (parallel with Phases 2–3)

**Status (2026-09-06): implemented.** `LAELIA_FINGERPRINT` override,
`setup --provisioned` (with `setup_provisioned_test.go`),
`scripts/docker/Dockerfile.machine-runtime` +
`scripts/docker/machine-runtime-entrypoint.sh` all exist; the bootstrap
template lives at `backend/manager/component/provision/provision_bootstrap.sh.tmpl`.

The "pod contract": how a container becomes a laelia machine with zero human
interaction.

**Tasks**

1. **Fingerprint override** — `LAELIA_FINGERPRINT` env checked in
   `client.ComputeFingerprint` (`backend/agent/client/client.go`); used by
   `setup`'s probe and `client.Connect` alike. Self-hosted unaffected (unset).
2. **`setup --provisioned`** (`backend/agent/cmd/setup.go`) — with a valid saved
   credential: boot normally; with a dead credential: fail fast with a clear log
   line instead of starting the device-code login.
3. **Bootstrap script template** —
   `backend/manager/component/provision/provision_bootstrap.sh.tmpl`
   rendered by the manager (created in Phase 2.8); verified here end-to-end.
   Image contract: POSIX `sh`, `curl`, `gzip`, `sha256sum`.
4. **Runtime image** — `scripts/docker/Dockerfile.machine-runtime` (today's
   machine image minus the Go build and binary) +
   `machine-runtime-entrypoint.sh`: maps `LAELIA_MANAGER_URL` / `LAELIA_INSECURE`
   / `LAELIA_PROVISIONED` (→ `--provisioned --no-browser --foreground`, auto
   `--allow-http` for `http://`), plus `LAELIA_DEBUG` and the
   `LAELIA_CODEX_HOME`→`CODEX_HOME` passthrough, and execs `$LAELIA_MACHINE_BIN`
   (default `/data/bin/laelia-machine`).
5. **Manual verification** — `docker run` the runtime image with a local
   directory as the future PVC: run the bootstrap script against the dev manager
   (binary lands in `<vol>/bin/`), seed `machine.json`, boot → machine ONLINE;
   push an upgrade → supervisor swaps + execs in place on the "PVC".

**Exit criteria**

- [x] Existing agent suites pass (self-hosted flow untouched).
- [x] The container journey is exercised by the k8s backend's init-container +
      runtime-image path (env per `machine-runtime-entrypoint.sh`); the manual
      `docker run` smoke remains a per-release checklist item rather than an
      automated test.

## Phase 5 — Kubernetes operator backend

**Status (2026-09-06): implemented.** `backend/provisioner/backend/kubernetes/`
has the kubebuilder-style scaffold (`api/v1` types + deepcopy, reconciler under
`internal/controller`), the CRD `laeliamachines.laelia.sh` (group `laelia.sh`,
namespace-scoped, status subresource), deploy manifests under
`backend/provisioner/backend/kubernetes/deploy/`, and — newer than this plan —
the Helm chart `charts/provisioner/` (CRD mirrored into its `crds/` by
`scripts/gen-provisioner-manifests.sh`). envtest: `internal/controller/suite_test.go`
drives the reconciler against a real apiserver+etcd but **skips when
`KUBEBUILDER_ASSETS`/`.gopath/envtest` assets are absent, and no CI workflow
runs it** (the repo's workflows only build releases). Unit-level contract tests
live in `builders_test.go` / `backend_test.go`.

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
5. **Deploy artifacts** — CRD + namespace-scoped RBAC + Deployment manifests
   (`backend/provisioner/backend/kubernetes/deploy/`); Helm chart
   `charts/provisioner/` wrapping them (crds/ + RBAC + ConfigMap + Secret +
   Deployment); `scripts/docker/Dockerfile.provisioner`;
   `scripts/gen-provisioner-manifests.sh` regenerates the deepcopy + CRD and
   mirrors the CRD into the chart.
6. **envtest suite** — `internal/controller/suite_test.go` (reconcile →
   children; delete → cleanup; retention honored), asset-gated; plus hermetic
   builder tests in `builders_test.go` (pod contract, retain-data retention,
   container env, pod-status observation).
7. **kind e2e** — planned but **not committed as a script**: no kind runner
   exists under `scripts/`; the closest automated coverage is the env-gated
   manager-side control-plane suite (`LAELIA_RUN_PROVISIONER_TESTS`) plus the
   envtest reconciler suite. The manual checklist lives in `docs/deploy.md` §8.

**Exit criteria**

- [x] envtest suite committed and green when envtest assets are installed
      (`setup-envtest` into `.gopath/envtest` or `KUBEBUILDER_ASSETS`); it
      self-skips otherwise — **not wired into CI** (repo has no test workflow).
- [ ] kind e2e green, including the offline-provisioner replay case — the
      automated kind loop was not built; replay is covered by the env-gated
      manager integration tests with the mock backend instead.
- [x] `kubectl get laeliamachines` shows status via the CRD's status
      subresource; the machine credential lives in a Secret, not the CR.

## Phase 6 — Frontend, docs, hardening

**Status (2026-09-06): implemented** — with additions beyond this plan. The
Provisioned tab, provisioning card, Provisioners settings, runtime-image field,
and deploy docs all shipped; the e2e checklist script did not (the env-gated
`LAELIA_RUN_PROVISIONER_TESTS` suites are the automated coverage). Post-plan
additions: per-provisioner IAM authorization (`roles/provisionerMachineCreator`
bindings checked via the provisioner roster), a cleanup-guide page
(`settings-provisioner-cleanup.tsx`) shown after a provisioner is deleted, a
provisioner access page, and Helm charts for the manager too (`charts/manager/`,
documented in `docs/deploy.md` §3b).

**Tasks**

1. **`/machines/new`** — "Provisioned" tab, visible with
   `laelia.provisioners.provision` or a non-empty provisioner roster
   (per-provisioner IAM grants don't appear in the workspace-scope permission
   set — `machine-new.tsx` detects them via `ListProvisioners`); self-hosted
   content became the "Self-hosted" tab with the provisioner picker (title,
   backend, connected badge), title field, Create → navigate to the machine
   profile; profile polls until ONLINE (3 s), showing
   `ProvisioningPhaseBadge` meanwhile.
2. **Machine profile** — "Provisioning" card (`MachineProvisioningCard` in
   `machine-profile-cards.tsx`: provisioner, backend, `workload_name`, phase
   timeline, last error); delete confirmation states the data-volume
   consequence per the provisioner's retention policy.
3. **Settings** — Provisioners page (`settings-provisioners.tsx`: table + Add
   dialog ending in the copy-once token dialog + Rotate/Delete with the
   machines-bound refusal inline; detail/access/cleanup sub-pages);
   runtime-image field in Settings → General (`settings-general.tsx`, setting
   `value.provisioning.runtime_image`).
4. **Frontend gates** — `pnpm --dir frontend biome:check && lint && type-check && test`.
5. **Docs** — `docs/deploy.md`: §8 covers the provisioner end to end (manager
   registration, token rotation, `machineProvisioner` self-service binding,
   kubernetes install incl. Helm, k8s ≥ 1.27 for PVC auto-delete, amd64-only
   note, cleanup steps).
6. **Hardening** — the e2e checklist script (test-server + kind) was **not
   committed**; automated coverage is the env-gated provisioner suites instead.
   Release-build wiring exists (`scripts/build_laelia_provisioner.sh`,
   `scripts/build_laelia_provisioner_docker.sh`, `Dockerfile.provisioner`).

**Exit criteria**

- [x] The whole story runs from the UI: admin registers a provisioner and
      configures the runtime image → an entitled user creates a machine → it
      appears and goes ONLINE with no further action → upgrade → delete
      (`TestProvisionerControlPlane` + `TestProvisionerIamPolicy` cover the
      server side; the UI flow is manual).
- [x] Deploy docs merged (deploy.md §8 + Helm sections); AGENTS.md gates are
      the standing repo-wide workflow.

---

## Cross-cutting rules

- **Quality gates per phase** (AGENTS.md): `gofmt`, `golangci-lint run
  --allow-parallel-runners` until clean, `buf lint` for protos, `biome:check` +
  `type-check` + tests for frontend; conventional commits.
- **Contract-first**: Phases 3/4 start from Phase 1's generated types only —
  stream-frame shape changes after Phase 2 begins require updating this plan.
- **No silent scope creep**: anything from design §14 (bootstrap exchange,
  arm64, retry RPC, …) stays out of these six phases. (2026-09-06: held — the
  manager drives only the `kubernetes` backend, the machine binary ships
  linux-x64 / windows-x64 / darwin-arm64 for self-hosted installs, provisioned
  pods are amd64-only per `docs/deploy.md` §8.2, and no retry RPC exists.)