package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/pi"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// newReloadTestRunner builds a runner wired for applyAssignment tests: a pi
// binary stub (dev ResolveBinary reads LAELIA_PI_BINARY), an isolated home so
// the working dirs land in a temp dir, and an unstarted daemon (buildPiConfig
// only reads socket path / session token / the mcp proxy URL from it — New
// initializes the daemon's maps without binding the socket).
func newReloadTestRunner(t *testing.T, agentID string) *agentRunner {
	t.Helper()
	tmp := t.TempDir()
	piBin := filepath.Join(tmp, "pi")
	require.NoError(t, os.WriteFile(piBin, []byte("stub"), 0o755))
	t.Setenv("LAELIA_PI_BINARY", piBin)
	t.Setenv(home.EnvDir, filepath.Join(tmp, "home"))
	daemon, err := daemonsrv.New("", "mach-1", func() string { return "" }, nil)
	require.NoError(t, err)
	return &agentRunner{
		machine:   &MachineClient{machineID: "mach-1"},
		daemon:    daemon,
		agentName: "agents/" + agentID,
		agentID:   agentID,
	}
}

func piAssignment(model string) *v1pb.AgentAssignment {
	return &v1pb.AgentAssignment{
		AgentName:        "agents/a1",
		AgentDisplayName: "A1",
		AcpConfig: &v1pb.AgentACPConfig{
			Provider:    pi.BuiltinPiProvider,
			ApiProvider: pi.APIProviderDeepseek,
			Model:       model,
			ApiKey:      "sk-test",
		},
	}
}

// TestApplyAssignmentUnchangedKeepsInFlightTurn guards the reconnect contract
// (design §3.2): the roster resync runs on every machine reconnect (e.g. a
// proxy cutting the control stream), and re-applying an UNCHANGED assignment
// must keep the warm session and leave a running turn alone — the reconcile
// must never be the thing that fails the turn with "config reloaded mid-turn".
func TestApplyAssignmentUnchangedKeepsInFlightTurn(t *testing.T) {
	r := newReloadTestRunner(t, "a1")
	r.applyAssignment(piAssignment("deepseek-chat"))
	require.NotNil(t, r.currentPiConfig(), "pi config must resolve in the test harness")

	// A turn in flight on the warm pi session: blocks until released (or
	// cancelled), then reports its own outcome.
	release := make(chan struct{})
	runtime := newScriptedRuntime(func(rt *scriptedRuntime) {
		select {
		case <-rt.Canceled():
		case <-release:
		}
		close(rt.outputCh)
		close(rt.eventCh)
		if rt.cancelCount.Load() > 0 {
			rt.resultCh <- executor.Result{ExitCode: -1, ErrorMessage: "context canceled"}
		} else {
			rt.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "completed"}
		}
		close(rt.resultCh)
		close(rt.doneCh)
	})
	sink := newMemoryTurnSink()
	cs := &commandStream{
		sink: sink,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}
	r.cs = cs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cs.runSession(ctx, "drain-keep", "A1", "", nil, "", nil)
	}()
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	r.mu.Lock()
	warmSession := r.piSession
	r.mu.Unlock()

	// The reconnect's roster resync re-applies the same assignment.
	r.applyAssignment(piAssignment("deepseek-chat"))

	assert.Equal(t, int32(0), runtime.cancelCount.Load(),
		"an unchanged reconcile must not cancel the in-flight turn")
	assert.True(t, cs.InFlight(), "the turn must still be running after the reconcile")
	r.mu.Lock()
	assert.Same(t, warmSession, r.piSession, "the warm session must be kept")
	r.mu.Unlock()

	// The turn then completes normally.
	close(release)
	require.Eventually(t, func() bool { return !cs.InFlight() }, 2*time.Second, 5*time.Millisecond)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSession did not finish")
	}
	result := sink.Entries()[len(sink.Entries())-1].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, int32(0), result.ExitCode, "the untouched turn must complete successfully")
	assert.Empty(t, result.ErrorMessage, "no reload cause may be recorded")
}

// TestApplyAssignmentChangedFingerprintRestartsPiSession is the complementary
// case: a genuinely changed launch shape still cancels the in-flight turn with
// the explicit reload cause, waits for it to end, and swaps the subprocess.
func TestApplyAssignmentChangedFingerprintRestartsPiSession(t *testing.T) {
	r := newReloadTestRunner(t, "a1")
	r.applyAssignment(piAssignment("deepseek-chat"))
	oldFp := r.currentPiConfig().LaunchFingerprint()
	r.mu.Lock()
	oldSession := r.piSession
	r.mu.Unlock()

	runtime := newScriptedRuntime(func(rt *scriptedRuntime) {
		<-rt.Canceled()
		close(rt.outputCh)
		close(rt.eventCh)
		rt.resultCh <- executor.Result{ExitCode: -1, ErrorMessage: "context canceled"}
		close(rt.resultCh)
		close(rt.doneCh)
	})
	sink := newMemoryTurnSink()
	cs := &commandStream{
		sink: sink,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}
	r.cs = cs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cs.runSession(ctx, "drain-reload", "A1", "", nil, "", nil)
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	r.applyAssignment(piAssignment("deepseek-reasoner"))

	require.Eventually(t, func() bool { return !cs.InFlight() }, 2*time.Second, 5*time.Millisecond,
		"coordination must wait for the cancelled turn to end")
	result := sink.Entries()[len(sink.Entries())-1].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, "config reloaded mid-turn", result.ErrorMessage,
		"the manager must see the explicit reload cause")
	assert.NotEqual(t, oldFp, r.currentPiConfig().LaunchFingerprint(), "the config must be swapped")
	r.mu.Lock()
	assert.NotSame(t, oldSession, r.piSession, "the subprocess session must be restarted")
	r.mu.Unlock()
}

// TestApplyAssignmentUnchangedKeepsInFlightAcpTurn guards the same reconnect
// contract for ACP agents: a same-shape reconcile only refreshes the config
// (a next-turn concern) and never cancels the running turn.
func TestApplyAssignmentUnchangedKeepsInFlightAcpTurn(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	assignment := &v1pb.AgentAssignment{
		AgentName:        "agents/a2",
		AgentDisplayName: "A2",
		AcpConfig: &v1pb.AgentACPConfig{
			Provider:   "custom",
			Executable: "my-agent",
			Protocol:   executor.ProtocolV1,
		},
	}
	r := &agentRunner{
		machine:   &MachineClient{machineID: "mach-1"},
		daemon:    &daemonsrv.Server{},
		agentName: "agents/a2",
		agentID:   "a2",
	}
	r.applyAssignment(assignment)
	require.NotNil(t, r.currentConfig(), "acp config must resolve in the test harness")

	release := make(chan struct{})
	runtime := newScriptedRuntime(func(rt *scriptedRuntime) {
		select {
		case <-rt.Canceled():
		case <-release:
		}
		close(rt.outputCh)
		close(rt.eventCh)
		if rt.cancelCount.Load() > 0 {
			rt.resultCh <- executor.Result{ExitCode: -1, ErrorMessage: "context canceled"}
		} else {
			rt.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "completed"}
		}
		close(rt.resultCh)
		close(rt.doneCh)
	})
	sink := newMemoryTurnSink()
	cs := &commandStream{
		sink: sink,
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}
	r.cs = cs

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cs.runSession(ctx, "drain-keep-acp", "A2", "", nil, "", nil)
	}()
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	// The reconnect's roster resync re-applies the same assignment.
	r.applyAssignment(assignment)

	assert.Equal(t, int32(0), runtime.cancelCount.Load(),
		"an unchanged ACP reconcile must not cancel the in-flight turn")
	assert.True(t, cs.InFlight(), "the turn must still be running after the reconcile")

	close(release)
	require.Eventually(t, func() bool { return !cs.InFlight() }, 2*time.Second, 5*time.Millisecond)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSession did not finish")
	}
	result := sink.Entries()[len(sink.Entries())-1].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, int32(0), result.ExitCode)
	assert.Empty(t, result.ErrorMessage)
}
