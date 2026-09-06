# Machine Hosts Many Agents — Design & Known Gaps

> Status: verified and updated against the current code on 2026-09-06. Main changes: the machine onboarding flow is now the device-code login (the registration/bootstrap-token path was removed entirely), `agent.machine_id` is a nullable FK with unbound agents supported, and known gap #4's residual refresh-token burn scenario is fixed (refresh tokens are multi-use with rolling renewal).

## Context

Laelia was refactored from **one daemon process per agent** to **one machine = one application hosting many agents**. A user opens the machine page, copies the one-line install command plus `laelia-machine --manager <url> setup`, and runs setup once on a host — the machine authenticates through the **OAuth2 device-code flow** (no token in the command; the machine row is created by the device approval, `backend/manager/api/v1/device.go` `approveNewMachine`) — then creates multiple **agents** parented to that machine — no per-agent process or token. All agents on a machine execute concurrently. (A machine can also be created headlessly by a provisioner: `ProvisionMachine` seeds the same machine.json credential.)

- **Control plane**: a machine authenticates once (device approval → refresh token; `RefreshMachineToken` mints access tokens) and holds one `MachineChannel` bidi stream for roster changes + provider discovery.
- **Data plane**: the machine app opens one `AgentChannel` bidi stream per assigned agent for that agent's drain loop. Each `AgentChannel` authenticates with the machine's access token and declares its agent in-stream (`AgentReady.agent_name`).
- **Binding**: `agent.machine_id` is a nullable FK (`0`/NULL = unbound, a legacy/unbound-agent path in `backend/manager/store/agent.go`) and is immutable once set — `UpdateAgent`'s update-mask paths never include it. Moving an agent = delete + recreate. ACP session state lives on the machine's disk (cold start on a new machine is acceptable).
- **Dispatcher**: keyed by `agent_id` for the data plane (`AgentSession`), plus a `machine_id` map for the control plane (`MachineSession`). `UnregisterMachine` detaches every owned `AgentSession` (each owned agent with an in-flight command gets a 60s grace period; reconnect re-registers and cancels the timers).
- **Liveness**: the **machine** heartbeats, not the agent. An agent's online/offline state is derived from the dispatcher (`agentReachable`): online when its runner has a live `AgentChannel` **or** the machine it is bound to is connected. It is **not** derived from `agent.status.LastHeartbeatAt` (that field is only written by the additive, unused per-agent `AgentHeartbeat` RPC path — via `component/state.HeartbeatBuffer` — never by the machine model).

Old per-agent token/session RPCs and tables (`RotateAgentToken`, `RevokeAgentToken`, `ConnectAgent`, `AgentHeartbeat`, `RefreshAgentToken`, `agent_token`, `agent_session`, `CreateAgentResponse.bootstrap_token`, …) are deliberately kept compiling-but-unused until a final cleanup phase. They are not bugs.

Sidebar: Home / Activity / Members / Machines / Settings. Machines is its own page (`/machines`); Members is a flat contacts page (humans + agents, not grouped by machine).

## Known gaps / follow-ups

### 1. AgentAssignment push is best-effort, not durable (OPEN)

**Where**: `backend/manager/api/v1/agent.go` `CreateAgent` / `DeleteAgent`, `backend/manager/api/v1/agent_config.go` `UpdateAgentACPConfig` (plus the stop/start/restart paths in `agent.go`) — each pushes over the owning machine's `MachineChannel` via `dispatcher.SendAgentAssignment` / `SendAgentConfigUpdate` / `SendRemoveAgent`. `backend/manager/component/dispatcher/dispatcher.go` `SendAgentAssignment` etc.

**Symptom**: When an agent is created (or its config updated / it is deleted) while the machine is **online**, the manager pushes the change to the machine app over the live `MachineChannel`. This push is **best-effort**: a send failure is logged, not queued, and the change is not retried. If the push fails (e.g. the machine's control stream is momentarily blocked, the send loses a race with a disconnect, or the machine is in a brief reconnect window), the machine app never learns about the change until the **next full `ConnectMachine` resync** — which only happens when the machine reconnects. So:

- A newly created agent may stay **offline** (no runner spawned) until the machine next reconnects, even though the machine is online.
- An `UpdateAgentACPConfig` hot-reload may never reach the runner until reconnect.
- A `DeleteAgent` may leave a zombie runner on the host until reconnect.

**Why it was accepted**: The plan explicitly chose best-effort-over-durable-queue. In the common case the machine is steadily connected and the push succeeds within a second, so the agent is online immediately (and `agentReachable`'s "machine connected" clause already reports it online the moment it is created on a connected machine, masking the runner-not-yet-spawned window). The next `ConnectMachine` resyncs the full roster from the DB, so any missed push is eventually self-healing — but only on reconnect, which for a long-lived machine may be never.

**Impact**: rare in practice (requires a push to fail exactly while the machine is online and not reconnecting), but when it hits, the agent appears offline / stale until a machine restart. Confusing for the operator because the machine shows online.

**Possible fixes (not yet implemented)**:
- **Durable pending-changes queue**: persist per-machine pending `AgentAssignment` / `AgentConfigUpdate` / `RemoveAgent` rows; the machine app acks each, and the manager replays the unacked set on `MachineReady` (and on every reconnect). Most robust; new table + ack proto + replay logic.
- **Periodic resync tick**: the manager periodically (e.g. every 30–60s) pushes the full assigned-agents roster to each connected machine (`ReloadAgentAssignment` per agent, or a bulk resync message), so a missed push is corrected within one tick without waiting for reconnect. Cheaper than a durable queue; no new table. The machine app's `spawnOrUpdate` is already idempotent, so a full resync is safe.
- **Reactive resync on agent read**: when `GetAgent`/`ListMachineAgents` finds an agent whose runner is not connected but whose machine is, trigger a one-shot `ReloadAgentAssignment` for that agent. Narrowest fix; only covers the "agent offline but machine online" case, not config/delete staleness.

**Recommendation**: the periodic-resync tick is the lowest-cost fix that covers all three staleness cases (create/config/delete) without a new table. Revisit when this is observed in practice.

### 2. Agent connection timestamps are not populated (MINOR)

`convertToV1AgentStatus` still reads `status.LastHeartbeatAt` / `status.ConnectedAt` for the *display* timestamps (`LastHeartbeatTime`, `ConnectedTime`). Under this model those are never written by the machine path — the only writers are the unused per-agent RPCs (`ConnectAgent` stamps `ConnectedAt` on its bootstrap connect; `AgentHeartbeat` feeds `HeartbeatBuffer`) — so an online agent shows no connected/heartbeat time. The connection **state** (the `ConnectionBadge` dot) is correct via `agentReachable`; only the timestamps are empty. Optional follow-up: populate `ConnectedAt` from the dispatcher's `AgentSession.connectedAt` (and/or the machine's connected time) when building the status proto.

### 3. No live status updates on the profile pages (PRE-EXISTING, narrowed)

`agent-profile` loads once on mount and does not poll. `machine-profile` polls (3s interval) **only while a lifecycle action is in flight** — an active provisioning job or an in-flight upgrade (`usePolling` gated on those states) — so ordinary status changes (machine disconnects, runner connects a second after page load) are still not reflected until a manual reload. This predates the refactor. With the `agentReachable` fix the common create-on-connected-machine case is already online on first load, so this is low priority.

### 4. Machine auto-reconnect after a manager restart (FIXED)

**Where**: `backend/agent/client/client.go` `Connect` / `Run` / `connectViaRefresh` / `applyConnectResponse`; `backend/manager/api/v1/machine_connection.go` `ConnectMachine`; `backend/manager/api/v1/machine_token.go` `RefreshMachineToken`.

**Symptom (reported)**: after restarting the manager, a connected machine failed to reconnect. The log showed `502 Bad Gateway` retries while the manager was down, then a successful `connected to manager via refresh token`, then immediately `machine control stream died ... error="unauthenticated: authorization header format must be Bearer {token}"`, and on the next reconnect `machine credentials are no longer valid ... error="unauthenticated: registration token is not active"` — the machine process exited.

**Root cause** (two bugs, both fixed):
1. *Empty access-token clobber.* On the refresh/reconnect path `ConnectMachine` returns **no** access token — the access token comes solely from `RefreshMachineToken`. `applyConnectResponse` unconditionally overwrote the good refresh-minted access token with that empty string, so the control stream sent `Authorization: Bearer ` and the manager rejected it with `authorization header format must be Bearer {token}` — the stream died within milliseconds of the connect. **Fix**: `applyConnectResponse` now only records the session (`connState` + `sessionID`) and never touches the access token, which `connectViaRefresh` set from the refresh response.
2. *Registration-token fallback + permanent bail.* The old registration (bootstrap) path was single-use and dead once a refresh token existed; the old `Connect` fell back to it whenever the refresh RPC failed — including a transient `502 Bad Gateway` while the manager was still coming up — producing `registration token is not active` (`CodeUnauthenticated`), which `isPermanentAuthFailure` treated as permanent and bailed on, exiting the machine. **Fix**: `Connect` reconnects through the persisted refresh token **exclusively** (`connectViaRefresh`); the `Run` loop distinguishes a genuine credential death (revoked family / version mismatch / machine deleted → bail) from a transient `502`/network failure (→ backoff + retry, auto-reconnecting once the manager is back). Since then the registration path has been **removed entirely**: `ConnectMachine` takes no token field and its handler comment states "there is no bootstrap/registration path anymore" — the machine authenticates with the access token minted by `RefreshMachineToken` from the device-flow refresh token.

**Residual gap (FIXED — the refresh token is no longer single-use)**: the burn scenario this section used to warn about — a lost refresh *response* after the server consumed the old token, so the retry re-presented a `CONSUMED` token and the manager revoked the whole family — no longer exists. `RefreshMachineToken` now treats the machine refresh token as a **durable, multi-use reconnection credential**: the common reconnect reuses the same token (no consumption, no new row), so a lost response is safely retryable. A replacement is minted only when the current token is within `machineRefreshRotateWindow` (10 days) of expiry (rolling renewal), and the old token is left `ACTIVE` to expire on its own — a lost renewal response can simply be retried. Theft detection is fingerprint binding plus token-version mismatch; a `CONSUMED`/`REVOKED` token presented on purpose still revokes the family (`machineRefreshReuseAction` in `machine_token.go`) as a safety net.