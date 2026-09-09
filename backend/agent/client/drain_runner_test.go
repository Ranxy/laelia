package client

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// scriptedStreamSender is a streamSender whose Send runs a test-supplied
// function and records every message. It lets tests fail specific message
// kinds (e.g. only the Result) to drive runCommand's failure paths without a
// real connection.
type scriptedStreamSender struct {
	mu       sync.Mutex
	messages []*v1pb.AgentStreamMessage
	onSend   func(msg *v1pb.AgentStreamMessage) error
}

func (s *scriptedStreamSender) Send(msg *v1pb.AgentStreamMessage) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	if s.onSend == nil {
		return nil
	}
	return s.onSend(msg)
}

// Sent returns a snapshot of the messages sent through this sender.
func (s *scriptedStreamSender) Sent() []*v1pb.AgentStreamMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*v1pb.AgentStreamMessage(nil), s.messages...)
}

// TestBeginSessionDiscardsStaleReplyAndConsumesFreshReply guards the drain
// loop's BeginSession bookkeeping: a response left behind by a previous
// attempt that gave up waiting must be discarded before the next send, so a
// session is never anchored to an already-reaped command.
func TestBeginSessionDiscardsStaleReplyAndConsumesFreshReply(t *testing.T) {
	old := beginSessionResponseTimeout
	beginSessionResponseTimeout = 2 * time.Second
	defer func() { beginSessionResponseTimeout = old }()

	c := newTestDrainCommandStream()
	stale := &v1pb.BeginSessionResponse{CommandId: "stale-command"}
	c.beginRespCh <- stale

	fresh := &v1pb.BeginSessionResponse{CommandId: "fresh-command"}
	sender := &scriptedStreamSender{onSend: func(msg *v1pb.AgentStreamMessage) error {
		if msg.GetBeginSession() != nil {
			// The manager's reply races in after the send; the stale value
			// must already be gone.
			require.Empty(t, c.beginRespCh, "stale reply must be discarded before the send")
			c.beginRespCh <- fresh
		}
		return nil
	}}

	resp, err := c.beginSession(context.Background(), sender, make(chan struct{}))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "fresh-command", resp.CommandId)
}

// TestBeginSessionTimesOutWhenManagerNeverReplies guards the drain-loop wedge:
// a manager that receives BeginSession but never replies (e.g. a DB hiccup on
// its side: it logs and sends nothing) must surface as a bounded error so the
// drain loop can back off and retry, instead of waiting on beginRespCh
// forever.
func TestBeginSessionTimesOutWhenManagerNeverReplies(t *testing.T) {
	old := beginSessionResponseTimeout
	beginSessionResponseTimeout = 100 * time.Millisecond
	defer func() { beginSessionResponseTimeout = old }()

	c := newTestDrainCommandStream()
	sender := &scriptedStreamSender{}

	start := time.Now()
	_, err := c.beginSession(context.Background(), sender, make(chan struct{}))
	require.Error(t, err, "a silent manager must time out instead of wedging the drain loop")
	require.Less(t, time.Since(start), 5*time.Second)
	require.Empty(t, c.beginRespCh)
}

// TestRunCommandKeepsLocalStateWhenResultSendFails guards the orphan-reap key:
// when the result cannot be delivered (dead stream), the persisted local state
// must keep the command id so the reconnecting AgentReady.lastCommandId leads
// the manager to reap the orphaned RUNNING command.
func TestRunCommandKeepsLocalStateWhenResultSendFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sender := &scriptedStreamSender{onSend: func(msg *v1pb.AgentStreamMessage) error {
		if msg.GetResult() != nil {
			// The stream died right before the result.
			return context.Canceled
		}
		return nil
	}}

	cs := &commandStream{machineID: "reap-m", agentID: "reap-a"}
	// The runtime finished normally; only the delivery failed. The return
	// value still carries the runtime's result — what matters for the reap key
	// is the persisted state below.
	result := cs.runCommand(context.Background(), runtime, sender, executor.Request{CommandID: "cmd-keep"}, &executor.ContextState{})
	require.NotNil(t, result)

	state, err := executor.LoadLocalState("reap-m", "reap-a")
	require.NoError(t, err)
	require.NotNil(t, state, "the local state must survive a failed result send")
	require.Equal(t, "cmd-keep", state.CommandID)
}

// TestRunCommandKeepsLocalStateOnStreamSendFailure guards the mid-turn death
// path: any send failure ends the turn without a result, and the local state
// must survive with the command id so the manager's reconnect reap can close
// the RUNNING command (the pre-fix behavior cleared the state first and left
// the command RUNNING forever).
func TestRunCommandKeepsLocalStateOnStreamSendFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		close(r.resultCh)
		close(r.doneCh)
	})
	sender := &scriptedStreamSender{onSend: func(*v1pb.AgentStreamMessage) error {
		return context.Canceled
	}}

	cs := &commandStream{machineID: "reap-m", agentID: "reap-b"}
	result := cs.runCommand(context.Background(), runtime, sender, executor.Request{CommandID: "cmd-keep-failure"}, &executor.ContextState{})
	require.Nil(t, result)

	state, err := executor.LoadLocalState("reap-m", "reap-b")
	require.NoError(t, err)
	require.NotNil(t, state, "the local state must survive a mid-turn send failure")
	require.Equal(t, "cmd-keep-failure", state.CommandID)
}

// TestRunCommandClearsLocalStateAfterDeliveredResult pins the happy path: a
// delivered result clears the local state, so a later reconnect does not reap
// a command the manager already completed.
func TestRunCommandClearsLocalStateAfterDeliveredResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sender := &scriptedStreamSender{}

	cs := &commandStream{machineID: "reap-m", agentID: "reap-c"}
	result := cs.runCommand(context.Background(), runtime, sender, executor.Request{CommandID: "cmd-clear"}, &executor.ContextState{})
	require.NotNil(t, result)

	state, err := executor.LoadLocalState("reap-m", "reap-c")
	require.NoError(t, err)
	require.Nil(t, state, "a delivered result clears the local state")
}

// TestRunCommandResumesInterruptedTurnSeqs guards the resume path: when the
// persisted local state carries the same command id, the seq counters continue
// (nothing already stored is re-sent — the manager dedups on
// command_id+seq_no) and the resume is marked in the command event stream.
func TestRunCommandResumesInterruptedTurnSeqs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, executor.SaveLocalState("resume-m", "resume-a", &executor.LocalState{
		CommandID:        "cmd-resume",
		ExecutorKind:     "ACP",
		Status:           "running",
		StartedAt:        time.Now().UnixMilli(),
		LastSeqSent:      5,
		LastEventSeqSent: 7,
	}))

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		r.eventCh <- executor.Event{
			Type:    v1pb.CommandEventType_TOOL_CALL_STARTED,
			Summary: "step",
			ToolCallStarted: &v1pb.ToolCallStartedPayload{
				Title:      "step",
				ToolCallId: "tc-1",
			},
		}
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sender := &scriptedStreamSender{}

	cs := &commandStream{machineID: "resume-m", agentID: "resume-a"}
	result := cs.runCommand(context.Background(), runtime, sender, executor.Request{CommandID: "cmd-resume"}, &executor.ContextState{})
	require.NotNil(t, result)

	var lifecycleSeq, warningSeq, toolSeq int32
	for _, m := range sender.Sent() {
		ev := m.GetEvent()
		if ev == nil {
			continue
		}
		switch ev.Type {
		case v1pb.CommandEventType_LIFECYCLE:
			lifecycleSeq = ev.SeqNo
		case v1pb.CommandEventType_WARNING:
			warningSeq = ev.SeqNo
		case v1pb.CommandEventType_TOOL_CALL_STARTED:
			toolSeq = ev.SeqNo
		default:
		}
	}
	require.Equal(t, int32(8), lifecycleSeq, "the resumed turn continues the interrupted turn's event seq")
	require.Equal(t, int32(9), warningSeq, "the resume is marked in the command event stream")
	require.Equal(t, int32(10), toolSeq, "turn events continue after the resume marker")
	require.Equal(t, strings.TrimSpace(resumeTurnNotice), strings.TrimSpace(warningSummary(t, sender.Sent())))

	state, err := executor.LoadLocalState("resume-m", "resume-a")
	require.NoError(t, err)
	require.Nil(t, state, "a delivered result clears the local state")
}

// TestRunCommandFreshStateForDifferentCommand guards the seq reset: a persisted
// state for a DIFFERENT command (e.g. a stale file after a machine restart)
// must not leak its seq counters into the fresh turn.
func TestRunCommandFreshStateForDifferentCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, executor.SaveLocalState("resume-m", "resume-b", &executor.LocalState{
		CommandID:        "cmd-stale",
		LastSeqSent:      99,
		LastEventSeqSent: 99,
	}))

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sender := &scriptedStreamSender{}

	cs := &commandStream{machineID: "resume-m", agentID: "resume-b"}
	result := cs.runCommand(context.Background(), runtime, sender, executor.Request{CommandID: "cmd-fresh"}, &executor.ContextState{})
	require.NotNil(t, result)

	for _, m := range sender.Sent() {
		if ev := m.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_LIFECYCLE {
			require.Equal(t, int32(1), ev.SeqNo, "a fresh turn starts at seq 1")
		}
		if ev := m.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_WARNING {
			t.Fatal("a fresh turn must not carry the resume warning")
		}
	}
}

// TestApplyResumeTurnNotice guards the resume prompt injection.
func TestApplyResumeTurnNotice(t *testing.T) {
	require.Equal(t, "batch", applyResumeTurnNotice("batch", false))
	require.Equal(t, resumeTurnNotice, applyResumeTurnNotice("", true))
	require.Equal(t, resumeTurnNotice+"\n\nbatch", applyResumeTurnNotice("batch", true))
}

func warningSummary(t *testing.T, msgs []*v1pb.AgentStreamMessage) string {
	t.Helper()
	for _, m := range msgs {
		if ev := m.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_WARNING {
			return ev.GetWarning().GetMessage()
		}
	}
	t.Fatal("no resume warning event found")
	return ""
}

func newTestDrainCommandStream() *commandStream {
	return &commandStream{
		machineID:   "m",
		agentID:     "a",
		wakeCh:      make(chan struct{}, 1),
		beginRespCh: make(chan *v1pb.BeginSessionResponse, 1),
	}
}
