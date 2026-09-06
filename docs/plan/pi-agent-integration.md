# Built-in pi Agent (non-ACP, RPC mode)

> Status: verified and updated against the current code on 2026-09-06. Main changes: the design is fully implemented and has grown beyond it — the RPC command/event set is pinned (terminal event is `agent_settled`, no `new_session`/`set_model`), a `custom` API provider and manager-managed global providers were added, release embedding is per-platform (`binary_release_<goos>_<goarch>.go` + `no_embed_pi` variant), same-turn steering and managed-MCP wiring exist, and manager validation/redaction moved to `agent_config.go`/`agent.go`.

## Context

At the time this design was written, laelia supported two agent runtimes — `opencode` and
`claude-code` — both driven over
**ACP** (a JSON-RPC-over-stdio subprocess protocol). Each agent is an admin-created record bound
to one machine; the machine app spawns the ACP subprocess per turn and resumes the session.

We want to add a third runtime, **pi** (`github.com/earendil-works/pi`), with different rules:

- **Non-ACP.** pi speaks its own `--mode rpc` JSONL-over-stdio protocol, not ACP. The two paths
  must coexist; the executor is chosen per-agent by config.
- **Built-in, no manual install.** pi is bundled with the laelia distribution; the user never
  runs `npm i`. (We bundle a standalone pi binary — see Build/Packaging.)
- **Provider + API key in the UI.** The user picks `builtin-pi` as the provider, then an
  **API provider** (`deepseek` or `openrouter` for phase 1) and enters an **API key**. No
  host-side binary detection, no `ANTHROPIC_API_KEY`-in-env ceremony.

Decisions confirmed with the user:
1. **Subprocess model:** a `pi --mode rpc` subprocess is **long-lived while in use** per pi
   agent (not per-turn spawn). Per-turn work is a `prompt` command streamed over the same
   process. After `IdleTimeout` (default 5min) of no turns, the subprocess is **idle-evicted**
   to free memory — but the conversation lives on in `pi-session.json`, so the next turn resumes
   it via `switch_session` (warm, no init prompt); the only cost is the 1-3s respawn. There is
   no cold restart during active use; idle eviction is expected, recoverable, and never loses
   the conversation.
2. **Built-in = a provider option, not auto-create.** `builtin-pi` appears in the provider
   dropdown on every agent (always available, not host-detected). Selecting it reveals
   API-provider + model + api-key fields. The agent is still created normally and bound to a
   machine.
3. **API key storage:** a new first-class plaintext `api_key` field in the agent config (stored
   in the `info` JSONB, same plaintext-at-rest posture as the existing `custom_env`).
4. **pi binary:** dev resolves it from an env var (`LAELIA_PI_BINARY`); release `//go:embed`s a
   downloaded standalone pi distribution and extracts it at runtime. Gated by
   `//go:build !release` vs `//go:build release` (details in §8).

Outcome: an admin creates an agent on a machine, picks `builtin-pi`, selects `deepseek` or
`openrouter`, pastes an API key, optionally sets a model + persona — and the agent runs an
autonomous drain loop exactly like the ACP agents, streaming text/tool events back over the
existing AgentChannel.

---

## How pi is driven (research summary)

pi (`@earendil-works/pi-coding-agent`) has four modes; we use **RPC mode**:

- Launch: `pi --mode rpc --provider <piProviderId> --model <modelId> --session-dir <dir> --no-skills --no-prompt-templates --approve` (the `--provider` flag is omitted when no laelia-managed API provider is configured, e.g. user-installed pi in "own model/auth" mode).
- Transport: JSONL over stdin/stdout, **LF (`\n`) delimited only**. (Go's `bufio.Reader.ReadString('\n')`
  is LF-only and safe; Node's `readline` is NOT compliant because it splits on U+2028/U+2029 —
  irrelevant to us since we're in Go.)
- Commands we send (each with an `id` for response correlation): `prompt`, `steer`,
  `abort` (no id, fire-and-forget), `get_state`, `get_session_stats`, `switch_session`
  (by session **path**). There is no `new_session`/`set_model` command — a fresh session
  is simply not switched, and the model is fixed per subprocess launch.
- Events pi streams (no `id`): `agent_start`, `message_update` (carries an
  `assistantMessageEvent` with `text_delta` / `thinking_delta` / `done` / `error`),
  `tool_execution_start` / `tool_execution_end` (`tool_execution_update` is currently
  ignored), `compaction_start` / `compaction_end`, `auto_retry_start` / `auto_retry_end`,
  `extension_error`, `bash_execution_update` (ignored), `agent_end` (informational;
  `willRetry` surfaces a warning), and the **terminal** `agent_settled`.
- Auth: API key via env var (`DEEPSEEK_API_KEY` / `OPENROUTER_API_KEY` /
  `LAELIA_CUSTOM_API_KEY`) — cleanest for per-agent secret injection. (auth.json priority >
  env, but env is sufficient and avoids writing files.)
- Tools: pi ships `read`/`write`/`edit`/`bash`. `--approve` trusts the working dir and
  `--no-skills`/`--no-prompt-templates` keep the headless drain loop free of extension-UI
  dialogs, so the LLM can shell out to `laelia-machine` exactly like the ACP agents.
- Session resume across machine restart: pi persists sessions to `--session-dir`; on runner start
  we `switch_session` to the persisted session **file path** (recorded in `pi-session.json`,
  mirroring the existing `acp-session.json`) to inherit conversation history + the init prompt.

Sources: [pi repo](https://github.com/earendil-works/pi), [rpc.md](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md), [usage.md](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/usage.md), [providers.md](https://github.com/earendil-works/pi/blob/v0.82.1/packages/coding-agent/docs/providers.md).

---

## Architecture: where pi plugs in

The existing pipeline is executor-agnostic above and below the `Runtime` interface — only the
executor and the runner's config differ. The seam is `commandStream.newSessionRuntime`
(a `func` field declared in `backend/agent/client/command_stream.go`, defaulting to
`buildRuntime` in `newCommandStream` and called from `runSession` in
`backend/agent/client/drain_runner.go`). The default `buildRuntime` hard-codes
`executor.NewACP`. The runner overrides the field with `buildRuntimeForAgent`, which branches
on the agent's config: a pi provider (`builtin-pi` or user-installed `pi`) → a `PiExecutor`
backed by a long-lived `pi.Session`; otherwise ACP (v1 session executor or the v2 thread
executor).

```
runner (per agent)
 ├─ ACP agent:  acpConfig *executor.ACPConfig   →  NewACP per turn (unchanged)
 └─ pi  agent:  piConfig *pi.PiConfig  +  piSession *pi.Session (long-lived)
                                                    ↑
       commandStream.newSessionRuntime(req) ───────┘  buildRuntimeForAgent branches on config:
       pi.IsPiProvider(provider) → pi.NewPi(req, piSession, piConfig)  # per-turn Runtime over the shared session
       else                      → executor.NewACP(req, acpConfig)     # unchanged
```

`PiExecutor` implements the existing `executor.Runtime` interface (`Start/Cancel/OutputChannel/
EventChannel/ResultChannel/Done`) so the rest of the drain loop (`runCommand` event/progress/result
pump, manager `HandleProgress/HandleEvent/HandleResult`, the daemon + `laelia-machine` CLI) carries
over unchanged.

---

## Implementation

### 1. Proto (`proto/v1/v1/agent.proto` + `proto/store/store/agent.proto`)

Reuse `AgentACPConfig` as the single config carrier (the whole pipeline — CreateAgent,
UpdateAgentACPConfig, `AgentAssignment.acp_config`, store, frontend — already flows through it),
extending it with two pi-only fields:

```proto
message AgentACPConfig {
  // ...existing 1–7...
  string api_provider = 8; // "deepseek" | "openrouter" | "custom". Only meaningful when provider selects a pi runtime.
  string api_key      = 9; // plaintext LLM API key. Only meaningful for a laelia-managed pi config.
}
```

As implemented, the carrier grew further: `global_provider = 10` /
`global_provider_entry = 11` (manager-managed global API providers, resolved to concrete
api_provider/api_key/model at the daemon boundary by `resolveAcpConfigForDaemon`),
`api_base_url = 13`, `context_window = 14` and `max_tokens = 15` (custom provider tuning).

`AgentCapability`: `bool supports_pi = 10;`. `AgentAssignment` is unchanged (it already
carries `acp_config`). Run `cd proto && buf format -w proto && buf lint proto && buf generate`.

### 2. New `backend/agent/pi/` package

- `protocol.go` — typed structs for the RPC commands (`prompt`, `steer`, `abort`, `get_state`,
  `get_session_stats`, `switch_session`) and events (`agent_start`, `message_update` with an
  `assistantMessageEvent` carrying `text_delta`/`thinking_delta`/`done`/`error`,
  `tool_execution_start/end`, `agent_end`, the **terminal** `agent_settled`, `compaction_*`,
  `auto_retry_*`, `extension_error`). `id` correlation for commands; events have no `id`.
  Also declares `BuiltinPiProvider`/`UserPiProvider`/`IsPiProvider`.
- `session.go` — `Session` owns the long-lived `pi --mode rpc` subprocess:
  - `Start(commandID)`: `exec.CommandContext(s.ctx, piBinary, cfg.launchArgs()...)` where
    `launchArgs` yields `--mode rpc [--provider <piProviderId>] --model <model> --session-dir
    <WorkingDir> --no-skills --no-prompt-templates --approve`; env = the fixed `piAllowEnv`
    whitelist + the provider key env (`DEEPSEEK_API_KEY`/`OPENROUTER_API_KEY`/
    `LAELIA_CUSTOM_API_KEY`) from `cfg.APIKey` + the daemon bootstrap env
    (`LAELIA_DAEMON_SOCKET`, `LAELIA_SESSION_TOKEN`, `LAELIA_AGENT`, `LAELIA_COMMAND`,
    `PATH` prepend `BinaryDir`). The subprocess is bound to the session ctx (independent of
    any turn ctx); the session is started lazily by the first turn (`ensureStarted`) so the
    opening turn's command id can seed `LAELIA_COMMAND`.
  - JSONL framing: write commands with `json.Marshal` + `"\n"` to a stdin writer (mutex-guarded); read with `bufio.Reader.ReadString('\n')` (LF-only) from stdout, dispatch response (by `id`) vs event (routed to the active turn channel).
  - Lifecycle: `Stop()` kills the process; auto-restart-on-death is handled by the runner (and by the next turn's `ensureStarted`).
  - Session resume: on start, `resumeOrCapture` loads `pi-session.json` (session **path** +
    fingerprint, keyed by machineID/agentID; fingerprint = api_provider+model+WorkingDir); if
    present and the fingerprint matches, send `switch_session` with the session path; else
    stay cold. The current session file (from a `get_state` response) is persisted for the
    next start. Startup is bounded by `StartupTimeout`; a wedged startup is killed and the
    turn fails fast.
- `config.go` — `PiConfig` struct (`APIProvider`, `Model`, `APIKey`, `BaseURL`,
  `ContextWindow`, `MaxTokens`, `ConfigDir` (custom providers' `PI_CODING_AGENT_DIR`),
  `WorkingDir`, `PersonaPrompt`, `PiBinaryPath`, `AgentResourceID`, `DaemonSocket`,
  `SessionToken`, `BinaryDir`, `MachineID`, `AgentID`, `executor.Limits`, `IdleTimeout`,
  `McpProxyURL`) + `BuildPiConfig(user *v1pb.AgentACPConfig, machineID, agentID,
  agentResourceID, piBinaryPath, daemonSocket, sessionToken, binaryDir) *PiConfig` (returns
  nil unless `IsPiProvider(provider)` with a model and either a known api_provider+api_key or
  the user-pi "own model/auth" mode) + `BuildPiCapability(user) *v1pb.AgentCapability`
  (`SupportsPi: true`, `SupportsAcp: false`, plus the diff/raw-events/tool-traces/
  autonomous-decision flags). `LaunchFingerprint` covers everything that shapes the launch
  (api provider, model, key, base URL, binary path, context overrides, persona) and gates
  hot-reload restarts.
- `executor.go` — `PiExecutor` implements `executor.Runtime`:
  - `NewPi(req executor.Request, sess *Session, cfg *PiConfig) (Runtime, error)`.
  - `Start()`: run the turn on the shared session — `ensureStarted` (lazy start / wait out an
    in-flight idle eviction), sample context usage, `beginTurn` to open a fresh event channel,
    then send a `prompt` command with the turn text (cold turn = `executor.BuildPrompt(name,
    owner, persona, team) + steering prompt + batch` via the `prompt/` assets; warm turn =
    re-anchor/steering prompt + batch — same shape as ACP). Pump pi events →
    `executor.Event`/`OutputChunk`:
    - `text_delta` → stdout-stream `OutputChunk` (buffered/batched via `executor.OutputBuffer`)
    - `thinking_delta` → ASSISTANT-stream `OutputChunk`
    - `tool_execution_start/end` → `ToolCallStarted`/`ToolCallFinished` (pi's tool-call shape is uniform, so a single adapter — no per-provider split like ACP)
    - `agent_settled` (terminal) → `FinalSummary` event + `Result` + close `Done()`
    - `agent_end` with `willRetry` → `Warning`
    - `compaction_*` → `CONTEXT_COMPACTION_*` events (+ gates same-turn steering);
      `auto_retry_*`/`extension_error` → `Warning`
  - Token/context usage: pi is pull-based, so the executor samples `get_session_stats` at
    turn start/end (emitting a per-command `TOKEN_USAGE` delta) and polls it every 60s
    (emitting `CONTEXT_USAGE_UPDATE`).
  - `Cancel()`: cancel the turn ctx + send `abort` (fire-and-forget).
    The session ctx is decoupled from the turn ctx, so the abort only ends the current
    turn; it does **not** cancel the session ctx, so the underlying process survives an
    in-flight cancel and a follow-up turn reuses it.
  - Channels/`Done()` are per-turn; the underlying `Session`/process outlives the turn.

#### 空闲回收 (idle eviction)

The subprocess is **long-lived while in use**, not permanently resident. After a turn ends,
`Session.armIdleTimer()` (called from `endTurn`) arms a `time.AfterFunc(IdleTimeout, evict)`.
`beginTurn` stops the timer, so an active session never thrashes. When it fires:

- `evict()` SIGTERMs the process group (`executor.KillGroup`), waits up to
  `idleEvictGrace = 3s` on `waitDone`, then SIGKILLs + reaps. It does **not** cancel the
  session ctx (so the runner re-spawns next turn) and does **not** delete `pi-session.json`.
- The go/no-go decision is under `startMu` (aborts if `beginTurn` already stopped the timer);
  the reap waits **outside** the lock (waitPump needs `startMu` to reset `started`, so holding
  it across the wait would deadlock).

**Config:** `PiConfig.IdleTimeout`, defaulted to `defaultIdleTimeout = 5 * time.Minute` by
`BuildPiConfig`. **Rationale:** a chat agent with a ~2s median cold-start respawn tolerates a
5min idle window well — memory is freed between conversation bursts while the warm-resume cost
stays bounded. Batch-dense agents can lower it per-agent; **zero or negative disables
eviction** (process stays resident — useful for debug or a tight batch loop). It is
internal-only for now (mirroring `StartupTimeout`); a per-agent proto surface and Prometheus
metrics (`pi_idle_evictions`, `pi_cold_start_seconds`) are deferred.

### 3. Executor + capability dispatch (`backend/agent/executor/`, `backend/manager/api/v1/`)

- `acp_config.go`: `BuildACPConfig` returns nil for any provider implementing the
  `provider.NonACPRuntime` marker (user-installed pi) and for `builtin-pi` (not in the
  registry, no executable), so the ACP path stays inert for pi agents. The pi capability
  branch does **not** live in the executor package: the manager's
  `buildCapabilityForACPConfig` (`backend/manager/api/v1/agent_convert.go`) delegates to
  `pi.BuildPiCapability` whenever `pi.IsPiProvider(provider)` — this avoids the import
  cycle (pi imports executor).
- `runtime.go`: unchanged (the `Runtime` interface is already general enough).

### 4. Runner (`backend/agent/client/runner.go`)

`agentRunner` holds **either** `acpConfig` **or** `piConfig` + `piSession` (and, for ACP v2
thread providers, a resident `threadSession`; the sides never coexist — `applyAssignment`
tears down the inactive one):
- `buildPiConfig(assignment)`: for `builtin-pi` resolve the binary with `pi.ResolveBinary()`;
  for user-installed `pi`, `Detect` it on PATH (5s timeout) and require `Compatible`; then
  `pi.BuildPiConfig(...)`, `MkdirAll` the per-agent working/session dir (the laelia data
  root's `<machineID>/<agentID>/`, via the `home` package), and attach the daemon's managed-
  MCP proxy URL. Returns nil (runner inert) when the agent is unconfigured or the binary is
  unavailable.
- The `pi.Session` is **not** started up front: `start(ctx)` opens the commandStream and sets
  `cs.newSessionRuntime = r.buildRuntimeForAgent`; the session object is created eagerly
  (cheap) but the subprocess starts lazily on the first turn (`ensureStarted`).
  `stop()` stops the session too.
- Hot-reload (`applyAssignment`, driven by `spawnOrUpdate`): an unchanged
  `LaunchFingerprint` (api provider/model/key/base URL/binary/context/persona) keeps the warm
  session; a changed one coordinates any in-flight turn and restarts the subprocess.
- `buildRuntimeForAgent(req)` returns `pi.NewPi(req, r.piSession, r.piConfig)` for pi, else
  the ACP path (`executor.NewACP`, or the v2 thread executor for thread providers).

### 5. command_stream (`backend/agent/client/`)

- `newSessionRuntime` is a `func` field on `commandStream` (`command_stream.go`), defaulted
  to `buildRuntime` in `newCommandStream` and called from `runSession` in
  `drain_runner.go`. The runner overrides it (§4) — no change to the call site needed. Keep
  `buildRuntime` as the ACP default. (The original monolithic `command_stream.go` has since
  been split into `command_stream.go` / `drain_runner.go` / `message_router.go`.)

### 6. Provider registry + validation (`backend/agent/provider/registry.go`, `backend/manager/api/v1/agent_config.go`)

- `knownProviderID` (`agent_config.go`) accepts `"custom"` and `"builtin-pi"` as literal
  ids plus everything in the provider registry (which includes user-installed `"pi"`);
  `builtin-pi` is **not** a `provider.Provider` (no `Detect`/`BuildCommand` — pi is bundled,
  not host-detected).
- `validateAgentACPConfig` (`agent_config.go`): when `pi.IsPiProvider(cfg.Provider)`:
  - a `global_provider`/`global_provider_entry` reference replaces the inline
    api_provider/api_key (both fields required and consistent; access checked by the handler);
  - user-installed `pi` with an empty api_provider is the "own model/auth" mode: only
    `model` is required, no api_key;
  - otherwise require `api_provider ∈ {deepseek, openrouter, custom}` (custom additionally
    requires `api_base_url`), `api_key != ""` and `model != ""`;
  - user-installed `pi` must be present and version-compatible on the owning machine when
    the machine has probed (`available_providers` + `compatible`);
  - **skip** the ACP host-detected `providerAvailable`/`supportsModelConfigOption` checks for
    pi (builtin-pi is always available — bundled).
- `CreateAgent` / `UpdateAgentACPConfig`: carry the pi fields through (inline api_key is
  additionally permission-gated by `canUseInlineAPIKey`); capability is re-derived with
  `buildCapabilityForACPConfig`. Best-effort config-update hot-push unchanged.
- **Redaction (implemented):** `GetAgent`/`ListAgents` blank out `api_key` for callers
  without edit permission and return a masked preview for editors (`agent.go`), so the
  secret never reaches a non-editor.

### 7. Dispatcher gate (`backend/manager/component/dispatcher/dispatcher.go`)

Implemented: the `HandleBeginSession` gate is
`!capability.GetSupportsAcp() && !capability.GetSupportsPi()` so a pi agent can start drain
sessions. (The suggested `SupportsAnyRuntime()` helper was not added — the inline check
remains.)

### 8. Bundled pi binary resolution (`backend/agent/pi/`)

Build-tagged implementations of a single `ResolveBinary() (path string, err error)`
function, so dev and release resolve pi from different sources without runtime branching:

- **Dev** — `backend/agent/pi/binary_dev.go` (`//go:build !release`): read the path from the env
  var `LAELIA_PI_BINARY` (which may point at the pi executable directly or at an extracted
  release directory containing `pi`). No embedding, no download. Fast iteration.
- **Release** — `backend/agent/pi/binary_release_<goos>_<goarch>.go` (one each for
  linux/amd64, darwin/arm64, windows/amd64, tagged `//go:build release && !no_embed_pi &&
  <goos> && <goarch>`): the whole pi distribution is `//go:embed`-ed at compile time from
  `backend/agent/pi/embedded/dist-<goos>-<goarch>` (binary + `theme/`, `node_modules/`, wasm
  — pi resolves runtime assets relative to its own executable, so embedding only the binary
  crashes at startup). Since an embedded blob can't be `exec`'d directly, the shared
  `resolveBinary` in `binary_release.go` extracts the distribution to a content-addressed
  cache directory under the laelia data root (`<data root>/bin/pi-<hash>-<goos>-<goarch>/`,
  binary chmod 0700) — written once and reused across restarts.
- **No-embed release** — `backend/agent/pi/binary_no_embed_pi.go`
  (`//go:build release && no_embed_pi`): `ResolveBinary` returns a clear error; the
  builtin-pi provider is unavailable and only user-installed pi can be used.

`//go:build !release` vs `release` (+ `no_embed_pi`) is the only switch — the runner always
calls `pi.ResolveBinary()` and gets the right behavior for the build, then passes the
resolved path into `BuildPiConfig`.

### 9. Frontend (`frontend/src/components/agent/acp-config-editor.tsx`, `frontend/src/stores/agent.ts`)

The config UI was later extracted into the shared `acp-config-editor.tsx` (used by
`agent-profile.tsx` and `machine-add-agent-sheet.tsx`, with draft state in
`hooks/use-acp-config-draft.ts`):

- Provider `<Select>`: always append a `builtin-pi` `<SelectItem>` (in addition to host-detected
  providers + `custom`), regardless of `availableProviders`; user-installed `pi` appears from
  `availableProviders` (disabled with its `incompatibility_reason` when incompatible).
- For a pi provider, render a **different config block** (hide executable/args/
  allow_env/custom_env; keep persona_prompt):
  - **Managed vs own**: global-provider picker (manager-managed API providers via the
    api-provider service), self-provided `api_provider`/`api_key`, or (user-installed pi
    only) "own model/auth" mode with no laelia-managed key.
  - **API provider** `<Select>`: `deepseek` / `openrouter` / `custom` (custom adds a base
    URL + optional context-window/max-tokens fields).
  - **Model**: populated dynamically via the `ListPiModels` RPC (`stores/agent.ts
    listPiModels` proxies the provider's model-listing API server-side so the api_key never
    reaches the browser's third-party call) — no hardcoded model list.
  - **API key**: password-type `<Input>` (saved via the same auto-save `saveConfig` chain →
    `updateAgentACPConfig`; the form does not echo the key back).
- `stores/agent.ts`: `createAgent`/`updateAgentACPConfig` carry the pi fields.
- Regenerate proto-es types (`frontend/src/types/proto-es/...`); add i18n strings.

### 10. Build / packaging (dev + release)

Dev needs nothing extra — point `LAELIA_PI_BINARY` at the local pi checkout. The laelia
binary itself is built with no extra tags (defaults to `!release`).

Release adds a build step that produces an embeddable pi distribution **before** `go build
-tags release`:

- `scripts/build-pi.sh` (invoked by the release pipeline, honoring `LAELIA_BUILD_PROXY`): for
  the target `GOOS/GOARCH`, download the matching prebuilt pi standalone archive from the pi
  GitHub releases, verify it against the release's `SHA256SUMS`, and extract the whole
  distribution to `backend/agent/pi/embedded/dist-<goos>-<goarch>/`. It is idempotent (the
  recorded version/platform in `pi.meta` skips a redundant download; `PI_FORCE=1` overrides).
  The three placeholder `dist-*` directories (empty `pi` files) are tracked so a fresh
  checkout compiles with `-tags release`; the real distributions are never committed.
- Each per-platform `binary_release_<goos>_<goarch>.go` then embeds its own
  `embedded/dist-<goos>-<goarch>` directory, so cross-platform releases build one target per
  invocation — same as laelia's own per-platform release.

**Risk (resolved):** pi publishes prebuilt standalone archives (with SHA256SUMS) for
linux/darwin/windows on x64+arm64, so the `bun build --compile` fallback was not needed.

---

## Phase 1 scope (per user)

(As designed; phase 1 has since shipped and several scope lines moved beyond it.)

- API providers: **deepseek** and **openrouter** only. → Since extended with **custom**
  (OpenAI-compatible base URL + optional context-window/max-tokens, per-agent models.json)
  and with manager-managed **global providers**.
- Models: deepseek dropdown + openrouter free-text. → Replaced by the dynamic `ListPiModels`
  RPC-backed model picker for both.
- No mid-turn `steer`/`follow_up` yet (the RPC protocol supports them; out of scope for phase 1).
  → Implemented later: same-turn steering via the `steer` command (see
  `pi-same-turn-steering.md`).
- No extension/MCP wiring (pi pushes these to extensions; not needed for the base chat loop).
  → Implemented later: the managed-MCP pi extension (`mcp_extension.go`) registers the
  agent's server-managed MCP tools through the localhost daemon proxy (`LAELIA_MCP_PROXY_URL`),
  and `windows_shell_extension.go` maps the `bash` tool to PowerShell on Windows.

---

## Verification

1. **Build:** `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`;
   `cd proto && buf lint proto && buf generate`; `pnpm --dir frontend biome:check && pnpm --dir frontend type-check`.
2. **Lint:** `golangci-lint run --allow-parallel-runners` until clean.
3. **Unit tests (implemented):** `pi` package — `backend/agent/pi/pi_test.go` (JSONL framing
   LF-only, command/response correlation, event→`executor.Event` mapping, `BuildPiConfig`
   gating, launch args/env, fingerprint, token-usage deltas), `lifecycle_test.go` (turn
   lifecycle, idle eviction, steering), `models_test.go`, plus
   `backend/agent/executor/acp_executor_test.go` / `acp_session_test.go` for the ACP mirror.
4. **Integration (local pi):** with a real bundled `pi` binary and a real deepseek key, run a
   machine, create a `builtin-pi` agent, post in a channel, and confirm:
   - streaming text appears token-by-token;
   - a tool call (e.g. `bash` invoking `laelia-machine message ...`) emits
     `ToolCallStarted`/`ToolCallFinished` and the message lands in the channel;
   - a second turn resumes the same pi session (no cold restart during active use — the
     conversation is warm) and the agent retains context; an idle-evicted session respawns once
     and resumes from `pi-session.json`, still no amnesia;
   - Cancel mid-turn stops the turn (abort) without killing the persistent process;
   - machine restart resumes the pi session from `pi-session.json`.
5. **Frontend:** create a pi agent on a machine, switch API provider, paste key, save; verify
   auto-save persists `api_provider`/`api_key` and the agent transitions from `pending-config`
   to ready.
6. **ACP regression:** confirm opencode/claude-code/codex agents are unaffected (the
   `newSessionRuntime` branch only diverts pi providers).

---

## Open items / risks

- **Embeddable pi binary** (§10): resolved — pi publishes prebuilt standalone archives with
  SHA256SUMS for our target platforms; `scripts/build-pi.sh` downloads + verifies them and the
  `bun build --compile` fallback was never needed.
- **API-key plaintext at rest** is accepted (matches `custom_env`); redaction for non-editors
  is implemented (`GetAgent`/`ListAgents` blank/mask the key). An encrypted secret table
  remains a possible follow-up.
- **pi RPC protocol specifics**: pinned to pi **v0.82.1** (`scripts/build-pi.sh`
  `PI_VERSION`, `provider.MinSupportedPiVersion`); command/event shapes are encoded in
  `backend/agent/pi/protocol.go` and covered by tests.
- **`//go:embed` size:** a compiled pi distribution is tens of MB; embedding inflates the
  laelia release binary. Accepted for release.
