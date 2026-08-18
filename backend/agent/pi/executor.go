package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// PiExecutor implements executor.Runtime over a long-lived pi.Session. One
// PiExecutor is created per turn; it borrows the shared session (which outlives
// the turn), opens a per-turn event channel via beginTurn, sends a prompt, and
// drains pi events into executor.Events/OutputChunks until the turn settles.
//
//nolint:revive // stutter: mirrors executor.ACPExecutor sibling for symmetry.
type PiExecutor struct {
	cfg      *PiConfig
	req      executor.Request
	session  *Session
	identity string

	ctx    context.Context
	cancel context.CancelFunc

	outputCh chan executor.OutputChunk
	eventCh  chan executor.Event
	resultCh chan executor.Result
	done     chan struct{}

	startedAt time.Time
	seqNo     atomic.Int32
	cancelled atomic.Bool

	// startTokens is the session-cumulative token usage sampled at turn start;
	// the per-command TOKEN_USAGE event is the turn-end sample minus this.
	startTokens *sessionTokens

	toolCallCount atomic.Int32
	outputLimited atomic.Bool
	eventLimited  atomic.Bool
	outputBytes   atomic.Int64

	// steerCh carries same-turn steering notices from the receive pump (via
	// Steer) to the drain loop, which forwards them to pi as `steer` commands.
	// Buffered and non-blocking: a full queue drops the notice and the
	// post-turn BeginSession wake is the durable fallback.
	steerCh chan string
	// compacting suppresses steering while a context compaction is in flight:
	// pi is rewriting the session history, so an injected message could be
	// lost or land in the wrong place. Set/cleared by the drain loop from
	// compaction_start/compaction_end events (same goroutine as the steerCh
	// case, so the atomic is only for Steer's external readers).
	compacting atomic.Bool

	// stdoutBuf accumulates text_delta content for the final summary when the
	// agent does not post its own reply (it normally does, via laelia-machine).
	stdoutBuf strings.Builder

	// buffer batches STDOUT/SYSTEM text deltas into consolidated CommandOutput
	// chunks, mirroring executor.outputBuffer. pi streams per-token text deltas;
	// without batching each token becomes its own command_output row (and, as the
	// timeline renders each chunk as a block-level div, its own line). LLM tokens
	// carry their own whitespace, so concatenating deltas before flushing
	// reproduces the original text exactly. Flushed on the byte threshold, a
	// 500ms tick, tool-call boundaries, and at finish.
	buffer outputBuffer

	// eventCounter caps structured events (separate from seqNo so ordering and
	// the cap do not conflate).
	eventCounter atomic.Int32

	// toolStarted tracks toolCallIds that have emitted STARTED so each emits
	// exactly one STARTED then one FINISHED.
	toolMu      sync.Mutex
	toolStarted map[string]bool
}

var _ executor.Runtime = (*PiExecutor)(nil)

// outputBufferSize bounds the in-flight channel between the executor and the
// stream pump. It mirrors executor.outputBufferSize; duplicated here to avoid
// exporting an internal constant from the executor package.
const outputBufferSize = 1024

// flushOutputInterval is the periodic buffer-flush cadence, mirroring
// executor.flushOutputInterval so pi's live-stream latency matches ACP's.
const flushOutputInterval = 500 * time.Millisecond

// usagePollInterval is how often a long turn re-samples pi's context usage
// (pi is pull-based; ACP pushes usage updates). A var so tests can shrink it.
var usagePollInterval = 60 * time.Second

// usagePollTimeout bounds a single get_session_stats round trip so a hung pi
// cannot delay a turn (the start-of-turn sample blocks before the prompt).
const usagePollTimeout = 5 * time.Second

// NewPi constructs a per-turn Runtime over the shared pi session. The session
// is started lazily on the first Start so the opening turn's command id seeds
// LAELIA_COMMAND.
func NewPi(req executor.Request, sess *Session, cfg *PiConfig) (executor.Runtime, error) {
	if cfg == nil {
		return nil, errors.New("pi: config not provided")
	}
	if sess == nil {
		return nil, errors.New("pi: session not provided")
	}

	timeout := int32(cfg.MaxTimeoutSeconds)
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < timeout {
		timeout = req.TimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)

	identity := req.AgentDisplayName
	if identity == "" {
		identity = req.AgentResourceID
	}

	return &PiExecutor{
		cfg:         cfg,
		req:         req,
		session:     sess,
		identity:    identity,
		ctx:         ctx,
		cancel:      cancel,
		outputCh:    make(chan executor.OutputChunk, outputBufferSize),
		eventCh:     make(chan executor.Event, outputBufferSize),
		resultCh:    make(chan executor.Result, 1),
		done:        make(chan struct{}),
		steerCh:     make(chan string, 8),
		toolStarted: map[string]bool{},
	}, nil
}

func (e *PiExecutor) Start() {
	go e.run()
}

func (e *PiExecutor) Cancel() {
	e.cancelled.Store(true)
	e.cancel()
	// e.cancel() tears down only this turn's ctx; the session ctx is independent
	// (s.ctx, derived from Background by the runner), so abort is fire-and-forget
	// and the session process stays alive for the next turn.
	e.session.abort()
}

// Steer delivers a notice into the running turn. It is non-blocking and
// best-effort: the notice is queued to the drain loop, which forwards it to pi
// as a `steer` command (suppressed during compaction). A full queue or a
// rejected steer is dropped — the caller's post-turn wake (BeginSession) is the
// durable fallback, so Steer never blocks the receive pump.
func (e *PiExecutor) Steer(text string) error {
	select {
	case e.steerCh <- text:
		return nil
	default:
		return errors.New("pi: steer queue full")
	}
}

func (e *PiExecutor) OutputChannel() <-chan executor.OutputChunk { return e.outputCh }
func (e *PiExecutor) EventChannel() <-chan executor.Event        { return e.eventCh }
func (e *PiExecutor) ResultChannel() <-chan executor.Result      { return e.resultCh }
func (e *PiExecutor) Done() <-chan struct{}                      { return e.done }

// run drives one turn: ensure the session is live, send the prompt, and pump
// events to the manager until the agent settles (or the turn times out / is
// cancelled). The session itself is not torn down here — it persists for the
// next turn.
func (e *PiExecutor) run() {
	e.startedAt = time.Now()
	defer close(e.outputCh)
	defer close(e.eventCh)
	defer close(e.resultCh)
	defer close(e.done)
	defer e.cancel()
	defer e.buffer.flush(e)

	// Periodic flush so buffered text reaches the stream even when the agent
	// emits slowly (the byte threshold alone would stall until enough deltas
	// accumulate). Exits when the turn ctx is cancelled below.
	go e.startFlushTimer()

	// Lazy-start: the first turn seeds LAELIA_COMMAND with its command id. A
	// session that died between turns is restarted the same way. ensureStarted
	// also waits out any in-progress idle eviction and claims the process so the
	// turn runs on a live subprocess. The session binds the subprocess to its own
	// ctx (independent of this turn's ctx), so the deferred e.cancel() below
	// tears down the turn but leaves the process alive for the next turn.
	if err := e.session.ensureStarted(e.req.CommandID); err != nil {
		e.finish(err, false)
		return
	}

	resumed := e.session.IsWarm()

	// Sample context usage at turn start (pi is pull-based, unlike ACP's
	// pushed UsageUpdate) and keep sampling during long turns. A failed sample
	// never blocks the turn. The token snapshot is the baseline for the
	// per-command TOKEN_USAGE delta emitted at finish.
	e.emitSessionUsage()
	e.startTokens = e.sampleTokens()
	e.startUsagePoller()

	events := e.session.beginTurn(e.ctx)
	defer e.session.endTurn()

	promptText := e.turnPromptText(resumed)
	if promptText == "" {
		// Defensive: a warm turn should always carry a batch. Persist the
		// session and finish cleanly so the drain loop re-gates.
		e.finish(nil, resumed)
		return
	}

	if err := e.session.prompt(e.ctx, promptText); err != nil {
		e.finish(err, resumed)
		return
	}
	// A cold prompt (init prompt + batch) is now in the session history; mark
	// the session primed so subsequent turns on this process are warm.
	if !resumed {
		e.session.MarkPrimed()
	}

	settled := false
	for !settled {
		select {
		case ev, ok := <-events:
			if !ok {
				// Event channel closed (session died mid-turn). Finish with what
				// we have.
				e.finish(errors.New("pi: session exited mid-turn"), resumed)
				return
			}
			settled = e.handleEvent(ev)
		case text := <-e.steerCh:
			// Same-turn steering: forward the notice to pi, which queues it and
			// delivers it after the current assistant turn's tool calls, before
			// the next LLM call — the turn naturally extends until the steered
			// work is processed (agent_settled only fires when fully settled).
			// Suppressed during compaction (pi is rewriting the session
			// history); the post-turn wake fallback recovers the notice.
			if e.compacting.Load() {
				continue
			}
			if err := e.session.steer(e.ctx, text); err != nil {
				// Best-effort: a steer racing agent_settled (or a wedged pi)
				// fails here; the wake fallback picks the messages up next turn.
				slog.Debug("pi: steer failed; post-turn wake is the fallback", "error", err)
			}
		case <-e.ctx.Done():
			err := e.ctx.Err()
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("pi: turn timed out")
			}
			e.finish(err, resumed)
			return
		}
	}

	e.finish(nil, resumed)
}

// emitSessionUsage polls pi's current context-window usage and forwards it as a
// CONTEXT_USAGE_UPDATE event. Failures are non-fatal: the turn proceeds without
// an observation (the next poll or turn retries).
func (e *PiExecutor) emitSessionUsage() {
	ctx, cancel := context.WithTimeout(e.ctx, usagePollTimeout)
	defer cancel()
	stats, err := e.session.sessionStats(ctx)
	if err != nil {
		slog.Debug("pi: get_session_stats failed; skipping usage observation", "error", err)
		return
	}
	event := usageEventFromStats(stats)
	if event == nil {
		return
	}
	e.sendEvent(*event)
}

// sampleTokens fetches the session's cumulative token usage, or nil when the
// stats call fails or pi does not report tokens. It uses a fresh background
// context so the finish-time sample still works after the turn ctx is done
// (e.g. timeout/cancel paths).
func (e *PiExecutor) sampleTokens() *sessionTokens {
	ctx, cancel := context.WithTimeout(context.Background(), usagePollTimeout)
	defer cancel()
	stats, err := e.session.sessionStats(ctx)
	if err != nil {
		slog.Debug("pi: get_session_stats failed; skipping token usage", "error", err)
		return nil
	}
	return stats.Tokens
}

// emitTokenUsage samples the session's cumulative token usage at turn end and
// emits a TOKEN_USAGE event carrying the per-command delta (turn-end minus the
// turn-start snapshot). Failures are non-fatal: the turn result is unaffected.
func (e *PiExecutor) emitTokenUsage() {
	usage := tokenUsageDelta(e.startTokens, e.sampleTokens())
	if usage == nil {
		return
	}
	event := executor.Event{
		Type:       v1pb.CommandEventType_TOKEN_USAGE,
		Summary:    fmt.Sprintf("Tokens: %d total (%d in / %d out)", usage.TotalTokens, usage.InputTokens, usage.OutputTokens),
		TokenUsage: usage,
	}
	if !e.allowEvent() {
		return
	}
	event.SeqNo = e.nextSeq()
	event.Timestamp = time.Now()
	// Send without the turn-ctx gate: finish() runs after the turn ctx is done
	// on timeout/cancel paths, and the usage must still reach the manager. The
	// pump drains the channel until the result arrives, so a full buffer is
	// unlikely; drop rather than block the terminal result.
	select {
	case e.eventCh <- event:
	default:
		slog.Debug("pi: token usage event dropped (turn channel full)")
	}
}

// tokenUsageDelta computes the per-command token consumption as the difference
// between the turn-end and turn-start session snapshots. A nil snapshot on
// either side yields nil (no baseline to diff against). Negative deltas are
// clamped to zero so a stats reset (e.g. session switch) never reports
// negative usage.
func tokenUsageDelta(start, end *sessionTokens) *v1pb.TokenUsagePayload {
	if start == nil || end == nil {
		return nil
	}
	clamp := func(v int64) int64 {
		if v < 0 {
			return 0
		}
		return v
	}
	return &v1pb.TokenUsagePayload{
		InputTokens:      clamp(end.Input - start.Input),
		OutputTokens:     clamp(end.Output - start.Output),
		CacheReadTokens:  clamp(end.CacheRead - start.CacheRead),
		CacheWriteTokens: clamp(end.CacheWrite - start.CacheWrite),
		TotalTokens:      clamp(end.Total - start.Total),
	}
}

// startUsagePoller re-samples pi usage on a fixed cadence until the turn ends,
// so the context-usage bar stays live during long turns.
func (e *PiExecutor) startUsagePoller() {
	ticker := time.NewTicker(usagePollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.emitSessionUsage()
			case <-e.ctx.Done():
				return
			}
		}
	}()
}

// usageEventFromStats maps a get_session_stats payload to a
// CONTEXT_USAGE_UPDATE event, or nil when no valid context-window usage is
// available (contextUsage omitted, or tokens/percent null right after a
// compaction). pi's displayed percent is preferred for the ratio when present;
// otherwise it is derived from tokens/contextWindow.
func usageEventFromStats(stats *sessionStatsData) *executor.Event {
	if stats == nil || stats.ContextUsage == nil {
		return nil
	}
	cu := stats.ContextUsage
	if cu.ContextWindow == nil || *cu.ContextWindow <= 0 {
		return nil
	}
	size := *cu.ContextWindow
	used := int64(0)
	ratio := float64(0)
	if cu.Tokens != nil && *cu.Tokens >= 0 {
		used = *cu.Tokens
	}
	if cu.Percent != nil && *cu.Percent >= 0 {
		ratio = *cu.Percent / 100
		if used == 0 {
			used = int64(math.Round(ratio * float64(size)))
		}
	} else if used > 0 {
		ratio = float64(used) / float64(size)
	} else {
		return nil
	}
	return &executor.Event{
		Type:    v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
		Summary: fmt.Sprintf("Context usage: %d/%d tokens", used, size),
		ContextUsage: &v1pb.ContextUsagePayload{
			Size:       size,
			Used:       used,
			UsageRatio: ratio,
		},
	}
}

// handleEvent maps one pi event to executor output/events. Returns true when the
// event is terminal (agent_settled) so the pump exits.
func (e *PiExecutor) handleEvent(ev *event) bool {
	switch ev.Type {
	case eventMessageUpdate:
		e.handleMessageUpdate(ev)
	case eventToolExecutionStart:
		e.handleToolStart(ev)
	case eventToolExecutionEnd:
		e.handleToolEnd(ev)
	case eventAgentEnd:
		if ev.WillRetry {
			e.sendWarning(fmt.Sprintf("pi agent will retry: %s", strings.TrimSpace(ev.Reason)))
		}
	case eventCompactionStart:
		e.compacting.Store(true)
		e.sendEvent(executor.Event{
			Type:    v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED,
			Summary: "Context compaction started",
			ContextCompaction: &v1pb.ContextCompactionPayload{
				Reason: strings.TrimSpace(ev.Reason),
			},
		})
	case eventCompactionEnd:
		e.compacting.Store(false)
		e.sendEvent(executor.Event{
			Type:    v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
			Summary: "Context compaction finished",
			ContextCompaction: &v1pb.ContextCompactionPayload{
				Reason: strings.TrimSpace(ev.Reason),
			},
		})
	case eventAutoRetryStart, eventAutoRetryEnd:
		e.sendWarning("pi auto-retry: " + strings.TrimSpace(ev.Reason))
	case eventExtensionError:
		e.sendWarning("pi extension error: " + strings.TrimSpace(ev.ErrorMessage))
	case eventAgentStart:
		// informational; no event emitted.
	case eventAgentSettled:
		return true
	default:
		// Unknown event type: ignore. pi may add event variants in future
		// versions; the drain loop must not choke on them.
	}
	return false
}

func (e *PiExecutor) handleMessageUpdate(ev *event) {
	ame := ev.AssistantMessageEvent
	if ame == nil {
		return
	}
	switch ame.Type {
	case assistantEventTextDelta:
		if ame.Delta == "" {
			return
		}
		_, _ = e.stdoutBuf.WriteString(ame.Delta)
		e.buffer.append(v1pb.CommandOutput_STDOUT, ame.Delta)
		e.flushIfNeeded()
	case assistantEventThinkingDelta:
		if ame.Delta == "" {
			return
		}
		e.buffer.append(v1pb.CommandOutput_ASSISTANT, ame.Delta)
		e.flushIfNeeded()
	case assistantEventDone:
		// message complete; nothing to emit.
	case assistantEventError:
		msg := strings.TrimSpace(ame.Reason)
		if msg == "" {
			msg = "pi assistant message error"
		}
		e.sendWarning(msg)
	default:
		// Unknown assistant-message event variant: ignore.
	}
}

func (e *PiExecutor) handleToolStart(ev *event) {
	if ev.ToolCallID == "" {
		return
	}
	e.toolMu.Lock()
	if e.toolStarted[ev.ToolCallID] {
		e.toolMu.Unlock()
		return
	}
	e.toolStarted[ev.ToolCallID] = true
	e.toolMu.Unlock()

	e.toolCallCount.Add(1)
	title := deriveToolTitle(ev.ToolName, ev.Args)
	// Flush buffered text so it streams before the tool card interleaves.
	e.buffer.flush(e)
	e.sendEvent(executor.Event{
		Type:    v1pb.CommandEventType_TOOL_CALL_STARTED,
		Summary: title,
		ToolCallStarted: &v1pb.ToolCallStartedPayload{
			Title:    title,
			RawInput: rawJSONToStruct(ev.Args),
		},
	})
}

func (e *PiExecutor) handleToolEnd(ev *event) {
	if ev.ToolCallID == "" {
		return
	}
	status := "success"
	if ev.IsError {
		status = "error"
	}
	e.buffer.flush(e)
	e.sendEvent(executor.Event{
		Type:    v1pb.CommandEventType_TOOL_CALL_FINISHED,
		Summary: status,
		ToolCallFinished: &v1pb.ToolCallFinishedPayload{
			Status:    status,
			RawOutput: rawJSONToStruct(ev.Result),
		},
	})
}

func (e *PiExecutor) sendOutput(streamType v1pb.CommandOutput_StreamType, content string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return
	}
	allowed, ok := e.limitOutput(trimmed)
	if !ok {
		return
	}
	chunk := executor.OutputChunk{StreamType: streamType, Content: allowed, SeqNo: e.nextSeq(), Timestamp: timestamppb.New(time.Now())}
	select {
	case e.outputCh <- chunk:
	case <-e.ctx.Done():
	}
}

// outputBuffer accumulates STDOUT/SYSTEM text deltas and flushes them as
// consolidated CommandOutput chunks. Mirrors executor.outputBuffer; duplicated
// here because that type is unexported. See the PiExecutor.buffer field doc for
// why batching is required.
type outputBuffer struct {
	mu        sync.Mutex
	stdout    strings.Builder
	system    strings.Builder
	assistant strings.Builder
	order     []v1pb.CommandOutput_StreamType
}

func (b *outputBuffer) append(streamType v1pb.CommandOutput_StreamType, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch streamType {
	case v1pb.CommandOutput_STDOUT:
		if b.stdout.Len() == 0 {
			b.order = append(b.order, streamType)
		}
		_, _ = b.stdout.WriteString(text)
	case v1pb.CommandOutput_SYSTEM:
		if b.system.Len() == 0 {
			b.order = append(b.order, streamType)
		}
		_, _ = b.system.WriteString(text)
	case v1pb.CommandOutput_ASSISTANT:
		if b.assistant.Len() == 0 {
			b.order = append(b.order, streamType)
		}
		_, _ = b.assistant.WriteString(text)
	default:
		// Other stream types (STDERR) are sent directly, not buffered.
	}
}

func (b *outputBuffer) totalLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.Len() + b.system.Len() + b.assistant.Len()
}

func (b *outputBuffer) flush(e *PiExecutor) {
	b.mu.Lock()
	stdout := b.stdout.String()
	b.stdout.Reset()
	system := b.system.String()
	b.system.Reset()
	assistant := b.assistant.String()
	b.assistant.Reset()
	order := b.order
	b.order = b.order[:0]
	b.mu.Unlock()

	for _, st := range order {
		switch st {
		case v1pb.CommandOutput_STDOUT:
			if stdout != "" {
				e.sendOutput(v1pb.CommandOutput_STDOUT, stdout)
				stdout = ""
			}
		case v1pb.CommandOutput_SYSTEM:
			if system != "" {
				e.sendOutput(v1pb.CommandOutput_SYSTEM, system)
				system = ""
			}
		case v1pb.CommandOutput_ASSISTANT:
			if assistant != "" {
				e.sendOutput(v1pb.CommandOutput_ASSISTANT, assistant)
				assistant = ""
			}
		default:
		}
	}
}

// flushIfNeeded drains the buffer once it crosses the configured byte threshold.
func (e *PiExecutor) flushIfNeeded() {
	if e.buffer.totalLen() >= int(e.cfg.OutputFlushBytes) {
		e.buffer.flush(e)
	}
}

// startFlushTimer emits any buffered text on a fixed interval so a slow stream
// still reaches the UI before the threshold is reached. Exits when the turn ctx
// is cancelled (run's deferred cancel).
func (e *PiExecutor) startFlushTimer() {
	ticker := time.NewTicker(flushOutputInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.buffer.flush(e)
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *PiExecutor) sendEvent(event executor.Event) {
	if !e.allowEvent() {
		return
	}
	event.SeqNo = e.nextSeq()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	select {
	case e.eventCh <- event:
	case <-e.ctx.Done():
	}
}

func (e *PiExecutor) sendWarning(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	e.sendEvent(executor.Event{
		Type:    v1pb.CommandEventType_WARNING,
		Summary: message,
		Warning: &v1pb.WarningPayload{Message: message},
	})
}

func (e *PiExecutor) allowEvent() bool {
	if e.cfg.MaxEventCount <= 0 {
		return true
	}
	count := e.eventCounter.Add(1)
	if count <= e.cfg.MaxEventCount {
		return true
	}
	if e.eventLimited.CompareAndSwap(false, true) {
		e.sendOutput(v1pb.CommandOutput_SYSTEM, "pi event limit reached; dropping further structured events")
	}
	return false
}

func (e *PiExecutor) limitOutput(content string) (string, bool) {
	if e.cfg.MaxOutputBytes <= 0 {
		return content, true
	}
	used := e.outputBytes.Load()
	remaining := e.cfg.MaxOutputBytes - used
	if remaining <= 0 {
		if e.outputLimited.CompareAndSwap(false, true) {
			return "pi output limit reached; dropping further text output", true
		}
		return "", false
	}
	if int64(len(content)) <= remaining {
		e.outputBytes.Add(int64(len(content)))
		return content, true
	}
	truncated := content[:remaining]
	e.outputBytes.Store(e.cfg.MaxOutputBytes)
	e.outputLimited.Store(true)
	return truncated, true
}

func (e *PiExecutor) nextSeq() int32 { return e.seqNo.Add(1) }

// piSteeringPrompt tells the agent that new messages can arrive mid-turn as a
// short notice and how to react. It is appended to the cold init prompt and the
// re-anchor prompt (both pi-only paths; ACP never sees it).
const piSteeringPrompt = `While you are working, new messages may be delivered into your current turn as a short notice (e.g. "[Laelia inbox notice: ...]"). When you see one, run ` + "`laelia-machine message check`" + ` (or ` + "`laelia-machine thread check`" + ` if the notice mentions a thread reply) at a natural breakpoint and process the new messages before ending your turn.`

// piWindowsPowerShellPrompt tells pi agents on Windows that the "bash" tool is
// actually PowerShell 5.1, so they use PowerShell syntax instead of Bash
// heredocs/Unix-only commands.
const piWindowsPowerShellPrompt = `On this Windows machine, the "bash" tool is a compatibility name: it executes native Windows PowerShell 5.1. Use PowerShell syntax (e.g. here-strings, not Bash heredocs) and avoid Unix-only commands.`

// turnPromptText mirrors acp_executor.turnPromptText: cold turn sends the full
// init prompt (identity + persona + communication + procedure + memory) plus
// the batch; warm turn sends only the batch.
func (e *PiExecutor) turnPromptText(resumed bool) string {
	batch := strings.TrimSpace(e.req.TurnPrompt)
	windowsNote := ""
	if runtime.GOOS == "windows" {
		windowsNote = piWindowsPowerShellPrompt
	}
	if resumed {
		anchor := strings.TrimSpace(e.req.ReanchorPrompt)
		if anchor == "" {
			return batch
		}
		parts := []string{anchor}
		if windowsNote != "" {
			parts = append(parts, windowsNote)
		}
		parts = append(parts, piSteeringPrompt)
		if batch != "" {
			parts = append(parts, batch)
		}
		return strings.Join(parts, "\n\n")
	}
	initPrompt := executor.BuildPrompt(e.identity, e.req.OwnerDisplayName, e.cfg.PersonaPrompt)
	parts := []string{initPrompt}
	if windowsNote != "" {
		parts = append(parts, windowsNote)
	}
	parts = append(parts, piSteeringPrompt)
	if batch != "" {
		parts = append(parts, batch)
	}
	return strings.Join(parts, "\n\n")
}

// finish flushes and emits the terminal FinalSummary event and Result, then
// returns so run() can close the channels.
func (e *PiExecutor) finish(err error, resumed bool) {
	// Flush any buffered tail text before the terminal summary so it streams in
	// order (the deferred flush in run is a no-op safety net after this).
	e.buffer.flush(e)

	// Emit the per-command token usage before the terminal summary/result so
	// the detail page can show it once the command completes.
	e.emitTokenUsage()

	sessionID := e.session.SessionFile()

	finalSummary := strings.TrimSpace(e.stdoutBuf.String())
	exitCode := int32(0)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		if executor.ClassifyInputTooLarge(err) {
			errMsg = strings.TrimRight(errMsg, "\n") + "\n\n" + executor.InputTooLargeGuidance
		}
		exitCode = 1
		if errors.Is(err, context.DeadlineExceeded) {
			exitCode = 124
		}
	}
	if finalSummary == "" {
		if err != nil {
			finalSummary = errMsg
		} else {
			finalSummary = "pi task finished"
		}
	}

	resultPayload, payloadErr := structpb.NewStruct(map[string]any{
		"executor_kind":   "PI",
		"executable":      e.cfg.PiBinaryPath,
		"session_id":      sessionID,
		"stop_reason":     "end_turn",
		"agent_name":      e.identity,
		"tool_call_count": e.toolCallCount.Load(),
		"output_limited":  e.outputLimited.Load(),
		"event_limited":   e.eventLimited.Load(),
	})
	if payloadErr != nil {
		resultPayload = nil
	}

	e.sendEvent(executor.Event{
		Type:    v1pb.CommandEventType_FINAL_SUMMARY,
		Summary: finalSummary,
		FinalSummary: &v1pb.FinalSummaryPayload{
			StopReason: "end_turn",
			SessionId:  sessionID,
		},
	})

	e.resultCh <- executor.Result{
		ExitCode:     exitCode,
		DurationMs:   time.Since(e.startedAt).Milliseconds(),
		ErrorMessage: errMsg,
		FinalSummary: finalSummary,
		Result:       resultPayload,
		SessionID:    sessionID,
		Resumed:      resumed,
		Fingerprint:  piFingerprint(e.cfg),
	}
}

// deriveToolTitle builds the tool-card title from the tool name + its args, so
// the card shows the operand directly (e.g. the bash command) instead of just
// "bash" requiring a click to expand. This mirrors how opencode sets the ACP
// ToolCall title to the bash command. For a tool whose args carry a "command"
// field (bash/shell), the command is the title; for read/edit the path is
// appended; otherwise the tool name is used. The full args remain in the
// expanded rawInput regardless.
func deriveToolTitle(toolName string, args json.RawMessage) string {
	name := toolName
	if name == "" {
		name = "tool"
	}
	if len(args) == 0 {
		return name
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return name
	}
	if cmd := stringField(m, "command"); cmd != "" {
		return cmd
	}
	if p := stringField(m, "path", "file_path", "file"); p != "" {
		return name + " " + p
	}
	return name
}

// stringField returns the first non-empty string value among the given keys in
// m, or "" if none match.
func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					return s
				}
			}
		}
	}
	return ""
}

// rawJSONToStruct decodes a pi event's json.RawMessage field (args/result) into
// a protobuf Struct, returning nil for empty/invalid payloads so the frontend
// renders its "not captured" fallback instead of an empty object.
func rawJSONToStruct(raw json.RawMessage) *structpb.Struct {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		slog.Debug("pi: undecodable raw payload", "raw", string(raw), "error", err)
		return nil
	}
	if v == nil {
		return nil
	}
	if s, ok := v.(map[string]any); ok && len(s) == 0 {
		return nil
	}
	st, err := structpb.NewStruct(map[string]any{"value": v})
	if err != nil {
		return nil
	}
	return st
}
