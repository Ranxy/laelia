# pi Same-Turn Steering

## Context

Today a laelia pi agent processes one drain turn at a time: the machine app's
drain loop calls `BeginSession`, gets a `command_id`, builds a "New messages
received:" batch, sends a `prompt` to the long-lived `pi --mode rpc`
subprocess, and drains events until `agent_settled`. New messages that arrive
**while a turn is running** only wake the drain loop (`NewMessagesAvailable` →
`wakeCh`); they are picked up by the next `BeginSession` after the current turn
ends.

We want **same-turn steering**: while a pi turn is in progress, deliver a
notification into the running turn so the agent can react immediately instead
of waiting for the turn to finish.

pi v0.82.1 (the version laelia pins) already speaks the `steer` RPC natively —
no pi upgrade and no manager changes are needed.

## How pi steering works

pi's RPC protocol has a `steer` command: `{"type": "steer", "message": "..."}`.
While the agent is running, pi queues the message and delivers it after the
current assistant turn finishes its tool calls, before the next LLM call.
Two properties make the turn extend naturally:

- `agent_settled` is emitted only when the agent is *fully* settled — no queued
  steering continuation remains — so a steered turn keeps draining until the
  steered work is processed.
- The default `set_steering_mode` (`one-at-a-time`) is fine: multiple steered
  messages queue and deliver one per assistant turn.

The steered payload is a **content-free inbox notice** (`[Laelia inbox notice:
...]`), never the raw messages: the agent pulls the real messages itself via
`laelia-machine message check` / `thread check` (the system prompt says so).

## Implementation

### 1. Protocol (`backend/agent/pi/protocol.go`)

```go
type steerCommand struct {
    Type    string `json:"type"`
    ID      string `json:"id,omitempty"`
    Message string `json:"message"`
}
```

### 2. Session (`backend/agent/pi/session.go`)

`Session.steer(ctx, message)` mirrors `prompt()`: it goes through the same
`send()` request/response correlation and returns an error when the response
has `success: false`. Callers treat failure as best-effort (see §5).

### 3. Executor (`backend/agent/pi/executor.go`)

- `PiExecutor` gains `steerCh chan string` (buffered, capacity 8) and a
  `compacting atomic.Bool`, both initialized in `NewPi`.
- `Steer(text string) error` is the public, non-blocking entry point: a
  select-default write into `steerCh`; a full queue returns
  `"pi: steer queue full"` immediately. `steerCh` is never closed (a close
  would race the drain loop).
- In `run()`'s drain select:

```go
case text := <-e.steerCh:
    if e.compacting.Load() {
        continue // pi is rewriting session history; the wake fallback recovers
    }
    if err := e.session.steer(e.ctx, text); err != nil {
        // best-effort; the post-turn BeginSession wake is the fallback
    }
```

- `compacting` is set/cleared by the drain loop from the
  `compaction_start`/`compaction_end` events in `handleEvent` (same goroutine
  as the `steerCh` case, so the atomic is only for `Steer`'s external readers).
- The turn keeps draining until `agent_settled`; pi only emits it after queued
  steering messages are processed, so the turn naturally extends.
- `Steer` is exposed via the optional interface `interface{ Steer(string) error }`
  — `executor.Runtime` (and the ACP executor) is untouched.

### 4. System prompt

`piSteeringPrompt` (a `pi`-package constant) tells the agent how to react to an
in-turn notice:

> While you are working, new messages may be delivered into your current turn
> as a short notice (e.g. "[Laelia inbox notice: ...]"). When you see one, run
> `laelia-machine message check` (or `laelia-machine thread check` if the
> notice mentions a thread reply) at a natural breakpoint and process the new
> messages before ending your turn.

It is appended to the cold init prompt and the re-anchor prompt in
`turnPromptText` (both pi-only paths; ACP never sees it).

### 5. Command stream (`backend/agent/client/command_stream.go`)

- New `steerer` interface (`Steer(text string) error`) — the optional
  in-turn injection capability a runtime may implement (pi does; ACP does not).
- `buildSteerNotice(nm *v1pb.NewMessagesAvailable)` renders the content-free
  notice, with three variants:
  - thread reply: `[Laelia inbox notice: new reply in a thread you follow. Run
    \`laelia-machine thread check\` at a natural breakpoint.]`;
  - single conversation: `[Laelia inbox notice: new messages arrived. Run
    \`laelia-machine message check\` at a natural breakpoint.]`;
  - multiple conversations: `[Laelia inbox notice: new messages arrived in N
    conversations. Run \`laelia-machine message check\` at a natural
    breakpoint.]`.
- In the receive pump's `NewMessages` case: always `c.wake()` first (the
  durable fallback), then — if the current executor implements `steerer` —
  call `Steer(buildSteerNotice(...))`. Any failure is logged at Debug only;
  the wake recovers the messages on the next `BeginSession`.

### 6. Races & fallbacks

- **Compaction**: steering is suppressed while `compaction_start`/`compaction_end`
  bracket an in-flight compaction — pi is rewriting the session history, so an
  injected message could be lost or land in the wrong place. The post-turn wake
  recovers the notice.
- **Steer racing `agent_settled`**: a `steer` that lands after the drain loop
  exited is accepted by the (never-closed) queue and silently dropped; a
  `success: false` response is Debug-logged and ignored. The durable cursor +
  `BeginSession` wake is the source of truth, so no message is ever lost.
- **Queue full**: `Steer` rejects immediately (non-blocking) — the receive pump
  is never stalled; the wake fallback applies.
- **Non-pi runtimes (ACP)**: they do not implement `steerer`, so behavior is
  unchanged (new messages wait for the next turn).

## Tests

`fakepi_test.go` extends the fake pi subprocess with three new modes and a
steer log:

- `steer` / `steer-fail`: accept the prompt, block until a `steer` command,
  reply `success` / `success:false`, then settle;
- `compact`: emit `compaction_start`, wait briefly for a steer (which the
  executor must suppress), then emit `compaction_end` + `agent_settled`;
- `fakePiSteersFile` records every accepted steer for assertions.

`lifecycle_test.go` (same-turn steering block):

| Test | Proves |
|---|---|
| `TestPiExecutor_SteerDeliveredMidTurn` | a steer reaches the running turn; exactly one steer logged; turn extends until the steered work is processed |
| `TestPiExecutor_SteerFailureDoesNotBlockTurn` | a `success:false` rejection does not fail or block the turn |
| `TestPiExecutor_SteerSuppressedDuringCompaction` | steering is suppressed between `compaction_start`/`compaction_end` |
| `TestPiExecutor_SteerAfterSettledNoBlock` | a steer racing `agent_settled` (post-drain-loop) returns immediately, never reaches the subprocess, and does not poison the next turn |
| `TestPiExecutor_SteerQueueFullRejectsNonBlocking` | the (capacity+1)-th steer is rejected immediately with "queue full"; a drained slot restores acceptance |
| `TestPiExecutor_SteerAfterCancelNoBlock` | a steer after `Cancel` is safe: no panic, no block, nothing delivered |

`command_stream_test.go` covers `TestBuildSteerNotice` (all three notice
variants); `pi_test.go` asserts both the cold init prompt and the re-anchor
prompt carry the steering instruction.

## Non-goals / unchanged

- Manager `BeginSession`/cursor logic: steering is an in-turn wake; the agent
  still consumes messages via `laelia-machine message check`/`thread check` +
  `AckProcessedVersion` (the durable cursor stays the source of truth).
- pi version, RPC protocol, session persistence/resume.
- ACP runtime: out of scope (no mid-turn injection).
