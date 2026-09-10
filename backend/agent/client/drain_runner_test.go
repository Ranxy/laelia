package client

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// scriptedStreamSender is a streamSender whose Send runs a test-supplied
// function and records every message. It lets control-plane tests (BeginSession
// bookkeeping) script the stream without a real connection.
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
	conn := newTestAgentConn()
	stale := &v1pb.BeginSessionResponse{CommandId: "stale-command"}
	conn.beginResps <- stale

	fresh := &v1pb.BeginSessionResponse{CommandId: "fresh-command"}
	conn.sender = &scriptedStreamSender{onSend: func(msg *v1pb.AgentStreamMessage) error {
		if msg.GetBeginSession() != nil {
			// The manager's reply races in after the send; the stale value
			// must already be gone.
			require.Empty(t, conn.beginResps, "stale reply must be discarded before the send")
			conn.beginResps <- fresh
		}
		return nil
	}}

	resp, err := c.beginSession(context.Background(), conn)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "fresh-command", resp.CommandId)
}

// TestBeginSessionTimesOutWhenManagerNeverReplies guards the drain-loop wedge:
// a manager that receives BeginSession but never replies (e.g. a DB hiccup on
// its side: it logs and sends nothing) must surface as a bounded error so the
// drain loop can back off and retry, instead of waiting on the reply channel
// forever.
func TestBeginSessionTimesOutWhenManagerNeverReplies(t *testing.T) {
	old := beginSessionResponseTimeout
	beginSessionResponseTimeout = 100 * time.Millisecond
	defer func() { beginSessionResponseTimeout = old }()

	c := newTestDrainCommandStream()
	conn := newTestAgentConn()

	start := time.Now()
	_, err := c.beginSession(context.Background(), conn)
	require.Error(t, err, "a silent manager must time out instead of wedging the drain loop")
	require.Less(t, time.Since(start), 5*time.Second)
	require.Empty(t, conn.beginResps)
}

// TestBeginSessionReplyIsBoundToItsConnection guards the per-connection reply
// channel: a BeginSessionResponse delivered after its connection died (or by a
// previous connection's pump) is unreachable from the next connection's
// beginSession, so a session is never anchored to a stale command.
func TestBeginSessionReplyIsBoundToItsConnection(t *testing.T) {
	c := newTestDrainCommandStream()

	oldConn := newTestAgentConn()
	oldConn.beginResps <- &v1pb.BeginSessionResponse{CommandId: "STALE-CMD"}
	c.setConn(oldConn)

	// Reconnect: the new connection's replies land in its own channel.
	newConn := newTestAgentConn()
	c.setConn(newConn)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	resp, err := c.beginSession(ctx, newConn)
	require.Error(t, err, "the new connection must wait for a NEW reply, not the stale one")
	require.Nil(t, resp)
}

// TestRunCommandKeepsLocalStateWhenTerminalUndeliverable guards the resume
// key: when the terminal record cannot be recorded (the outbox rejects it and
// the bypass fails), the persisted local state must keep the command id + seq
// counters so the next BeginSession resumes the interrupted turn and the
// manager's (command_id, seq_no) dedup keys line up.
func TestRunCommandKeepsLocalStateWhenTerminalUndeliverable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()
	sink.onAppend = func(entry *outbox.Entry) error {
		if entry.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			// The WAL rejects the terminal; no uploader to bypass through.
			return context.Canceled
		}
		return nil
	}

	cs := &commandStream{machineID: "reap-m", agentID: "reap-a", sink: sink}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-keep"}, &executor.ContextState{})
	require.NotNil(t, result)

	state, err := executor.LoadLocalState("reap-m", "reap-a")
	require.NoError(t, err)
	require.NotNil(t, state, "an undeliverable terminal keeps the local state as the resume key")
	require.Equal(t, "cmd-keep", state.CommandID)
}

// TestRunCommandAbortsTurnOnRecordAppendFailure guards the mid-turn death
// path: a record-append failure (WAL fault) ends the turn without the real
// terminal, and the synthetic FAILED terminal goes out through the bypass —
// the manager sees the turn's failure even though the WAL rejected the data.
func TestRunCommandAbortsTurnOnRecordAppendFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()
	sink.onAppend = func(entry *outbox.Entry) error {
		if entry.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT {
			// The WAL rejects the start lifecycle event.
			return context.Canceled
		}
		return nil
	}

	cs := &commandStream{machineID: "reap-m", agentID: "reap-b", sink: sink}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-keep-failure"}, &executor.ContextState{})
	require.Nil(t, result)

	// The synthetic FAILED terminal was recorded once the turn aborted.
	var failures []*v1pb.CommandResult
	for _, e := range sink.Entries() {
		if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			failures = append(failures, e.GetResult())
		}
	}
	require.Len(t, failures, 1)
	require.Equal(t, int32(-1), failures[0].GetExitCode())
	require.NotEmpty(t, failures[0].GetErrorMessage())
}

// TestRunCommandBypassesTerminalWhenOutboxFaults locks the §3.1 terminal
// bypass: when the WAL rejects the terminal, the real result goes out through
// the uploader's bypass, and a delivered bypass clears the resume key.
func TestRunCommandBypassesTerminalWhenOutboxFaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()
	sink.onAppend = func(entry *outbox.Entry) error {
		if entry.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			return context.Canceled
		}
		return nil
	}
	transport := &fakeUploadTransport{}

	ob, err := outbox.Open(t.TempDir() + "/outbox")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ob.Close() })

	cs := &commandStream{
		machineID: "reap-m",
		agentID:   "reap-d",
		sink:      sink,
		uploader:  outbox.NewUploader(ob, transport.upload),
	}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-bypass"}, &executor.ContextState{})
	require.NotNil(t, result)

	// The real terminal went out through the bypass, not a synthetic failure.
	var bypassed []*v1pb.CommandResult
	for _, e := range transport.allEntries() {
		if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			bypassed = append(bypassed, e.GetResult())
		}
	}
	require.Len(t, bypassed, 1)
	require.Equal(t, int32(0), bypassed[0].GetExitCode())
	require.Equal(t, "done", bypassed[0].GetFinalSummary())

	state, err := executor.LoadLocalState("reap-m", "reap-d")
	require.NoError(t, err)
	require.Nil(t, state, "a delivered terminal clears the resume key")
}

// TestRunCommandClearsLocalStateAfterRecordedResult pins the happy path: a
// recorded terminal clears the local state, so a later reconnect does not reap
// a command the manager already completed.
func TestRunCommandClearsLocalStateAfterRecordedResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()

	cs := &commandStream{machineID: "reap-m", agentID: "reap-c", sink: sink}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-clear"}, &executor.ContextState{})
	require.NotNil(t, result)

	state, err := executor.LoadLocalState("reap-m", "reap-c")
	require.NoError(t, err)
	require.Nil(t, state, "a recorded terminal clears the local state")
}

// TestRunCommandInterruptedTurnKeepsResumeKey locks the hybrid-phase resume
// path: a turn whose ctx dies (runner/stream teardown) records no terminal,
// and the local state survives so the next BeginSession resumes the command
// with continued seq counters.
func TestRunCommandInterruptedTurnKeepsResumeKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		<-r.Canceled() // hang until cancelled
	})
	sink := newMemoryTurnSink()

	cs := &commandStream{machineID: "resume-m", agentID: "resume-c", sink: sink}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cs.runCommand(ctx, runtime, sink, executor.Request{CommandID: "cmd-interrupted"}, &executor.ContextState{})
		close(done)
	}()
	require.Eventually(t, func() bool {
		return len(sink.Entries()) > 0
	}, time.Second, 5*time.Millisecond, "the turn must record its start event before interruption")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCommand did not return on ctx cancel")
	}

	// No terminal was recorded and the resume key survives.
	for _, e := range sink.Entries() {
		if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			t.Fatal("an interrupted turn must not record a terminal")
		}
	}
	state, err := executor.LoadLocalState("resume-m", "resume-c")
	require.NoError(t, err)
	require.NotNil(t, state, "the interrupted turn keeps the resume key")
	require.Equal(t, "cmd-interrupted", state.CommandID)
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
	sink := newMemoryTurnSink()

	cs := &commandStream{machineID: "resume-m", agentID: "resume-a", sink: sink}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-resume"}, &executor.ContextState{})
	require.NotNil(t, result)

	var lifecycleSeq, warningSeq, toolSeq int32
	for _, e := range sink.Entries() {
		ev := e.GetEvent()
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
	require.Equal(t, strings.TrimSpace(resumeTurnNotice), strings.TrimSpace(warningSummary(t, sink.Entries())))

	state, err := executor.LoadLocalState("resume-m", "resume-a")
	require.NoError(t, err)
	require.Nil(t, state, "a recorded terminal clears the local state")
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
	sink := newMemoryTurnSink()

	cs := &commandStream{machineID: "resume-m", agentID: "resume-b", sink: sink}
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-fresh"}, &executor.ContextState{})
	require.NotNil(t, result)

	for _, e := range sink.Entries() {
		if ev := e.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_LIFECYCLE {
			require.Equal(t, int32(1), ev.SeqNo, "a fresh turn starts at seq 1")
		}
		if ev := e.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_WARNING {
			t.Fatal("a fresh turn must not carry the resume warning")
		}
	}
}

// TestRunSessionBarrierBlocksUntilOtherCommandGroupDrains locks the turn-start
// barrier wiring (§3.4): a turn must not run while the outbox still holds
// another command's un-acked records; once the uploader drains them, the turn
// proceeds.
func TestRunSessionBarrierBlocksUntilOtherCommandGroupDrains(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldWindow := outbox.BatchWindow
	outbox.BatchWindow = 5 * time.Millisecond
	t.Cleanup(func() { outbox.BatchWindow = oldWindow })

	ob, err := outbox.Open(t.TempDir() + "/outbox")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ob.Close() })

	// Another command's complete group (with its terminal) is still queued.
	require.NoError(t, ob.Append(outboxProgress("prev-cmd", 1)))
	require.NoError(t, ob.Append(outboxResult("prev-cmd", 0)))

	transport := &fakeUploadTransport{}
	uploader := outbox.NewUploader(ob, transport.upload)
	uploaderCtx, uploaderCancel := context.WithCancel(context.Background())
	t.Cleanup(uploaderCancel)
	go uploader.Run(uploaderCtx)

	runtimeStarted := make(chan struct{}, 1)
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		runtimeStarted <- struct{}{}
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()
	cs := &commandStream{
		sink:     sink,
		uploader: uploader,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer runCancel()
	go func() {
		cs.runSession(runCtx, nil, "new-cmd", "TestAgent", "", nil, "", nil)
	}()

	select {
	case <-runtimeStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never ran; the barrier did not release after the group drained")
	}
	require.NotEmpty(t, sink.Entries(), "the turn's records must land in the sink once it runs")
}

// TestRunSessionBarrierAbortsOnUndrainedGroup locks the barrier's ctx escape:
// when the group cannot drain (a dead manager), the barrier releases on ctx
// expiry and the turn is not run — the drain loop must not wedge on a blocked
// outbox.
func TestRunSessionBarrierAbortsOnUndrainedGroup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldWindow := outbox.BatchWindow
	outbox.BatchWindow = 5 * time.Millisecond
	t.Cleanup(func() { outbox.BatchWindow = oldWindow })

	ob, err := outbox.Open(t.TempDir() + "/outbox")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ob.Close() })
	require.NoError(t, ob.Append(outboxProgress("prev-cmd", 1)))
	require.NoError(t, ob.Append(outboxResult("prev-cmd", 0)))

	transport := &fakeUploadTransport{err: errors.New("manager unreachable")}
	uploader := outbox.NewUploader(ob, transport.upload)

	runtimeStarted := make(chan struct{}, 1)
	sink := newMemoryTurnSink()
	cs := &commandStream{
		sink:     sink,
		uploader: uploader,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			runtimeStarted <- struct{}{}
			return nil, nil
		},
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer runCancel()
	cs.runSession(runCtx, nil, "new-cmd", "TestAgent", "", nil, "", nil)

	select {
	case <-runtimeStarted:
		t.Fatal("the turn must not run while the outbox cannot drain")
	case <-time.After(100 * time.Millisecond):
	}
	require.Empty(t, sink.Entries())
}

// outboxProgress builds a progress record for barrier tests.
//
// nolint:unused // used by barrier tests
func outboxProgress(commandID string, seq int32) *outbox.Entry {
	return progressEnvelope(commandID, executor.OutputChunk{
		StreamType: v1pb.CommandOutput_STDOUT,
		Content:    "x",
		SeqNo:      seq,
	})
}

// outboxResult builds a terminal record for barrier tests.
//
// nolint:unused // used by barrier tests
func outboxResult(commandID string, exitCode int32) *outbox.Entry {
	return resultEnvelope(&v1pb.CommandResult{
		CommandId: commandID,
		ExitCode:  exitCode,
	})
}

// TestApplyResumeTurnNotice guards the resume prompt injection.
func TestApplyResumeTurnNotice(t *testing.T) {
	require.Equal(t, "batch", applyResumeTurnNotice("batch", false))
	require.Equal(t, resumeTurnNotice, applyResumeTurnNotice("", true))
	require.Equal(t, resumeTurnNotice+"\n\nbatch", applyResumeTurnNotice("batch", true))
}

func warningSummary(t *testing.T, entries []*outbox.Entry) string {
	t.Helper()
	for _, e := range entries {
		if ev := e.GetEvent(); ev != nil && ev.Type == v1pb.CommandEventType_WARNING {
			return ev.GetWarning().GetMessage()
		}
	}
	t.Fatal("no resume warning event found")
	return ""
}

func newTestDrainCommandStream() *commandStream {
	return &commandStream{
		machineID: "m",
		agentID:   "a",
		wakeCh:    make(chan struct{}, 1),
		connReady: make(chan struct{}, 1),
	}
}

// newTestAgentConn returns a connection whose sender records messages.
func newTestAgentConn() *agentConn {
	return &agentConn{
		sender:     &scriptedStreamSender{},
		done:       make(chan struct{}),
		beginResps: make(chan *v1pb.BeginSessionResponse, 1),
	}
}
