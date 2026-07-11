package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
	pkgerrors "github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const flushOutputInterval = 500 * time.Millisecond
const maxRawEventBatchSize = 256
const permissionTimeout = 120 * time.Second

// debugToolCalls gates verbose logging of every ACP ToolCall / ToolCallUpdate
// frame the agent receives, so we can see exactly which fields (esp. rawInput,
// where the bash command should live) the ACP agent populates. Enable with
// LAELIA_DEBUG_TOOL_CALLS=1 on the agent process.
var debugToolCalls = os.Getenv("LAELIA_DEBUG_TOOL_CALLS") == "1"

type outputBuffer struct {
	mu     sync.Mutex
	stdout strings.Builder
	system strings.Builder
	order  []v1pb.CommandOutput_StreamType
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
	default:
	}
}

func (b *outputBuffer) totalLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.Len() + b.system.Len()
}

func (b *outputBuffer) flush(e *ACPExecutor) {
	b.mu.Lock()
	stdout := b.stdout.String()
	b.stdout.Reset()
	system := b.system.String()
	b.system.Reset()
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
		default:
		}
	}
}

func (b *outputBuffer) hasContent() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.Len() > 0 || b.system.Len() > 0
}

type rawEventBatch struct {
	mu      sync.Mutex
	summary string
	chunks  []*structpb.Struct
}

func (b *rawEventBatch) append(e *ACPExecutor, summary string, payload map[string]any) {
	if summary == "" || payload == nil {
		return
	}
	s, err := structpb.NewStruct(payload)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.summary != "" && b.summary != summary {
		b.flushLocked(e)
	}
	b.summary = summary
	b.chunks = append(b.chunks, s)
	if len(b.chunks) >= maxRawEventBatchSize {
		b.flushLocked(e)
	}
}

func (b *rawEventBatch) flush(e *ACPExecutor) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushLocked(e)
}

func (b *rawEventBatch) flushLocked(e *ACPExecutor) {
	if len(b.chunks) == 0 {
		b.summary = ""
		return
	}
	if e.config.SupportsRawEvents {
		e.sendEvent(Event{
			Type:    v1pb.CommandEventType_RAW_ACP,
			Summary: b.summary,
			RawAcp:  &v1pb.RawAcpPayload{Data: b.buildData()},
		})
	}
	b.summary = ""
	b.chunks = b.chunks[:0]
}

func (b *rawEventBatch) buildData() *structpb.Struct {
	if len(b.chunks) == 1 {
		return b.chunks[0]
	}
	events := make([]any, len(b.chunks))
	for i, chunk := range b.chunks {
		events[i] = chunk.AsMap()
	}
	s, _ := structpb.NewStruct(map[string]any{
		"batch_size": len(b.chunks),
		"events":     events,
	})
	return s
}

type ACPExecutor struct {
	ctx           context.Context
	cancel        context.CancelFunc
	request       Request
	config        *ACPConfig
	workingDir    string
	allowedRoots  []string
	cmd           *exec.Cmd
	conn          *acp.ClientSideConnection
	client        *acpRuntimeClient
	outputCh      chan OutputChunk
	eventCh       chan Event
	resultCh      chan Result
	done          chan struct{}
	seqNo         atomic.Int32
	startedAt     time.Time
	outputBytes   atomic.Int64
	eventCount    atomic.Int32
	toolCallCount atomic.Int32
	outputLimited atomic.Bool
	eventLimitHit atomic.Bool
	summaryText   string
	warnMu        sync.Mutex
	// toolCallStartMu guards toolCallStates, which tracks per-toolCallId
	// metadata used to defer TOOL_CALL_STARTED emission until the tool's input
	// is available. Some ACP agents (e.g. claude-code) send the ToolCall create
	// event with an empty RawInput and only deliver the actual command in a
	// later content-only ToolCallUpdate; emitting STARTED at the create would
	// surface an empty input, so the title is recorded at create and the
	// STARTED is emitted when RawInput arrives (or as a late fallback when the
	// status update arrives with no input ever captured).
	toolCallStartMu  sync.Mutex
	toolCallStates   map[string]*toolCallState
	sessionID        string
	initializedAgent string
	// toolCallAdapter maps the agent's ACP ToolCall create/update frames to
	// TOOL_CALL_STARTED/FINISHED events per its wire shape (e.g. opencode
	// delivers the command in the first in_progress status update, not the
	// create). Resolved once at NewACP.
	toolCallAdapter provider.ToolCallAdapter
	buffer          outputBuffer
	rawEvents       rawEventBatch
	permissionCh    chan acp.PermissionOptionId
	// permMu guards perCommandAllow/perCommandReject. The ACP client methods
	// may be invoked concurrently; without a lock these maps would race.
	permMu           sync.Mutex
	perCommandAllow  map[string]bool
	perCommandReject map[string]bool
}

type acpRuntimeClient struct {
	executor *ACPExecutor
}

var _ acp.Client = (*acpRuntimeClient)(nil)

func NewACP(req Request, cfg *ACPConfig) (Runtime, error) {
	if cfg == nil || cfg.Executable == "" {
		return nil, errors.New("ACP is not configured on this agent")
	}

	workingDir, roots, err := resolveACPWorkingDir(req, cfg)
	if err != nil {
		return nil, err
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

	cmd := exec.CommandContext(ctx, cfg.Executable, cfg.Args...)
	cmd.Dir = workingDir
	cmd.Env = buildACPEnv(cfg, req.Env, req)

	exec := &ACPExecutor{
		ctx:              ctx,
		cancel:           cancel,
		request:          req,
		config:           cfg,
		workingDir:       workingDir,
		allowedRoots:     roots,
		cmd:              cmd,
		outputCh:         make(chan OutputChunk, outputBufferSize),
		eventCh:          make(chan Event, outputBufferSize),
		resultCh:         make(chan Result, 1),
		done:             make(chan struct{}),
		permissionCh:     make(chan acp.PermissionOptionId, 1),
		perCommandAllow:  map[string]bool{},
		perCommandReject: map[string]bool{},
		toolCallStates:   map[string]*toolCallState{},
		toolCallAdapter:  resolveToolCallAdapter(cfg),
	}
	exec.client = &acpRuntimeClient{executor: exec}
	return exec, nil
}

// resolveToolCallAdapter picks the ToolCallAdapter for an agent's ACP wire shape
// from the configured provider id. The provider id is authoritative when set
// (it comes from the provider registry); when it is empty or "custom" the
// launch command is sniffed so a legacy/unclassified binary (e.g. Rei's
// /home/ran/.opencode/bin/opencode with Provider="") still gets the right
// adapter. Newly configured agents always set a provider, so the sniff is only
// a fallback for that legacy case.
func resolveToolCallAdapter(cfg *ACPConfig) provider.ToolCallAdapter {
	if p, ok := provider.Default().Lookup(cfg.Provider); ok {
		return p.ToolCallAdapter()
	}
	if strings.Contains(filepath.Base(cfg.Executable), "opencode") {
		return provider.OpenCodeAdapter{}
	}
	for _, a := range cfg.Args {
		if strings.Contains(a, "claude-agent-acp") {
			return provider.DefaultAdapter{}
		}
	}
	return provider.DefaultAdapter{}
}

// BeginStarted claims the first TOOL_CALL_STARTED for a toolCallId. It is the
// provider.ToolCallSink wrapper over beginToolCallStarted.
func (e *ACPExecutor) BeginStarted(id string) (string, bool) { return e.beginToolCallStarted(id) }

// EmitStarted emits a TOOL_CALL_STARTED event. provider.ToolCallSink wrapper
// over emitToolCallStarted.
func (e *ACPExecutor) EmitStarted(title string, rawIn *structpb.Struct, source string) {
	e.emitToolCallStarted(title, rawIn, source)
}

// EmitFinished emits a TOOL_CALL_FINISHED event with the terminal status and
// raw output. provider.ToolCallSink wrapper.
func (e *ACPExecutor) EmitFinished(status string, rawOut *structpb.Struct) {
	e.sendEvent(Event{
		Type:    v1pb.CommandEventType_TOOL_CALL_FINISHED,
		Summary: status,
		ToolCallFinished: &v1pb.ToolCallFinishedPayload{
			Status:    status,
			RawOutput: rawOut,
		},
	})
}

// Payload converts an ACP RawInput/RawOutput value into the protobuf Struct
// stored on the event (or nil when empty). provider.ToolCallSink wrapper over
// toolPayloadStruct.
func (*ACPExecutor) Payload(in any) *structpb.Struct { return toolPayloadStruct(in) }

// ToolTracesEnabled reports whether the session opts into tool-call tracing.
// provider.ToolCallSink wrapper.
func (e *ACPExecutor) ToolTracesEnabled() bool { return e.config.SupportsToolTraces }

func (e *ACPExecutor) Start() {
	go e.run()
}

func (e *ACPExecutor) Cancel() {
	e.cancel()
	if e.conn != nil && e.sessionID != "" {
		_ = e.conn.Cancel(context.Background(), acp.CancelNotification{SessionId: acp.SessionId(e.sessionID)})
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
}

func (e *ACPExecutor) OutputChannel() <-chan OutputChunk {
	return e.outputCh
}

func (e *ACPExecutor) EventChannel() <-chan Event {
	return e.eventCh
}

func (e *ACPExecutor) ResultChannel() <-chan Result {
	return e.resultCh
}

func (e *ACPExecutor) Done() <-chan struct{} {
	return e.done
}

func (e *ACPExecutor) ResolvePermission(optionID string) {
	select {
	case e.permissionCh <- acp.PermissionOptionId(optionID):
	default:
	}
}

func (e *ACPExecutor) run() {
	e.startedAt = time.Now()
	defer close(e.outputCh)
	defer close(e.eventCh)
	defer close(e.resultCh)
	defer close(e.done)
	defer e.cancel()

	stdin, err := e.cmd.StdinPipe()
	if err != nil {
		e.sendACPResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("acp stdin pipe: %v", err)})
		return
	}
	stdout, err := e.cmd.StdoutPipe()
	if err != nil {
		e.sendACPResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("acp stdout pipe: %v", err)})
		return
	}
	stderr, err := e.cmd.StderrPipe()
	if err != nil {
		e.sendACPResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("acp stderr pipe: %v", err)})
		return
	}

	if err := e.cmd.Start(); err != nil {
		e.sendACPResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: fmt.Sprintf("start ACP subprocess: %v", err)})
		return
	}

	e.conn = acp.NewClientSideConnection(e.client, stdin, stdout)
	go e.scanACPStderr(stderr)
	go e.startFlushTimer()

	initResp, err := e.conn.Initialize(e.ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  e.config.ReadTextFiles,
				WriteTextFile: e.config.WriteTextFiles,
			},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{Name: "laelia-agent", Version: "0.2.0"},
	})
	if err != nil {
		e.finishACPProcess(err)
		return
	}
	if initResp.AgentInfo != nil {
		e.initializedAgent = initResp.AgentInfo.Name
	}

	// mcpServers is sent as an empty array (not omitted): some ACP servers
	// (e.g. @agentclientprotocol/claude-agent-acp) validate the field with a
	// strict array schema and reject a missing/null value with -32602 Invalid
	// params. We expose no MCP servers — the LLM drives the chat loop via the
	// `laelia-agent` CLI instead.
	mcpServers := []acp.McpServer{}
	extraDirs := additionalRoots(e.allowedRoots, e.workingDir)

	// Session inheritance: each turn spawns a fresh subprocess but resumes the
	// SAME ACP SessionId when one is persisted for this agent with a matching
	// config fingerprint. The init prompt (identity + persona + communication +
	// memory + procedure) is sent only on a cold NewSession and lives in the
	// resumed session's history thereafter — that is the per-turn token saving.
	fingerprint := sessionFingerprint(e.config.Provider, e.config.Model, e.workingDir)
	resumed := false
	var configOpts []acp.SessionConfigOption
	if existing, loadErr := loadACPSession(e.request.AgentID); loadErr != nil {
		slog.Warn("failed to load persisted acp session state; cold-starting", "agent", e.request.AgentID, "error", loadErr)
	} else if existing != nil && existing.SessionID != "" && existing.Fingerprint == fingerprint {
		resumeResp, resumeErr := e.conn.ResumeSession(e.ctx, acp.ResumeSessionRequest{
			SessionId:             acp.SessionId(existing.SessionID),
			Cwd:                   e.workingDir,
			AdditionalDirectories: extraDirs,
			McpServers:            mcpServers,
		})
		if resumeErr != nil {
			// The provider lost the session (crash, eviction, config drift the
			// fingerprint did not catch). Drop the stale id and cold-start so we
			// do not loop forever on a dead session — the cursor is the source of
			// truth, so no message is lost, only the init prompt is re-sent.
			slog.Warn("acp session resume failed; cold-starting", "agent", e.request.AgentID, "session_id", existing.SessionID, "error", resumeErr)
			clearACPSession(e.request.AgentID)
		} else {
			e.sessionID = existing.SessionID
			configOpts = resumeResp.ConfigOptions
			resumed = true
		}
	}

	if !resumed {
		sessionResp, newErr := e.conn.NewSession(e.ctx, acp.NewSessionRequest{
			Cwd:                   e.workingDir,
			AdditionalDirectories: extraDirs,
			McpServers:            mcpServers,
		})
		if newErr != nil {
			e.finishACPProcess(newErr)
			return
		}
		e.sessionID = string(sessionResp.SessionId)
		configOpts = sessionResp.ConfigOptions
	}

	// Apply the admin-selected model via the ACP session config option round
	// trip: find the model config option the agent advertised in NewSession (or
	// re-advertised on ResumeSession) and set its value before the prompt. This
	// is the protocol-sanctioned way to select a model (ACP has no model field on
	// initialize/newSession/prompt). If the agent did not advertise a model
	// option, or the selected valueId is not among the advertised options, we
	// log and continue with the agent's default rather than failing the session.
	if err := e.applySelectedModel(configOpts); err != nil {
		slog.Warn("failed to apply selected model", "agent", e.initializedAgent, "model", e.config.Model, "error", err)
	}

	// Persist the session id now that NewSession/ResumeSession has accepted it,
	// so the next turn can resume even if the Prompt below fails — the cursor is
	// the source of truth, so a re-prompt next turn is safe.
	if saveErr := saveACPSession(e.request.AgentID, &acpSessionState{
		SessionID:   e.sessionID,
		Fingerprint: fingerprint,
		CreatedAt:   time.Now().Unix(),
	}); saveErr != nil {
		slog.Warn("failed to persist acp session state; next turn will cold-start", "agent", e.request.AgentID, "error", saveErr)
	}

	promptText := e.turnPromptText(resumed)
	if promptText == "" {
		// Defensive: a warm turn should always carry a batch. If it does not,
		// do not send an empty Prompt — finish cleanly and let the drain loop
		// re-gate. The session is already persisted for the next turn.
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
		e.buffer.flush(e)
		e.rawEvents.flush(e)
		e.sendACPResult(Result{
			ExitCode:     0,
			DurationMs:   time.Since(e.startedAt).Milliseconds(),
			FinalSummary: "no turn prompt; session persisted",
			SessionID:    e.sessionID,
			Resumed:      resumed,
		})
		return
	}

	promptResp, err := e.conn.Prompt(e.ctx, acp.PromptRequest{
		SessionId: acp.SessionId(e.sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock(promptText)},
	})
	if err != nil {
		e.finishACPProcess(err)
		return
	}

	_ = e.cmd.Process.Kill()
	_ = e.cmd.Wait()

	e.buffer.flush(e)
	e.rawEvents.flush(e)

	finalSummary := strings.TrimSpace(e.client.finalSummary())
	if finalSummary == "" {
		finalSummary = fmt.Sprintf("ACP task finished with stop reason %s", promptResp.StopReason)
	}
	resultPayload, payloadErr := structpb.NewStruct(map[string]any{
		"executor_kind":   "ACP",
		"executable":      e.config.Executable,
		"session_id":      e.sessionID,
		"stop_reason":     string(promptResp.StopReason),
		"agent_name":      e.initializedAgent,
		"tool_call_count": e.toolCallCount.Load(),
		"output_limited":  e.outputLimited.Load(),
		"event_limited":   e.eventLimitHit.Load(),
	})
	if payloadErr != nil {
		resultPayload = nil
	}

	e.sendEvent(Event{
		Type:    v1pb.CommandEventType_FINAL_SUMMARY,
		Summary: finalSummary,
		FinalSummary: &v1pb.FinalSummaryPayload{
			StopReason: string(promptResp.StopReason),
			SessionId:  e.sessionID,
		},
	})
	e.sendACPResult(Result{
		ExitCode:     stopReasonExitCode(promptResp.StopReason),
		DurationMs:   time.Since(e.startedAt).Milliseconds(),
		FinalSummary: finalSummary,
		Result:       resultPayload,
		SessionID:    e.sessionID,
		Resumed:      resumed,
	})
}

// turnPromptText composes the user message for this turn's Prompt call. On a
// warm (resumed) turn only the "New messages received:" batch is sent — the
// init prompt is already in the session history. On a cold turn the full init
// prompt (identity + persona + communication + procedure + memory) is sent
// first, with the batch appended when there is pending work so the first turn
// is not wasted; a cold turn with no batch just primes the session for future
// notifications.
func (e *ACPExecutor) turnPromptText(resumed bool) string {
	// TurnPrompt is the drain-loop batch. Direct callers (e.g. integration
	// tests) may set Instruction instead; honor it as a fallback so those paths
	// still reach the LLM without every caller knowing about TurnPrompt.
	batch := strings.TrimSpace(e.request.TurnPrompt)
	if batch == "" {
		batch = strings.TrimSpace(e.request.Instruction)
	}
	if resumed {
		return batch
	}
	identityName := e.request.AgentDisplayName
	if identityName == "" {
		identityName = e.request.AgentResourceID
	}
	initPrompt := buildPrompt(identityName, e.config.PersonaPrompt)
	if batch == "" {
		return initPrompt
	}
	return initPrompt + "\n\n" + batch
}

func (e *ACPExecutor) finishACPProcess(err error) {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	_ = e.cmd.Wait()
	e.buffer.flush(e)
	e.rawEvents.flush(e)
	if errors.Is(e.ctx.Err(), context.DeadlineExceeded) {
		e.sendACPResult(Result{ExitCode: 124, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}
	if errors.Is(e.ctx.Err(), context.Canceled) {
		e.sendACPResult(Result{ExitCode: 130, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: e.ctx.Err().Error()})
		return
	}
	e.sendACPResult(Result{ExitCode: 1, DurationMs: time.Since(e.startedAt).Milliseconds(), ErrorMessage: simplifyACPError(err)})
}

func (e *ACPExecutor) scanACPStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		e.sendOutput(v1pb.CommandOutput_STDERR, line)
	}
}

func (e *ACPExecutor) sendACPResult(result Result) {
	result.LastSeqNo = e.seqNo.Load()
	e.resultCh <- result
}

func (e *ACPExecutor) nextSeq() int32 {
	return e.seqNo.Add(1)
}

func (e *ACPExecutor) sendOutput(streamType v1pb.CommandOutput_StreamType, content string) {
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
	// close(e.outputCh) must not race a blocked/racing send. Selecting on
	// e.ctx.Done lets a Cancel unblock every producer so close can proceed and
	// the goroutines exit instead of leaking on a full (cap 1024) channel.
	select {
	case e.outputCh <- chunk:
	case <-e.ctx.Done():
	}
}

func (e *ACPExecutor) startFlushTimer() {
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

func (e *ACPExecutor) flushIfNeeded() {
	if e.buffer.totalLen() >= int(e.config.OutputFlushBytes) {
		e.buffer.flush(e)
	}
}

// toolCallState tracks per-toolCallId metadata used to defer TOOL_CALL_STARTED
// emission until the tool's input is available. title is captured from the
// ToolCall create event so a later deferred STARTED (driven by a
// ToolCallUpdate carrying RawInput) can use it when the update has no title.
type toolCallState struct {
	title   string
	started bool
}

// recordToolCallTitle stores the title observed on a ToolCall create event so
// a later deferred TOOL_CALL_STARTED can use it. It does NOT mark the id as
// started; the STARTED is emitted only when RawInput arrives (or as a late
// fallback), so an agent that sends the command in a content-only update is
// not short-circuited by an empty-input create.
func (e *ACPExecutor) recordToolCallTitle(id, title string) {
	if id == "" {
		return
	}
	e.toolCallStartMu.Lock()
	st, ok := e.toolCallStates[id]
	if !ok {
		st = &toolCallState{}
		e.toolCallStates[id] = st
	}
	if title != "" {
		st.title = title
	}
	e.toolCallStartMu.Unlock()
}

// beginToolCallStarted claims a toolCallId for TOOL_CALL_STARTED emission the
// first time it is seen, returning the stored title (from a prior create, or
// empty) and ok=true. Subsequent calls return ok=false so each tool call emits
// exactly one STARTED. This drives both the synthesized STARTED for
// ToolCallUpdate-only agents and the deferred STARTED for agents whose create
// carried an empty RawInput.
func (e *ACPExecutor) beginToolCallStarted(id string) (title string, ok bool) {
	if id == "" {
		return "", false
	}
	e.toolCallStartMu.Lock()
	defer e.toolCallStartMu.Unlock()
	st, exists := e.toolCallStates[id]
	if !exists {
		st = &toolCallState{}
		e.toolCallStates[id] = st
	}
	if st.started {
		return st.title, false
	}
	st.started = true
	return st.title, true
}

// emitToolCallStarted emits one TOOL_CALL_STARTED event for the given tool call
// and (when debug logging is on) records the source frame and the captured
// input so the deferred-emission logic is observable from the agent logs.
func (e *ACPExecutor) emitToolCallStarted(title string, rawIn *structpb.Struct, source string) {
	e.toolCallCount.Add(1)
	if debugToolCalls {
		slog.Info("acp tool_call_started emitted", "source", source, "title", title, "rawInput", toJSONString(rawIn))
	}
	e.sendEvent(Event{
		Type:    v1pb.CommandEventType_TOOL_CALL_STARTED,
		Summary: title,
		ToolCallStarted: &v1pb.ToolCallStartedPayload{
			Title:    title,
			RawInput: rawIn,
		},
	})
}

// toolPayloadStruct converts an ACP RawInput/RawOutput value into a protobuf
// Struct for the ToolCallStartedPayload/ToolCallFinishedPayload fields. Inputs
// are JSON objects (e.g. {"command": "...", "description": "..."}); outputs are
// often a JSON string. Map values pass through; scalar values are wrapped under
// "value" so they survive structpb.NewStruct (which only accepts maps). Empty
// or nil input returns nil so the frontend renders its "Input not captured"
// fallback instead of an empty object.
func toolPayloadStruct(in any) *structpb.Struct {
	if in == nil {
		return nil
	}
	s := toJSONString(in)
	if s == "{}" || s == "null" {
		return nil
	}
	if _, ok := toJSONMap(in)["unmarshal_error"]; ok {
		// Scalar (string/number) — wrap so structpb.NewStruct accepts it.
		return toProtobufStruct(map[string]any{"value": in})
	}
	return toProtobufStruct(in)
}

func (e *ACPExecutor) sendEvent(event Event) {
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

func (e *ACPExecutor) allowEvent() bool {
	if e.config.MaxEventCount <= 0 {
		return true
	}
	count := e.eventCount.Add(1)
	if count <= e.config.MaxEventCount {
		return true
	}
	if e.eventLimitHit.CompareAndSwap(false, true) {
		e.sendOutput(v1pb.CommandOutput_SYSTEM, "ACP event limit reached; dropping further structured events")
	}
	return false
}

func (e *ACPExecutor) limitOutput(content string) (string, bool) {
	if e.config.MaxOutputBytes <= 0 {
		return content, true
	}
	used := e.outputBytes.Load()
	remaining := e.config.MaxOutputBytes - used
	if remaining <= 0 {
		if e.outputLimited.CompareAndSwap(false, true) {
			return "ACP output limit reached; dropping further text output", true
		}
		return "", false
	}
	if int64(len(content)) <= remaining {
		e.outputBytes.Add(int64(len(content)))
		return content, true
	}
	truncated := content[:remaining]
	e.outputBytes.Store(e.config.MaxOutputBytes)
	e.outputLimited.Store(true)
	return truncated, true
}

func (c *acpRuntimeClient) ReadTextFile(_ context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	path, err := c.executor.validatePath(params.Path, c.executor.config.ReadTextFiles)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	content := string(b)
	if params.Line != nil || params.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 0
		if params.Line != nil && *params.Line > 0 {
			start = minInt(maxInt(*params.Line-1, 0), len(lines))
		}
		end := len(lines)
		if params.Limit != nil && *params.Limit > 0 && start+*params.Limit < end {
			end = start + *params.Limit
		}
		content = strings.Join(lines[start:end], "\n")
	}
	return acp.ReadTextFileResponse{Content: content}, nil
}

func (c *acpRuntimeClient) WriteTextFile(_ context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	path, err := c.executor.validatePath(params.Path, c.executor.config.WriteTextFiles)
	if err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (c *acpRuntimeClient) RequestPermission(_ context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	kind := ""
	if params.ToolCall.Kind != nil {
		kind = string(*params.ToolCall.Kind)
	}

	if allowsToolKind(c.executor.config.AutoApproveToolKinds, params.ToolCall.Kind) {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: allowPermissionOption(params.Options),
			}},
		}, nil
	}

	c.executor.permMu.Lock()
	allowed := c.executor.perCommandAllow[kind]
	rejected := c.executor.perCommandReject[kind]
	c.executor.permMu.Unlock()

	if allowed {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: allowPermissionOption(params.Options),
			}},
		}, nil
	}

	if rejected {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: rejectPermissionOption(params.Options),
			}},
		}, nil
	}

	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}

	c.executor.sendEvent(Event{
		Type:    v1pb.CommandEventType_PERMISSION_REQUESTED,
		Summary: fmt.Sprintf("Permission required for %s: %s", kind, title),
		PermissionRequested: &v1pb.PermissionRequestedPayload{
			ToolCallId: string(params.ToolCall.ToolCallId),
			Kind:       kind,
			Title:      title,
			Options:    permissionOptionsToProto(params.Options),
			ExpiresAt:  time.Now().Add(permissionTimeout).Unix(),
		},
	})

	select {
	case optionID := <-c.executor.permissionCh:
		if optionID == "" {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
			}, nil
		}

		permissionKind := findPermissionOptionKind(params.Options, optionID)
		switch permissionKind {
		case acp.PermissionOptionKindAllowAlways:
			c.executor.permMu.Lock()
			c.executor.perCommandAllow[kind] = true
			c.executor.permMu.Unlock()
		case acp.PermissionOptionKindRejectAlways:
			c.executor.permMu.Lock()
			c.executor.perCommandReject[kind] = true
			c.executor.permMu.Unlock()
		default:
		}

		c.executor.sendEvent(Event{
			Type:    v1pb.CommandEventType_PERMISSION_DECIDED,
			Summary: fmt.Sprintf("Permission %s for %s: %s", string(permissionKind), kind, title),
			PermissionDecided: &v1pb.PermissionDecidedPayload{
				ToolCallId: string(params.ToolCall.ToolCallId),
				Kind:       kind,
				OptionId:   string(optionID),
				OptionKind: string(permissionKind),
			},
		})

		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: optionID,
			}},
		}, nil

	case <-time.After(permissionTimeout):
		c.executor.sendEvent(Event{
			Type:    v1pb.CommandEventType_PERMISSION_TIMED_OUT,
			Summary: fmt.Sprintf("Permission timed out for %s", kind),
			PermissionTimedOut: &v1pb.PermissionTimedOutPayload{
				ToolCallId: string(params.ToolCall.ToolCallId),
				Kind:       kind,
			},
		})
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
		}, nil

	case <-c.executor.ctx.Done():
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{Outcome: "cancelled"}},
		}, nil
	}
}

func (c *acpRuntimeClient) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	u := params.Update
	switch {
	case u.AgentMessageChunk != nil:
		text := contentBlockText(u.AgentMessageChunk.Content)
		c.executor.client.appendSummary(text)
		if text != "" {
			c.executor.buffer.append(v1pb.CommandOutput_STDOUT, text)
			c.executor.flushIfNeeded()
		}
		c.executor.rawEvents.append(c.executor, "agent_message_chunk", toJSONMap(u.AgentMessageChunk))
	case u.AgentThoughtChunk != nil:
		text := contentBlockText(u.AgentThoughtChunk.Content)
		if text != "" {
			c.executor.buffer.append(v1pb.CommandOutput_SYSTEM, text)
			c.executor.flushIfNeeded()
		}
		c.executor.rawEvents.append(c.executor, "agent_thought_chunk", toJSONMap(u.AgentThoughtChunk))
	case u.ToolCall != nil:
		c.executor.buffer.flush(c.executor)
		c.executor.rawEvents.flush(c.executor)
		id := string(u.ToolCall.ToolCallId)
		c.executor.recordToolCallTitle(id, u.ToolCall.Title)
		if debugToolCalls {
			slog.Info("acp tool_call create", "toolCallId", id,
				"title", u.ToolCall.Title, "kind", string(u.ToolCall.Kind),
				"rawInput", toJSONString(u.ToolCall.RawInput),
				"contentCount", len(u.ToolCall.Content))
		}
		// The adapter decides whether to emit STARTED at the create. claude-code
		// sends an empty RawInput here and delivers the command in a later
		// content-only update; opencode sends partial {cwd} metadata and delivers
		// the command in the first in_progress status update. For both, emitting
		// STARTED now would surface an empty/partial input, so the adapter defers.
		c.executor.toolCallAdapter.OnCreate(c.executor, u.ToolCall)
	case u.ToolCallUpdate != nil:
		id := string(u.ToolCallUpdate.ToolCallId)
		if debugToolCalls {
			status := ""
			if u.ToolCallUpdate.Status != nil {
				status = string(*u.ToolCallUpdate.Status)
			}
			slog.Info("acp tool_call_update", "toolCallId", id,
				"status", status,
				"rawInput", toJSONString(u.ToolCallUpdate.RawInput),
				"rawOutput", toJSONString(u.ToolCallUpdate.RawOutput),
				"contentCount", len(u.ToolCallUpdate.Content))
		}
		// A content-only update (Status==nil) may carry the tool input for agents
		// that send an empty/partial create (e.g. claude-code); the adapter emits
		// the deferred STARTED from it. Status updates are handled after the
		// content loop below so output streams before the FINISHED.
		if u.ToolCallUpdate.Status == nil {
			c.executor.toolCallAdapter.OnContentUpdate(c.executor, u.ToolCallUpdate)
		}
		for _, content := range u.ToolCallUpdate.Content {
			if content.Content != nil {
				text := contentBlockText(content.Content.Content)
				if text != "" {
					c.executor.buffer.append(v1pb.CommandOutput_SYSTEM, text)
					c.executor.flushIfNeeded()
				}
			}
			if content.Diff != nil && c.executor.request.AllowDiff && c.executor.config.SupportsDiff {
				oldText := ""
				if content.Diff.OldText != nil {
					oldText = *content.Diff.OldText
				}
				c.executor.sendEvent(Event{
					Type:    v1pb.CommandEventType_DIFF_EMITTED,
					Summary: content.Diff.Path,
					DiffEmitted: &v1pb.DiffEmittedPayload{
						Path:    content.Diff.Path,
						OldText: oldText,
						NewText: content.Diff.NewText,
					},
				})
			}
		}
		if u.ToolCallUpdate.Status != nil {
			c.executor.rawEvents.append(c.executor, "tool_call_update", toJSONMap(u.ToolCallUpdate))
		}
		if u.ToolCallUpdate.Status != nil {
			// The adapter emits a late STARTED if none was emitted yet (using
			// this update's RawInput when present, e.g. opencode's in_progress
			// update carrying the command) and a FINISHED only on terminal
			// status — so repeated in_progress updates no longer orphan the
			// real completed event.
			c.executor.toolCallAdapter.OnStatusUpdate(c.executor, u.ToolCallUpdate)
		}
	case u.Plan != nil:
		c.executor.buffer.flush(c.executor)
		c.executor.rawEvents.flush(c.executor)
		c.executor.rawEvents.append(c.executor, "plan", toJSONMap(u.Plan))
		c.executor.rawEvents.flush(c.executor)
	case u.UsageUpdate != nil:
		c.executor.buffer.flush(c.executor)
		c.executor.rawEvents.flush(c.executor)
		c.executor.rawEvents.append(c.executor, "usage", toJSONMap(u.UsageUpdate))
		c.executor.rawEvents.flush(c.executor)
	default:
		c.executor.rawEvents.append(c.executor, "session_update", toJSONMap(u))
	}
	return nil
}

func (c *acpRuntimeClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	_ = c
	return acp.CreateTerminalResponse{}, errors.New("ACP terminal bridge is disabled")
}

func (c *acpRuntimeClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	_ = c
	return acp.KillTerminalResponse{}, errors.New("ACP terminal bridge is disabled")
}

func (c *acpRuntimeClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	_ = c
	return acp.TerminalOutputResponse{}, errors.New("ACP terminal bridge is disabled")
}

func (c *acpRuntimeClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	_ = c
	return acp.ReleaseTerminalResponse{}, errors.New("ACP terminal bridge is disabled")
}

func (c *acpRuntimeClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	_ = c
	return acp.WaitForTerminalExitResponse{}, errors.New("ACP terminal bridge is disabled")
}

func (c *acpRuntimeClient) appendSummary(text string) {
	if text == "" {
		return
	}
	c.executor.warnMu.Lock()
	defer c.executor.warnMu.Unlock()
	if len(c.executor.summaryText) >= 8192 {
		return
	}
	c.executor.summaryText += text
}

func (c *acpRuntimeClient) finalSummary() string {
	c.executor.warnMu.Lock()
	defer c.executor.warnMu.Unlock()
	return strings.TrimSpace(c.executor.summaryText)
}

func (e *ACPExecutor) validatePath(path string, enabled bool) (string, error) {
	if !enabled {
		return "", errors.New("filesystem access is disabled for this ACP profile")
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", pkgerrors.Errorf("path must be absolute: %s", path)
	}
	// Same symlink-escape hardening as daemon.validateWorkspacePath: the
	// previous version fell back to the lexical cleaned path when EvalSymlinks
	// failed on a not-yet-existing target, so a dangling symlink inside a root
	// pointing outside it (root/evil -> /etc/...) passed the prefix check and a
	// subsequent write followed the symlink out of the roots.
	fi, err := os.Lstat(cleaned)
	switch {
	case err == nil:
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", pkgerrors.Errorf("path %s is a symlink; refusing to follow it outside the ACP roots", path)
		}
		resolved, lerr := filepath.EvalSymlinks(cleaned)
		if lerr != nil {
			return "", lerr
		}
		if e.pathInsideRoots(resolved) {
			return resolved, nil
		}
		return "", pkgerrors.Errorf("path %s is outside ACP workspace roots", path)
	case errors.Is(err, os.ErrNotExist):
		parent := filepath.Dir(cleaned)
		parentResolved, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return "", perr
		}
		if e.pathInsideRoots(parentResolved) {
			return filepath.Join(parentResolved, filepath.Base(cleaned)), nil
		}
		return "", pkgerrors.Errorf("path %s is outside ACP workspace roots", path)
	default:
		return "", err
	}
}

// pathInsideRoots reports whether target is at or below one of the allowed
// ACP workspace roots.
func (e *ACPExecutor) pathInsideRoots(target string) bool {
	for _, root := range e.allowedRoots {
		if target == root || strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func resolveACPWorkingDir(_ Request, cfg *ACPConfig) (string, []string, error) {
	// working_dir is always the per-agent dir baked into cfg by BuildACPConfig;
	// the per-request WorkingDir is ignored.
	workingDir := cfg.WorkingDir
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", nil, err
	}
	roots := []string{filepath.Clean(absWorkingDir)}
	for _, dir := range cfg.AdditionalDirectories {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return "", nil, err
		}
		roots = append(roots, filepath.Clean(absDir))
	}
	return filepath.Clean(absWorkingDir), uniqueStrings(roots), nil
}

// applySelectedModel drives the ACP session config option round trip to apply
// the admin-selected model. It scans the NewSession config options for the
// one whose category is "model" and, when the selected valueId is among the
// advertised options, calls SetSessionConfigOption. Missing model option or
// unknown valueId are not errors: the session continues with the agent's
// default model. Returns an error only when the SetSessionConfigOption RPC
// itself fails.
func (e *ACPExecutor) applySelectedModel(opts []acp.SessionConfigOption) error {
	if e.config.Model == "" {
		return nil
	}
	var modelSelect *acp.SessionConfigOptionSelect
	for i := range opts {
		sel := opts[i].Select
		if sel == nil || sel.Category == nil {
			continue
		}
		if *sel.Category == acp.SessionConfigOptionCategoryModel {
			modelSelect = sel
			break
		}
	}
	if modelSelect == nil {
		slog.Warn("agent did not advertise a model config option; cannot apply selected model", "agent", e.initializedAgent, "model", e.config.Model)
		return nil
	}
	if !modelOptionContains(modelSelect.Options, e.config.Model) {
		slog.Warn("selected model not among advertised options; using agent default", "agent", e.initializedAgent, "model", e.config.Model, "optionId", modelSelect.Id)
		return nil
	}
	if _, err := e.conn.SetSessionConfigOption(e.ctx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: acp.SessionId(e.sessionID),
			ConfigId:  modelSelect.Id,
			Value:     acp.SessionConfigValueId(e.config.Model),
		},
	}); err != nil {
		return pkgerrors.Wrapf(err, "set session config option %q=%q", modelSelect.Id, e.config.Model)
	}
	return nil
}

// modelOptionContains reports whether the given valueId appears in the model
// config option's selectable values (ungrouped or grouped).
func modelOptionContains(opts acp.SessionConfigSelectOptions, want string) bool {
	for _, o := range flattenSelectOptions(opts) {
		if string(o.Value) == want {
			return true
		}
	}
	return false
}

// flattenSelectOptions returns the selectable values of a session config
// select, whether advertised as a flat ungrouped list or grouped under
// headers.
func flattenSelectOptions(opts acp.SessionConfigSelectOptions) []acp.SessionConfigSelectOption {
	if opts.Ungrouped != nil {
		return *opts.Ungrouped
	}
	if opts.Grouped != nil {
		flat := make([]acp.SessionConfigSelectOption, 0)
		for _, g := range *opts.Grouped {
			flat = append(flat, g.Options...)
		}
		return flat
	}
	return nil
}

func buildACPEnv(cfg *ACPConfig, requestEnv map[string]string, req Request) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if len(cfg.AllowEnv) > 0 {
		filtered := map[string]string{}
		for _, key := range cfg.AllowEnv {
			if value, ok := values[key]; ok {
				filtered[key] = value
			}
		}
		values = filtered
	} else {
		values = map[string]string{}
	}
	for key, value := range requestEnv {
		values[key] = value
	}
	for key, value := range cfg.Env {
		values[key] = value
	}
	// Overlay admin-authored custom env (key-value) on top of the inherited
	// allow_env set; custom env wins over inherited values but is itself
	// overridden by the LAELIA_* bootstrap vars injected below.
	for key, value := range cfg.CustomEnv {
		values[key] = value
	}

	// Inject the CLI bootstrap env so the LLM can call `laelia-agent` from its
	// shell with no flags. These overlay the (filtered) inherited env, so they
	// pass through regardless of the agent's AllowEnv whitelist. The session
	// token + socket path are stable for the whole daemon lifetime; the live
	// (rotating) access token stays here in the daemon, never in the subprocess.
	if req.DaemonSocket != "" {
		values["LAELIA_DAEMON_SOCKET"] = req.DaemonSocket
	}
	if req.SessionToken != "" {
		values["LAELIA_SESSION_TOKEN"] = req.SessionToken
	}
	if req.AgentResourceID != "" {
		values["LAELIA_AGENT"] = req.AgentResourceID
	}
	if req.CommandID != "" {
		values["LAELIA_COMMAND"] = req.CommandID
	}
	// Prepend the agent binary's directory to PATH so `laelia-agent` resolves
	// regardless of the host's PATH configuration.
	if req.BinaryDir != "" {
		existing := values["PATH"]
		if existing == "" {
			values["PATH"] = req.BinaryDir
		} else {
			values["PATH"] = req.BinaryDir + string(os.PathListSeparator) + existing
		}
	}

	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func additionalRoots(roots []string, workingDir string) []string {
	additional := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == workingDir {
			continue
		}
		additional = append(additional, root)
	}
	return additional
}

func simplifyACPError(err error) string {
	var requestErr *acp.RequestError
	if errors.As(err, &requestErr) {
		return fmt.Sprintf("ACP request failed (%d): %s", requestErr.Code, requestErr.Message)
	}
	return err.Error()
}

func stopReasonExitCode(reason acp.StopReason) int32 {
	switch reason {
	case acp.StopReasonEndTurn:
		return 0
	case acp.StopReasonCancelled:
		return 130
	default:
		return 1
	}
}

func contentBlockText(block acp.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	if block.ResourceLink != nil {
		return block.ResourceLink.Uri
	}
	return ""
}

func toJSONMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{"unmarshal_error": err.Error()}
	}
	return payload
}

// toJSONString renders any value as a compact JSON string for debug logging,
// tolerating nil and marshal errors so it never panics the producer.
func toJSONString(value any) string {
	if value == nil {
		return "<nil>"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<marshal_error: %v>", err)
	}
	return string(data)
}

func toProtobufStruct(value any) *structpb.Struct {
	s, _ := structpb.NewStruct(toJSONMap(value))
	return s
}

func permissionOptionsToProto(options []acp.PermissionOption) []*v1pb.PermissionOptionPayload {
	result := make([]*v1pb.PermissionOptionPayload, len(options))
	for i, opt := range options {
		result[i] = &v1pb.PermissionOptionPayload{
			OptionId: string(opt.OptionId),
			Name:     opt.Name,
			Kind:     string(opt.Kind),
		}
	}
	return result
}

func allowPermissionOption(options []acp.PermissionOption) acp.PermissionOptionId {
	for _, option := range options {
		if option.Kind == acp.PermissionOptionKindAllowOnce || option.Kind == acp.PermissionOptionKindAllowAlways {
			return option.OptionId
		}
	}
	return ""
}

func rejectPermissionOption(options []acp.PermissionOption) acp.PermissionOptionId {
	for _, option := range options {
		if option.Kind == acp.PermissionOptionKindRejectOnce || option.Kind == acp.PermissionOptionKindRejectAlways {
			return option.OptionId
		}
	}
	return ""
}

func allowsToolKind(allowed []string, kind *acp.ToolKind) bool {
	if kind == nil {
		return false
	}
	for _, candidate := range allowed {
		if candidate == string(*kind) {
			return true
		}
	}
	return false
}

func findPermissionOptionKind(options []acp.PermissionOption, optionID acp.PermissionOptionId) acp.PermissionOptionKind {
	for _, opt := range options {
		if opt.OptionId == optionID {
			return opt.Kind
		}
	}
	return acp.PermissionOptionKindAllowOnce
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
