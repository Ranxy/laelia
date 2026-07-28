# Built-in pi Agent (non-ACP, RPC mode)

## Context

Today laelia only supports two agent runtimes — `opencode` and `claude-code` — both driven over
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
1. **Subprocess model:** persistent `pi --mode rpc` subprocess, one long-lived process per pi
   agent (not per-turn spawn). Per-turn work is a `prompt` command streamed over the same
   process.
2. **Built-in = a provider option, not auto-create.** `builtin-pi` appears in the provider
   dropdown on every agent (always available, not host-detected). Selecting it reveals
   API-provider + model + api-key fields. The agent is still created normally and bound to a
   machine.
3. **API key storage:** a new first-class plaintext `api_key` field in the agent config (stored
   in the `info` JSONB, same plaintext-at-rest posture as the existing `custom_env`).
4. **pi binary:** dev resolves it from an env var (`LAELIA_PI_BINARY`, example
   `/home/ran/project/pi`); release `//go:embed`s a downloaded/built standalone pi binary and
   extracts it at runtime. Gated by `//go:build !release` vs `//go:build release`.

Outcome: an admin creates an agent on a machine, picks `builtin-pi`, selects `deepseek` or
`openrouter`, pastes an API key, optionally sets a model + persona — and the agent runs an
autonomous drain loop exactly like the ACP agents, streaming text/tool events back over the
existing AgentChannel.

---

## How pi is driven (research summary)

pi (`@earendil-works/pi-coding-agent`) has four modes; we use **RPC mode**:

- Launch: `pi --mode rpc --provider <deepseek|openrouter> --model <modelId> --session-dir <dir> --approve`
- Transport: JSONL over stdin/stdout, **LF (`\n`) delimited only**. (Go's `bufio.Reader.ReadString('\n')`
  is LF-only and safe; Node's `readline` is NOT compliant because it splits on U+2028/U+2029 —
  irrelevant to us since we're in Go.)
- Commands we send (each with an `id` for response correlation): `prompt`, `abort`, `new_session`,
  `switch_session`, `set_model`, `get_state`.
- Events pi streams (no `id`): `agent_start`, `message_update` (carries `text_delta` / `thinking_delta`),
  `tool_execution_start` / `tool_execution_update` / `tool_execution_end`, `compaction_*`,
  `auto_retry_*`, `agent_end`.
- Auth: API key via env var (`DEEPSEEK_API_KEY` / `OPENROUTER_API_KEY`) — cleanest for per-agent
  secret injection. (auth.json priority > env, but env is sufficient and avoids writing files.)
- Tools: pi ships `read`/`write`/`edit`/`bash`. `--approve` (or `defaultProjectTrust=always`)
  makes them run autonomously, so the LLM can shell out to `laelia-agent` exactly like the ACP
  agents.
- Session resume across machine restart: pi persists sessions to `--session-dir`; on runner start
  we `switch_session` to the last session id (recorded in `pi-session.json`, mirroring the existing
  `acp-session.json`) to inherit conversation history + the init prompt.

Sources: [pi repo](https://github.com/earendil-works/pi), [rpc.md](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md), [usage.md](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/usage.md), [providers.md](https://github.com/earendil-works/pi/blob/v0.82.1/packages/coding-agent/docs/providers.md).

---

## Architecture: where pi plugs in

The existing pipeline is executor-agnostic above and below the `Runtime` interface — only the
executor and the runner's config differ. The seam is `commandStream.newSessionRuntime`
(`backend/agent/client/command_stream.go:121`, a `func` field defaulting to `buildRuntime`,
called at `:431`). Today `buildRuntime` hard-codes `executor.NewACP`. We branch on the agent's
config: `builtin-pi` → a new `PiExecutor` backed by a long-lived `pi.Session`; otherwise ACP.

```
runner (per agent)
 ├─ ACP agent:  acpConfig *executor.ACPConfig   →  NewACP per turn (unchanged)
 └─ pi  agent:  piConfig *pi.PiConfig  +  piSession *pi.Session (long-lived)
                                                    ↑
       commandStream.newSessionRuntime(req) ───────┘  branch on config:
       provider=="builtin-pi" → executor.NewPi(req, piSession)   # per-turn Runtime over the shared session
       else                   → executor.NewACP(req, acpConfig)  # unchanged
```

`PiExecutor` implements the existing `executor.Runtime` interface (`Start/Cancel/OutputChannel/
EventChannel/ResultChannel/Done`) so the rest of the drain loop (`runCommand` event/progress/result
pump, manager `HandleProgress/HandleEvent/HandleResult`, the daemon + `laelia-agent` CLI) carries
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
  string api_provider = 8; // "deepseek" | "openrouter". Only meaningful when provider=="builtin-pi".
  string api_key      = 9; // plaintext LLM API key. Only meaningful when provider=="builtin-pi".
}
```

`AgentCapability`: add `bool supports_pi = 10;`. `AgentAssignment` is unchanged (it already
carries `acp_config`). Run `cd proto && buf format -w proto && buf lint proto && buf generate`.

### 2. New `backend/agent/pi/` package

- `protocol.go` — typed structs for the RPC commands (`prompt`, `abort`, `new_session`,
  `switch_session`, `set_model`) and events (`agent_start`, `message_update` with
  `text_delta`/`thinking_delta`, `tool_execution_start/update/end`, `agent_end`, `compaction_*`,
  `auto_retry_*`). `id` correlation for commands; events have no `id`.
- `session.go` — `Session` owns the long-lived `pi --mode rpc` subprocess:
  - `Start(ctx, cfg)`: `exec.CommandContext(ctx, piBinary, "--mode", "rpc", "--provider", cfg.APIProvider, "--model", cfg.Model, "--session-dir", cfg.SessionDir, "--approve")`; env = base `AllowEnv` whitelist + `DEEPSEEK_API_KEY`/`OPENROUTER_API_KEY` from `cfg.APIKey` + the daemon bootstrap env (`LAELIA_DAEMON_SOCKET`, `LAELIA_SESSION_TOKEN`, `LAELIA_AGENT`, `LAELIA_COMMAND`, `PATH` prepend `BinaryDir`) — reuse the ACP executor's `buildACPEnv` pattern.
  - JSONL framing: write commands with `json.Marshal` + `"\n"` to a stdin writer (mutex-guarded); read with `bufio.Reader.ReadString('\n')` (LF-only) from stdout, dispatch response (by `id`) vs event (fan-out to a subscriber channel).
  - Lifecycle: `Stop()` kills the process; auto-restart-on-death is handled by the runner.
  - Session resume: on `Start`, load `pi-session.json` (last session id, keyed by machineID/agentID, fingerprint = api_provider+model+sessionDir); if present and fingerprint matches, send `switch_session`; else `new_session`. Persist the session id once known (from a `get_state`/`agent_start` response). Mirror `acp_session.go`.
- `config.go` — `PiConfig` struct (`APIProvider`, `Model`, `APIKey`, `SessionDir`, `WorkingDir`, `PersonaPrompt`, `PiBinaryPath`, `DaemonSocket`, `SessionToken`, `BinaryDir`, `AgentID`, `MachineID`, max timeouts/output limits) + `BuildPiConfig(user *v1pb.AgentACPConfig, machineID, agentID, piBinaryPath) *PiConfig` (returns nil if provider != "builtin-pi" or api_key empty) + `(*PiConfig).Capability() *v1pb.AgentCapability` (`SupportsPi: true`, `SupportsAcp: false`, plus the diff/raw-events/tool-traces flags pi supports).
- `executor.go` — `PiExecutor` implements `executor.Runtime`:
  - `NewPi(req executor.Request, sess *Session) (Runtime, error)`.
  - `Start()`: subscribe to `sess` event channel; send a `prompt` command with the turn text (cold turn = `buildPrompt(name, persona) + "\n\n" + batch` via the existing `executor.buildPrompt`/`prompt/` assets; warm turn = batch only — same as ACP). Pump pi events → `executor.Event`/`OutputChunk`:
    - `text_delta` → `OutputChunk` (stdout stream) + `TextDelta` event
    - `thinking_delta` → system-stream `OutputChunk` + `TextDelta`
    - `tool_execution_start/update/end` → `ToolCallStarted`/`ToolCallFinished` (pi's tool-call shape is uniform, so a single adapter — no per-provider split like ACP)
    - `agent_end` → `FinalSummary` event + `Result` + close `Done()`
    - `compaction_*`/`auto_retry_*` → `Warning`/`RawAcp`-equivalent raw event
  - `Cancel()`: send `abort` command (graceful); fall back to process kill if unresponsive.
  - Channels/`Done()` are per-turn; the underlying `Session`/process outlives the turn.

### 3. Executor + capability dispatch (`backend/agent/executor/`)

- `acp_config.go`: `BuildCapability(user)` branches — if `user.Provider == "builtin-pi"`, delegate to `pi.BuildPiCapability(user)`; else existing ACP path. `BuildACPConfig` keeps returning nil for pi (no executable), so the ACP path stays inert for pi agents.
- `runtime.go`: unchanged (the `Runtime` interface is already general enough).

### 4. Runner (`backend/agent/client/runner.go`)

`agentRunner` holds **either** `acpConfig` **or** `piConfig` + `piSession`:
- `buildConfig(assignment)`: if `provider=="builtin-pi"` → `pi.BuildPiConfig(...)`, `MkdirAll` the per-agent working/session dir (`~/.laelia/<machineID>/<agentID>/`), return a marker that this is a pi runner; else existing `buildAcpConfig`.
- `start(ctx)`: for a pi runner, additionally construct and `Start` the `pi.Session` before opening the commandStream; `stop()` stops the session too.
- Hot-reload (`spawnOrUpdate`): on config change, if the pi fingerprint (api_provider/model/api_key) changed, restart the `pi.Session`; otherwise keep it warm.
- Set `cs.newSessionRuntime = r.buildRuntimeForAgent` (the branch point) instead of relying on the default. `buildRuntimeForAgent(req)` returns `executor.NewPi(req, r.piSession)` for pi, else `executor.NewACP(req, r.currentConfig())`.

### 5. command_stream (`backend/agent/client/command_stream.go`)

- `newSessionRuntime` is already a `func` field (`:121`) defaulting to `buildRuntime` (`:142`) and called at `:431`. The runner overrides it (§4) — no change to the call site needed. Keep `buildRuntime` as the ACP default.

### 6. Provider registry + validation (`backend/agent/provider/registry.go`, `backend/manager/api/v1/agent.go`)

- `knownProviderID` (agent.go:1530) must accept `"builtin-pi"` as a known id, but it is **not** a `provider.Provider` (no `Detect`/`BuildCommand` — pi is bundled, not host-detected). Add a separate `isBuiltinRuntime(id)` set or special-case `"builtin-pi"` in `knownProviderID`.
- `validateAgentACPConfig` (agent.go:1436): when `provider=="builtin-pi"`:
  - require `api_provider ∈ {deepseek, openrouter}` and `api_key != ""`;
  - require `model` non-empty (default per api_provider if the UI doesn't supply one);
  - **skip** the host-detected `providerAvailable` check (pi is always available — bundled) and the `supportsModelConfigOption` check.
- `CreateAgent` / `UpdateAgentACPConfig`: carry `api_provider`/`api_key` through; `capability = BuildCapability(reqACP)` (now pi-aware). Best-effort `SendAgentAssignment` hot-push unchanged.
- **Redaction (recommended, small):** in `GetAgent`/`ListAgents`, blank out `api_key` for callers without `can_edit` (the `can_edit` field already exists). Phase-1 acceptable to ship without, but call it out.

### 7. Dispatcher gate (`backend/manager/component/dispatcher/dispatcher.go:570`)

Relax the `HandleBeginSession` gate from `!capability.GetSupportsAcp()` to
`!capability.GetSupportsAcp() && !capability.GetSupportsPi()` so a pi agent can start drain
sessions. (Add a one-line `(*AgentCapability).SupportsAnyRuntime()` helper if preferred.)

### 8. Bundled pi binary resolution (`backend/agent/pi/binary.go`)

Two build-tagged implementations of a single `BinaryPath() (path string, cleanup func(), err error)`
function, so dev and release resolve pi from different sources without runtime branching:

- **Dev** — `backend/agent/pi/binary_dev.go` (`//go:build !release`): read the path from the env
  var `LAELIA_PI_BINARY` (example `/home/ran/project/pi` — the local checkout on the dev machine).
  No embedding, no download. Fast iteration.
- **Release** — `backend/agent/pi/binary_release.go` (`//go:build release`): the pi binary is
  `//go:embed`-ed into the binary at compile time (the release build script downloads the
  platform-specific pi binary into `backend/agent/pi/embedded/pi` before `go build`). Since an
  embedded blob can't be `exec`'d directly, `BinaryPath()` writes it to a per-machine temp file
  (e.g. `~/.laelia/<machineID>/pi.bin`, chmod 0700, content-addressed by a baked-in hash so it's
  written once and reused) and returns that path. The returned `cleanup` lets the runner remove it
  on shutdown (optional; leaving it cached is fine).

`//go:build !release` / `//go:build release` is the only switch — the runner always calls
`pi.BinaryPath()` and gets the right behavior for the build.

The runner passes the resolved path into `BuildPiConfig`.

### 9. Frontend (`frontend/src/pages/dashboard/agent-profile.tsx`, `machine-profile.tsx`, `stores/agent.ts`)

- Provider `<Select>`: always append a `builtin-pi` `<SelectItem>` (in addition to host-detected
  providers + `custom`), regardless of `availableProviders`.
- When `provider === "builtin-pi"`, render a **different config block** (hide executable/args/
  allow_env/custom_env; keep persona_prompt):
  - **API provider** `<Select>`: `deepseek` / `openrouter`.
  - **Model**: `deepseek` → dropdown (`deepseek-chat`, `deepseek-reasoner`); `openrouter` →
    free-text `<Input>` (openrouter has thousands of models; curated preset optional).
  - **API key**: password-type `<Input>` (saved via the same auto-save `saveConfig` chain →
    `updateAgentACPConfig`).
- `stores/agent.ts`: `createAgent`/`updateAgentACPConfig` carry `apiProvider` + `apiKey`.
- Regenerate proto-es types (`frontend/src/types/proto-es/...`); add i18n strings.

### 10. Build / packaging (dev + release)

Dev needs nothing extra — point `LAELIA_PI_BINARY` at the local pi checkout (default
`/home/ran/project/pi`). The laelia binary itself is built with no extra tags (defaults to
`!release`).

Release adds a build step that produces an embeddable pi binary **before** `go build -tags
release`:

- `scripts/build-pi.sh` (invoked by the release pipeline): for the target `GOOS/GOARCH`, download
  the matching pi standalone binary from the pi GitHub releases (or build it from source via
  `bun build --compile --target=<os>-<arch>` if no prebuilt release artifact exists), and write it
  to `backend/agent/pi/embedded/pi`. Then `go build -tags release` compiles
  `binary_release.go`, whose `//go:embed embedded/pi` bakes the blob into the laelia binary.
- The embed path is relative to the `.go` file, so a single `//go:embed embedded/pi` works; for
  cross-platform releases the script overwrites `embedded/pi` with the correct target binary per
  build (one binary per target platform, built separately — same as laelia's own per-platform
  release).

**Risk:** this depends on either (a) pi publishing prebuilt standalone binaries on GitHub releases
for the target platforms, or (b) `bun build --compile` producing a working standalone binary. This
must be validated early in implementation; the fallback (bundling `node` + the npm package behind
a wrapper) keeps the `BinaryPath()` contract intact.

---

## Phase 1 scope (per user)

- API providers: **deepseek** and **openrouter** only. (pi supports many more; the design
  generalizes — adding another provider is just a registry entry + a UI option.)
- Models: deepseek dropdown + openrouter free-text.
- No mid-turn `steer`/`follow_up` yet (the RPC protocol supports them; out of scope for phase 1).
- No extension/MCP wiring (pi pushes these to extensions; not needed for the base chat loop).

---

## Verification

1. **Build:** `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`;
   `cd proto && buf lint proto && buf generate`; `pnpm --dir frontend biome:check && pnpm --dir frontend type-check`.
2. **Lint:** `golangci-lint run --allow-parallel-runners` until clean.
3. **Unit tests (new):** `pi` package — JSONL framing (LF-only, embedded U+2028/U+2029 must NOT
   split), command/response correlation, event→executor.Event mapping, session resume fingerprint
   logic. Mirror the ACP executor test style
   (`backend/agent/executor/acp_executor_test.go`, `acp_session_test.go`).
4. **Integration (local pi):** with a real bundled `pi` binary and a real deepseek key, run a
   machine, create a `builtin-pi` agent, post in a channel, and confirm:
   - streaming text appears token-by-token;
   - a tool call (e.g. `bash` invoking `laelia-agent message ...`) emits
     `ToolCallStarted`/`ToolCallFinished` and the message lands in the channel;
   - a second turn resumes the same pi session (no cold restart) and the agent retains context;
   - Cancel mid-turn stops the turn (abort) without killing the persistent process;
   - machine restart resumes the pi session from `pi-session.json`.
5. **Frontend:** create a pi agent on a machine, switch API provider, paste key, save; verify
   auto-save persists `api_provider`/`api_key` and the agent transitions from `pending-config`
   to ready.
6. **ACP regression:** confirm opencode/claude-code agents are unaffected (the `newSessionRuntime`
   branch only diverts `builtin-pi`).

---

## Open items / risks

- **Embeddable pi binary** (§10): confirm pi ships prebuilt standalone binaries on its GitHub
  releases for our target platforms, or that `bun build --compile` works. Validate early; fallback
  is bundling `node` + the npm package behind a wrapper (the `BinaryPath()` contract is unchanged).
- **API-key plaintext at rest** is accepted for phase 1 (matches `custom_env`); add redaction for
  non-editors and an encrypted secret table as a follow-up.
- **pi RPC protocol specifics** (exact `prompt`/`switch_session`/`abort` field shapes, the
  session-id source) need to be pinned to a pi version during implementation by reading
  `packages/coding-agent/docs/rpc.md` and `src/modes/rpc/rpc-types.ts` at the chosen tag.
- **`//go:embed` size:** a compiled pi binary is tens of MB; embedding inflates the laelia binary.
  Acceptable for release; flag if it crosses a threshold we care about.