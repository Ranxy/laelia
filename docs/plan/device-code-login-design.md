# Device-Code Login & Machine Provisioning — Design

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
  string verification_uri = 3;   // full URL: <external_url>/login/device?user_code=...
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
}

message ApproveDeviceLoginRequest { string user_code = 1; }
message ApproveDeviceLoginResponse {}
```

### Device session store (in-memory)

Consistent with the existing single-instance in-memory components (dispatcher,
roomhub, `state.State`). A new `backend/manager/component/device` package:

```go
type Session struct {
    DeviceCode  string
    UserCode    string
    Status      Status            // PENDING / APPROVED / EXPIRED / DENIED
    MachineID   string            // existing machine to re-auth, or ""
    Hostname, OS, Arch, IP, Version string
    Fingerprint string
    CreatedAt   time.Time
    ExpiresAt  time.Time          // CreatedAt + 10 min
    ApprovedAt time.Time
    ApprovedBy int                // user id
    Result     *Result            // set on approval
}
type Result struct {
    MachineID    string
    MachineTitle string
    RefreshToken string
}
```

- Maps keyed by `device_code` and `user_code`; mutex-guarded; lazy expiry sweep
  on access (plus a background ticker to purge expired sessions).
- **Post-approval grace window** (e.g. 10 min): an APPROVED session keeps
  returning `Result` on poll so a CLI that crashed between approval and saving
  state can recover by re-polling; after the window it is purged.
- Manager restart mid-flow loses pending sessions → CLI poll returns
  EXPIRED/not-found → user re-runs `setup`. Acceptable (short-lived flow,
  single-instance manager).

### Approval handler (`ApproveDeviceLogin`)

1. Look up session by `user_code`; must be PENDING and unexpired, else
   `InvalidArgument`/`FailedPrecondition`.
2. Resolve the approving user from context.
3. If `session.MachineID != ""` (re-auth of an existing machine):
   - Load machine; if it exists and is not deleted:
     - **Policy**: allow only the machine's creator or a workspace admin to
       approve re-auth (a different account must not silently take over a
       machine). Otherwise `PermissionDenied`.
     - Bump `token_version`, revoke all machine tokens, mint a **new refresh
       token** (new family = machine resource id, fingerprint from the
       session) — reuse/extend the existing `RotateMachineTokens` store
       transaction (it currently mints a bootstrap token; change it to mint a
       refresh token).
   - If the machine is missing/deleted → fall through to creation (the CLI
     will receive the new machine id and update its state).
4. Else (new machine): create the machine row with `title = hostname`,
   `info` from the session, `created_by = approving user`. Mint a refresh
   token (family = machine resource id, fingerprint from session).
5. Mark session APPROVED, store `Result`, audit.

### Poll handler (`PollDeviceLogin`)

- PENDING → `PENDING` (enforce a minimum poll interval server-side, e.g. 2s).
- APPROVED → return `Result` (idempotent within the grace window).
- EXPIRED/DENIED → return the status.
- Unknown device_code → `EXPIRED` (do not leak whether a code ever existed).

### Start handler (`StartDeviceLogin`)

- Rate limit per source IP (e.g. 5/min).
- `device_code`: 32 random bytes, base64url. `user_code`: 8 chars from an
  unambiguous alphabet (`ABCDEFGHJKLMNPQRSTUVWXYZ23456789`), formatted
  `XXXX-XXXX`.
- `verification_uri`: `<workspace external_url>/login/device?user_code=<code>`,
  falling back to the request `Host` when `external_url` is unset.

### MachineService changes

- **Remove** `CreateMachine` RPC and the bootstrap-token machinery
  (`TokenTypeBootstrap` minting in `CreateMachine`/`RotateMachineToken`,
  `registration_token` in `ConnectMachineRequest` /
  `RotateMachineTokenResponse`, `authenticateMachineRegistrationToken`,
  `ConsumeMachineToken` usage on the connect path).
- `ConnectMachine` now authenticates **only** via the machine access token
  (minted by `RefreshMachineToken`); the first-connect token-minting branch is
  removed. The refresh token is minted by the device approval instead.
- **Remove** `RotateMachineToken` (its only output was a bootstrap token).
  `RevokeMachineToken` remains: revoke all tokens + bump `token_version`; the
  machine's next refresh fails permanently and the user recovers by re-running
  `setup` (device re-auth of the existing machine).
- **Add** `UpdateMachine(UpdateMachineRequest) → Machine` (title + labels) for
  the frontend confirm/rename step. Authorized in the handler for the machine's
  creator or `laelia.machines.edit` (same pattern as `DeleteMachine`).
- **Add** `created_at` to `MachineSummary` (needed by the frontend waiting
  page to detect "new" machines).

## CLI design (`backend/agent`)

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

- Written atomically with `0600` (reuse `atomicfile.WriteFileAtomicSync`).
- New `backend/agent/state` package: `Load()`, `Save()`, `Clear()`.
- Replaces `credential.Manager` and the per-machine `machine-token-<id>`
  files. The refresh token is the only credential; the bootstrap token is
  gone.
- One machine per computer ⇒ a single state file. Running `setup` against a
  different manager URL than the state's re-flows (creates a new machine on
  the new manager; the old machine stays orphaned/offline on the old manager).

### `setup` command (new)

```
laelia-machine --manager <url> setup
```

1. **Already-running check**: probe the well-known daemon socket
   `~/.laelia/daemon.sock` (see below). If live → print "laelia-machine is
   already running" and exit 0.
2. Load state. If state exists and `manager_url` matches:
   - `RefreshMachineToken(refresh_token, fingerprint)`:
     - success → print "Already logged in as machine <title> (<id>)" and
       proceed to run;
     - permanent failure (revoked/expired/deleted) → drop the dead refresh
       token but keep the machine id, then continue to the device flow so the
       approval re-authenticates the existing machine (no duplicate);
     - transient failure (manager unreachable) → warn and proceed to run (the
       run loop retries with backoff).
3. No state (or cleared): device flow:
   - `StartDeviceLogin(hostname, os, arch, ip, version, fingerprint,
     machine_id?)` — `machine_id` from the (cleared) state if present.
   - Print the verification URL and the user code; optionally auto-open the
     browser (`xdg-open`/`open`/`rundll32`, `--no-browser` to disable).
   - Poll `PollDeviceLogin` every `interval` seconds until APPROVED /
     EXPIRED / DENIED (print a progress line with remaining time).
   - On APPROVED: atomically save state, print
     "Machine <hostname> registered as <machine_id>".
   - On EXPIRED: "code expired, run setup again".
4. Proceed to run (foreground) — see open question Q1.

### `run` command (changed)

- `--token` flag removed. Loads state; missing state → error "not configured,
  run `laelia-machine setup` first".
- `manager_url` mismatch → error pointing at the configured manager.
- Already-running check first (same as setup).
- Connect via refresh token only (`connectViaRefresh`); the registration
  paths (`connectViaRegistration`, `connectWithRegistrationToken`,
  `parseResourceIDFromBootstrapToken`) are deleted.
- Permanent auth failure → error "credentials rejected; run `laelia-machine
  setup` to re-authenticate" (the run loop already bails on permanent
  failures).
- Refresh-token rolling renewal is saved back to `machine.json` (the client
  gets a `saveRefreshToken` callback).

### One-process-per-computer enforcement

- The daemon socket moves from `~/.laelia/<machineID>/daemon.sock` to the
  well-known `~/.laelia/daemon.sock` (workspace dirs stay under
  `~/.laelia/<machineID>/<agentID>/`; the daemon still receives the machine id
  for that).
- The existing `ensureStaleSocket` live-probe pattern is reused: `setup`/`run`
  dial the well-known socket; a successful dial means another instance is
  running → print "already running" and exit 0. This is robust against stale
  PID files and works even when the state file is missing.

## Frontend design

### Approval page: `/login/device?user_code=XXXX-XXXX` (public)

- New route, **exempt from the auth guard** in both directions (logged-out
  users must reach it; logged-in users must not be redirected away). Extend
  `resolveAuthRedirect` with a public-path check.
- Reads `user_code` from the query string; polls `GetDeviceLoginStatus` every
  3s.
- Renders:
  - device hostname + OS/arch, the user code prominently (user verifies it
    matches the code on their device screen), and the machine title when
    `reauth_existing`;
  - logged in → `[Approve]` button + "use another account" (logout → sign-in
    with `redirect` back to this page);
  - logged out → inline sign-in (reuse the sign-in form) or link to
    `/auth/signin?redirect=/login/device?...`;
  - APPROVED → "Approved! You can close this page.";
  - EXPIRED → "This code has expired. Run the setup command again on the
    device."
- Approve calls `ApproveDeviceLogin(user_code)`.

### Create-machine waiting page: `/machines/new` (protected)

- The Machines page "create" button navigates here instead of opening the
  name+token dialog.
- Shows the command `laelia-machine --manager <url> setup` (from
  `getManagerURL()`) with a copy button, and a short explanation.
- Polls `ListMachines` every 5s (silent). A machine is "new" when
  `created_at > page-open time` **and** `created_by == current user`
  (requires the new `created_at` on `MachineSummary`).
- When a new machine appears: card with hostname/os/arch/ip + editable name
  input (prefilled with the hostname) + `[Confirm]` → `UpdateMachine` →
  navigate to `/machines/<id>`. A "not mine / dismiss" action leaves the
  machine as-is (it can be renamed later from its profile).
- Note: if the user approves with a *different* account, the machine's
  `created_by` is that account and this page will not show it (the machine
  still appears in the full list for users with permission).

### Machine profile changes

- The rotate-token dialog is replaced by a "revoke + re-authenticate" flow:
  `RevokeMachineToken` then instruct "run `laelia-machine setup` on the
  machine to re-authenticate". The registration-token display dialog is
  removed.

### i18n

New strings in `en-US.json` / `zh-CN.json` for both pages and the profile
change.

## Docker machine image

- The entrypoint no longer maps `LAELIA_TOKEN`; it runs
  `laelia-machine run --manager $LAELIA_MANAGER_URL` (plus `--allow-http` for
  `http://`).
- The state file must live on a mounted volume (`-v laelia-state:/root/.laelia`
  or a `LAELIA_HOME`-style env override). Without state, `run` errors with
  "run setup first".
- Open question Q3: whether the entrypoint should auto-run `setup` when no
  state exists (print the URL to the container logs, wait for approval, then
  run) so a single `docker run -d` works.

## Security

- `device_code` is a 32-byte random secret; `user_code` is 8 chars from an
  unambiguous alphabet. Polling is rate-limited (min 2s interval server-side);
  `StartDeviceLogin` is rate-limited per IP.
- The approval page shows the hostname + user code so the user can verify the
  code matches their device screen (standard device-flow phishing mitigation).
- `ApproveDeviceLogin` requires a logged-in session; same CSRF posture as all
  other cookie-authenticated Connect RPCs.
- Re-auth of an existing machine is restricted to the creator or a workspace
  admin (open question Q2).
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

## Cleanup / removals

- `CreateMachine` RPC + frontend store method + dialog.
- Bootstrap token minting/consumption on the machine path
  (`TokenTypeBootstrap` stays for the unused agent path).
- `RotateMachineToken` RPC + frontend method.
- `credential.Manager`, `machine-token-<id>` files,
  `parseResourceIDFromBootstrapToken`, `connectViaRegistration`.
- `--token` flag and the docker `LAELIA_TOKEN` mapping.

## Implementation order

1. Proto: `device.proto`, `MachineService` changes; `buf format/lint/generate`.
2. Backend: device session store, `DeviceService`, `UpdateMachine`,
   `MachineSummary.created_at`, remove bootstrap paths.
3. CLI: `state` package, `setup`, `run` changes, well-known daemon socket,
   already-running check.
4. Frontend: approval page, waiting page, guard, machines list/profile,
   i18n.
5. Docker entrypoint.
6. Tests (backend unit + handler tests, CLI tests, frontend page tests),
   `gofmt`, `golangci-lint`, `pnpm` checks, build.

## Open questions

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

### Design deltas from the decisions

- `PollDeviceLoginResponse` gains `denial_reason` (set on DENIED).
- `ApproveDeviceLogin` marks the session DENIED (with reason) on the
  not-creator/not-admin policy failure instead of leaving it PENDING.
- New proto RPCs: `UpdateMachine` (title/labels, creator-or-admin authorized)
  and `TransferMachineOwnership` (creator-or-admin authorized, audited).
- `MachineSummary.created_at` added; `created_by` populated (was declared but
  never filled in ListMachines).
- The rate limiter gets a dedicated per-IP "device" bucket (60/min, burst 30)
  for `StartDeviceLogin` / `PollDeviceLogin` / `GetDeviceLoginStatus` so
  5s-polling anonymous calls are not throttled by the tiny connect bucket.
- `setup` flow: already-running check → load state → state+URL match:
  refresh-token probe → success: "already logged in" → run; permanent
  failure: drop the dead refresh token, keep the machine id → device flow
  (re-auth of the existing machine); transient failure: warn → run (run loop
  retries with backoff). No state / `--force` / different URL: device flow →
  poll → APPROVED: save state → run.
