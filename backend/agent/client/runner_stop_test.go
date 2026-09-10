package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestRunnerStopRecordsTerminalForCancelledTurn (design §3.6 rule 5): a runner
// torn down while a turn is in flight (RemoveAgent, a roster reconcile, machine
// shutdown) cancels the turn while the runner ctx is still alive, so the dying
// turn records its own terminal carrying the teardown cause — the manager sees
// the failure immediately instead of waiting out the reaper's grace.
func TestRunnerStopRecordsTerminalForCancelledTurn(t *testing.T) {
	sink := newMemoryTurnSink()
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		<-r.Canceled() // block until the teardown cancels the turn
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: -1, ErrorMessage: "context canceled"}
		close(r.resultCh)
		close(r.doneCh)
	})
	cs := &commandStream{
		sink: sink,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go cs.runSession(ctx, "drain-remove", "TestAgent", "", nil, "", nil)
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	r := &agentRunner{agentName: "agents/x", cs: cs}
	r.stop("agent removed from this machine")

	entries := sink.Entries()
	require.NotEmpty(t, entries)
	result := entries[len(entries)-1].GetResult()
	require.NotNil(t, result, "the torn-down turn must record its terminal")
	assert.Equal(t, "drain-remove", result.CommandId)
	assert.Equal(t, "agent removed from this machine", result.ErrorMessage,
		"the manager must see the teardown cause, not a generic cancel")
}

// TestRunnerStopSynthesizesTerminalForStalledTurn (design §3.7): a turn that
// outlives the bounded cancel (a runtime ignoring Cancel) leaves records
// without a terminal; the teardown self-check synthesizes the FAILED terminal
// with the teardown cause durably in the WAL before closing it, and the bounded
// drain delivers it to a healthy manager.
func TestRunnerStopSynthesizesTerminalForStalledTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldTimeout := inFlightTurnTimeout
	inFlightTurnTimeout = 50 * time.Millisecond
	t.Cleanup(func() { inFlightTurnTimeout = oldTimeout })

	ob, err := outbox.Open(outbox.AgentOutboxDir(home.Dir(), "mach-1", "a1"))
	require.NoError(t, err)

	transport := &fakeUploadTransport{}
	cs := &commandStream{agentName: "agents/a1", agentID: "a1", machineID: "mach-1"}
	cs.ob = ob
	cs.sink = outboxSink{ob: ob}
	cs.uploader = outbox.NewUploader(ob, transport.upload)

	// A runtime that ignores Cancel entirely: the coordination wait expires and
	// the runner ctx kills the turn with no terminal (the gap the self-check
	// closes).
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	runtime := newScriptedRuntime(func(_ *scriptedRuntime) {
		<-block
	})
	cs.newSessionRuntime = func(_ executor.Request) (executor.Runtime, error) {
		return runtime, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cs.runSession(ctx, "drain-stalled", "TestAgent", "", nil, "", nil)
	}()
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	r := &agentRunner{agentName: "agents/a1", cs: cs, cancel: cancel, done: done}
	r.stop("machine shutting down")

	transport.mu.Lock()
	var synth *v1pb.CommandResult
	for _, call := range transport.calls {
		for _, e := range call {
			if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT {
				synth = e.GetResult()
			}
		}
	}
	transport.mu.Unlock()
	require.NotNil(t, synth, "the teardown drain must upload the synthesized terminal")
	assert.Equal(t, "drain-stalled", synth.CommandId)
	assert.Equal(t, int32(-1), synth.ExitCode)
	assert.Equal(t, "machine shutting down", synth.ErrorMessage)
}
