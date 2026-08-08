package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestACPValidatePath(t *testing.T) {
	workspace := t.TempDir()
	insidePath := filepath.Join(workspace, "inside.txt")
	require.NoError(t, os.WriteFile(insidePath, []byte("ok"), 0o644))

	exec := &ACPExecutor{allowedRoots: []string{workspace}}

	resolved, err := exec.validatePath(insidePath, true)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(insidePath), resolved)

	_, err = exec.validatePath(filepath.Join(workspace, "..", "outside.txt"), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside ACP workspace roots")

	_, err = exec.validatePath(insidePath, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem access is disabled")
}

func TestACPRequestPermissionSelectsAllowOption(t *testing.T) {
	kind := acp.ToolKindRead
	client := &acpRuntimeClient{executor: &ACPExecutor{config: &ACPConfig{}}}

	resp, err := client.RequestPermission(context.Background(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "reject", Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject"},
			{OptionId: "allow", Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once"},
		},
		ToolCall: acp.ToolCallUpdate{Kind: &kind},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Outcome.Selected)
	assert.Equal(t, acp.PermissionOptionId("allow"), resp.Outcome.Selected.OptionId)
}

func TestACPSessionUpdateEmitsDiffEvent(t *testing.T) {
	status := acp.ToolCallStatusCompleted
	exec := &ACPExecutor{
		ctx:             context.Background(),
		request:         Request{AllowDiff: true},
		config:          &ACPConfig{SupportsDiff: true, SupportsToolTraces: true, MaxEventCount: 10},
		outputCh:        make(chan OutputChunk, 4),
		eventCh:         make(chan Event, 4),
		toolCallStates:  map[string]*toolCallState{},
		toolCallAdapter: provider.DefaultAdapter{},
	}
	client := &acpRuntimeClient{executor: exec}

	err := client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCallUpdate: &acp.SessionToolCallUpdate{
				Status: &status,
				Content: []acp.ToolCallContent{
					acp.ToolDiffContent("/tmp/test.txt", "new content", "old content"),
				},
			},
		},
	})
	require.NoError(t, err)

	var eventTypes []v1pb.CommandEventType
	for len(exec.eventCh) > 0 {
		eventTypes = append(eventTypes, (<-exec.eventCh).Type)
	}

	assert.Contains(t, eventTypes, v1pb.CommandEventType_DIFF_EMITTED)
	assert.Contains(t, eventTypes, v1pb.CommandEventType_TOOL_CALL_FINISHED)
}

func TestACPSessionUpdateBuffersConsecutiveMessageChunks(t *testing.T) {
	exec := newTestBufferedExecutor()
	exec.config.SupportsRawEvents = true
	client := &acpRuntimeClient{executor: exec}

	for i := 0; i < 5; i++ {
		err := client.SessionUpdate(context.Background(), acp.SessionNotification{
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.TextBlock("chunk " + fmt.Sprintf("%d", i)),
				},
			},
		})
		require.NoError(t, err)
	}

	assert.Empty(t, exec.outputCh, "consecutive message chunks should be buffered in output")

	exec.buffer.flush(exec)
	assert.NotEmpty(t, exec.outputCh)
	output := <-exec.outputCh
	assert.Equal(t, v1pb.CommandOutput_STDOUT, output.StreamType)
	assert.Contains(t, output.Content, "chunk 0")
	assert.Contains(t, output.Content, "chunk 4")
	assert.Empty(t, exec.outputCh, "only one output after flush")

	exec.rawEvents.flush(exec)
	assert.NotEmpty(t, exec.eventCh)
	ev := <-exec.eventCh
	assert.Equal(t, v1pb.CommandEventType_RAW_ACP, ev.Type)
	assert.Equal(t, "agent_message_chunk", ev.Summary)
	require.NotNil(t, ev.RawAcp)
	require.NotNil(t, ev.RawAcp.Data)
	assert.Equal(t, float64(5), ev.RawAcp.Data.AsMap()["batch_size"])
}

// TestACPSessionUpdateDropsReplayedHistory guards the opencode-replay fix.
// opencode v1.17.x replays the prior conversation as session/update
// notifications DURING session/resume (before the resume response). While
// replayingHistory is set, every such notification — agent messages, tool
// calls, diffs — must be dropped so the current command does not inherit the
// prior turn's events. With the flag clear, the same notification flows through.
func TestACPSessionUpdateDropsReplayedHistory(t *testing.T) {
	exec := newTestBufferedExecutor()
	exec.config.SupportsRawEvents = true
	exec.config.SupportsDiff = true
	exec.config.SupportsToolTraces = true
	exec.request.AllowDiff = true
	client := &acpRuntimeClient{executor: exec}

	// A replayed agent message, tool call, and diff must all be dropped while
	// replayingHistory is set: nothing lands on either channel.
	exec.replayingHistory.Store(true)
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.TextBlock("replayed agent text"),
		}},
	}))
	status := acp.ToolCallStatusCompleted
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
			Status: &status,
			Content: []acp.ToolCallContent{
				acp.ToolDiffContent("/tmp/replayed.txt", "new", "old"),
			},
		}},
	}))
	exec.buffer.flush(exec)
	exec.rawEvents.flush(exec)
	assert.Empty(t, exec.eventCh, "replayed history must not emit events")
	assert.Empty(t, exec.outputCh, "replayed history must not emit output")
	assert.Empty(t, exec.toolCallStates, "replayed tool calls must not register state")

	// Once the resume RPC has returned (flag clear), the same notification flows.
	exec.replayingHistory.Store(false)
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.TextBlock("live agent text"),
		}},
	}))
	exec.buffer.flush(exec)
	assert.NotEmpty(t, exec.outputCh, "live agent text must flow to output")
}

func TestACPSessionUpdateBatchesRawEventsAcrossBoundaries(t *testing.T) {
	exec := newTestBufferedExecutor()
	exec.config.SupportsRawEvents = true
	client := &acpRuntimeClient{executor: exec}

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("hello"),
			},
		},
	}))

	kind := acp.ToolKindRead
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCall: &acp.SessionUpdateToolCall{
				Title: "Read",
				Kind:  kind,
			},
		},
	}))

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("world"),
			},
		},
	}))

	exec.buffer.flush(exec)
	exec.rawEvents.flush(exec)

	var rawEvents []Event
	for len(exec.eventCh) > 0 {
		ev := <-exec.eventCh
		if ev.Type == v1pb.CommandEventType_RAW_ACP {
			rawEvents = append(rawEvents, ev)
		}
	}

	assert.Len(t, rawEvents, 2, "should have 2 batched raw events (message batch + tool_call boundary flushes, then new message batch)")
}

func TestACPSessionUpdateFlushesOnToolCallBoundary(t *testing.T) {
	exec := newTestBufferedExecutor()
	client := &acpRuntimeClient{executor: exec}

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("hello"),
			},
		},
	}))

	assert.Empty(t, exec.outputCh, "should be buffered before tool call")

	kind := acp.ToolKindRead
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCall: &acp.SessionUpdateToolCall{
				Title: "Read",
				Kind:  kind,
			},
		},
	}))

	assert.NotEmpty(t, exec.outputCh)
	output := <-exec.outputCh
	assert.Equal(t, v1pb.CommandOutput_STDOUT, output.StreamType)
	assert.Equal(t, "hello", output.Content)
}

// TestACPSessionUpdateEmitsContextUsage guards UsageUpdate parsing: the first
// update emits a structured CONTEXT_USAGE_UPDATE event, updates inside the
// rate-limit window are suppressed, and updates after the interval flow again.
func TestACPSessionUpdateEmitsContextUsage(t *testing.T) {
	exec := newTestBufferedExecutor()
	client := &acpRuntimeClient{executor: exec}

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Size: 200000, Used: 180000}},
	}))
	// Same window: rate-limited away.
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Size: 200000, Used: 190000}},
	}))
	// Interval elapsed: the next update flows.
	exec.usageMu.Lock()
	exec.lastUsageEmit = time.Now().Add(-usageUpdateMinInterval - time.Second)
	exec.usageMu.Unlock()
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Size: 200000, Used: 195000}},
	}))

	var usageEvents []Event
	for len(exec.eventCh) > 0 {
		ev := <-exec.eventCh
		if ev.Type == v1pb.CommandEventType_CONTEXT_USAGE_UPDATE {
			usageEvents = append(usageEvents, ev)
		}
	}
	require.Len(t, usageEvents, 2, "rate limit suppresses the middle update")
	require.NotNil(t, usageEvents[0].ContextUsage)
	assert.Equal(t, int64(200000), usageEvents[0].ContextUsage.Size)
	assert.Equal(t, int64(180000), usageEvents[0].ContextUsage.Used)
	assert.InDelta(t, 0.9, usageEvents[0].ContextUsage.UsageRatio, 1e-9)
	require.NotNil(t, usageEvents[1].ContextUsage)
	assert.Equal(t, int64(195000), usageEvents[1].ContextUsage.Used)
}

// TestACPSessionUpdateSkipsUsageDuringReplay: usage notifications replayed
// during session/resume must not surface as fresh CONTEXT_USAGE_UPDATE events.
func TestACPSessionUpdateSkipsUsageDuringReplay(t *testing.T) {
	exec := newTestBufferedExecutor()
	client := &acpRuntimeClient{executor: exec}

	exec.replayingHistory.Store(true)
	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Size: 200000, Used: 100000}},
	}))
	assert.Empty(t, exec.eventCh, "replayed usage must not emit events")
}

func TestACPTurnPromptText_ReanchorOnWarmTurn(t *testing.T) {
	exec := &ACPExecutor{request: Request{
		TurnPrompt:     "New messages received:\n\nwork",
		ReanchorPrompt: BuildReanchorPrompt("alice", ""),
	}}
	got := exec.turnPromptText(true)
	assert.Contains(t, got, "Re-anchor (context compaction recovery)")
	assert.Contains(t, got, "New messages received:")
	assert.True(t, strings.Index(got, "Re-anchor") < strings.Index(got, "New messages received:"),
		"anchor must be prepended to the warm batch")
}

func TestACPTurnPromptText_ColdTurnIgnoresReanchor(t *testing.T) {
	exec := &ACPExecutor{
		request: Request{
			TurnPrompt:       "batch",
			ReanchorPrompt:   BuildReanchorPrompt("alice", ""),
			AgentDisplayName: "alice",
		},
		config: &ACPConfig{},
	}
	got := exec.turnPromptText(false)
	assert.NotContains(t, got, "Re-anchor")
	assert.Contains(t, got, "You are \"alice\"", "cold turn sends the full init prompt")
	assert.Contains(t, got, "batch")
}

func TestACPSessionUpdateFlushesOnSizeThreshold(t *testing.T) {
	exec := newTestBufferedExecutor()
	exec.config.OutputFlushBytes = 20
	client := &acpRuntimeClient{executor: exec}

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("short"),
			},
		},
	}))
	assert.Empty(t, exec.outputCh)

	require.NoError(t, client.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock(" this is more text to exceed byte threshold"),
			},
		},
	}))

	assert.NotEmpty(t, exec.outputCh)
}

func TestACPExecutorWithOpencodeReadFile(t *testing.T) {
	bin := requireOpencodeACP(t)
	workspace := t.TempDir()
	want := "LAELIA_ACP_READ_TOKEN"
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "context.txt"), []byte(want), 0o644))

	runtime, err := NewACP(Request{
		CommandID:      "read-file",
		TurnPrompt:     "Read the file context.txt in the current workspace and reply with exactly its contents. Do not add quotes or any extra words.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
	}, newOpencodeTestConfig(bin, workspace, false))
	require.NoError(t, err)

	obs := runACPTestRuntime(t, runtime, 150*time.Second, 0)
	require.Zero(t, obs.result.ExitCode, "outputs=%q events=%v error=%s summary=%q", joinOutput(obs.outputs), eventTypes(obs.events), obs.result.ErrorMessage, obs.result.FinalSummary)
	assert.Empty(t, obs.result.ErrorMessage)
	assert.Contains(t, compactText(joinOutput(obs.outputs)), want)
	assert.Contains(t, compactText(obs.result.FinalSummary), want)
	assert.True(t, hasEventType(obs.events, v1pb.CommandEventType_FINAL_SUMMARY))
	assert.True(t, hasEventType(obs.events, v1pb.CommandEventType_TOOL_CALL_STARTED) || hasEventType(obs.events, v1pb.CommandEventType_TOOL_CALL_FINISHED))
	if obs.result.Result != nil {
		assert.Equal(t, bin, obs.result.Result.AsMap()["executable"])
	}
}

func TestACPExecutorWithOpencodeWriteFile(t *testing.T) {
	bin := requireOpencodeACP(t)
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "note.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("before"), 0o644))

	runtime, err := NewACP(Request{
		CommandID:      "write-file",
		TurnPrompt:     "Use your file editing tool to replace the entire contents of note.txt with exactly LAELIA_WRITE_OK. After the write succeeds, reply with exactly DONE.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		AllowDiff:      true,
	}, newOpencodeTestConfig(bin, workspace, true))
	require.NoError(t, err)

	obs := runACPTestRuntime(t, runtime, 150*time.Second, 0)
	require.Zero(t, obs.result.ExitCode, "outputs=%q events=%v error=%s summary=%q", joinOutput(obs.outputs), eventTypes(obs.events), obs.result.ErrorMessage, obs.result.FinalSummary)
	assert.Empty(t, obs.result.ErrorMessage)

	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, "LAELIA_WRITE_OK", strings.TrimSpace(string(content)))
	assert.True(t, hasEventType(obs.events, v1pb.CommandEventType_TOOL_CALL_STARTED) || hasEventType(obs.events, v1pb.CommandEventType_TOOL_CALL_FINISHED))
}

// TestACPExecutor_WedgedStartupFailsFast (T2): when the ACP subprocess spawns
// but never completes the Initialize handshake (here `sleep 9999` produces no
// JSON-RPC), the turn must fail at ~StartupTimeout — not hang to the turn
// timeout (MaxTimeoutSeconds). Before Phase 5 Initialize used the turn ctx, so a
// wedged startup (slow npx download, bad config, unresponsive server) hung for
// the whole turn. Phase 5 bounds Initialize/ResumeSession/NewSession with a
// dedicated startupCtx; the Prompt call stays on the turn ctx.
func TestACPExecutor_WedgedStartupFailsFast(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary not found in PATH")
	}
	workspace := t.TempDir()

	// A small startup timeout so the test is fast; the turn timeout stays
	// generous so a regression (Initialize on the turn ctx) hangs past the
	// runACPTestRuntime deadline below, not masked by it.
	cfg := &ACPConfig{
		MaxTimeoutSeconds: 1800,
		MaxEventCount:     10000,
		MaxOutputBytes:    1 << 20,
		OutputFlushBytes:  defaultOutputFlushBytes,
		StartupTimeout:    500 * time.Millisecond,
		Provider:          "custom",
		Executable:        sleep,
		Args:              []string{"9999"},
		WorkingDir:        workspace,
		ReadTextFiles:     true,
		WriteTextFiles:    true,
		AllowEnv:          []string{"PATH", "HOME"},
	}

	runtime, err := NewACP(Request{
		CommandID:      "wedged-startup",
		TurnPrompt:     "noop",
		WorkingDir:     workspace,
		TimeoutSeconds: 30,
	}, cfg)
	require.NoError(t, err)

	start := time.Now()
	// 8s bounds the whole drive: a Phase-5 turn finishes at ~500ms; a
	// pre-Phase-5 regression (Initialize on the 30s turn ctx) hits this and fatals.
	obs := runACPTestRuntime(t, runtime, 8*time.Second, 0)
	elapsed := time.Since(start)

	require.NotZero(t, obs.result.ExitCode, "a wedged startup must fail the turn")
	require.NotEmpty(t, obs.result.ErrorMessage, "the startup timeout must surface an error")
	// ~StartupTimeout (500ms) plus spawn/drain slack; well under the 30s turn
	// timeout a pre-Phase-5 regression would hit (which runACPTestRuntime would
	// catch as a timeout fatality at 8s).
	require.Less(t, elapsed, 5*time.Second, "wedged startup must fail at ~StartupTimeout, not the turn timeout")
}

type acpTestObservation struct {
	outputs   []OutputChunk
	events    []Event
	result    Result
	gotResult bool
}

// runACPTestRuntime drives a runtime until it produces a result. timeout is a
// per-call knob (kept configurable even though current callers use 150s) so a
// hung subprocess can be bounded tighter or looser without rewriting the helper.
//
//nolint:unparam // timeout is intentionally a tunable knob
func runACPTestRuntime(t *testing.T, runtime Runtime, timeout time.Duration, cancelAfter time.Duration) acpTestObservation {
	t.Helper()
	runtime.Start()

	if cancelAfter > 0 {
		go func() {
			timer := time.NewTimer(cancelAfter)
			defer timer.Stop()
			<-timer.C
			runtime.Cancel()
		}()
	}

	timeoutCh := time.After(timeout)
	outputCh := runtime.OutputChannel()
	eventCh := runtime.EventChannel()
	resultCh := runtime.ResultChannel()
	obs := acpTestObservation{}

	for outputCh != nil || eventCh != nil || resultCh != nil {
		select {
		case chunk, ok := <-outputCh:
			if !ok {
				outputCh = nil
				continue
			}
			obs.outputs = append(obs.outputs, chunk)
		case event, ok := <-eventCh:
			if !ok {
				eventCh = nil
				continue
			}
			obs.events = append(obs.events, event)
		case result, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			obs.result = result
			obs.gotResult = true
		case <-timeoutCh:
			runtime.Cancel()
			t.Fatalf("timed out waiting for ACP runtime; outputs=%q events=%v", joinOutput(obs.outputs), eventTypes(obs.events))
		}
	}

	require.True(t, obs.gotResult, "expected ACP result")
	return obs
}

func requireOpencodeACP(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping opencode ACP integration test in short mode")
	}
	if os.Getenv("LAELIA_RUN_OPENCODE_ACP_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_OPENCODE_ACP_TESTS=1 to run local opencode ACP integration tests")
	}
	bin := os.Getenv("LAELIA_OPENCODE_BIN")
	if bin == "" {
		lookedUp, err := exec.LookPath("opencode")
		if err != nil {
			t.Skip("opencode binary not found in PATH")
		}
		bin = lookedUp
	}
	return bin
}

func newOpencodeTestConfig(bin string, workspace string, writable bool) *ACPConfig {
	args := []string{"acp", "--pure", "--cwd", workspace}
	if model := os.Getenv("LAELIA_OPENCODE_MODEL"); model != "" {
		args = append(args, "--model", model)
	}
	if agent := os.Getenv("LAELIA_OPENCODE_AGENT"); agent != "" {
		args = append(args, "--agent", agent)
	}

	return &ACPConfig{
		MaxTimeoutSeconds:     120,
		MaxEventCount:         2000,
		MaxOutputBytes:        256 * 1024,
		Executable:            bin,
		Args:                  args,
		WorkingDir:            workspace,
		AdditionalDirectories: []string{workspace},
		AllowEnv: []string{
			"PATH",
			"HOME",
			"LANG",
			"TERM",
			"XDG_CONFIG_HOME",
			"XDG_DATA_HOME",
			"XDG_CACHE_HOME",
			"ANTHROPIC_API_KEY",
			"OPENAI_API_KEY",
			"GOOGLE_API_KEY",
			"OPENROUTER_API_KEY",
		},
		ReadTextFiles:      true,
		WriteTextFiles:     writable,
		SupportsDiff:       writable,
		SupportsRawEvents:  true,
		SupportsToolTraces: true,
	}
}

func hasEventType(events []Event, want v1pb.CommandEventType) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func eventTypes(events []Event) []v1pb.CommandEventType {
	types := make([]v1pb.CommandEventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func joinOutput(chunks []OutputChunk) string {
	parts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		parts = append(parts, fmt.Sprintf("[%s] %s", chunk.StreamType.String(), chunk.Content))
	}
	return strings.Join(parts, "\n")
}

func compactText(input string) string {
	return strings.Join(strings.Fields(input), "")
}

func newTestBufferedExecutor() *ACPExecutor {
	e := &ACPExecutor{
		ctx:             context.Background(),
		config:          &ACPConfig{OutputFlushBytes: defaultOutputFlushBytes, MaxEventCount: 10},
		outputCh:        make(chan OutputChunk, 16),
		eventCh:         make(chan Event, 16),
		toolCallStates:  map[string]*toolCallState{},
		toolCallAdapter: provider.DefaultAdapter{},
	}
	e.client = &acpRuntimeClient{executor: e}
	return e
}

// TestACPValidatePath_RejectsDanglingSymlinkEscape guards the T20 hardening of
// ACPExecutor.validatePath: a symlink inside a root pointing outside it must be
// rejected rather than followed. The pre-fix lexical fallback let it escape.
func TestACPValidatePath_RejectsDanglingSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-target")
	require.NoError(t, os.Symlink(outside, filepath.Join(workspace, "evil")))

	exec := &ACPExecutor{allowedRoots: []string{workspace}}
	_, err := exec.validatePath(filepath.Join(workspace, "evil"), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

// TestACPValidatePath_AllowsFreshPathInsideRoot: a not-yet-existing file under a
// real directory inside the root resolves and is allowed.
func TestACPValidatePath_AllowsFreshPathInsideRoot(t *testing.T) {
	workspace := t.TempDir()
	sub := filepath.Join(workspace, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o700))

	exec := &ACPExecutor{allowedRoots: []string{workspace}}
	got, err := exec.validatePath(filepath.Join(sub, "new.txt"), true)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(sub, "new.txt"), got)
}

// TestSendOutput_NonBlockingAfterCancel guards the T15 cancel-safe channel fix:
// once the session ctx is cancelled, sendOutput/sendEvent must not block on a
// full channel (the consumer has stopped draining). Before the fix a producer
// blocked forever on the cap-1024 channel, run()'s deferred close never ran,
// and the goroutine leaked.
func TestSendOutput_NonBlockingAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := &ACPExecutor{
		ctx:      ctx,
		cancel:   cancel,
		config:   &ACPConfig{MaxOutputBytes: 0}, // no output limit
		outputCh: make(chan OutputChunk, 2),
		eventCh:  make(chan Event, 2),
	}
	// Fill both channels so a blocking send would wedge.
	e.outputCh <- OutputChunk{StreamType: v1pb.CommandOutput_STDOUT, Content: "fill-out-1"}
	e.outputCh <- OutputChunk{StreamType: v1pb.CommandOutput_STDOUT, Content: "fill-out-2"}
	e.eventCh <- Event{Type: v1pb.CommandEventType_WARNING, Summary: "fill-evt-1"}
	e.eventCh <- Event{Type: v1pb.CommandEventType_WARNING, Summary: "fill-evt-2"}

	var wg sync.WaitGroup
	const producers = 8
	wg.Add(producers)
	for range producers {
		go func() {
			defer wg.Done()
			e.sendOutput(v1pb.CommandOutput_STDOUT, "flood chunk that would block a full channel")
			e.sendEvent(Event{Type: v1pb.CommandEventType_WARNING, Summary: "flood event"})
		}()
	}

	cancel() // cancelled ctx: every blocked producer selects ctx.Done and returns.

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendOutput/sendEvent leaked goroutines after cancel (blocked on full channel)")
	}
}
