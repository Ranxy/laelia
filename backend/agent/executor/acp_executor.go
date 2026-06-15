package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const flushOutputInterval = 500 * time.Millisecond
const maxRawEventBatchSize = 256
const permissionTimeout = 120 * time.Second

type outputBuffer struct {
	mu        sync.Mutex
	stdout    strings.Builder
	system    strings.Builder
	order     []v1pb.CommandOutput_StreamType
	lastFlush time.Time
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
	b.lastFlush = time.Now()
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
	if e.profile.SupportsRawEvents {
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
	ctx              context.Context
	cancel           context.CancelFunc
	request          Request
	config           *ACPConfig
	profileName      string
	profile          ACPProfile
	workingDir       string
	allowedRoots     []string
	cmd              *exec.Cmd
	conn             *acp.ClientSideConnection
	client           *acpRuntimeClient
	outputCh         chan OutputChunk
	eventCh          chan Event
	resultCh         chan Result
	done             chan struct{}
	seqNo            atomic.Int32
	startedAt        time.Time
	outputBytes      atomic.Int64
	eventCount       atomic.Int32
	toolCallCount    atomic.Int32
	outputLimited    atomic.Bool
	eventLimitHit    atomic.Bool
	lastUsage        atomic.Value
	summaryText      string
	warnMu           sync.Mutex
	sessionID        string
	initializedAgent string
	buffer           outputBuffer
	rawEvents        rawEventBatch
	permissionCh     chan acp.PermissionOptionId
	perCommandAllow  map[string]bool
	perCommandReject map[string]bool
}

type acpRuntimeClient struct {
	executor *ACPExecutor
}

var _ acp.Client = (*acpRuntimeClient)(nil)

func NewACP(req Request, cfg *ACPConfig) (Runtime, error) {
	profileName, profile, err := cfg.ResolveProfile(req.Profile)
	if err != nil {
		return nil, err
	}

	workingDir, roots, err := resolveACPWorkingDir(req, profile)
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

	cmd := exec.CommandContext(ctx, profile.Executable, profile.Args...)
	cmd.Dir = workingDir
	cmd.Env = buildACPEnv(profile, req.Env)

	exec := &ACPExecutor{
		ctx:              ctx,
		cancel:           cancel,
		request:          req,
		config:           cfg,
		profileName:      profileName,
		profile:          profile,
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
	}
	exec.client = &acpRuntimeClient{executor: exec}
	return exec, nil
}

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
				ReadTextFile:  e.profile.ReadTextFiles,
				WriteTextFile: e.profile.WriteTextFiles,
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

	sessionResp, err := e.conn.NewSession(e.ctx, acp.NewSessionRequest{
		Cwd:                   e.workingDir,
		AdditionalDirectories: additionalRoots(e.allowedRoots, e.workingDir),
		McpServers:            []acp.McpServer{},
	})
	if err != nil {
		e.finishACPProcess(err)
		return
	}
	e.sessionID = string(sessionResp.SessionId)

	promptText := e.request.Instruction
	if promptText == "" {
		promptText = e.request.Command
	}

	promptResp, err := e.conn.Prompt(e.ctx, acp.PromptRequest{
		SessionId: sessionResp.SessionId,
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
		"executor_kind":   e.request.ExecutorKind.String(),
		"profile":         e.profileName,
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
	})
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
	e.outputCh <- OutputChunk{StreamType: streamType, Content: allowed, SeqNo: e.nextSeq()}
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

func (e *ACPExecutor) sendEvent(event Event) {
	if !e.allowEvent() {
		return
	}
	event.SeqNo = e.nextSeq()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	e.eventCh <- event
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
	path, err := c.executor.validatePath(params.Path, c.executor.profile.ReadTextFiles)
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
	path, err := c.executor.validatePath(params.Path, c.executor.profile.WriteTextFiles)
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

	if allowsToolKind(c.executor.profile.AutoApproveToolKinds, params.ToolCall.Kind) {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: allowPermissionOption(params.Options),
			}},
		}, nil
	}

	if c.executor.perCommandAllow[kind] {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{
				Outcome:  "selected",
				OptionId: allowPermissionOption(params.Options),
			}},
		}, nil
	}

	if c.executor.perCommandReject[kind] {
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
			c.executor.perCommandAllow[kind] = true
		case acp.PermissionOptionKindRejectAlways:
			c.executor.perCommandReject[kind] = true
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
		c.executor.toolCallCount.Add(1)
		if c.executor.profile.SupportsToolTraces {
			c.executor.sendEvent(Event{
				Type:            v1pb.CommandEventType_TOOL_CALL_STARTED,
				Summary:         u.ToolCall.Title,
				ToolCallStarted: &v1pb.ToolCallStartedPayload{Title: u.ToolCall.Title, RawInput: toProtobufStruct(u.ToolCall)},
			})
		}
	case u.ToolCallUpdate != nil:
		for _, content := range u.ToolCallUpdate.Content {
			if content.Content != nil {
				text := contentBlockText(content.Content.Content)
				if text != "" {
					c.executor.buffer.append(v1pb.CommandOutput_SYSTEM, text)
					c.executor.flushIfNeeded()
				}
			}
			if content.Diff != nil && c.executor.request.AllowDiff && c.executor.profile.SupportsDiff {
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
		if c.executor.profile.SupportsToolTraces && u.ToolCallUpdate.Status != nil {
			c.executor.sendEvent(Event{
				Type:    v1pb.CommandEventType_TOOL_CALL_FINISHED,
				Summary: string(*u.ToolCallUpdate.Status),
				ToolCallFinished: &v1pb.ToolCallFinishedPayload{
					Status:    string(*u.ToolCallUpdate.Status),
					RawOutput: toProtobufStruct(u.ToolCallUpdate),
				},
			})
		}
	case u.Plan != nil:
		c.executor.buffer.flush(c.executor)
		c.executor.rawEvents.flush(c.executor)
		c.executor.rawEvents.append(c.executor, "plan", toJSONMap(u.Plan))
		c.executor.rawEvents.flush(c.executor)
	case u.UsageUpdate != nil:
		c.executor.buffer.flush(c.executor)
		c.executor.rawEvents.flush(c.executor)
		c.executor.lastUsage.Store(toJSONMap(u.UsageUpdate))
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
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	c.executor.warnMu.Lock()
	defer c.executor.warnMu.Unlock()
	if len(c.executor.summaryText) >= 8192 {
		return
	}
	c.executor.summaryText += trimmed
}

func (c *acpRuntimeClient) finalSummary() string {
	c.executor.warnMu.Lock()
	defer c.executor.warnMu.Unlock()
	return c.executor.summaryText
}

func (e *ACPExecutor) validatePath(path string, enabled bool) (string, error) {
	if !enabled {
		return "", errors.New("filesystem access is disabled for this ACP profile")
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", pkgerrors.Errorf("path must be absolute: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if resolved == "" {
		resolved = cleaned
	}
	for _, root := range e.allowedRoots {
		if resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return resolved, nil
		}
	}
	return "", pkgerrors.Errorf("path %s is outside ACP workspace roots", path)
}

func resolveACPWorkingDir(req Request, profile ACPProfile) (string, []string, error) {
	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = profile.WorkingDir
	}
	if workingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		workingDir = cwd
	}
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", nil, err
	}
	roots := []string{filepath.Clean(absWorkingDir)}
	for _, dir := range profile.AdditionalDirectories {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return "", nil, err
		}
		roots = append(roots, filepath.Clean(absDir))
	}
	return filepath.Clean(absWorkingDir), uniqueStrings(roots), nil
}

func buildACPEnv(profile ACPProfile, requestEnv map[string]string) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if len(profile.AllowEnv) > 0 {
		filtered := map[string]string{}
		for _, key := range profile.AllowEnv {
			if value, ok := values[key]; ok {
				filtered[key] = value
			}
		}
		values = filtered
	}
	for key, value := range requestEnv {
		values[key] = value
	}
	for key, value := range profile.Env {
		values[key] = value
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
