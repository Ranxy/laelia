package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestReanchorPromptDecision(t *testing.T) {
	state := &executor.ContextState{NeedsReanchor: true}
	got := reanchorPrompt(state, "alice")
	assert.Contains(t, got, "Re-anchor (context compaction recovery)")
	assert.False(t, state.NeedsReanchor, "decision is consumed")
	assert.Zero(t, state.Session.Turns)

	state = &executor.ContextState{Session: executor.SessionHealth{Turns: reanchorEveryTurns}}
	assert.NotEmpty(t, reanchorPrompt(state, "alice"), "periodic re-anchor fires at the warm-turn threshold")
	assert.Zero(t, state.Session.Turns)

	state = &executor.ContextState{Session: executor.SessionHealth{Turns: reanchorEveryTurns - 1}}
	assert.Empty(t, reanchorPrompt(state, "alice"))
	assert.Equal(t, reanchorEveryTurns-1, state.Session.Turns, "below threshold leaves the counter alone")

	assert.Empty(t, reanchorPrompt(nil, "alice"))
}

func TestAppendContextWarning(t *testing.T) {
	base := "New messages received:\n\nwork"
	state := &executor.ContextState{Usage: executor.ContextUsage{Size: 200000, Used: 190000}}
	got := appendContextWarning(base, state)
	assert.Contains(t, got, "Context warning: your context window is ~95% full (190000/200000 tokens)")
	assert.Contains(t, got, "write durable knowledge to MEMORY.md")

	state.Usage.Used = 100000
	assert.Equal(t, base, appendContextWarning(base, state), "below threshold leaves the batch unchanged")
	assert.Equal(t, base, appendContextWarning(base, nil))
	assert.Equal(t, "", appendContextWarning("", &executor.ContextState{Usage: executor.ContextUsage{Size: 100, Used: 100}}))
}

func TestRunCommandEmitsInferredCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	ctxState := &executor.ContextState{
		Usage: executor.ContextUsage{Size: 200000, Used: 180000},
	}
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		r.eventCh <- executor.Event{
			Type:    v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
			Summary: "Context usage: 100000/200000 tokens",
			ContextUsage: &v1pb.ContextUsagePayload{
				Size:       200000,
				Used:       100000,
				UsageRatio: 0.5,
			},
		}
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done", Fingerprint: "fp", Resumed: true}
		close(r.resultCh)
		close(r.doneCh)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	var result *executor.Result
	go func() {
		result = (&commandStream{machineID: "m", agentID: "a"}).runCommand(ctx, runtime, stream, executor.Request{CommandID: "ctx-1"}, ctxState)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCommand did not complete")
	}

	require.NotNil(t, result)
	var inferredFinished bool
	for _, m := range recorder.Messages() {
		ev := m.GetEvent()
		if ev == nil || ev.Type != v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED {
			continue
		}
		require.NotNil(t, ev.GetContextCompaction())
		inferredFinished = ev.GetContextCompaction().Inferred
	}
	assert.True(t, inferredFinished, "usage drop must emit an inferred compaction finish")
	assert.True(t, ctxState.NeedsReanchor, "inferred compaction marks the next turn for re-anchor")
	assert.Equal(t, int64(1), ctxState.Compaction.Count)
	assert.Equal(t, int64(100000), ctxState.Usage.Used, "usage state must reflect the latest observation")
	assert.False(t, ctxState.Compaction.Active)
}

func TestRunCommandUsageDropDuringAgentChunkDoesNotInfer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	ctxState := &executor.ContextState{
		Usage: executor.ContextUsage{Size: 200000, Used: 180000},
	}
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		r.eventCh <- executor.Event{
			Type:    v1pb.CommandEventType_RAW_ACP,
			Summary: "agent_message_chunk",
			RawAcp:  &v1pb.RawAcpPayload{Data: nil},
		}
		r.eventCh <- executor.Event{
			Type:    v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
			Summary: "Context usage: 100000/200000 tokens",
			ContextUsage: &v1pb.ContextUsagePayload{
				Size:       200000,
				Used:       100000,
				UsageRatio: 0.5,
			},
		}
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done", Fingerprint: "fp", Resumed: true}
		close(r.resultCh)
		close(r.doneCh)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		(&commandStream{machineID: "m", agentID: "a"}).runCommand(ctx, runtime, stream, executor.Request{CommandID: "ctx-2"}, ctxState)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCommand did not complete")
	}

	for _, m := range recorder.Messages() {
		ev := m.GetEvent()
		if ev != nil && ev.Type == v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED {
			t.Fatal("usage drop during active agent message streaming must not infer compaction")
		}
	}
	assert.False(t, ctxState.NeedsReanchor)
	assert.Equal(t, int64(100000), ctxState.Usage.Used)
}

func TestRunCommandCompactionWatchdogWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old := compactionStaleTimeout
	compactionStaleTimeout = 80 * time.Millisecond
	defer func() { compactionStaleTimeout = old }()

	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	ctxState := &executor.ContextState{}
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		r.eventCh <- executor.Event{
			Type:    v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED,
			Summary: "Context compaction started",
			ContextCompaction: &v1pb.ContextCompactionPayload{
				Reason: "window full",
			},
		}
		time.Sleep(200 * time.Millisecond) // let the watchdog fire
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done", Fingerprint: "fp", Resumed: true}
		close(r.resultCh)
		close(r.doneCh)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		(&commandStream{machineID: "m", agentID: "a"}).runCommand(ctx, runtime, stream, executor.Request{CommandID: "ctx-3"}, ctxState)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runCommand did not complete")
	}

	var sawStale bool
	for _, m := range recorder.Messages() {
		ev := m.GetEvent()
		if ev != nil && ev.Type == v1pb.CommandEventType_WARNING {
			assert.Equal(t, "Context compaction still running; no finish event observed", ev.Summary)
			sawStale = true
		}
	}
	assert.True(t, sawStale, "watchdog must surface the stale compaction warning")
	assert.True(t, ctxState.Compaction.Active, "no finish event observed, so compaction stays active")
}

func TestRunSessionReanchorInjectionAndPersistence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, executor.SaveContextState("m", "a", &executor.ContextState{
		NeedsReanchor: true,
		Fingerprint:   "fp",
	}))

	stream, _, cleanup := newTestCommandChannel(t)
	defer cleanup()

	cs := &commandStream{machineID: "m", agentID: "a"}
	var gotReq executor.Request
	cs.newSessionRuntime = func(req executor.Request) (executor.Runtime, error) {
		gotReq = req
		runtime := newScriptedRuntime(func(r *scriptedRuntime) {
			close(r.outputCh)
			close(r.eventCh)
			r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done", Fingerprint: "fp", Resumed: true}
			close(r.resultCh)
			close(r.doneCh)
		})
		return runtime, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		cs.runSession(ctx, stream, "drain-1", "TestAgent")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runSession did not complete")
	}

	assert.Contains(t, gotReq.ReanchorPrompt, "Re-anchor (context compaction recovery)")
	assert.Contains(t, gotReq.ReanchorPrompt, `You are "TestAgent"`)

	state, err := executor.LoadContextState("m", "a")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.False(t, state.NeedsReanchor, "flag consumed by the injection")
	assert.Equal(t, 1, state.Session.Turns, "warm anchored turn counts toward the next periodic re-anchor")
	assert.Equal(t, "fp", state.Fingerprint)
}

func TestRunSessionInitializesContextStateForFreshAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stream, _, cleanup := newTestCommandChannel(t)
	defer cleanup()

	cs := &commandStream{machineID: "m", agentID: "fresh"}
	cs.newSessionRuntime = func(_ executor.Request) (executor.Runtime, error) {
		runtime := newScriptedRuntime(func(r *scriptedRuntime) {
			close(r.outputCh)
			close(r.eventCh)
			r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done", Fingerprint: "fp", Resumed: true}
			close(r.resultCh)
			close(r.doneCh)
		})
		return runtime, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		cs.runSession(ctx, stream, "drain-1", "FreshAgent")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runSession did not complete")
	}

	state, err := executor.LoadContextState("m", "fresh")
	require.NoError(t, err)
	require.NotNil(t, state, "a fresh agent must get a persisted context state after the first turn")
	assert.Equal(t, "fp", state.Fingerprint)
	assert.Equal(t, 1, state.Session.Turns)
	assert.False(t, state.NeedsReanchor)
}

func TestPersistContextStateFingerprintReset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cs := &commandStream{machineID: "m", agentID: "a"}
	state := &executor.ContextState{
		Usage:         executor.ContextUsage{Size: 100, Used: 90},
		Compaction:    executor.CompactionInfo{Count: 3, Active: true},
		Session:       executor.SessionHealth{Turns: 5, ColdStarts: 1},
		NeedsReanchor: true,
		Fingerprint:   "old",
	}
	cs.persistContextState(state, &executor.Result{Fingerprint: "new", Resumed: false})

	assert.Equal(t, "new", state.Fingerprint)
	assert.Zero(t, state.Usage)
	assert.Zero(t, state.Compaction)
	assert.Zero(t, state.Session.Turns)
	assert.Equal(t, 1, state.Session.ColdStarts, "the cold turn is counted after the reset")
	assert.False(t, state.NeedsReanchor)

	loaded, err := executor.LoadContextState("m", "a")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "new", loaded.Fingerprint)
	assert.Equal(t, 1, loaded.Session.ColdStarts)
}
