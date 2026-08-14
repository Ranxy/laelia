package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/acp2"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// ThreadConfig is the fully-resolved configuration for the thread executor.
// It carries the shared limit fields from ACPConfig plus the v2-specific
// inputs (model, developer instructions, MCP servers). It is never
// user-authored: BuildThreadConfig derives it from the resolved ACPConfig.
type ThreadConfig struct {
	MaxTimeoutSeconds int32
	MaxEventCount     int32
	MaxOutputBytes    int64
	OutputFlushBytes  int32
	// StartupTimeout bounds the Initialize + thread/start|resume handshake,
	// mirroring ACPConfig.StartupTimeout.
	StartupTimeout time.Duration
	// IdleTimeout is how long a resident ThreadSession subprocess stays alive
	// after its last turn before idle eviction. Zero disables eviction. Only
	// the resident (session) mode reads it; the per-turn executor ignores it.
	IdleTimeout time.Duration

	Provider      string
	Model         string
	WorkingDir    string
	PersonaPrompt string
	// Protocol is the declared ACP protocol generation ("acp-v2"); empty
	// defaults to acp-v2 (the thread executor only runs the v2 protocol).
	Protocol string
	// DeveloperInstructions is passed to thread/start as the thread-level
	// system instructions. Empty in the first version; the plumbing exists for
	// future agent-specific tuning.
	DeveloperInstructions string
	Env                   map[string]string
	CustomEnv             map[string]string
	AllowEnv              []string
	McpServers            []acp.McpServer
	SupportsRawEvents     bool
}

// BuildThreadConfig derives the thread executor config from the resolved ACP
// config, carrying over the shared limit fields and the v2-specific inputs.
func BuildThreadConfig(cfg *ACPConfig) *ThreadConfig {
	if cfg == nil {
		return nil
	}
	return &ThreadConfig{
		MaxTimeoutSeconds: cfg.MaxTimeoutSeconds,
		MaxEventCount:     cfg.MaxEventCount,
		MaxOutputBytes:    cfg.MaxOutputBytes,
		OutputFlushBytes:  cfg.OutputFlushBytes,
		StartupTimeout:    cfg.StartupTimeout,
		Provider:          cfg.Provider,
		Model:             cfg.Model,
		WorkingDir:        cfg.WorkingDir,
		PersonaPrompt:     cfg.PersonaPrompt,
		Protocol:          cfg.Protocol,
		Env:               cfg.Env,
		CustomEnv:         cfg.CustomEnv,
		AllowEnv:          cfg.AllowEnv,
		McpServers:        cfg.McpServers,
		SupportsRawEvents: cfg.SupportsRawEvents,
	}
}

// ThreadExecutor implements executor.Runtime over the ACP v2 thread protocol.
// Each turn spawns a fresh app-server subprocess; a cold turn starts a new
// thread, a warm turn resumes the persisted thread id (the provider persists
// thread state server-side, so the thread survives process restarts). The
// provider's EventMapper narrows notifications into neutral acp2 events,
// which this executor maps onto the laelia event surface.
type ThreadExecutor struct {
	ctx    context.Context
	cancel context.CancelFunc
	req    Request
	cfg    *ThreadConfig
	p      provider.ThreadProvider
	// sess, when non-nil, makes this executor drive its turn over a shared
	// resident ThreadSession instead of spawning a fresh app-server.
	sess *ThreadSession
	cmd  *exec.Cmd
	// client is the live protocol client. It is stored atomically because the
	// command stream's receive pump may Steer() from another goroutine while
	// run() owns the read loop.
	client atomic.Pointer[acp2.Client]
	gate   *acp2.TurnGate

	outputCh chan OutputChunk
	eventCh  chan Event
	resultCh chan Result
	done     chan struct{}

	seqNo         atomic.Int32
	startedAt     time.Time
	outputBytes   atomic.Int64
	eventCount    atomic.Int32
	outputLimited atomic.Bool
	eventLimitHit atomic.Bool
	buffer        outputBuffer

	summaryMu      sync.Mutex
	summaryText    string
	turnError      string
	threadID       string
	fingerprint    string
	resumeFailures int
	resumed        bool
}

var _ Runtime = (*ThreadExecutor)(nil)
var _ SteerResolver = (*ThreadExecutor)(nil)

// NewThread constructs a per-turn thread executor driven by the given
// ThreadProvider. The provider supplies the launch command and the EventMapper
// that translates its notification shapes.
// NewThread constructs a per-turn thread executor driven by the given
// ThreadProvider. The provider supplies the launch command and the EventMapper
// that translates its notification shapes.
func NewThread(req Request, cfg *ThreadConfig, p provider.ThreadProvider) (Runtime, error) {
	return newThreadExecutor(req, cfg, p, nil)
}

// NewThreadWithSession returns a per-turn thread executor that drives its turn
// over a shared resident ThreadSession instead of spawning a fresh app-server.
// The session owns the subprocess and the thread; the executor owns the turn's
// event surface, limits, and result. sess must be non-nil.
func NewThreadWithSession(req Request, cfg *ThreadConfig, p provider.ThreadProvider, sess *ThreadSession) (Runtime, error) {
	if sess == nil {
		return nil, errors.New("thread session is not configured on this agent")
	}
	return newThreadExecutor(req, cfg, p, sess)
}

func newThreadExecutor(req Request, cfg *ThreadConfig, p provider.ThreadProvider, sess *ThreadSession) (Runtime, error) {
	if cfg == nil {
		return nil, errors.New("thread protocol is not configured on this agent")
	}
	if p == nil {
		return nil, errors.New("thread provider is not configured on this agent")
	}
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 || timeoutSeconds > cfg.MaxTimeoutSeconds {
		timeoutSeconds = cfg.MaxTimeoutSeconds
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	return &ThreadExecutor{
		ctx:      ctx,
		cancel:   cancel,
		req:      req,
		cfg:      cfg,
		p:        p,
		sess:     sess,
		gate:     acp2.NewTurnGate(),
		outputCh: make(chan OutputChunk, outputBufferSize),
		eventCh:  make(chan Event, outputBufferSize),
		resultCh: make(chan Result, 1),
		done:     make(chan struct{}),
	}, nil
}

func (e *ThreadExecutor) Start() { go e.run() }

func (e *ThreadExecutor) Cancel() {
	e.cancel()
	if e.sess != nil {
		// Resident mode: kill the shared process so the server cannot keep an
		// in-flight turn; the session ctx stays alive so the next turn
		// respawns and resumes the persisted thread.
		e.sess.Kill()
		return
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = KillGroup(e.cmd, syscall.SIGKILL)
	}
}

// Steer implements executor.SteerResolver over the v2 thread protocol's
// turn/steer. It is a no-op when the client is not up yet or the turn gate
// reports that steering is unsafe (no active turn, a turn request still
// pending, or the post-tool window where the server would reject the steer).
func (e *ThreadExecutor) Steer(text string) {
	if text == "" {
		return
	}
	client := e.client.Load()
	if client == nil || !e.gate.CanSteerBusy() {
		slog.Debug("steer ignored: no steerable turn in flight", "agent", e.req.AgentID)
		return
	}
	turnID := e.gate.ActiveTurnID()
	if turnID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	turn, err := client.SteerTurn(ctx, e.threadID, turnID, text)
	if err != nil {
		slog.Warn("steer failed", "agent", e.req.AgentID, "turnID", turnID, "error", err)
		return
	}
	e.gate.MarkNonEmptyInput(turn.ResolvedID())
	slog.Info("steered in-flight turn", "agent", e.req.AgentID, "turnID", turnID)
}

func (e *ThreadExecutor) OutputChannel() <-chan OutputChunk { return e.outputCh }
func (e *ThreadExecutor) EventChannel() <-chan Event        { return e.eventCh }
func (e *ThreadExecutor) ResultChannel() <-chan Result      { return e.resultCh }
func (e *ThreadExecutor) Done() <-chan struct{}             { return e.done }

// run drives one turn: spawn the app-server, handshake, start or resume the
// thread, start the turn, and pump mapped events until the turn completes.
func (e *ThreadExecutor) run() {
	e.startedAt = time.Now()
	defer close(e.outputCh)
	defer close(e.eventCh)
	defer close(e.resultCh)
	defer close(e.done)
	defer e.cancel()

	if e.sess != nil {
		e.runSessionTurn()
		return
	}
	e.runPerTurn()
}

// runPerTurn drives one turn with a freshly spawned app-server subprocess: a
// cold turn starts a new thread, a warm turn resumes the persisted thread id
// (the provider persists thread state server-side, so the thread survives
// process restarts).
func (e *ThreadExecutor) runPerTurn() {
	cmd, client, stderr, err := spawnThreadAppServer(e.ctx, e.req, e.cfg, e.p)
	if err != nil {
		e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: err.Error()})
		return
	}
	e.cmd = cmd
	e.client.Store(client)
	defer client.Close()
	go e.scanStderr(stderr)
	go e.startFlushTimer()

	// The startup handshake (Initialize + thread/start|resume) is bounded by
	// its own timeout, NOT the turn ctx: a server that spawns but never
	// completes the handshake is failed fast at ~StartupTimeout instead of
	// hanging to MaxTimeoutSeconds. The turn/start call below stays on e.ctx so
	// a slow turn still respects the turn timeout.
	startupTimeout := e.cfg.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = defaultACPStartupTimeout
	}
	startupCtx, cancelStartup := context.WithTimeout(e.ctx, startupTimeout)
	defer cancelStartup()

	threadID, resumed, resumeFailures, fingerprint, err := establishThread(startupCtx, e.req, e.cfg, client, e.sendEvent)
	if err != nil {
		e.finish(err)
		return
	}
	e.threadID = threadID
	e.resumed = resumed
	e.resumeFailures = resumeFailures
	e.fingerprint = fingerprint

	promptText := e.turnPromptText(resumed)
	if promptText == "" {
		// Defensive: a warm turn should always carry a batch. If it does not,
		// do not start a turn — finish cleanly and let the drain loop re-gate.
		// The thread is already persisted for the next turn.
		_ = KillGroup(e.cmd, syscall.SIGKILL)
		_ = e.cmd.Wait()
		e.buffer.flush(e)
		e.sendResult(Result{
			ExitCode:     0,
			DurationMs:   time.Since(e.startedAt).Milliseconds(),
			FinalSummary: "no turn prompt; thread persisted",
			SessionID:    threadID,
			Resumed:      resumed,
		})
		return
	}

	turn, err := client.StartTurn(e.ctx, threadID, promptText)
	if err != nil {
		e.finish(err)
		return
	}
	e.gate.NoteTurnAccepted(turn.ResolvedID())

	e.pumpUntilTurnDone()

	_ = KillGroup(e.cmd, syscall.SIGKILL)
	_ = e.cmd.Wait()
	e.completeTurn(threadID, resumed)
}

// runSessionTurn drives one turn over a shared resident ThreadSession. The
// session owns the subprocess and the thread; this executor owns the turn's
// event surface, limits, and result.
func (e *ThreadExecutor) runSessionTurn() {
	sess := e.sess
	// Ensure the shared subprocess is up (spawn + handshake + thread
	// start/resume on the first turn or after an eviction; no-op when warm).
	if err := sess.EnsureStarted(e.sendEvent); err != nil {
		e.finish(err)
		return
	}
	e.client.Store(sess.Client())
	e.threadID = sess.ThreadID()
	e.resumed = sess.Warm()
	e.resumeFailures = sess.ResumeFailures()
	e.fingerprint = sess.ThreadFingerprint()

	promptText := e.turnPromptText(e.resumed)
	if promptText == "" {
		sess.EndTurn()
		e.buffer.flush(e)
		e.sendResult(Result{
			ExitCode:     0,
			DurationMs:   time.Since(e.startedAt).Milliseconds(),
			FinalSummary: "no turn prompt; thread persisted",
			SessionID:    e.threadID,
			Resumed:      e.resumed,
		})
		return
	}

	go e.startFlushTimer()

	turnID, err := sess.BeginTurn(e.ctx, promptText)
	if err != nil {
		// The server may still hold an in-flight turn; kill the process so the
		// next turn respawns cleanly and resumes the persisted thread.
		sess.Kill()
		sess.EndTurn()
		e.finish(err)
		return
	}
	e.gate.NoteTurnAccepted(turnID)

	e.pumpUntilTurnDone()

	if e.ctx.Err() != nil {
		// Turn ended by timeout/cancel: the server may still be running the
		// turn, so kill the process to un-wedge it; the next turn respawns.
		sess.Kill()
	}
	sess.EndTurn()
	e.completeTurn(e.threadID, e.resumed)
}

// completeTurn flushes the turn buffer and emits the terminal result. Shared
// by the per-turn and session-backed paths; the caller has already reaped (or
// handed back) the subprocess.
func (e *ThreadExecutor) completeTurn(threadID string, resumed bool) {
	e.buffer.flush(e)

	if errors.Is(e.ctx.Err(), context.DeadlineExceeded) {
		e.sendResult(Result{ExitCode: 124, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}
	if errors.Is(e.ctx.Err(), context.Canceled) {
		e.sendResult(Result{ExitCode: 130, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}

	finalSummary := strings.TrimSpace(e.finalSummary())
	if finalSummary == "" {
		finalSummary = "Thread task finished"
	}
	e.sendEvent(Event{
		Type:    v1pb.CommandEventType_FINAL_SUMMARY,
		Summary: finalSummary,
		FinalSummary: &v1pb.FinalSummaryPayload{
			StopReason: "turn_completed",
			SessionId:  threadID,
		},
	})
	resultPayload, payloadErr := structpb.NewStruct(map[string]any{
		"executor_kind":  "THREAD",
		"provider":       e.cfg.Provider,
		"thread_id":      threadID,
		"resumed":        resumed,
		"output_limited": e.outputLimited.Load(),
		"event_limited":  e.eventLimitHit.Load(),
	})
	if payloadErr != nil {
		resultPayload = nil
	}
	if e.turnError != "" {
		e.sendResult(Result{
			ExitCode:     1,
			DurationMs:   time.Since(e.startedAt).Milliseconds(),
			ErrorMessage: e.turnError,
			FinalSummary: finalSummary,
			Result:       resultPayload,
			SessionID:    threadID,
			Resumed:      resumed,
		})
		return
	}
	e.sendResult(Result{
		ExitCode:     0,
		DurationMs:   time.Since(e.startedAt).Milliseconds(),
		FinalSummary: finalSummary,
		Result:       resultPayload,
		SessionID:    threadID,
		Resumed:      resumed,
	})
}

// pumpUntilTurnDone drains mapped events until the current turn completes
// (turn/completed lifecycle), the process dies, or the turn ctx ends.
func (e *ThreadExecutor) pumpUntilTurnDone() {
	client := e.client.Load()
	if client == nil {
		return
	}
	for {
		select {
		case ev, ok := <-client.Events():
			if !ok {
				return
			}
			if e.handleEvent(ev) {
				return
			}
		case <-e.ctx.Done():
			return
		}
	}
}

// handleEvent narrows one neutral acp2 event onto the executor event surface
// and drives the turn gate. It returns true when the current turn completed.
func (e *ThreadExecutor) handleEvent(ev acp2.Event) bool {
	switch ev.Type {
	case acp2.EventLifecycle:
		switch ev.Text {
		case "turn_started":
			e.gate.MarkTurnStarted(ev.TurnID)
		case "turn_completed":
			e.gate.MarkTurnCompleted()
			return true
		default:
			// Other lifecycle frames (review_started/finished) carry no gate
			// transition; they still surface as LIFECYCLE events below.
		}
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_LIFECYCLE,
			Summary: ev.Text,
			Lifecycle: &v1pb.LifecyclePayload{
				ExecutorKind: "THREAD",
			},
		})
	case acp2.EventTextDelta:
		e.gate.MarkProgress()
		e.appendSummary(ev.Text)
		e.buffer.append(v1pb.CommandOutput_STDOUT, ev.Text)
		e.flushIfNeeded()
	case acp2.EventThinkingDelta:
		e.gate.MarkProgress()
		e.buffer.append(v1pb.CommandOutput_ASSISTANT, ev.Text)
		e.flushIfNeeded()
	case acp2.EventToolCallStarted:
		e.gate.MarkProgress()
		e.buffer.flush(e)
		title := ev.ToolCall.Title
		if title == "" {
			title = ev.ToolCall.Kind
		}
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_TOOL_CALL_STARTED,
			Summary: title,
			ToolCallStarted: &v1pb.ToolCallStartedPayload{
				Title:    title,
				RawInput: toolPayloadStruct(ev.ToolCall.Input),
			},
		})
	case acp2.EventToolCallFinished:
		e.gate.MarkToolBoundary()
		e.buffer.flush(e)
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_TOOL_CALL_FINISHED,
			Summary: ev.ToolCall.Status,
			ToolCallFinished: &v1pb.ToolCallFinishedPayload{
				Status:    ev.ToolCall.Status,
				RawOutput: toolPayloadStruct(ev.ToolCall.Output),
			},
		})
	case acp2.EventWarning:
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_WARNING,
			Summary: ev.Text,
			Warning: &v1pb.WarningPayload{Message: ev.Text},
		})
	case acp2.EventContextCompactionStarted:
		e.sendEvent(Event{
			Type:              v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED,
			Summary:           "context compaction started",
			ContextCompaction: &v1pb.ContextCompactionPayload{},
		})
	case acp2.EventContextCompactionFinished:
		e.sendEvent(Event{
			Type:              v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
			Summary:           "context compaction finished",
			ContextCompaction: &v1pb.ContextCompactionPayload{},
		})
	case acp2.EventContextUsageUpdate:
		e.gate.MarkTokenUsage()
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
			Summary: fmt.Sprintf("Context usage: %d/%d tokens", ev.ContextUsage.TotalTokens, ev.ContextUsage.ModelContextWindow),
			ContextUsage: &v1pb.ContextUsagePayload{
				Size:       ev.ContextUsage.ModelContextWindow,
				Used:       ev.ContextUsage.TotalTokens,
				UsageRatio: contextUsageRatio(ev.ContextUsage),
			},
		})
	case acp2.EventError:
		// The laelia event surface has no error type; surface the failure as a
		// warning and record it so the turn result fails with the message.
		e.turnError = ev.Text
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_WARNING,
			Summary: ev.Text,
			Warning: &v1pb.WarningPayload{Message: ev.Text},
		})
	case acp2.EventRaw:
		if e.cfg.SupportsRawEvents {
			e.sendEvent(Event{
				Type:    v1pb.CommandEventType_RAW_ACP,
				Summary: "raw",
				RawAcp:  &v1pb.RawAcpPayload{Data: toProtobufStruct(ev.Raw)},
			})
		}
	default:
		// Unknown event types carry no laelia event surface; ignore.
	}
	return false
}

// contextUsageRatio is used/size for the CONTEXT_USAGE_UPDATE payload.
func contextUsageRatio(u *acp2.ContextUsageInfo) float64 {
	if u == nil || u.ModelContextWindow <= 0 {
		return 0
	}
	return float64(u.TotalTokens) / float64(u.ModelContextWindow)
}

func (e *ThreadExecutor) turnPromptText(resumed bool) string {
	return turnPromptText(e.req, e.cfg.PersonaPrompt, resumed)
}

// finish tears down the subprocess and reports a failed turn. It is the
// failure path for handshake/start errors; the normal completion path in run()
// tears down inline. In session mode e.cmd is always nil (the resident session
// owns the subprocess), so only the buffer flush and result are emitted.
func (e *ThreadExecutor) finish(err error) {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = KillGroup(e.cmd, syscall.SIGKILL)
	}
	if e.cmd != nil {
		_ = e.cmd.Wait()
	}
	e.buffer.flush(e)
	if errors.Is(e.ctx.Err(), context.DeadlineExceeded) {
		e.sendResult(Result{ExitCode: 124, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}
	if errors.Is(e.ctx.Err(), context.Canceled) {
		e.sendResult(Result{ExitCode: 130, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}
	errMsg := simplifyACPError(err)
	if ClassifyInputTooLarge(err) {
		errMsg = strings.TrimRight(errMsg, "\n") + "\n\n" + InputTooLargeGuidance
	}
	e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: errMsg})
}

func (e *ThreadExecutor) scanStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		e.sendOutput(v1pb.CommandOutput_STDERR, line)
	}
}

func (e *ThreadExecutor) sendResult(result Result) {
	if result.Fingerprint == "" {
		result.Fingerprint = e.fingerprint
	}
	if result.ResumeFailures == 0 {
		result.ResumeFailures = e.resumeFailures
	}
	result.LastSeqNo = e.seqNo.Load()
	e.resultCh <- result
}

func (e *ThreadExecutor) nextSeq() int32 {
	return e.seqNo.Add(1)
}

func (e *ThreadExecutor) sendOutput(streamType v1pb.CommandOutput_StreamType, content string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return
	}
	allowed, ok := e.limitOutput(trimmed)
	if !ok {
		return
	}
	chunk := OutputChunk{StreamType: streamType, Content: allowed, SeqNo: e.nextSeq(), Timestamp: timestamppb.New(time.Now())}
	// Never block a producer once the session is cancelled: the consumer
	// (runCommand) stops draining on its own ctx.Done, and run()'s deferred
	// close(e.outputCh) must not race a blocked/racing send.
	select {
	case e.outputCh <- chunk:
	case <-e.ctx.Done():
	}
}

func (e *ThreadExecutor) startFlushTimer() {
	ticker := time.NewTicker(flushOutputInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if e.buffer.hasContent() {
				e.buffer.flush(e)
			}
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *ThreadExecutor) flushIfNeeded() {
	if e.buffer.totalLen() >= int(e.cfg.OutputFlushBytes) {
		e.buffer.flush(e)
	}
}

func (e *ThreadExecutor) sendEvent(event Event) {
	if !e.allowEvent() {
		return
	}
	event.SeqNo = e.nextSeq()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	// See sendOutput: never block a producer after Cancel, so run()'s deferred
	// close(e.eventCh) cannot race a blocked send and goroutines exit cleanly.
	select {
	case e.eventCh <- event:
	case <-e.ctx.Done():
	}
}

func (e *ThreadExecutor) allowEvent() bool {
	if e.cfg.MaxEventCount <= 0 {
		return true
	}
	count := e.eventCount.Add(1)
	if count <= e.cfg.MaxEventCount {
		return true
	}
	if e.eventLimitHit.CompareAndSwap(false, true) {
		e.sendOutput(v1pb.CommandOutput_SYSTEM, "Thread event limit reached; dropping further structured events")
	}
	return false
}

func (e *ThreadExecutor) limitOutput(content string) (string, bool) {
	if e.cfg.MaxOutputBytes <= 0 {
		return content, true
	}
	used := e.outputBytes.Load()
	remaining := e.cfg.MaxOutputBytes - used
	if remaining <= 0 {
		if e.outputLimited.CompareAndSwap(false, true) {
			return "Thread output limit reached; dropping further text output", true
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

func (e *ThreadExecutor) appendSummary(text string) {
	if text == "" {
		return
	}
	e.summaryMu.Lock()
	defer e.summaryMu.Unlock()
	if len(e.summaryText) >= 8192 {
		return
	}
	e.summaryText += text
}

func (e *ThreadExecutor) finalSummary() string {
	e.summaryMu.Lock()
	defer e.summaryMu.Unlock()
	return e.summaryText
}

// Session returns the resident ThreadSession this executor drives, or nil in
// the per-turn mode. The runner and tests use it to inspect the shared
// session's lifecycle.
func (e *ThreadExecutor) Session() *ThreadSession { return e.sess }
