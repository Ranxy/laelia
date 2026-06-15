package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	client := &acpRuntimeClient{executor: &ACPExecutor{profile: ACPProfile{AutoApproveToolKinds: []string{"read"}}}}

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
		request:  Request{AllowDiff: true},
		profile:  ACPProfile{SupportsDiff: true, SupportsToolTraces: true},
		config:   &ACPConfig{MaxEventCount: 10},
		outputCh: make(chan OutputChunk, 4),
		eventCh:  make(chan Event, 4),
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
	exec.profile.SupportsRawEvents = true
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
	payload := ev.Payload
	assert.NotNil(t, payload)
	assert.Equal(t, 5, payload["batch_size"])
	events, ok := payload["events"].([]any)
	require.True(t, ok, "events should be a slice")
	assert.Len(t, events, 5)
}

func TestACPSessionUpdateBatchesRawEventsAcrossBoundaries(t *testing.T) {
	exec := newTestBufferedExecutor()
	exec.profile.SupportsRawEvents = true
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
		Instruction:    "Read the file context.txt in the current workspace and reply with exactly its contents. Do not add quotes or any extra words.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		ExecutorKind:   v1pb.ExecutorKind_ACP,
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
		assert.Equal(t, "opencode", obs.result.Result.AsMap()["profile"])
	}
}

func TestACPExecutorWithOpencodeWriteFile(t *testing.T) {
	bin := requireOpencodeACP(t)
	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "note.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("before"), 0o644))

	runtime, err := NewACP(Request{
		CommandID:      "write-file",
		Instruction:    "Use your file editing tool to replace the entire contents of note.txt with exactly LAELIA_WRITE_OK. After the write succeeds, reply with exactly DONE.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		ExecutorKind:   v1pb.ExecutorKind_ACP,
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

type acpTestObservation struct {
	outputs   []OutputChunk
	events    []Event
	result    Result
	gotResult bool
}

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

	toolKinds := []string{"read", "search", "think", "fetch"}
	if writable {
		toolKinds = append(toolKinds, "edit", "move")
	}

	return &ACPConfig{
		DefaultProfile:    "opencode",
		MaxTimeoutSeconds: 120,
		MaxEventCount:     2000,
		MaxOutputBytes:    256 * 1024,
		Profiles: map[string]ACPProfile{
			"opencode": {
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
				ReadTextFiles:        true,
				WriteTextFiles:       writable,
				AutoApproveToolKinds: toolKinds,
				SupportsDiff:         writable,
				SupportsRawEvents:    true,
				SupportsToolTraces:   true,
			},
		},
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
		config:   &ACPConfig{OutputFlushBytes: defaultOutputFlushBytes, MaxEventCount: 10},
		outputCh: make(chan OutputChunk, 16),
		eventCh:  make(chan Event, 16),
	}
	e.client = &acpRuntimeClient{executor: e}
	return e
}
