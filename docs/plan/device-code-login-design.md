# Device-Code Login & Machine Provisioning — Design

> Status: verified and updated against the current code on 2026-09-06. Main changes: the design is fully implemented (backend DeviceService, CLI `setup`/state, `/login/device` + `/machines/new` pages, docker auto-setup); corrections cover the shipped deltas — `verification_uri` became the relative `verification_path`, a shared `DeviceLoginStatus` enum, `UpdateMachine` is title-only, the planned per-IP rate limiter was not built (the 2s poll minimum is), and new-machine approval additionally requires the `laelia.machines.create` IAM check.

## Goals

Replace the bootstrap-token machine registration with an OAuth2-style **device
code flow**:

1. `laelia-machine --manager <url> setup` prints a URL
   (`https://<manager>/login/device?user_code=XXXX-XXXX`); the user opens it in
   a browser, signs in (or is already signed in), and approves. The machine is
   then authenticated and ready.
2. Re-running `setup` after a reboot/process exit reads the locally persisted
   refresh token, validates it against the manager, and reports "already logged
   in" (then proceeds to run).
3. Machine creation moves into the device flow: no local machine id → the
   manager auto-creates a machine named after the hostname at approval time.
   A local machine id → the flow re-authenticates that existing machine (no
   duplicate).
4. The frontend "create machine" button no longer creates a machine directly:
   it opens a page that shows the `setup` command, waits for a new machine to
   appear, and lets the user confirm/rename it.
5. One `laelia-machine` process per computer: starting a second instance is
   detected and reported ("already running").

No backward compatibility is required (project is pre-launch).

## Current flow (what we are replacing)

> **Implemented & removed (2026-09-06)**: the bootstrap-token flow described
> here is gone. `CreateMachine`/`RotateMachineToken` RPCs, the frontend
> create-token dialog, `credential.Manager`, the `machine-token-<id>` files,
> and the docker `LAELIA_TOKEN` mapping no longer exist; `ConnectMachine`
> authenticates only via the machine access token minted by
> `RefreshMachineToken` (`backend/manager/api/v1/machine_connection.go`). This
> section is kept as historical context for the design.

- `CreateMachine` (frontend) mints a single-use **bootstrap (registration)
  token**; the UI shows `laelia-machine run --manager <url> --token <token>`.
- `laelia-machine run` parses the machine id out of the bootstrap JWT
  (`parseResourceIDFromBootstrapToken`), stores the refresh token at
  `~/.laelia/machine-token-<machineID>`, and connects:
  - first connect: `ConnectMachine(registration_token)` → mints access+refresh;
  - later connects: `RefreshMachineToken(refresh_token)` → access token →
    `ConnectMachine(access_token)`.
- The daemon socket lives at `~/.laelia/<machineID>/daemon.sock`; a live-socket
  probe (`ensureStaleSocket`) already exists to detect a running daemon for a
  given machine id.
- The frontend create-machine dialog collects a name, calls `CreateMachine`,
  and shows the run command with the token.

## New flow overview

```
┌─ device (CLI) ──────────────┐      ┌─ manager ────────────────┐      ┌─ browser ─────────────┐
│ laelia-machine setup        │      │                          │      │ /login/device?        │
│  1. already-running check   │      │                          │      │   user_code=XXXX-XXXX │
│  2. load ~/.laelia/machine  │      │                          │      │  show hostname+code   │
│     .json (state)           │      │                          │      │  sign in (if needed)  │
│  3. state+token valid? ─────┼─────▶│ RefreshMachineToken       │      │  [Approve]            │
│     yes → "already logged   │      │                          │      │        │              │
│     in" → run               │      │                          │      │        ▼              │
│  4. no state / token dead:  │      │                          │      │ ApproveDeviceLogin    │
│     StartDeviceLogin ───────┼─────▶│ create DeviceSession     │◀─────┼───────┘              │
│     print URL + user_code   │      │ (pending, TTL 10 min)    │      │                      │
│  5. poll PollDeviceLogin ◀──┼─────▶│ on approve:             │      │                      │
│     (every 5s)              │      │  • no machine_id →       │      │                      │
│  6. APPROVED → save state   │      │    create machine        │      │                      │
│     (machine_id+refresh)    │      │    (title=hostname)      │      │                      │
│  7. run (foreground)        │      │  • machine_id → re-auth  │      │                      │
└─────────────────────────────┘      │    existing machine      │      └──────────────────────┘
                                     │  • mint refresh token    │
                                     │  • mark session APPROVED │
                                     └──────────────────────────┘
```

## Backend design

> **Implemented (2026-09-06)**: `proto/v1/v1/device.proto` exists with all four
> RPCs below. Shipped deltas vs this sketch: a shared top-level enum
> `DeviceLoginStatus` (`DEVICE_LOGIN_STATUS_*`) replaces the per-message
> nested enums; `verification_uri` became `verification_path` (relative path —
> the CLI composes the full URL from its `--manager` value);
> `PollDeviceLoginResponse` carries `denial_reason` (see design deltas).

### New proto: `DeviceService` (`proto/v1/v1/device.proto`)

```proto
service DeviceService {
  // No credential required (the CLI has none yet). Rate-limited per IP.
  rpc StartDeviceLogin(StartDeviceLoginRequest) returns (StartDeviceLoginResponse) {
    option (laelia.v1.allow_without_credential) = true;
  }
  // No credential required; device_code is the bearer secret. Rate-limited.
  rpc PollDeviceLogin(PollDeviceLoginRequest) returns (PollDeviceLoginResponse) {
    option (laelia.v1.allow_without_credential) = true;
  }
  // No credential required; used by the approval page to render device info.
  rpc GetDeviceLoginStatus(GetDeviceLoginStatusRequest) returns (GetDeviceLoginStatusResponse) {
    option (laelia.v1.allow_without_credential) = true;
  }
  // Any logged-in user may approve. Audited.
  rpc ApproveDeviceLogin(ApproveDeviceLoginRequest) returns (ApproveDeviceLoginResponse) {
    option (laelia.v1.auth_method) = IAM;
    option (laelia.v1.audit) = true;
  }
}

message StartDeviceLoginRequest {
  string hostname = 1;   // machine name at creation
  string os = 2;
  string arch = 3;
  string ip = 4;
  string version = 5;
  string fingerprint = 6;   // hostname:os:arch hash (existing computeFingerprint)
  string machine_id = 7;    // existing machine resource id when re-authenticating
}

message StartDeviceLoginResponse {
  string device_code = 1;        // high-entropy secret, never displayed
  string user_code = 2;          // 8 chars, XXXX-XXXX, displayed on device + page
  string verification_path = 3;  // RELATIVE: "/login/device?user_code=XXXX-XXXX";
                                 // the CLI composes the full URL from --manager
  int32 expires_in = 4;          // 600
  int32 interval = 5;           // 5
}

message PollDeviceLoginRequest { string device_code = 1; }

message PollDeviceLoginResponse {
  enum Status { STATUS_UNSPECIFIED = 0; PENDING = 1; APPROVED = 2; EXPIRED = 3; DENIED = 4; }
  Status status = 1;
  string machine_id = 2;        // on APPROVED
  string machine_title = 3;     // on APPROVED
  string refresh_token = 4;     // on APPROVED, single delivery
  string denial_reason = 5;     // on DENIED (implemented per design deltas)
}

message GetDeviceLoginStatusRequest { string user_code = 1; }

message GetDeviceLoginStatusResponse {
  enum Status { STATUS_UNSPECIFIED = 0; PENDING = 1; APPROVED = 2; EXPIRED = 3; DENIED = 4; }
  Status status = 1;
  string user_code = 2;
  string hostname = 3;
  string os = 4;
  string arch = 5;
  bool reauth_existing = 6;     // true when the CLI supplied an existing machine_id
  string machine_title = 7;     // existing machine title when reauth_existing
  string denial_reason = 8;     // shipped beyond the sketch: set on DENIED
  string ip = 9;                // shipped beyond the sketch: device IP for verification
  string machine_owner = 10;    // shipped beyond the sketch: owner handle on reauth
}

message ApproveDeviceLoginRequest { string user_code = 1; }
message ApproveDeviceLoginResponse {}
```

### Device session store (in-memory) — implemented

Consistent with the existing single-instance in-memory components (dispatcher,
roomhub, `state.State`). Shipped as `backend/manager/component/device/device.go`:

```go
type Session struct {
    DeviceCode string
    UserCode   string
    Status     Status            // StatusPending / StatusApproved / StatusExpired / StatusDenied
    MachineID  string            // existing machine to re-auth, or ""
    Hostname, OS, Arch, IP, Version string
    Fingerprint string
    CreatedAt   time.Time
    ExpiresAt   time.Time        // CreatedAt + SessionTTL (10 min)
    // LastPolledAt backs the server-side minimum poll interval.
    LastPolledAt time.Time
    ApprovedAt  time.Time
    ApprovedBy  int              // user id
    Result      *Result          // set on approval
    DenialReason string          // set on Deny (policy failure)
}
type Result struct {
    MachineID    string
    MachineTitle string
    RefreshToken string
}
```

- Maps keyed by `device_code` and `user_code`; mutex-guarded; lazy expiry sweep
  on access (plus `StartSweeper`, a background ticker that purges expired
  sessions and post-grace-window approvals).
- **Post-approval grace window**: shipped as `GraceWindow = 10 * time.Minute`
  (equal to `SessionTTL`): an APPROVED session keeps returning `Result` on poll
  so a CLI that crashed between approval and saving state can recover by
  re-polling; after the window it is purged.
- Manager restart mid-flow loses pending sessions → CLI poll returns
  EXPIRED/not-found → user re-runs `setup`. Acceptable (short-lived flow,
  single-instance manager).

### Approval handler (`ApproveDeviceLogin`) — implemented

Shipped in `backend/manager/api/v1/device.go`:

1. Look up session by `user_code`; must be PENDING and unexpired, else
   `FailedPrecondition` (missing → `NotFound`).
2. Resolve the approving user from context (`Unauthenticated` when absent).
3. If `session.MachineID != ""` (re-auth of an existing machine):
   - Load machine; if it exists and is not deleted:
     - **Policy (implemented)**: allow only the machine's creator or a
       workspace admin (`isMachineAdmin`) to approve re-auth. Anyone else →
       the session is marked **DENIED** with a reason carrying the owner's
       handle and machine resource name, and the RPC returns
       `PermissionDenied` with that same reason (per the 2026-08-13 decision).
     - Bump `token_version`, revoke all machine tokens, mint a **new refresh
       token** bound to the session fingerprint via
       `auth.GenerateMachineTokenWithFamily` (the `RotateMachineTokens` store
       transaction named here originally was removed together with the
       `RotateMachineToken` RPC; approval mints the refresh token directly).
   - If the machine is missing/deleted → fall through to creation (the CLI
     will receive the new machine id and update its state).
4. Else (new machine) — **additional shipped gate**: `requireCanCreateNewMachine`
   first checks the approving user against the `laelia.machines.create` IAM
   permission (a workspace may disable user-created machines →
   `PermissionDenied` "machine creation is disabled for ordinary users").
   Then: create the machine row with `title = hostname`, `info` from the
   session, `created_by = approving user`. Mint a refresh token (family =
   machine resource id, fingerprint from session).
5. Mark session APPROVED, store `Result`, audit.

### Poll handler (`PollDeviceLogin`) — implemented

- PENDING → `PENDING`; the server-side minimum poll interval is
  `deviceMinPollInterval = 2s` (`TouchPoll`) — polls that arrive too fast get
  `ResourceExhausted`, not a silent pass.
- APPROVED → return `Result` (idempotent within the 10-min grace window).
- DENIED → return the status plus `denial_reason`.
- Unknown device_code → `EXPIRED` (do not leak whether a code ever existed).

### Start handler (`StartDeviceLogin`) — implemented with one gap

- `device_code`: 32 random bytes, base64url (`generateDeviceCode`).
  `user_code`: 8 chars from the unambiguous alphabet
  (`ABCDEFGHJKLMNPQRSTUVWXYZ23456789`), formatted `XXXX-XXXX`.
- `verification_path`: shipped as the RELATIVE path
  `/login/device?user_code=<code>`; the CLI composes the full URL from its
  configured manager URL (no `external_url` fallback involved).
- **Not implemented**: the planned per-source-IP rate limit on
  `StartDeviceLogin`. The only throttling shipped is the 2s poll minimum above
  (plus Connect-level auth handling); see the design-deltas note below.

### MachineService changes — implemented

- **Removed** `CreateMachine` RPC and the bootstrap-token machinery on the
  machine path (`registration_token` in `ConnectMachineRequest`,
  `authenticateMachineRegistrationToken`, bootstrap-token minting/consumption
  on the connect path). `TokenTypeBootstrap` survives only on the unused
  **agent** path (`backend/manager/api/v1/agent_token.go`,
  `agent_connection.go`), as planned.
- `ConnectMachine` now authenticates **only** via the machine access token
  (minted by `RefreshMachineToken`); the first-connect token-minting branch is
  gone (`machine_connection.go`: "there is no bootstrap/registration path
  anymore"). The refresh token is minted by the device approval instead.
- **Removed** `RotateMachineToken`. `RevokeMachineToken` remains: revoke all
  tokens + bump `token_version`; the machine's next refresh fails permanently
  and the user recovers by re-running `setup` (device re-auth of the existing
  machine).
- **Added** `UpdateMachine(UpdateMachineRequest) → Machine` — shipped
  **title-only** (no labels; `UpdateMachineRequest.title = 2`). Authorized for
  the machine's creator or `laelia.machines.edit`.
- **Added** `created_at` (field 10) **and `created_by` (field 6, populated)** on
  `MachineSummary`; the waiting page filters on both.
- Also shipped (not from this design): `ForceDisconnectMachine`,
  `TransferMachineOwnership` (from the Q2 decision), and the
  `provisioner` filter on `ListMachines` (provisioner plan).

## CLI design (`backend/agent`) — implemented

### Local state file: `~/.laelia/machine.json`

```json
{
  "manager_url": "https://laelia.metaxisdata.com",
  "machine_id": "machines/abc123",
  "refresh_token": "eyJ...",
  "hostname": "my-laptop",
  "created_at": "2026-08-13T10:00:00Z"
}
```

- Written atomically with `0600` (`atomicfile.WriteFileAtomicSync` —
  `backend/agent/state/state.go`).
- Shipped as `backend/agent/state` (`Load()`, `Save()`, `Clear()`); the path is
  `home.Join("machine.json")`, i.e. `~/.laelia` by default with a `LAELIA_HOME`
  override (`backend/agent/home`).
- Replaced `credential.Manager` and the per-machine `machine-token-<id>` files
  (both deleted). The refresh token is the only credential; the bootstrap
  token is gone.
- One machine per computer ⇒ a single state file. Running `setup` against a
  different manager URL than the state's re-flows (creates a new machine on
  the new manager; the old machine stays orphaned/offline on the old manager).

### `setup` command (new) — implemented (`backend/agent/cmd/setup.go`)

```
laelia-machine --manager <url> setup
```

1. **Already-running check**: probe the well-known daemon socket
   `~/.laelia/daemon.sock` (`alreadyRunning()`). If live → print "laelia-machine
   is already running on this computer" and exit 0.
2. Load state. `--force` clears it first ("the old machine stays registered on
   the manager"). A `manager_url` mismatch prints a warning and re-flows.
3. State exists and `manager_url` matches:
   - `RefreshMachineToken(refresh_token, fingerprint)`:
     - success → print "Already logged in as machine <title> (<id>)" and
       proceed to run;
     - permanent failure (revoked/expired/deleted) → drop the dead refresh
       token but keep the machine id, then continue to the device flow so the
       approval re-authenticates the existing machine (no duplicate);
     - transient failure (manager unreachable) → warn ("could not validate the
       saved login (manager unreachable); starting anyway") and proceed to run
       (the run loop retries with backoff).
4. No state (or cleared): device flow:
   - `StartDeviceLogin(hostname, os, arch, ip, version, fingerprint,
     machine_id?)` — `machine_id` from the (cleared) state if present.
   - Compose the full verification URL from `--manager` +
     `VerificationPath`, print it; auto-open the browser unless
     `--no-browser` (`--foreground` keeps it as PID 1 inside containers).
   - Poll `PollDeviceLogin` every `interval` seconds until APPROVED /
     EXPIRED / DENIED (progress line with remaining time).
   - On APPROVED: atomically save state.
   - On EXPIRED: report and exit.
5. Proceed to run (foreground) — per the Q1 decision, `setup` is the single
   entry command and runs the machine itself.

### `run` command (changed) — implemented (`backend/agent/cmd/run.go`)

- `--token` flag removed. Loads state; missing/incomplete state → error "not
  configured, run `laelia-machine setup` first".
- `manager_url` mismatch → error pointing at the configured manager.
- Already-running check first (same as setup).
- Connect via refresh token only (`client.New(managerURL, machineID,
  refreshToken, …)`); the registration paths
  (`connectViaRegistration`, `connectWithRegistrationToken`,
  `parseResourceIDFromBootstrapToken`) are deleted.
- Permanent auth failure → error pointing at re-running `setup` (the run loop
  bails on permanent failures).
- Refresh-token rolling renewal is saved back to `machine.json` (the client
  gets a `saveRefreshToken` callback — `backend/agent/client/client.go`).

### One-process-per-computer enforcement

- The daemon socket moves from `~/.laelia/<machineID>/daemon.sock` to the
  well-known `~/.laelia/daemon.sock` (workspace dirs stay under
  `~/.laelia/<machineID>/<agentID>/`; the daemon still receives the machine id
  for that).
- The existing `ensureStaleSocket` live-probe pattern is reused: `setup`/`run`
  dial the well-known socket; a successful dial means another instance is
  running → print "already running" and exit 0. This is robust against stale
  PID files and works even when the state file is missing.

## Frontend design — implemented

### Approval page: `/login/device?user_code=XXXX-XXXX` (public) — implemented

- Route registered under `frontend/src/router/routes/auth.tsx`
  (`frontend/src/pages/auth/device-login.tsx`); exempt from the auth guard in
  both directions via the public-path check in
  `frontend/src/router/auth-redirect.ts` (`isPublicPath("/login/device")`).
- Reads `user_code` from the query string; polls `GetDeviceLoginStatus` with a
  self-scheduling loop (base 3s, doubling per consecutive failure, capped at
  15s, paused while the tab is hidden; unreachable-server and
  close-blocking UX included).
- `GetDeviceLoginStatusResponse` shipped with two fields beyond this sketch:
  `ip` (device IP, shown for verification) and `machine_owner` (owner handle
  shown when `reauth_existing`), plus `denial_reason`.
- Renders device hostname + OS/arch + IP, the user code prominently, the
  machine title and owner when `reauth_existing`; logged in → `[Approve]`;
  logged out → sign-in link that returns here; APPROVED → "you can close this
  page"; DENIED → shows the denial reason (or a hint); EXPIRED → re-run-setup
  message.
- Approve calls `ApproveDeviceLogin(user_code)`.

### Create-machine waiting page: `/machines/new` (protected) — implemented, merged with the provisioner flow

- Shipped as `frontend/src/pages/dashboard/machine-new.tsx`: the original
  device-flow waiting page became the **"Self-hosted" tab**, and the
  provisioner plan's machine creation page became the **"Provisioned" tab**
  (`machine-new-provisioned.tsx`); without the provision permission only the
  self-hosted content renders.
- The self-hosted tab shows the install command (per-OS) and
  `laelia-machine --manager <url> setup` (from `getManagerURL()` in
  `frontend/src/lib/machine-token.ts`) with copy buttons.
- Polls `ListMachines` every 5s (silent, `usePolling`). A machine is "new"
  when `created_at > page-open time` **and** `created_by == current user`
  (uses the shipped `MachineSummary.created_at`/`created_by`).
- When a new machine appears: card with hostname/os/arch/ip + editable name
  input (prefilled with the hostname) + `[Confirm]` → `UpdateMachine` →
  navigate to `/machines/<id>`. A "not mine / dismiss" action records the
  machine name in a dismissed set (it can be renamed later from its profile).
- Note: if the user approves with a *different* account, the machine's
  `created_by` is that account and this page will not show it (the machine
  still appears in the full list for users with permission).

### Machine profile changes — implemented

- The rotate-token dialog is replaced by the revoke flow
  (`RevokeMachineToken` in `machine-profile.tsx`) that instructs "run
  `laelia-machine setup` on the machine to re-authenticate". The
  registration-token display dialog is removed.

### i18n — implemented

New strings under `auth.device-login` / `machine.new` / `settings.provisioners`
in `en-US.json` / `zh-CN.json`.

## Docker machine image — implemented

- `scripts/docker/machine-entrypoint.sh` maps env to flags and **always runs
  `laelia-machine setup --no-browser --foreground`** (per the Q3 decision): no
  state → device flow printing the approval URL to the container logs, waits
  for approval, then keeps running; existing state → validates and runs.
  `--allow-http` is added automatically for `http://` manager URLs; `--insecure`
  and `--debug` map from `LAELIA_INSECURE`/`LAELIA_DEBUG`; `LAELIA_CODEX_HOME`
  is exported as `CODEX_HOME`. No `LAELIA_TOKEN` exists anywhere.
- The state file must live on a mounted volume (`LAELIA_HOME` overrides the
  data root, default `~/.laelia`).
- Provisioned pods use the separate runtime image
  (`scripts/docker/Dockerfile.machine-runtime` +
  `machine-runtime-entrypoint.sh`), whose `LAELIA_PROVISIONED=true` switches to
  the fail-fast `setup --provisioned` headless mode (provisioner plan §8.4).

## Security — as shipped

- `device_code` is a 32-byte random secret; `user_code` is 8 chars from an
  unambiguous alphabet. Polling is throttled server-side (min 2s interval →
  `ResourceExhausted`). **The planned per-IP rate limit on
  `StartDeviceLogin`/`PollDeviceLogin`/`GetDeviceLoginStatus` was not built**
  (no limiter exists for these RPCs); the anonymous surface is otherwise
  limited to read-only session status.
- The approval page shows the hostname + user code (plus the device IP and,
  for re-auth, the machine title + owner handle) so the user can verify the
  code matches their device screen (standard device-flow phishing mitigation).
- `ApproveDeviceLogin` requires a logged-in session; same CSRF posture as all
  other cookie-authenticated Connect RPCs. New-machine approval additionally
  requires the caller to pass the `laelia.machines.create` IAM check.
- Re-auth of an existing machine is restricted to the creator or a workspace
  admin; anyone else gets an explicit DENIED + `PermissionDenied` (Q2
  decision, implemented).
- The refresh token is only ever returned over TLS (CLI enforces https unless
  `--allow-http`); the state file is `0600` and written atomically.
- The refresh token is bound to the device fingerprint (existing
  `RefreshMachineToken` check).

## Edge cases

| Case | Behavior |
| --- | --- |
| CLI crashes after approval, before saving state | APPROVED session stays retrievable for a grace window; re-running `setup` re-polls and recovers. |
| Refresh token dead (revoked/expired) | `setup` drops the dead refresh token (keeping the machine id) and re-auths the existing machine via device flow (no duplicate); `setup --force` registers a brand-new machine. |
| Machine deleted server-side, state still local | Approval-time lookup finds it deleted → creates a new machine; CLI updates state. |
| Manager restarts mid-flow | In-memory session lost; CLI poll → EXPIRED; user re-runs `setup`. |
| Hostname changes (laptop renamed) | Fingerprint mismatch → refresh rejected → re-run `setup` to re-auth. |
| `setup` against a different manager URL | Re-flows and creates a new machine on the new manager; old machine stays offline on the old manager. |
| Second instance started | Well-known socket probe → "already running", exit 0. |
| User approves with a different account (new machine) | That account owns the machine; the waiting page (other account) won't show it. |

## Cleanup / removals — all done

- `CreateMachine` RPC + frontend store method + dialog.
- Bootstrap token minting/consumption on the machine path
  (`TokenTypeBootstrap` stays for the agent path).
- `RotateMachineToken` RPC + frontend method.
- `credential.Manager`, `machine-token-<id>` files,
  `parseResourceIDFromBootstrapToken`, `connectViaRegistration`.
- `--token` flag and the docker `LAELIA_TOKEN` mapping.

## Implementation order — all shipped

1. Proto: `device.proto`, `MachineService` changes; `buf format/lint/generate`.
2. Backend: device session store (`component/device`), `DeviceService`
   (`api/v1/device.go`), `UpdateMachine`, `TransferMachineOwnership`,
   `MachineSummary.created_at`/`created_by`, bootstrap paths removed.
3. CLI: `state` package, `setup` (incl. `--force`/`--provisioned`),
   `run` changes, well-known daemon socket, already-running check.
4. Frontend: approval page, waiting page (now the Self-hosted tab), guard,
   machines list/profile, i18n.
5. Docker entrypoint (auto-setup per Q3) + the separate provisioned runtime
   image.
6. Tests: frontend `frontend/src/pages/auth/device-login.test.tsx` +
   `machine-new-provisioned.test.tsx`, CLI `backend/agent/state/state_test.go`
   + `backend/agent/cmd/setup_provisioned_test.go`. **Gap**: no automated
   backend tests for `DeviceService`/`component/device` (the handler and the
   session store ship without unit tests).

## Open questions (all answered — see the Decisions section below)

- **Q1 — `setup` semantics**: should `setup` also start the machine in the
  foreground after configuring/validating (making it the single entry command,
  matching the "already running" wording), or should it only configure and
  print "run `laelia-machine run`"?
- **Q2 — re-auth approval policy**: who may approve re-authentication of an
  existing machine — only its creator, or creator + workspace admin (my
  recommendation)?
- **Q3 — Docker image**: should the machine image entrypoint auto-run `setup`
  when no state exists (print URL to logs, wait for approval, then run), or
  should the operator run `setup` manually in an interactive container first?

## Decisions (2026-08-13, user answers)

- **Q1 — `setup` is the single entry command**: `setup` configures/validates
  the login AND then runs the machine in the foreground. `run` stays as a
  separate command for automation (requires existing state; errors with "run
  `laelia-machine setup` first" when the state file is missing).
- **Q2 — re-auth approval policy (creator or admin) with explicit denial**:
  - Approver is the machine's creator or a workspace admin → approve: bump
    token_version, revoke all tokens, mint a fresh refresh token for the
    existing machine.
  - Approver is neither → the session is marked **DENIED** with a
    human-readable reason carrying the owner's handle and machine title, and
    `ApproveDeviceLogin` returns `PermissionDenied`. The approval page shows
    the reason; the CLI prints: "This machine is already registered to
    <owner-handle> (machine <title>). Ask the owner or a workspace admin to
    transfer it to you, then run setup again. To wipe local data and create a
    brand-new machine on this host, run `laelia-machine setup --force`."
  - **Ownership transfer**: new `TransferMachineOwnership` RPC (mirrors
    `TransferAgentOwnership`): creator or workspace admin reassigns the
    machine's `created_by` to another user. The machine keeps running; its
    tokens are NOT revoked by a transfer. Frontend: transfer dialog on the
    machine profile (creator/admin only).
  - **`setup --force`**: clears the local state file before the device flow,
    so no `machine_id` is sent and approval creates a brand-new machine. The
    old machine row stays on the server (orphaned/offline). Running `setup`
    against a different `--manager` URL than the state's also re-flows
    (prints a warning that the existing machine belongs to another manager).
- **Q3 — Docker auto-setup**: the machine image entrypoint always runs
  `laelia-machine setup --manager <url>` (plus `--no-browser`):
  - no state → device flow: prints the approval URL to the container logs,
    waits for approval, then runs in the foreground;
  - existing state → validates the refresh token and runs.
  `LAELIA_TOKEN` is removed; the state file must live on a mounted volume
  (`-v laelia-state:/root/.laelia`).

### Design deltas from the decisions — implementation status (2026-09-06)

- `PollDeviceLoginResponse` gains `denial_reason` (set on DENIED) — **done**.
- `ApproveDeviceLogin` marks the session DENIED (with reason) on the
  not-creator/not-admin policy failure instead of leaving it PENDING —
  **done**.
- New proto RPCs: `UpdateMachine` (**shipped title-only**, creator-or-admin
  authorized) and `TransferMachineOwnership` (creator-or-admin authorized,
  audited) — **done**.
- `MachineSummary.created_at` added; `created_by` populated — **done**.
- The rate limiter's dedicated per-IP "device" bucket (60/min, burst 30) for
  `StartDeviceLogin` / `PollDeviceLogin` / `GetDeviceLoginStatus` — **not
  implemented**; the shipped throttling is only the 2s poll minimum
  (`TouchPoll` → `ResourceExhausted`). Anonymous calls are not otherwise
  rate-limited.
- `setup` flow: already-running check → load state → state+URL match:
  refresh-token probe → success: "already logged in" → run; permanent
  failure: drop the dead refresh token, keep the machine id → device flow
  (re-auth of the existing machine); transient failure: warn → run (run loop
  retries with backoff). No state / `--force` / different URL: device flow →
  poll → APPROVED: save state → run — **done as specified**
  (`backend/agent/cmd/setup.go`).
- Shipped beyond these deltas: new-machine approval is additionally gated by
  the `laelia.machines.create` IAM check (`requireCanCreateNewMachine`), and
  the approval page receives the device `ip` + `machine_owner` fields.
