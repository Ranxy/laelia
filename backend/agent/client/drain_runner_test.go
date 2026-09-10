package client

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

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

// TestRunCommandInterruptedTurnRecordsNoTerminal locks the interrupt path: a
// turn whose ctx dies (runner teardown) records no terminal — the manager
// keeps the command RUNNING until its reaper closes it. The leftover local
// state is harmless: seq spaces are per-command and the manager only mints
// fresh ids, so no later turn can collide with it.
func TestRunCommandInterruptedTurnRecordsNoTerminal(t *testing.T) {
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

	// No terminal was recorded.
	for _, e := range sink.Entries() {
		if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
			t.Fatal("an interrupted turn must not record a terminal")
		}
	}
}

// TestRunCommandStartsSeqAtOneForEachTurn guards the per-turn seq spaces: a
// fresh turn starts both counters at 1 even when a stale local state file (an
// interrupted turn from an earlier command) survives — the manager's
// (command_id, seq_no) dedup keys are scoped per command, and a leftover
// RUNNING row is never resumed, so there is nothing to continue from.
func TestRunCommandStartsSeqAtOneForEachTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, executor.SaveLocalState("resume-m", "resume-a", &executor.LocalState{
		CommandID:        "cmd-stale",
		ExecutorKind:     "ACP",
		Status:           "running",
		StartedAt:        time.Now().UnixMilli(),
		LastSeqSent:      99,
		LastEventSeqSent: 99,
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
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-fresh"}, &executor.ContextState{})
	require.NotNil(t, result)

	var lifecycleSeq, toolSeq int32
	var progressSeqs []int32
	for _, e := range sink.Entries() {
		if ev := e.GetEvent(); ev != nil {
			switch ev.Type {
			case v1pb.CommandEventType_LIFECYCLE:
				lifecycleSeq = ev.SeqNo
			case v1pb.CommandEventType_TOOL_CALL_STARTED:
				toolSeq = ev.SeqNo
			default:
			}
		}
		if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS {
			progressSeqs = append(progressSeqs, e.GetProgress().GetSeqNo())
		}
	}
	// The stale state file must not leak its counters: lifecycle=1, the tool
	// event follows, and the progress chunk keeps its own per-turn numbering.
	require.Equal(t, int32(1), lifecycleSeq, "a fresh turn starts the event seq at 1")
	require.Equal(t, int32(2), toolSeq, "events count per-turn from the lifecycle")
	require.Empty(t, progressSeqs, "no progress this turn; the seq space is fresh")
	require.Equal(t, int32(0), result.LastSeqNo, "the result's progress cursor counts only this turn's chunks")

	state, err := executor.LoadLocalState("resume-m", "resume-a")
	require.NoError(t, err)
	require.Nil(t, state, "a recorded terminal clears the local state")
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
		cs.runSession(runCtx, "new-cmd", "TestAgent", "", nil, "", nil)
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
	cs.runSession(runCtx, "new-cmd", "TestAgent", "", nil, "", nil)

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
