# Machine Hosts Many Agents — Design & Known Gaps

## Context

Laelia was refactored from **one daemon process per agent** to **one machine = one application hosting many agents**. A user creates a **Machine** (gets a registration token + run command), runs the machine app once on a host (`laelia-machine run --manager <url> --token <token>`), then creates multiple **agents** parented to that machine — no per-agent process or token. All agents on a machine execute concurrently.

- **Control plane**: a machine authenticates once (registration → access + refresh token) and holds one `MachineChannel` bidi stream for roster changes + provider discovery.
- **Data plane**: the machine app opens one `AgentChannel` bidi stream per assigned agent for that agent's drain loop. Each `AgentChannel` authenticates with the machine's access token and declares its agent in-stream (`AgentReady.agent_name`).
- **Binding**: `agent.machine_id` is NOT NULL and immutable. Moving an agent = delete + recreate. ACP session state lives on the machine's disk (cold start on a new machine is acceptable).
- **Dispatcher**: keyed by `agent_id` for the data plane (`AgentSession`), plus a `machine_id` map for the control plane (`MachineSession`). `UnregisterMachine` detaches every owned `AgentSession`.
- **Liveness**: the **machine** heartbeats, not the agent. An agent's online/offline state is derived from the dispatcher (`agentReachable`): online when its runner has a live `AgentChannel` **or** the machine it is bound to is connected. It is **not** derived from `agent.status.LastHeartbeatAt` (that field is no longer written under this model — kept only for the additive, unused per-agent RPC path).

Old per-agent token/session RPCs and tables (`RotateAgentToken`, `RevokeAgentToken`, `ConnectAgent`, `AgentHeartbeat`, `RefreshAgentToken`, `agent_token`, `agent_session`, `CreateAgentResponse.bootstrap_token`, …) are deliberately kept compiling-but-unused until a final cleanup phase. They are not bugs.

Sidebar: Home / Activity / Machines / Members / Settings. Machines is its own page (`/machines`); Members is a flat contacts page (humans + agents, not grouped by machine).

## Known gaps / follow-ups

### 1. AgentAssignment push is best-effort, not durable (OPEN)

**Where**: `backend/manager/api/v1/agent.go` `CreateAgent`, `UpdateAgentACPConfig`, `DeleteAgent` — each pushes over the owning machine's `MachineChannel` via `dispatcher.SendAgentAssignment` / `SendAgentConfigUpdate` / `SendRemoveAgent`. `backend/manager/component/dispatcher/dispatcher.go` `SendAgentAssignment` etc.

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

`convertToV1AgentStatus` still reads `status.LastHeartbeatAt` / `status.ConnectedAt` for the *display* timestamps (`LastHeartbeatTime`, `ConnectedTime`). Under this model those are never written, so an online agent shows no connected/heartbeat time. The connection **state** (the `ConnectionBadge` dot) is correct via `agentReachable`; only the timestamps are empty. Optional follow-up: populate `ConnectedAt` from the dispatcher's `AgentSession.connectedAt` (and/or the machine's connected time) when building the status proto.

### 3. No live status updates on the profile pages (PRE-EXISTING)

`agent-profile` and `machine-profile` load once on mount and do not poll, so a status change (machine disconnects, runner connects a second after page load) is not reflected until a manual reload. This predates the refactor. With the `agentReachable` fix the common create-on-connected-machine case is already online on first load, so this is low priority.

### 4. Machine auto-reconnect after a manager restart (FIXED), with one residual gap

**Where**: `backend/agent/client/client.go` `Connect` / `Run` / `applyConnectResponse`; `backend/manager/api/v1/machine.go` `ConnectMachine` / `RefreshMachineToken`.

**Symptom (reported)**: after restarting the manager, a connected machine failed to reconnect. The log showed `502 Bad Gateway` retries while the manager was down, then a successful `connected to manager via refresh token`, then immediately `machine control stream died ... error="unauthenticated: authorization header format must be Bearer {token}"`, and on the next reconnect `machine credentials are no longer valid ... error="unauthenticated: registration token is not active"` — the machine process exited.

**Root cause** (two bugs, both fixed):
1. *Empty access-token clobber.* On the refresh/reconnect path `ConnectMachine` returns **no** access token — it only mints access+refresh on the bootstrap path (`bootstrapTokenID != 0`, `machine.go:521`). `applyConnectResponse` unconditionally overwrote the good refresh-minted access token with that empty string, so the control stream sent `Authorization: Bearer ` and the manager rejected it with `authorization header format must be Bearer {token}` — the stream died within milliseconds of the connect. **Fix**: `applyConnectResponse` only overwrites `accessToken` when `ConnectMachine` actually returns one; on the reconnect path the refresh-minted token (set in `connectViaRefresh`) is preserved.
2. *Registration-token fallback + permanent bail.* The registration (bootstrap) token is single-use and consumed on the first successful `ConnectMachine`, so once a refresh token exists the registration token is dead. The old `Connect` fell back to it whenever the refresh RPC failed — including a transient `502 Bad Gateway` while the manager was still coming up — producing `registration token is not active` (`CodeUnauthenticated`), which `isPermanentAuthFailure` treated as permanent and bailed on, exiting the machine. **Fix**: once a refresh token is persisted, `Connect` reconnects through it **exclusively** (`connectViaRefresh`); there is no registration fallback. The refresh path returns its own error, so the `Run` loop distinguishes a genuine credential death (refresh family revoked / version mismatch / machine deleted → bail) from a transient `502`/network failure (→ backoff + retry, auto-reconnecting once the manager is back). The registration token is only used on the first-ever connect (`connectViaRegistration`), the only path that consumes it.

**Residual gap (OPEN, not yet hit in practice)**: the refresh token is single-use with reuse detection (`RefreshMachineToken` revokes the whole family when a `CONSUMED` refresh token is re-presented, `machine.go:706`). The client persists the new refresh token as soon as the refresh response arrives, so a clean retry is safe. But if the refresh RPC reaches the manager, the manager consumes the old token and mints the new one, and the **response is then lost** (network drop / proxy 502 on the response path), the client never learns the new token and retries with the old (now-consumed) one → the server sees reuse → revokes the family → permanent bail. This "burn" requires a response loss *while the manager is up and processing*, not a clean restart (during a restart the manager is down and does not consume the token, so the retry succeeds). Possible fixes (not implemented): idempotent refresh — when a `CONSUMED` refresh token is re-presented, return its already-minted ACTIVE successor within a short grace window instead of revoking the family (requires storing enough to re-issue, or a "rotation window" where the old token stays valid until the new one is acknowledged); or make refresh-token rotation two-phase (consume only after the new token is confirmed received). Revisit if observed.