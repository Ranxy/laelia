package executor

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestBeginToolCallStartedDedup(t *testing.T) {
	e := &ACPExecutor{toolCallStates: map[string]*toolCallState{}}

	// First sighting of an id claims it and returns the (empty) stored title.
	title, ok := e.beginToolCallStarted("tc-1")
	assert.True(t, ok)
	assert.Equal(t, "", title)
	// Subsequent sightings are no-ops.
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)

	// A different id is still claimed on first sight.
	_, ok = e.beginToolCallStarted("tc-2")
	assert.True(t, ok)
	_, ok = e.beginToolCallStarted("tc-2")
	assert.False(t, ok)

	// Empty id never claims.
	_, ok = e.beginToolCallStarted("")
	assert.False(t, ok)
}

func TestRecordToolCallTitleCarriesTitleToDeferredStart(t *testing.T) {
	e := &ACPExecutor{toolCallStates: map[string]*toolCallState{}}

	// A ToolCall create event records the title but does NOT mark started, so a
	// later ToolCallUpdate carrying RawInput can still emit the STARTED with the
	// stored title (this is the claude-code flow: empty-input create then a
	// content-only update with the command).
	e.recordToolCallTitle("tc-1", "Terminal")
	title, ok := e.beginToolCallStarted("tc-1")
	assert.True(t, ok)
	assert.Equal(t, "Terminal", title)
	// The STARTED has now been emitted; a later status update must not emit a
	// second STARTED.
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)

	// Empty id is a no-op and does not pollute state.
	e.recordToolCallTitle("", "Whatever")
	_, ok = e.beginToolCallStarted("")
	assert.False(t, ok)
}

func TestToolPayloadStruct(t *testing.T) {
	// nil/empty -> nil (frontend "Input not captured" fallback).
	assert.Nil(t, toolPayloadStruct(nil))
	assert.Nil(t, toolPayloadStruct(map[string]any{}))

	// Map value passes through unchanged.
	s := toolPayloadStruct(map[string]any{"command": "ls", "description": "list files"})
	assert.NotNil(t, s)
	assert.Equal(t, "ls", s.AsMap()["command"])

	// Scalar (string) output is wrapped under "value" so structpb.NewStruct
	// accepts it (rawOutput is frequently a JSON string).
	out := toolPayloadStruct("Channels with unread messages")
	assert.NotNil(t, out)
	assert.Equal(t, "Channels with unread messages", out.AsMap()["value"])
}

func TestResolveToolCallAdapter(t *testing.T) {
	assert.Equal(t, provider.OpenCodeAdapter{}, resolveToolCallAdapter(&ACPConfig{Provider: "opencode"}))
	assert.Equal(t, provider.DefaultAdapter{}, resolveToolCallAdapter(&ACPConfig{Provider: "claude-code"}))
	// Legacy/empty provider (Rei): sniff the launch command.
	assert.Equal(t, provider.OpenCodeAdapter{}, resolveToolCallAdapter(&ACPConfig{
		Executable: "/home/ran/.opencode/bin/opencode",
		Args:       []string{"acp", "--pure"},
	}))
	assert.Equal(t, provider.DefaultAdapter{}, resolveToolCallAdapter(&ACPConfig{
		Executable: "npx",
		Args:       []string{"-y", "@agentclientprotocol/claude-agent-acp@latest"},
	}))
	// Unknown agent falls back to the default adapter.
	assert.Equal(t, provider.DefaultAdapter{}, resolveToolCallAdapter(&ACPConfig{
		Executable: "/usr/local/bin/something-else",
		Args:       []string{"acp"},
	}))
}

func newToolCallTestExecutor(adapter provider.ToolCallAdapter) *ACPExecutor {
	e := &ACPExecutor{
		ctx:             context.Background(),
		config:          &ACPConfig{SupportsToolTraces: true},
		outputCh:        make(chan OutputChunk, 16),
		eventCh:         make(chan Event, 32),
		toolCallStates:  map[string]*toolCallState{},
		toolCallAdapter: adapter,
	}
	e.client = &acpRuntimeClient{executor: e}
	return e
}

func ptrStatus(s acp.ToolCallStatus) *acp.ToolCallStatus { return &s }

func drainToolCallEvents(ch <-chan Event) []Event {
	var out []Event
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func countToolCallEvents(events []Event) (started, finished int) {
	for _, ev := range events {
		switch ev.Type {
		case v1pb.CommandEventType_TOOL_CALL_STARTED:
			started++
		case v1pb.CommandEventType_TOOL_CALL_FINISHED:
			finished++
		default:
		}
	}
	return started, finished
}

func sessionUpdate(t *testing.T, e *ACPExecutor, upd acp.SessionUpdate) {
	t.Helper()
	require.NoError(t, e.client.SessionUpdate(context.Background(), acp.SessionNotification{Update: upd}))
}

// TestSessionUpdateOpenCodeToolCallFlow is the regression test for the opencode
// bug: opencode's create carries only partial {cwd} metadata under a generic
// title, and the real command arrives in the first in_progress status update.
// The fix must surface the command in the STARTED and emit exactly one
// STARTED+FINISHED pair per tool call.
func TestSessionUpdateOpenCodeToolCallFlow(t *testing.T) {
	e := newToolCallTestExecutor(provider.OpenCodeAdapter{})
	realTitle := "laelia-machine reminder list-due"

	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "tc-1", Title: "bash",
		RawInput: map[string]any{"cwd": "/home/ran"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Title: &realTitle,
		Status: ptrStatus(acp.ToolCallStatusInProgress),
		RawInput: map[string]any{
			"cwd":     "/home/ran",
			"command": "laelia-machine reminder list-due",
		},
	}})
	// A repeated in_progress update must not re-emit STARTED or a FINISHED.
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Status: ptrStatus(acp.ToolCallStatusInProgress),
		RawInput: map[string]any{"cwd": "/home/ran", "command": "laelia-machine reminder list-due"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Status: ptrStatus(acp.ToolCallStatusCompleted),
		RawOutput: map[string]any{"output": "no due reminders"},
	}})

	events := drainToolCallEvents(e.eventCh)
	started, finished := countToolCallEvents(events)
	assert.Equal(t, 1, started, "opencode should emit exactly one STARTED (from the first in_progress)")
	assert.Equal(t, 1, finished, "opencode should emit exactly one FINISHED (on completed only)")

	var st *v1pb.ToolCallStartedPayload
	var fin *v1pb.ToolCallFinishedPayload
	for _, ev := range events {
		switch ev.Type {
		case v1pb.CommandEventType_TOOL_CALL_STARTED:
			st = ev.ToolCallStarted
		case v1pb.CommandEventType_TOOL_CALL_FINISHED:
			fin = ev.ToolCallFinished
		default:
		}
	}
	require.NotNil(t, st)
	require.NotNil(t, fin)
	assert.Equal(t, "laelia-machine reminder list-due", st.GetTitle())
	assert.Equal(t, "laelia-machine reminder list-due", st.GetRawInput().AsMap()["command"])
	assert.Equal(t, "completed", fin.GetStatus())
	assert.Equal(t, "no due reminders", fin.GetRawOutput().AsMap()["output"])
}

// TestSessionUpdateClaudeCodeToolCallFlow is the no-regression test for
// claude-code: empty-input create, then a content-only update carrying the
// command, then a completed status update with the output.
func TestSessionUpdateClaudeCodeToolCallFlow(t *testing.T) {
	e := newToolCallTestExecutor(provider.DefaultAdapter{})

	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "tc-1", Title: "Terminal",
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1",
		RawInput:   map[string]any{"command": "ls", "description": "list files"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Status: ptrStatus(acp.ToolCallStatusCompleted),
		RawOutput: map[string]any{"value": "file1\nfile2"},
	}})

	events := drainToolCallEvents(e.eventCh)
	started, finished := countToolCallEvents(events)
	assert.Equal(t, 1, started, "claude-code should emit one STARTED (from the content-only update)")
	assert.Equal(t, 1, finished, "claude-code should emit one FINISHED (on completed)")

	var st *v1pb.ToolCallStartedPayload
	for _, ev := range events {
		if ev.Type == v1pb.CommandEventType_TOOL_CALL_STARTED {
			st = ev.ToolCallStarted
		}
	}
	require.NotNil(t, st)
	assert.Equal(t, "Terminal", st.GetTitle())
	assert.Equal(t, "ls", st.GetRawInput().AsMap()["command"])
}

// TestSessionUpdateGenericToolCallFlow: an agent that puts full RawInput at the
// create emits STARTED at the create and FINISHED on completed.
func TestSessionUpdateGenericToolCallFlow(t *testing.T) {
	e := newToolCallTestExecutor(provider.DefaultAdapter{})

	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "tc-1", Title: "bash",
		RawInput: map[string]any{"command": "echo hi"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Status: ptrStatus(acp.ToolCallStatusCompleted),
		RawOutput: map[string]any{"output": "hi"},
	}})

	started, finished := countToolCallEvents(drainToolCallEvents(e.eventCh))
	assert.Equal(t, 1, started)
	assert.Equal(t, 1, finished)
}

// TestSessionUpdateOpenCodeNeverReachesTerminal: a tool call that only sends
// in_progress updates emits a STARTED but no FINISHED.
func TestSessionUpdateOpenCodeNeverReachesTerminal(t *testing.T) {
	e := newToolCallTestExecutor(provider.OpenCodeAdapter{})
	realTitle := "laelia-machine reminder list-due"

	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "tc-1", Title: "bash", RawInput: map[string]any{"cwd": "/home/ran"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "tc-1", Title: &realTitle, Status: ptrStatus(acp.ToolCallStatusInProgress),
		RawInput: map[string]any{"cwd": "/home/ran", "command": "laelia-machine reminder list-due"},
	}})

	started, finished := countToolCallEvents(drainToolCallEvents(e.eventCh))
	assert.Equal(t, 1, started)
	assert.Equal(t, 0, finished, "no FINISHED until a terminal status arrives")
}

// TestSessionUpdateInterleavedToolCalls: two interleaved opencode tool calls
// each produce one STARTED + one FINISHED, FIFO-paired by id order.
func TestSessionUpdateInterleavedToolCalls(t *testing.T) {
	e := newToolCallTestExecutor(provider.OpenCodeAdapter{})
	titleA := "cmdA"
	titleB := "cmdB"

	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "A", Title: "bash", RawInput: map[string]any{"cwd": "/x"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		ToolCallId: "B", Title: "bash", RawInput: map[string]any{"cwd": "/x"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "A", Title: &titleA, Status: ptrStatus(acp.ToolCallStatusInProgress),
		RawInput: map[string]any{"cwd": "/x", "command": "cmdA"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "B", Title: &titleB, Status: ptrStatus(acp.ToolCallStatusInProgress),
		RawInput: map[string]any{"cwd": "/x", "command": "cmdB"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "A", Status: ptrStatus(acp.ToolCallStatusCompleted),
		RawOutput: map[string]any{"output": "outA"},
	}})
	sessionUpdate(t, e, acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		ToolCallId: "B", Status: ptrStatus(acp.ToolCallStatusCompleted),
		RawOutput: map[string]any{"output": "outB"},
	}})

	events := drainToolCallEvents(e.eventCh)
	started, finished := countToolCallEvents(events)
	assert.Equal(t, 2, started)
	assert.Equal(t, 2, finished)

	// FIFO pairing: STARTED order is A, B; FINISHED order is A, B.
	var startedTitles, finishedOutputs []string
	for _, ev := range events {
		switch ev.Type {
		case v1pb.CommandEventType_TOOL_CALL_STARTED:
			startedTitles = append(startedTitles, ev.ToolCallStarted.GetTitle())
		case v1pb.CommandEventType_TOOL_CALL_FINISHED:
			finishedOutputs = append(finishedOutputs, ev.ToolCallFinished.GetRawOutput().AsMap()["output"].(string))
		default:
		}
	}
	assert.Equal(t, []string{"cmdA", "cmdB"}, startedTitles)
	assert.Equal(t, []string{"outA", "outB"}, finishedOutputs)
}
