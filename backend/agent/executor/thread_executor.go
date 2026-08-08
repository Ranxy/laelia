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

	Provider      string
	Model         string
	WorkingDir    string
	PersonaPrompt string
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
	cmd    *exec.Cmd
	client *acp2.Client
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

// NewThread constructs a per-turn thread executor driven by the given
// ThreadProvider. The provider supplies the launch command and the EventMapper
// that translates its notification shapes.
func NewThread(req Request, cfg *ThreadConfig, p provider.ThreadProvider) (Runtime, error) {
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
	if e.cmd != nil && e.cmd.Process != nil {
		_ = KillGroup(e.cmd, syscall.SIGKILL)
	}
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

	exe, args := e.p.ThreadCommand(e.cfg.WorkingDir)
	args = append(args, e.p.ThreadMcpArgs(e.cfg.McpServers)...)
	cmd := exec.CommandContext(e.ctx, exe, args...)
	cmd.Dir = e.cfg.WorkingDir
	cmd.Env = buildThreadEnv(e.cfg, e.req.Env, e.req)
	// Own process group so KillGroup reaps the whole tree (node, MCP servers);
	// on Linux also kill on parent death so a SIGKILL'd manager leaves no orphans.
	SetProcessGroup(cmd)
	e.cmd = cmd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("thread stdin pipe: %v", err)})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("thread stdout pipe: %v", err)})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("thread stderr pipe: %v", err)})
		return
	}
	if err := cmd.Start(); err != nil {
		e.sendResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("start thread subprocess: %v", err)})
		return
	}

	client := acp2.NewClient(acp2.NewTransport(stdin), stdout, e.p.NewThreadMapper())
	e.client = client
	client.Start()
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

	if _, err := client.Initialize(startupCtx, "laelia-machine", "0.2.0"); err != nil {
		e.finish(err)
		return
	}
	if err := client.Initialized(); err != nil {
		e.finish(err)
		return
	}

	// Thread inheritance: each turn spawns a fresh subprocess but resumes the
	// SAME thread id when one is persisted for this agent with a matching
	// config fingerprint. The init prompt is sent only on a cold thread/start
	// and lives in the resumed thread's history thereafter — that is the
	// per-turn token saving.
	e.fingerprint = sessionFingerprint(e.cfg.Provider, e.cfg.Model, e.cfg.WorkingDir, "v2")
	threadID := ""
	if existing, loadErr := loadACPSession(e.req.MachineID, e.req.AgentID); loadErr != nil {
		slog.Warn("failed to load persisted thread session state; cold-starting", "agent", e.req.AgentID, "error", loadErr)
	} else if existing != nil && existing.ThreadID != "" && existing.Fingerprint == e.fingerprint {
		thread, resumeErr := client.ResumeThread(startupCtx, existing.ThreadID, e.threadStartParams())
		if resumeErr != nil {
			// The provider lost the thread (crash, eviction, config drift the
			// fingerprint did not catch). Drop the stale id and cold-start so
			// we do not loop forever on a dead thread — the cursor is the
			// source of truth, so no message is lost, only the init prompt is
			// re-sent.
			slog.Warn("thread resume failed; cold-starting", "agent", e.req.AgentID, "thread_id", existing.ThreadID, "error", resumeErr)
			clearACPSession(e.req.MachineID, e.req.AgentID)
			failures, warned := recordResumeFailure(e.req.MachineID, e.req.AgentID)
			e.resumeFailures = failures
			if warned {
				e.sendEvent(Event{
					Type:    v1pb.CommandEventType_WARNING,
					Summary: "thread resume failed repeatedly; starting a fresh thread",
					Warning: &v1pb.WarningPayload{Message: "Thread resume failed 3 times in a row; cold-starting a fresh thread."},
				})
			}
		} else {
			threadID = thread.ID
			e.resumed = true
		}
	}
	if threadID == "" {
		thread, startErr := client.StartThread(startupCtx, e.threadStartParams())
		if startErr != nil {
			e.finish(startErr)
			return
		}
		threadID = thread.ID
	}
	e.threadID = threadID

	// Persist the thread id now that thread/start|resume has accepted it, so
	// the next turn can resume even if the turn below fails — the cursor is
	// the source of truth, so a re-prompt next turn is safe.
	if saveErr := saveACPSession(e.req.MachineID, e.req.AgentID, &acpSessionState{
		ThreadID:    threadID,
		Fingerprint: e.fingerprint,
		CreatedAt:   time.Now().Unix(),
	}); saveErr != nil {
		slog.Warn("failed to persist thread session state; next turn will cold-start", "agent", e.req.AgentID, "error", saveErr)
	}

	promptText := e.turnPromptText(e.resumed)
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
			Resumed:      e.resumed,
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
		"resumed":        e.resumed,
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
			Resumed:      e.resumed,
		})
		return
	}
	e.sendResult(Result{
		ExitCode:     0,
		DurationMs:   time.Since(e.startedAt).Milliseconds(),
		FinalSummary: finalSummary,
		Result:       resultPayload,
		SessionID:    threadID,
		Resumed:      e.resumed,
	})
}

// pumpUntilTurnDone drains mapped events until the current turn completes
// (turn/completed lifecycle), the process dies, or the turn ctx ends.
func (e *ThreadExecutor) pumpUntilTurnDone() {
	for {
		select {
		case ev, ok := <-e.client.Events():
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
		e.buffer.append(v1pb.CommandOutput_SYSTEM, ev.Text)
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

// threadStartParams builds the thread/start|resume payload. Approval is never
// requested (the drain loop is autonomous), the sandbox is full access, and
// raw events are requested so the mapper can track per-phase deltas.
func (e *ThreadExecutor) threadStartParams() acp2.ThreadStartParams {
	return acp2.ThreadStartParams{
		Cwd:                   e.cfg.WorkingDir,
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		DeveloperInstructions: e.cfg.DeveloperInstructions,
		Model:                 e.cfg.Model,
		ExperimentalRawEvents: true,
	}
}

func (e *ThreadExecutor) turnPromptText(resumed bool) string {
	return turnPromptText(e.req, e.cfg.PersonaPrompt, resumed)
}

// finish tears down the subprocess and reports a failed turn. It is the
// failure path for handshake/start errors; the normal completion path in run()
// tears down inline.
func (e *ThreadExecutor) finish(err error) {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = KillGroup(e.cmd, syscall.SIGKILL)
	}
	_ = e.cmd.Wait()
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
	chunk := OutputChunk{StreamType: streamType, Content: allowed, SeqNo: e.nextSeq()}
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
