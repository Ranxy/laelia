# Provisioner — Managed Machine Provisioning — Design

> Status: verified and updated against the current code on 2026-09-06. Main changes: the design is fully implemented (manager services + stream replay, k8s operator, bootstrap script, frontend, Helm charts); §6.2/§11 rewritten for the shipped fail-fast-when-offline behavior, §6.5 rewritten for the dual-scope (workspace + per-provisioner) IAM model, §7/§8 updated for the `Backend.Shutdown` hook, `retain_data` plumbing, and the Helm charts.

## 0. Decision record (confirmed with product owner)

| # | Question | Decision |
|---|----------|----------|
| 1 | How does a provisioner authenticate to the manager? | **Dedicated provisioner registration flow**: an admin creates a provisioner record in the manager UI, the manager mints a long-lived **provisioner token** shown once. Not a service account. |
| 2 | How does the new machine's refresh token reach the pod? | **Directly in the provisioning job**: the manager mints the machine refresh token (bound to a provisioner-chosen fingerprint) and hands it to the provisioner over the control stream; the provisioner writes it into a backend secret. The provisioner (an enterprise-trusted component) sees the token. |
| 3 | Can one manager host multiple provisioners? | **Yes.** Each provisioner registers with a name + backend type (`kubernetes`, `docker`, …); the create-machine UI picks the target provisioner. |
| 4 | PVC retention when a machine is deleted | **Default: delete** the PVC with the machine (StatefulSet retention policy `Delete`), configurable per provisioner to `Retain`. |
| 5 | Container image for the machine pod | **Manager-side configuration**: the workspace admin configures a *runtime image* reference. The image never contains the machine binary — it only provides the agent runtime environment (node, python, toolchain, codex CLI, …). |
| 6 | Upgrade policy | **Manual** via the existing `UpgradeMachine` flow, plus an optional per-provisioner `auto_upgrade` (default off) that auto-triggers upgrades for its machines. |
| 7 | Who may provision | **New permission `laelia.provisioners.provision`**, granted via role binding (default: nobody but workspace admins; admins bind a role for members). |
| 8 | arm64 clusters | **MVP is amd64-only**: the provisioner pins `kubernetes.io/arch=amd64`. `linux-arm64` embedding is a follow-up. |

### Implementation status (2026-09-06)

All phases of §13 are implemented and verifiable in the tree:

- **Manager services**: `backend/manager/api/v1/provisioner.go` (CRUD + one-time token + `ProvisionMachine` + job state machine), `provisioner_stream.go` (channel + reconnect replay), `provisioner_auto_upgrade.go` (5-min ticker), wired in `backend/manager/server/grpc_routes.go`.
- **Store + migration**: `backend/manager/store/provisioner.go`, `provisioner` table + `machine.provisioner_id`/`machine.provisioning` in `backend/manager/migration/migration/LATEST.sql`.
- **Auth**: fourth audience branch `ProvisionerAccessTokenAudienceFmt` in `backend/manager/api/auth/auth.go`.
- **Provisioner binary**: `backend/provisioner/` (client loop, `Backend` registry, kubernetes operator with `LaeliaMachine` CRD, docker stub, mock backend).
- **Machine-side**: `LAELIA_FINGERPRINT` override (`backend/agent/client/client.go`), `setup --provisioned` (`backend/agent/cmd/setup.go`), runtime image (`scripts/docker/Dockerfile.machine-runtime` + `machine-runtime-entrypoint.sh`).
- **Frontend**: machine-new "Provisioned" tab, Settings → Provisioners (+ per-provisioner IAM access + post-delete cleanup pages), machine profile provisioning card.
- **Deploy**: manifests `backend/provisioner/backend/kubernetes/deploy/`, Helm charts `charts/provisioner/` and `charts/manager/`, `docs/deploy.md` §3b + §8 (k8s ≥ 1.27 pinned).
- **Not carried out**: the planned kind e2e runner script was never committed (`scripts/` has none; the manual checklist lives in `docs/deploy.md` §8), and the k8s envtest suite (`backend/provisioner/backend/kubernetes/internal/controller/suite_test.go`) is asset-gated (`KUBEBUILDER_ASSETS`) and runs in no CI workflow — exercise it locally. (The former phased implementation plan was consolidated into this status block once all six phases landed.)

## 1. Goals

Add an **optional enterprise layer above machines**: a *provisioner* that connects
out to the manager and creates machine workloads on demand, fully automated:

1. **Self-service, zero-touch**: an authorized user clicks "create" on the machine
   page; the provisioner creates the machine's workload in its backend (k8s
   StatefulSet today), the machine connects to the manager, and the machine appears
   online — no shell access, no approval URL, no copied tokens.
2. **Non-invasive**: the existing self-hosted flow (user installs
   `laelia-machine`, runs `setup`, approves the device-code login) is unchanged.
   A provisioned machine is a normal machine row; every existing machine
   feature (agents, IAM, upgrade, workspaces) works identically.
3. **Backend-pluggable**: the provisioner is designed around a small `Backend`
   interface. Kubernetes is the only implemented backend; docker/other
   virtualization backends plug in later without manager changes.
4. **Image ≠ binary**: the machine binary is *never* baked into the runtime
   image. The pod downloads it from the manager into a PVC once (init
   container); upgrades reuse the existing in-place self-upgrade machinery.

### Non-goals (MVP)

- Docker/VM backends: interface + registry stub only.
- `linux-arm64` embedded binaries.
- Per-machine resource/size selection in the UI (provisioner-level defaults only).
- Stop/start (scale-to-zero) of provisioned machines.
- Re-issuing credentials of a provisioned machine in place (re-auth = deprovision
  + provision again).
- Namespace-per-machine isolation; all machines land in the provisioner's
  configured namespace (see §12 Security for what this implies).

## 2. Background — what exists today

- **Machine registration** is exclusively the device-code flow
  (`proto/v1/v1/device.proto`, `backend/manager/api/v1/device.go`): the CLI starts
  a session, a user approves on `/login/device`, and `approveNewMachine` creates
  the `machine` row + mints a **refresh token** bound to the client fingerprint
  (`hostname:os:arch` sha256[:16]) — `CreateMachineWithToken` stores only the hash.
- **Machine state** lives in `LAELIA_HOME/machine.json`
  (`backend/agent/state/state.go`): `{manager_url, machine_id, refresh_token,
  hostname, created_at}`. All other state (daemon socket, logs, per-agent
  workspaces) is under `LAELIA_HOME` too (`backend/agent/home`).
- **Machine runtime**: `laelia-machine daemon` (supervisor, foreground in
  containers) spawns `run` (MachineChannel control stream + heartbeats + one
  AgentChannel per assigned agent). Self-upgrade: manager pushes
  `UpgradeRequest{version, target, sha256}` → supervisor downloads
  `/machine/bin/<target>`, verifies checksums against `/machine/manifest.json`,
  swaps the binary next to itself, and **execs the new supervisor in place**
  (`backend/agent/supervisor`). The binary path is arbitrary — putting it on a
  PVC changes nothing.
- **Binary distribution**: the manager embeds per-platform binaries and serves
  them **unauthenticated** at `/machine/manifest.json`, `/machine/bin/:target`,
  plus install scripts (`backend/manager/server/machine_download.go`). Embedded
  targets today: `linux-x64`, `windows-x64`, `darwin-arm64`.
- **Auth interceptor** (`backend/manager/api/auth/auth.go`) branches on the JWT
  audience (`user` / `agent` / `machine` / `provisioner` — the fourth kind is
  the provisioner branch, `ProvisionerAccessTokenAudienceFmt`), verifies the
  signature once, loads the row, and checks `token_version`.
- **Permissions** are a generated catalog (`backend/common/permission`);
  workspaceAdmin holds everything, workspaceMember holds a baseline list
  (`backend/manager/store/predefined_roles.go`).

## 3. Architecture overview

```
                      ┌────────────────────────────────────────────────┐
                      │ manager                                        │
                      │  ProvisionerService (IAM RPCs)                 │
                      │   • admin: create/list/delete provisioners      │
                      │   • user: ProvisionMachine  ────────────────┐   │
                      │  ProvisionerStreamService (CUSTOM auth)     │   │
                      │   • ProvisionerChannel bidi stream         │   │
                      │  machine rows + provisioning jobs (DB)      │   │
                      └─────────────▲──────────────────────────────┼───┘
             HTTPS /machine/bin/*  │  (ProvisionerChannel: jobs ↓, status ↑)
        (binary download from pod) │                                  │
                      ┌─────────────┴──────────────────────────────┼────┐
                      │ provisioner (laelia-provisioner, in cluster)│    │
                      │  manager client loop ── Backend interface ──┘    │
                      │        │                                        │
                      │        ▼                                        │
                      │  k8s backend = operator (controller-runtime)    │
                      │   watches LaeliaMachine CRs:                     │
                      │   Secret + headless svc + StatefulSet + PVC      │
                      │   init container: download binary → PVC          │
                      └─────────────┬───────────────────────────────────┘
                                    │ creates/owns
                                    ▼
              ┌──────────────── Pod (runtime image, no binary) ─────────────┐
              │ init: fetch manifest+binary from manager → /data/bin        │
              │ main: setup --provisioned → supervisor (PID 1) → run        │
              │       LAELIA_HOME=/data/laelia (PVC)  ← machine.json,       │
              │                                          workspaces, logs  │
              │  connects out to manager as a normal machine ──────────────►│
              └────────────────────────────────────────────────────────────┘
```

Responsibilities:

- **Manager** is the source of truth: provisioner registry, machine rows,
  provisioning job state, machine credentials. It *never* talks to the cluster
  directly — it relays jobs to connected provisioners (same connection direction
  as machines: provisioner → manager, so nothing inbound must be opened).
- **Provisioner** is a worker deployed inside the customer's infrastructure
  (e.g. the k8s cluster). One binary, two loops: the manager client (stream,
  job queue, status reporting) and the backend (the k8s operator).
- **Pod** is a plain machine: state and binary on the PVC; outbound-only.

### End-to-end provisioning sequence

```
User          Manager                          Provisioner            k8s
 │ ProvisionMachine(provisioner,title)  │                             │
 │─────────────────────────────────────►│                             │
 │        create machine row (created_by=user, OFFLINE)               │
 │        mint machine refresh token (fingerprint F)                   │
 │        job PENDING (machine.provisioning)                           │
 │        ProvisionerChannel: ProvisionMachineJob ───►│               │
 │                                     │ job_progress(PROVISIONING)  │
 │                                     │ create Secret{machine.json}  │
 │                                     │ create LaeliaMachine CR ───►│
 │                                     │                reconcile:   │
 │                                     │                 StatefulSet │
 │        job_progress(PROVISIONED) ◄──│  init: download binary, copy │
 │        (pod Running)                │        machine.json → PVC    │
 │                                     │                 setup ─────►│
 │  ConnectMachine/heartbeat ◄─────────│── pod connects as machine ──│
 │        machine ONLINE; UI shows it ready                            │
```

## 4. Manager: provisioner registry and authentication

### 4.1 Provisioner identity

A provisioner is a first-class manager resource, `provisioners/{provisioner}`
(uuid), like a machine but for the management plane. New table (§5.1).

**Registration (admin, one time):**

1. Admin opens Settings → Provisioners → *Add provisioner*: name, backend
   (`kubernetes` | `docker` | …), optional description.
2. `CreateProvisioner` inserts the row and mints the **provisioner token** —
   returned once in the response (the store keeps only `token_version`). The UI
   shows a copy-once dialog with the same treatment as a password.
3. The admin pastes the token into the provisioner's config file:
   ```yaml
   manager_url: https://laelia.example.com
   token: llprov_xxx…        # provisioner token
   backend: kubernetes
   namespace: laelia-machines
   # Optional keys (backend/config.Config): manager_url_override (in-cluster
   # manager URL for egress-restricted clusters), retain_data (PVC retention),
   # auto_upgrade (manager-driven upgrades for this provisioner's machines),
   # resources / storage (workload sizing passthrough), extra_env (e.g.
   # LAELIA_INSECURE). The token can alternatively come from the
   # LAELIA_PROVISIONER_TOKEN env var (what the deploy manifest / Helm chart use).
   ```
4. The provisioner connects with `Authorization: Bearer <token>`; the auth
   interceptor resolves the provisioner row and injects it into the context
   (`common.ProvisionerContextKey`).

**Provisioner token** mirrors machine token mechanics, minus expiry churn:

- JWT, HS256, kid `v1`, audience `ll.provisioner.access.<mode>`
  (`ProvisionerAccessTokenAudienceFmt`), subject = provisioner resource id,
  custom claims `{token_version}`. **No expiry** (revocation is version-based,
  like bootstrap-era machine tokens); rotation is the recovery path.
- `RotateProvisionerToken` bumps `token_version`, mints a new token (shown
  once), and closes the provisioner's active stream so the old token dies on
  the next dial.
- `DeleteProvisioner` soft-deletes the row, bumps the token version, and
  closes the stream. **Refused with `FailedPrecondition` while any non-deleted
  machine still references the provisioner** (`machine.provisioner_id`), so
  workloads can never silently become unmanaged; admins delete/reassign the
  machines first.

### 4.2 Auth interceptor changes

`backend/manager/api/auth/auth.go`:

- Add `ProvisionerAccessTokenAudienceFmt = "ll.provisioner.access.%s"` and a
  `provisionerClaimsMessage` (`Subject`, `TokenVersion`).
- Extend `expected` audiences + `audienceKind` with a fourth kind;
  `authenticateProvisionerByClaims` loads the row via a new
  `GetProvisionerByResourceID` store lookup and checks deleted/version.
- `authResult.provisioner` + `ProvisionerContextKey` injection (parallel to
  `MachineContextKey`).
- Provisioner RPCs annotate `option (laelia.v1.auth_method) = CUSTOM;` exactly
  like machine-side RPCs.

### 4.3 Connection / stream service

`ProvisionerStreamService.ProvisionerChannel` — a bidi stream over the
provisioner token, deliberately mirroring `MachineChannel`
(`backend/agent/client/machine_control.go` ↔ `backend/manager/api/v1/machine_command.go`):

```protobuf
service ProvisionerStreamService {
  rpc ProvisionerChannel(stream ProvisionerStreamMessage)
      returns (stream ManagerProvisionerStreamMessage);
}

message ProvisionerStreamMessage {
  oneof message {
    ProvisionerReady ready = 1;        // first frame: version, backend, capabilities
    ProvisionJobProgress job_progress = 2;
    Ping ping = 3;                     // 30s keepalive
  }
}

message ManagerProvisionerStreamMessage {
  oneof message {
    ProvisionMachineJob provision_job = 1;
    DeprovisionMachineJob deprovision_job = 2;
    Pong pong = 3;
    ProvisionerDisconnectNotice disconnect_notice = 4;  // token rotated/deleted
  }
}
```

- On connect the provisioner sends `ProvisionerReady{version, backend,
  capabilities, config_digest, auto_upgrade, retain_data}`; the manager registers the session
  in a small provisioner dispatcher (parallel to the machine dispatcher) and
  stamps `provisioner.status` (connected, last_seen, version, auto_upgrade,
  retain_data, config_digest).
- **Job replay on (re)connect**: the manager pushes every machine row in a
  non-terminal provisioning phase (PENDING/PROVISIONING) or with pending
  deprovision, exactly like `ConnectMachineResponse.assigned_agents` resyncs the
  roster. Provision jobs are **idempotent** (see §7.1), so replays are safe.
  Because the plaintext refresh token only ever existed at provision time (the
  store keeps the hash), the manager re-mints a fresh refresh token for each
  replayed job, bound to the same deterministic fingerprint (§6.2), and stores
  it alongside the old one **without a version bump** — the pod may already hold
  the earlier token and both expire on the normal rolling schedule. Deprovision
  replays scan soft-deleted rows too: `DeleteMachine` deletes the row long
  before the provisioner may reconnect, and the teardown must survive that.
- Ping/pong every 30s; a dead stream marks the provisioner offline in status
  (machines keep running — they never depend on the provisioner after boot).

Implemented in `backend/manager/api/v1/provisioner_stream.go` +
`backend/manager/component/dispatcher` (`RegisterProvisioner`,
`SendProvisionMachineJob`, `SendDeprovisionMachineJob`,
`SendProvisionerDisconnectNotice`, …) and `backend/provisioner/client/client.go`
on the binary side (exponential backoff, no durable job state locally).

## 5. Manager: data model

### 5.1 Migration (new) — implemented

```sql
-- Provisioner registry (management-plane principal, parallel to machine)
CREATE TABLE IF NOT EXISTS provisioner (
    id serial PRIMARY KEY,
    resource_id text NOT NULL,
    name text NOT NULL,
    backend text NOT NULL,                    -- "kubernetes" | "docker" | ...
    description text NOT NULL DEFAULT '',
    token_version int NOT NULL DEFAULT 1,
    created_by int NOT NULL DEFAULT 0,
    deleted boolean NOT NULL DEFAULT FALSE,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- stored as proto/store ProvisionerStatus: connected, last_seen, version,
    -- capabilities, config digest (auto_upgrade etc.)
    status jsonb NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_provisioner_unique_resource_id ON provisioner(resource_id);

-- Machine ↔ provisioner binding. A machine created via a provisioner keeps the
-- link for its whole life so DeleteMachine can deprovision the workload.
ALTER TABLE machine ADD COLUMN IF NOT EXISTS provisioner_id int REFERENCES provisioner(id);
-- Stored as proto/store ProvisioningStatus: phase, error, workload ref,
-- phase timestamps (see §6.2)
ALTER TABLE machine ADD COLUMN IF NOT EXISTS provisioning jsonb;
CREATE INDEX IF NOT EXISTS idx_machine_provisioner ON machine(provisioner_id) WHERE provisioner_id IS NOT NULL;
-- Replay of in-flight jobs on provisioner reconnect is a provisioner_id-bounded
-- lookup (machines per provisioner are few); phases are filtered in Go, so no
-- phase-partial index is needed. (The original design filtered the jsonb phase
-- in SQL, but the store serializes jsonb columns with encoding/json, which
-- writes enum values as numbers — a string-predicate index would never match.)
```

This block is in `backend/manager/migration/migration/LATEST.sql` verbatim
(plus comments); `store/provisioner.go` + `store/machine.go` implement
`CreateProvisioner`, `GetProvisionerByResourceID`, `ListProvisioners`,
`UpdateProvisioner`, `UpdateProvisionerStatus`, `CountMachinesByProvisioner`,
`UpdateMachineProvisioning`, and `ListReplayableProvisioningMachines`
(deprovision replay scans soft-deleted rows too).

### 5.2 Store proto (`proto/store/store/provisioner.proto`)

```protobuf
message ProvisionerStatus {
  // Connected reports whether a ProvisionerChannel stream is live right now.
  bool connected = 1;
  // Last connect/ready frame epoch seconds (0 = never seen). Store protos use
  // int64 epoch seconds (see MachineStatus), not google.protobuf.Timestamp.
  int64 last_seen = 2;
  string version = 3;          // provisioner binary version
  string backend = 4;          // "kubernetes" | ...
  bool auto_upgrade = 5;       // config flag echoed from ProvisionerReady
  string config_digest = 6;    // hash of the provisioner's config, for drift hints
  bool retain_data = 7;        // config flag echoed back as keep_data on teardown jobs
}

enum ProvisioningPhase {
  PROVISIONING_PHASE_UNSPECIFIED = 0;
  PROVISIONING_PHASE_PENDING = 1;         // job recorded, not yet acked by provisioner
  PROVISIONING_PHASE_PROVISIONING = 2;    // provisioner acked, workload being created
  PROVISIONING_PHASE_PROVISIONED = 3;     // workload exists & pod Running (machine may be OFFLINE)
  PROVISIONING_PHASE_FAILED = 4;          // terminal failure; error message set
  PROVISIONING_PHASE_DEPROVISIONING = 5;  // delete accepted, teardown in progress
  PROVISIONING_PHASE_DELETED = 6;         // workload removed (terminal for the delete path)
}

message ProvisioningStatus {
  ProvisioningPhase phase = 1;
  string error = 2;
  string workload_name = 3;   // backend-specific, e.g. "namespace/name"
  map<string, string> workload_labels = 4;
  int64 pending_at = 5;       // epoch seconds (store-proto convention)
  int64 provisioned_at = 6;
  int64 failed_at = 7;
}
```

On the API side, `laelia.v1` mirrors `ProvisioningPhase`/`ProvisioningStatus`
with `google.protobuf.Timestamp` fields, living in `v1/machine.proto` (next to
`Machine`, which carries them as fields 18/19) — `v1/provisioner.proto` imports
`v1/machine.proto` for `ProvisionMachine`'s `Machine` return, so the API-level
provisioning types must live in machine.proto to avoid an import cycle.
Implemented exactly this way: see `proto/store/store/provisioner.proto` and
`proto/v1/v1/machine.proto` (`Machine.provisioning` = 18,
`Machine.provisioner` = 19, plus a `provisioning` mirror on `MachineSummary`).

## 6. Manager: API surface

### 6.1 `provisioner.proto` (new, `laelia.v1`) — implemented as `proto/v1/v1/provisioner.proto`

```protobuf
service ProvisionerService {
  // ---- admin management ----
  rpc CreateProvisioner(CreateProvisionerRequest) returns (CreateProvisionerResponse) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.create";
    option (laelia.v1.audit) = true;
  }
  rpc ListProvisioners(ListProvisionersRequest) returns (ListProvisionersResponse) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.get";
  }
  rpc GetProvisioner(GetProvisionerRequest) returns (Provisioner) {
    option (google.api.method_signature) = "name";
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.get";
  }
  rpc RotateProvisionerToken(RotateProvisionerTokenRequest) returns (RotateProvisionerTokenResponse) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.delete"; // destructive, admin-tier
    option (laelia.v1.audit) = true;
  }
  rpc DeleteProvisioner(DeleteProvisionerRequest) returns (google.protobuf.Empty) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.delete";
    option (laelia.v1.audit) = true;
  }

  // ---- user self-service ----
  // ProvisionMachine creates a machine owned by `owner` (defaults to the
  // caller; only a workspaceAdmin may name another user) and enqueues a
  // provisioning job on the named provisioner. Fully automated: the machine
  // appears and connects without any further user action.
  rpc ProvisionMachine(ProvisionMachineRequest) returns (Machine) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.permission) = "laelia.provisioners.provision";
    option (laelia.v1.audit) = true;
  }
}
```

Key messages:

```protobuf
message Provisioner {
  option (google.api.resource) = { type: "laelia/Provisioner" pattern: "provisioners/{provisioner}" };
  string name = 1;                 // provisioners/{uuid}
  string title = 2;
  string backend = 3;              // "kubernetes" | "docker" | ...
  string description = 4;
  ProvisionerStatus status = 5;   // connected/last_seen/version/auto_upgrade/retain_data
  int32 machine_count = 6;         // live machines bound to it
  google.protobuf.Timestamp created_at = 7;
  string created_by = 8;
}

message CreateProvisionerRequest {
  Provisioner provisioner = 1;    // title + backend (+description)
}
message CreateProvisionerResponse {
  Provisioner provisioner = 1;
  // The one-time provisioner token; never stored server-side in plaintext and
  // never returned again. Shown in a copy-once dialog.
  string token = 2;
}

message ProvisionMachineRequest {
  string provisioner = 1;  // provisioners/{uuid} [(resource_reference) Provisioner]
  string title = 2;        // machine title
  // Target owner, users/{id}. Empty = caller. Naming another user requires
  // workspaceAdmin (checked in the handler, like machine ownership transfer).
  string owner = 3;
}
```

### 6.2 `ProvisionMachine` handler — the headless twin of device approval

The handler reuses the same store path as `approveNewMachine`
(`backend/manager/api/v1/device.go`), minus the device session:

1. Validate: provisioner exists, not deleted, backend implemented
   (`knownProvisionerBackends` — `kubernetes` only today), and the provisioner
   is **connected** (an offline provisioner would park the job with no worker;
   the handler fails fast with `FailedPrecondition` instead of enqueueing);
   workspace `ProvisioningSetting.runtime_image` non-empty (clear error
   pointing at Settings otherwise); the workspace external URL is configured
   (it becomes the job's `manager_url`); the manager embeds machine binaries
   and holds the configured `binary_target` (`machinebuild` guards); `owner`
   is caller or caller is a workspace admin.
2. Choose the **fingerprint**: `F = sha256("provisioner:" + provisioner.resourceID
   + ":" + machineUUID)[:16]` — opaque, stable across pod restarts/reschedules,
   and independent of node architecture. The pod learns `F` via env, never
   computes it (§8.2).
3. `CreateMachineWithToken`: machine row with
   - `created_by` = owner user id,
   - `info` seeded with `{hostname: "<planned workload name>", os: "linux",
     arch: "amd64", version: "", labels: {provisioner: <name>}}` (replaced by
     real values when the machine connects, like every machine),
   - refresh token minted with `GenerateMachineTokenWithFamily(...,
   TokenTypeRefresh, fingerprint=F)`, hash stored in `machine_token` — identical
     to the device-flow mint, so revocation/rotation/re-auth semantics are shared.
4. `machine.provisioning = {phase: PENDING, pending_at: now}` and push
   `ProvisionMachineJob` on the provisioner's live stream. Because step 1
   already required a connected provisioner, a push failure is the only way a
   job stays parked at PENDING — the machine row is kept (no rollback) and the
   job is replayed on the provisioner's next connect.
5. Return the `Machine` (with `provisioning` status).

The machine resource gains one output-only field:

```protobuf
// In message Machine (and a lighter mirror on MachineSummary for list badges):
ProvisioningStatus provisioning = 18 [(google.api.field_behavior) = OUTPUT_ONLY];
// And: string provisioner = 19 — the provisioner resource name when provisioned.
```

### 6.3 Job messages (on the stream)

```protobuf
message ProvisionMachineJob {
  string machine = 1;            // machines/{uuid}
  string title = 2;
  string owner_handle = 3;        // display only (provisioner never needs it)
  string refresh_token = 4;       // the machine's durable credential (once)
  string fingerprint = 5;         // LAELIA_FINGERPRINT for the pod
  string manager_url = 6;         // pods download binaries + connect here
  string runtime_image = 7;       // from workspace ProvisioningSetting
  string binary_target = 8;        // "linux-x64" (per §8.3 / future arm64)
  map<string, string> machine_labels = 9;  // e.g. provisioner name, owner
  // The manager-rendered bootstrap script (§8.3), carried in the job so the
  // runtime image needs no laelia knowledge beyond the image contract.
  string bootstrap_script = 10;
}

message DeprovisionMachineJob {
  string machine = 1;
  bool keep_data = 2;              // PVC retention override (default false)
}

message ProvisionJobProgress {
  string machine = 1;
  ProvisioningPhase phase = 2;     // PROVISIONING | PROVISIONED | FAILED | DEPROVISIONING | DELETED
  string error = 3;
  string workload_name = 4;        // e.g. "laelia-machines/laelia-machine-abc123"
}
```

Manager-side state transitions are driven purely by `ProvisionJobProgress` frames;
the manager never guesses cluster state. A `FAILED` phase stores the error on the
row for the UI; retry is delete + re-create in the MVP.

### 6.4 `DeleteMachine` integration

`backend/manager/api/v1/machine.go` `DeleteMachine`: when `provisioner_id` is
set, it first records `provisioning.phase = DEPROVISIONING` (before the
soft-delete, so the reconnect replay still finds the row), then soft-deletes,
then pushes a `DeprovisionMachineJob` best-effort (`keep_data` = the
provisioner's configured retention, `provisioner.status.retain_data`, default
false → PVC deleted with the StatefulSet). The row deletion is **not** blocked
on teardown: the machine disappears from the UI immediately; workload cleanup
happens async and is replayed if the provisioner was offline. (Known deviation:
the reconnect replay of a deprovision job currently sends `keep_data=false`
unconditionally — `provisioner_stream.go` `replayProvisionJobs`.) If the
provisioner is gone forever (deleted last), the workload is orphaned in the
cluster — visible to the cluster admin, documented in the UI warning (§10.3).

`ForceDisconnectMachine` / `RevokeMachineToken` need **no changes**: a
provisioned machine reacts like any machine (connection dies; the pod's
permanent-auth-failure path is §8.4).

### 6.5 Permissions & settings

**Permission catalog additions** (`backend/common/permission/permission.json`):

| Permission | Grants |
|---|---|
| `laelia.provisioners.get` | list/get provisioners (needed to pick one in the create UI) |
| `laelia.provisioners.create` | create provisioner records |
| `laelia.provisioners.delete` | delete/rotate provisioners |
| `laelia.provisioners.provision` | call `ProvisionMachine` |

`workspaceAdmin` picks all of them up automatically via `allPermissionSet`.
The workspaceMember baseline now includes `laelia.provisioners.get` (so a
member can resolve provisioner names), but **nothing else** — members cannot
provision by default. Three grants create provisioning rights
(`backend/manager/api/v1/provisioner.go` `canProvisionOnProvisioner`):

- workspace-scope: `workspaceAdmin` (via `allPermissionSet`) or a user bound to
  the predefined role **machineProvisioner** = `{provisioners.get,
  provisioners.provision}` (shown on the Roles page, `workspaceAdmin` binds it
  through the existing IAM policy machinery); or
- provisioner-scope: a principal bound to the marker role
  **provisionerMachineCreator** in the *provisioner's own IAM policy*
  (resolved by `component/iam.provisionerRolePermissions`; like
  `machineAgentCreator` it never appears on the management Roles page). This
  is how an admin delegates "create machines on this one provisioner" without
  any workspace-wide rights.

Provisioner visibility follows the same rule: `ListProvisioners` filters to
provisioners the caller may provision on, and `GetProvisioner` returns
`NotFound` for invisible ones (existence is not leaked).

**Workspace setting** — new `ProvisioningSetting` message in
`proto/store/store/setting.proto`, wired into the `SettingValue` oneof in
`proto/v1/v1/setting.proto`:

```protobuf
// ProvisioningSetting configures machine provisioning via provisioners.
message ProvisioningSetting {
  // Container image that provides the agent runtime environment for
  // provisioned machine pods. It must NOT contain the laelia-machine binary —
  // the binary is downloaded at pod start from this manager into the machine's
  // PVC. Required before ProvisionMachine will succeed.
  string runtime_image = 1;
  // Machine binary target to install into provisioned pods. Default
  // "linux-x64". (When linux-arm64 embedding lands, this becomes per-provisioner.)
  string binary_target = 2;
}
```

Admin-managed through the existing `GetSetting`/`UpdateSetting`
(`laelia.settings.update`); surfaced on Settings → General (§10.2).

### 6.6 Optional auto-upgrade

`ProvisionerReady.auto_upgrade` (from the provisioner config, default off) is
persisted in `provisioner.status`. A small manager loop
(`provisioner_auto_upgrade.go`; first scan 2 min after boot, then a 5-min
ticker, connected provisioners only):

for each machine with `provisioner_id` set whose provisioner has
`auto_upgrade=true`, that is ONLINE, whose reported version ≠
`machinebuild.LatestVersion()` and which has no in-flight `upgrade_status` →
push the same `UpgradeRequest` the manual `UpgradeMachine` RPC sends (target
fixed to the embedded `linux-x64` build). The existing progress reporting and
crash-safety (supervisor swap + exec in place) apply unchanged. Manual upgrade
stays available regardless.

## 7. Provisioner binary

Implemented as the top-level package `backend/provisioner`:

```
backend/provisioner/
  bin/provisioner/main.go     # entry: flags --manager --token --backend --config
  cmd/                        # flag parsing, config load (yaml), config digest
  version/                    # binary version
  client/                     # manager client: ProvisionerChannel loop, job ack/queue,
                              #   status reporting, reconnect/backoff (mirrors agent/client)
  backend/                    # Backend abstraction
    backend.go                # interface + Factory registry + ErrUnsupportedBackend
    mock/                     # in-memory Backend for tests
    kubernetes/               # the operator (only implemented backend)
      api/v1/                 # CRD types (LaeliaMachine) + generated deepcopy
      internal/controller/    # reconciler + builders
      backend.go              # Backend impl: jobs → CRs; CR status → events
      deploy/                 # CRD + RBAC + Deployment manifests
    docker/                   # registry stub returning ErrUnsupportedBackend
```

### 7.1 `Backend` interface — the extension point for other virtualization stacks

Implemented in `backend/provisioner/backend/backend.go` (below matches the
code, plus two post-design additions: `MachineSpec.BootstrapScript` and the
`Shutdown` method):

```go
// MachineSpec is the backend-neutral description of one machine workload,
// derived from a ProvisionMachineJob. The manager and the client layer never
// learn backend specifics beyond the status events.
type MachineSpec struct {
    MachineID       string            // manager resource id (stable workload identity)
    Title           string
    ManagerURL      string
    Fingerprint     string
    RuntimeImage    string
    BinaryTarget    string
    BootstrapScript string            // manager-rendered init-container script (§8.3)
    Labels          map[string]string
}

// Event is a workload status change reported back to the manager.
type Event struct {
    MachineID    string
    Phase        storepb.ProvisioningPhase // PROVISIONING / PROVISIONED / FAILED / DEPROVISIONING / DELETED
    Error        string
    WorkloadName string                    // human-readable locator, e.g. "namespace/name"
}

// Backend provisions machine workloads in one virtualization stack. All
// methods must be idempotent: jobs are replayed on reconnect.
type Backend interface {
    // Name matches the provisioner row's backend field.
    Name() string
    // Start launches the backend's controllers/watchers and emits Events on ch
    // until ctx is done.
    Start(ctx context.Context, ch chan<- Event) error
    // Provision creates or updates the workload for spec (idempotent upsert).
    // Credential handoff is backend-owned: the backend stores the machine's
    // bootstrap credential wherever the workload can read it (k8s Secret).
    Provision(ctx context.Context, spec MachineSpec, refreshToken string) error
    // Deprovision removes the workload; keepData preserves machine data
    // volumes when the backend supports retention.
    Deprovision(ctx context.Context, machineID string, keepData bool) error
    // Shutdown runs when the manager permanently deletes this provisioner
    // (not on a token rotate): e.g. the kubernetes operator scales its own
    // Deployment to 0 so the process stops crash-looping on a dead credential.
    Shutdown(ctx context.Context) error
}
```

Backends register through a `Factory` registry (`backend.Register(name,
factory)`; `backend.New(name, cfg)` builds one from the backend-neutral
`Config{Namespace, RetainData, Resources, Storage, ExtraEnv}`). A `mock`
backend exists for hermetic tests. The client layer is the only translator:
stream frames → `Backend.Provision` / `Deprovision`; `Backend` events →
`ProvisionJobProgress` frames. Adding docker later means implementing one
interface + a deployment/docker-compose runtime template; **no manager
change** (a new backend type string flows through the provisioner row, job,
and UI select).

### 7.2 Client loop (mirrors the machine app's `Run`)

connect (Bearer provisioner token) → send `ProvisionerReady` → receive pump:
`ProvisionMachineJob` → `Backend.Provision` (ack by first event), 
`DeprovisionMachineJob` → `Backend.Deprovision` → ping ticker → on stream death:
exponential backoff and reconnect (jobs resync from the manager on reconnect —
the provisioner keeps no durable job state of its own). One config knob worth
calling out: `manager_url_override` — when set, job `manager_url` is replaced
with it, so pods can reach the manager through an in-cluster service URL
instead of the public one (egress-restricted clusters). The override is
reported in `ProvisionerReady.config_digest` for visibility.

## 8. Kubernetes backend (operator)

### 8.1 CRD

Group `laelia.sh/v1`, kind **`LaeliaMachine`**, namespace-scoped (default: the
provisioner's own namespace). One CR per provisioned machine; the CR is the
desired state, the operator is the reconciler, and `kubectl get laeliamachines`
is the enterprise operator's window into fleet state.

```yaml
apiVersion: laelia.sh/v1
kind: LaeliaMachine
metadata:
  name: laelia-machine-<machine-uuid-prefix>      # owner of all children
  namespace: laelia-machines
spec:
  machineId: "<manager machine uuid>"             # immutable
  title: "ran's cloud machine"
  managerUrl: https://laelia.example.com
  fingerprint: "1f0a9c…"                          # → pod env LAELIA_FINGERPRINT
  runtimeImage: "laelia/machine-runtime:1.2.3"
  binaryTarget: linux-x64
  bootstrapSecret: laelia-machine-<uuid-prefix>   # Secret holding machine.json
  resources: { requests: {cpu: "1", memory: 2Gi}, limits: {memory: 4Gi} }   # from provisioner config
  storage: { size: 10Gi, storageClass: "" }       # from provisioner config
  retainData: false                               # from provisioner config (PVC retention)
  extraEnv: [ …passthrough from provisioner config… ]
status:
  phase: Ready            # Pending | Creating | Ready | Failed | Deleting
  podName: laelia-machine-<p>-0
  conditions: [ … ]
  error: ""
  observedGeneration: 3
```

The refresh token **never appears in the CR** (CRs are broadly readable); it
lives only in the Secret (§8.2).

### 8.2 What the operator creates

For each `LaeliaMachine` CR (all children carry `ownerReferences` to the CR):

1. **Headless Service** `laelia-machine-<p>` (required by StatefulSet identity).
2. **StatefulSet** (replicas: 1, `serviceName` as above) with:
   - `volumeClaimTemplates: [data]` — the machine's persistent volume;
   - `persistentVolumeClaimRetentionPolicy: { whenDeleted: Delete | Retain }` —
     from the provisioner config `retain_data` (default `Delete`). The
     StatefulSet auto-delete-PVC behavior is beta (on by default) since k8s
     1.27 and GA in 1.32 — the deploy doc pins k8s ≥ 1.27, and the operator
     also explicitly garbage-collects the PVC on CR delete as a fallback.
   - **nodeSelector `kubernetes.io/arch: amd64`** (MVP; §9 follow-ups);
   - **init container** (runtime image, mounts PVC at `/data`): runs the
     manager-generated bootstrap script (§8.3): download + verify the machine
     binary into `/data/bin/laelia-machine` (skipped when the file already
     exists — restarts stay fast; forced re-download by setting
     `LAELIA_FORCE_REDOWNLOAD=true` via the provisioner's `extra_env`), then
     seed `/data/laelia/machine.json` from the mounted bootstrap Secret **only
     if absent** (first boot). The Secret is mounted read-only at
     `/bootstrap`; the PVC copy is authoritative afterwards (rolling refresh
     renewals persist there).
   - **main container** (runtime image): runs the image's
     `machine-runtime-entrypoint` (env→flag mapping like today's
     `machine-entrypoint.sh`, but execs the binary at
     `LAELIA_MACHINE_BIN`, default `/data/bin/laelia-machine`) with env:
     - `LAELIA_HOME=/data/laelia` — **the whole `.laelia` world is on the PVC**:
       machine.json, logs, daemon socket, and every agent workspace
       (`<LAELIA_HOME>/<machineID>/…`) survive pod restarts/reschedules.
     - `LAELIA_FINGERPRINT=<spec.fingerprint>` — replaces hostname-derived
       fingerprinting (small machine change, §8.4).
     - `LAELIA_MANAGER_URL=<spec.managerUrl>` (+ automatic `--allow-http`
       mapping in the entrypoint, as today).
     - `LAELIA_PROVISIONED=true` — entrypoint appends `--provisioned` and
       passes `--no-browser --foreground` (§8.4).
     - `CODEX_HOME=/data/laelia/codex` — default onto the PVC so codex
       login/config survives restarts (admins can override via `extraEnv`).
3. Reconcile loop: create/patch the Service+StatefulSet; watch the pod; phase
   `Pending → Creating → Ready` (pod Running) or `Failed` (image pull backoff,
   crashloop, init failure — message from the pod's status); on CR delete, let
   ownerRefs + PVC retention clean up.

### 8.3 Init-container bootstrap script

Generated by the manager from
`backend/manager/component/provision/provision_bootstrap.sh.tmpl` (rendered by
`RenderBootstrapScript` in the `provision` component, so the API layer uses it
without an import cycle) and carried inside the job — the runtime image only
needs POSIX `sh`, `curl`, `gzip`, `sha256sum` (the *image contract*, documented
on the Settings page next to the runtime-image field). The shipped template:

- prefers `LAELIA_MANAGER_URL` (set by the provisioner, which may override the
  manager's public URL with an in-cluster service URL) and falls back to the
  manager-rendered URL;
- downloads `manifest.json` + the target's `.gz` from `/machine/…`, verifies
  the archive sha256, gunzips, verifies the decompressed sha256, then installs
  to `/data/bin/laelia-machine` (skip + `LAELIA_FORCE_REDOWNLOAD=true`
  override, exactly as designed);
- seeds `/data/laelia/machine.json` from the read-only `/bootstrap` mount on
  first boot only, and fails loudly when neither the state file nor the secret
  is present;
- deliberately avoids the `<<<` heredoc (a bashism) — checksums are verified
  with `printf '%s  %s\n' … | sha256sum -c -` — and checks `curl`/`gunzip`/
  `sha256sum` availability before starting.

`machine.json` content is composed by the provisioner from the job — exactly
`state.State`: `{"manager_url", "machine_id", "refresh_token", "hostname":
"<workload name>", "created_at"}`. Because the state file is present and valid,
the pod's `setup` skips the device flow entirely — this is what makes
authentication "automatic".

### 8.4 Machine-side changes (small, deliberate)

1. **Fingerprint override**: `client.ComputeFingerprint` gains an env check —
   `LAELIA_FINGERPRINT` set → returned verbatim. Used by both `setup`'s probe
   and `client.Connect`. Self-hosted machines are unaffected (env unset).
2. **`setup --provisioned` flag**: when the saved credential is dead, fail fast
   with a clear log line instead of starting the device-code login (nobody
   watches pod logs to click an approval URL). Self-hosted `setup` keeps the
   interactive fallback.
3. **Runtime image** `scripts/docker/Dockerfile.machine-runtime`: the machine
   image's final stage *minus* the Go build and binary — node:slim base, apt
   packages (curl/git/jq/python/build-essential/ripgrep/…), codex CLI, and
   `machine-runtime-entrypoint.sh` (env→flag mapping like the current
   `machine-entrypoint.sh`, but execs `$LAELIA_MACHINE_BIN`, default
   `/data/bin/laelia-machine`). We publish this reference image; enterprises
   may run any image satisfying the contract (§8.3) — typically
   `FROM laelia/machine-runtime`.

Everything else already fits containers: the supervisor's foreground mode execs
the upgraded supervisor in place (PID 1 preserved), downloads land next to the
binary on the PVC, and `machine.json` rotations are PVC writes.

## 9. What other backends will need (design hooks, not MVP)

- The job (`ProvisionMachineJob`) is already backend-neutral except
  `binary_target`; docker/VM backends consume the same fields. A backend that
  cannot mount a "PVC" implements its own durable-state story (docker named
  volume, VM disk) behind the same `Provision/Deprovision/Event` trio.
- `provisioner.backend` strings are free-form; the manager's UI select lists
  known backends (`kubernetes` implemented; others shown disabled with
  "backend not yet available" only if a provisioner of that type registered).
- `binary_target` grows values when `scripts/build-embedded-machines.sh` adds
  `linux-arm64` (tracked follow-up with the k8s arch selector flip).

## 10. Frontend

1. **`/machines/new`** — a "Provisioned" tab next to the classic
   "Self-hosted" tab (`frontend/src/pages/dashboard/machine-new.tsx`,
   `machine-new-provisioned.tsx`). The tab is visible when the caller may
   provision: `laelia.provisioners.provision` permission **or** a non-empty
   provisioner roster (`ListProvisioners` already filters to provisioners the
   caller may provision on, which covers provisioner-scope
   `provisionerMachineCreator` bindings that carry no workspace permission).
   Provisioner picker (radio cards: title, backend, connected badge), title
   field, [Create] → `ProvisionMachine` → navigate to the machine profile.
   The profile polls until `status.state == ONLINE` (and shows the
   provisioning phase chip meanwhile).
2. **Settings → Provisioners** (admin): table of provisioners (title, backend,
   connected/last-seen, version, auto_upgrade, machine count) + *Add
   provisioner* dialog ending in the **copy-once token dialog**; Rotate /
   Delete actions (Delete shows the "N machines still bound" refusal inline
   and lands on a full-page post-delete cleanup guide,
   `settings-provisioner-cleanup.tsx`, with the manual kubectl teardown steps).
   Two extra pages ship beyond the original design:
   `settings-provisioner-access.tsx` (per-provisioner IAM policy: who may
   provision on this provisioner, via the `provisionerMachineCreator` role)
   and the cleanup page above. Plus the runtime-image field (Settings →
   General, next to the other workspace settings, with the image-contract
   help text).
3. **Machine profile** (provisioned machines): a "Provisioning" card —
   provisioner title, backend, `workload_name`, phase timeline, last error if
   any — and the delete confirmation explicitly says the machine data volume is
   deleted (or retained, per the provisioner's policy).

## 11. Failure modes & edge cases

| Scenario | Behavior |
|---|---|
| Provisioner offline when user clicks create | `ProvisionMachine` fails fast with `FailedPrecondition` ("connect it before provisioning a machine") — no job is enqueued with no worker to run it. Jobs can still park at `PENDING` when the post-create push loses a race; those are replayed on the next connect. |
| Manager restarts mid-provision | Job state is in the DB row; provisioner reconnects → replay (idempotent upsert). |
| Provisioner dies mid-provision | Workload may exist without the machine connecting; machine shows `PROVISIONED` but OFFLINE; pod keeps retrying via StatefulSet. |
| Job replayed while pod already exists | `Backend.Provision` is an upsert of CR/Secret/StatefulSet — no-op if in sync. |
| Pod crash-loops (bad image, download failure) | CR phase `Failed` with pod status message → `provisioning.phase = FAILED` + error on the machine row (UI). User deletes and recreates. |
| Machine refresh token revoked (RevokeMachineToken) | The pod's `run` worker hits permanent auth failure and exits; the supervisor restarts it with backoff (retry loop), and a *fresh* pod start fails fast at `setup --provisioned`. Recovery = delete + re-provision. |
| User deleted in workspace | Machine ownership follows existing machine semantics (created_by; workspaceAdmin can manage). |
| DeleteMachine while provisioner offline | Row soft-deleted immediately; deprovision job replayed on reconnect; if provisioner never returns, cluster admin removes orphaned StatefulSet/PVC manually (UI warns). |
| Manager has no embedded binaries (dev build) | `ProvisionMachine` fails fast with "this manager embeds no machine binaries" (same guard as manual upgrades). |
| `https` manager with self-signed cert | `LAELIA_INSECURE` passthrough via provisioner config `extra_env`, same as today's machine image. |
| Two provisioners with the same backend | Fine — names differ; machines bind to exactly one provisioner id. |
| Cluster has arm64 nodes | MVP nodeSelector pins amd64; pods never land on arm64 nodes. |

## 12. Security & threat model

- **Provisioner token**: long-lived, version-revocable, admin-minted. It grants
  *exactly* the provisioner plane (stream + its RPCs) — it cannot call user
  RPCs (different audience; interceptor rejects). Rotation invalidates at next
  request and kills the live stream. On rotation/deletion the manager sends a
  `ProvisionerDisconnectNotice` (`deleted=true` when permanently deleted); the
  provisioner's `Backend.Shutdown` then scales its own Deployment to 0 so the
  process stops crash-looping with a dead credential (the Deployment/CRD/RBAC/
  namespace remain for the user to clean up manually — the UI shows the kubectl
  steps, `settings-provisioner-cleanup.tsx`).
- **Machine refresh token transits the provisioner** (decision #2). The
  provisioner is deployed by the same enterprise that operates the cluster and
  already holds cluster-admin-equivalent power over machine pods (it could read
  the PVC or exec into the pod regardless). The token is stored in a Secret
  (not the CR, which is more broadly readable); RBAC on Secrets is the
  isolation boundary. The alternative (one-time bootstrap exchange so the
  provisioner never sees a durable credential) is documented as a follow-up.
- **Unauthenticated binary downloads** are the existing `/machine/bin/*`
  endpoints; checksums (gz + decompressed) are verified against the manifest in
  the init script and by the supervisor on upgrades.
- **Pod security**: runtime image runs as non-root uid 1001 (as today); pods run
  in the provisioner's namespace; `extraEnv`/`resources` passthrough is
  admin-configured. Agents execute arbitrary code by design — enterprises are
  expected to enforce namespace-level (Pod)Security policies; the deploy
  manifest documents the minimum RBAC (CRD + CR + sts/svc/pvc CRUD in the
  namespace, no cluster-admin).
- **Fingerprint binding** prevents token replay from a different identity: the
  provisioned fingerprint is opaque and only meaningful inside the pod it was
  injected into.

## 13. Implementation plan

The work was originally grouped into six execution phases with dependency
boundaries, exit criteria, and milestone demos (that separate plan document was
retired once all phases landed). **All phases are implemented** (see the status
note under §0); the map from phase to code:

1. **Proto + store + auth** — `proto/v1/v1/provisioner.proto`, stream messages,
   `ProvisioningStatus`/`ProvisionerStatus` store protos, migration (§5.1),
   interceptor audience branch + context key, permission catalog + predefined
   `machineProvisioner` role, `ProvisioningSetting`.
2. **Manager services** — `ProvisionerService` CRUD (+ token mint/rotate/
   delete-with-guard), provisioner dispatcher, `ProvisionerChannel` handler with
   replay, `ProvisionMachine` (reuse `CreateMachineWithToken`), `DeleteMachine`
   deprovision hook, auto-upgrade loop.
3. **Machine-side** — `LAELIA_FINGERPRINT`, `setup --provisioned`, runtime
   image + entrypoint + bootstrap script template.
4. **Provisioner skeleton** — client loop (connect/replay/ack/report),
   `Backend` interface + registry, docker stub, config.
5. **k8s backend** — CRD types (kubebuilder/controller-runtime), reconciler
   (Secret/svc/STS/render pod), status events, deploy manifests (CRD + RBAC +
   Deployment), operator/controller tests.
6. **Frontend** — machine-new provision tab, machine profile provisioning card,
   Settings → Provisioners (+ runtime image field), permission-gated visibility.
7. **Docs** — `docs/deploy.md` §3b + §8: provisioner install (manifests and the
   `charts/provisioner` Helm chart), runtime image contract, k8s version
   requirement (≥ 1.27 for PVC auto-delete), RBAC.

Testing: hermetic unit tests for the job state machine, bootstrap renderer, and
backend builders; the env-gated control-plane integration suite
(`LAELIA_RUN_PROVISIONER_TESTS=1` + `LAELIA_TEST_PG_URL`,
`backend/manager/api/v1/provisioner_integration_test.go`); e2e on kind via
`scripts/test-server.sh` + a locally built provisioner (verify: create →
provision → machine ONLINE → upgrade → delete → PVC gone).

## 14. Open follow-ups (explicitly out of MVP)

- One-time bootstrap exchange RPC (provisioner never touches the machine
  credential).
- `linux-arm64` embedded target + per-provisioner arch.
- Per-machine spec overrides (resources, storage size) in the create dialog.
- `RetryProvisionMachine` (re-run a FAILED job without delete/recreate).
- Re-credentialing a provisioned machine in place.
- Namespace-per-machine / k8s `SecurityContext` hardening profiles.
- Multi-replica provisioner HA (leader election inside the operator comes free
  with controller-runtime; the manager-client loop would need a lock).