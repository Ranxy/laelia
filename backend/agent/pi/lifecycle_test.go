package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
)

// These tests drive the real Session/PiExecutor against a fake pi subprocess
// (the test binary re-exec'd; see fakepi_test.go). They prove the Phase 1 root
// fix: the pi subprocess is bound to the session ctx (independent of any turn
// ctx), so a turn ending or a Cancel no longer SIGKILLs the persistent process.

// newFakePiSession builds a Session whose PiBinaryPath is the running test
// binary (re-exec'd as the fake pi). The fake-pi mode file is seeded in the
// session working dir; writeFakePiMode can change it between turns. HOME is
// redirected so pi-session.json never touches the real ~/.laelia.
func newFakePiSession(t *testing.T, mode string) (*Session, *PiConfig) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	work := t.TempDir()
	writeFakePiMode(t, work, mode)

	cfg := &PiConfig{
		APIProvider:       APIProviderDeepseek,
		Model:             "deepseek-chat",
		APIKey:            "sk-test",
		PiBinaryPath:      os.Args[0],
		WorkingDir:        work,
		AgentResourceID:   "agents/test-agent",
		MachineID:         "test-machine",
		AgentID:           "test-agent",
		MaxTimeoutSeconds: 30,
		MaxEventCount:     10000,
		MaxOutputBytes:    1 << 20,
		OutputFlushBytes:  defaultOutputFlushBytes,
		StartupTimeout:    defaultStartupTimeout,
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess := NewSession(ctx, cancel, cfg)
	t.Cleanup(func() { sess.Stop() })
	return sess, cfg
}

func writeFakePiMode(t *testing.T, work, mode string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(work, fakePiModeFile), []byte(mode), 0o600))
}

// runTurn drives one full PiExecutor turn (settle mode): build the per-turn
// runtime, start it, and drain its channels to completion.
func runTurn(t *testing.T, sess *Session, cfg *PiConfig, commandID string) executor.Result {
	t.Helper()
	rt, err := NewPi(executor.Request{
		CommandID:        commandID,
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()
	return drainTurn(t, rt)
}

func drainTurn(t *testing.T, rt executor.Runtime) executor.Result {
	t.Helper()
	var result executor.Result
	deadline := time.After(20 * time.Second)
	for {
		select {
		case r, ok := <-rt.ResultChannel():
			if ok {
				result = r
			}
		case <-rt.OutputChannel():
		case <-rt.EventChannel():
		case <-rt.Done():
			// finish() sends the result before run's defers close done (LIFO),
			// so the value may still be buffered here.
			select {
			case r, ok := <-rt.ResultChannel():
				if ok {
					result = r
				}
			default:
			}
			return result
		case <-deadline:
			t.Fatal("turn did not finish")
		}
	}
}

func sessPID(t *testing.T, sess *Session) int {
	t.Helper()
	sess.startMu.Lock()
	defer sess.startMu.Unlock()
	require.NotNil(t, sess.cmd, "subprocess must be started")
	require.NotNil(t, sess.cmd.Process)
	return sess.cmd.Process.Pid
}

// TestPiSession_SurvivesAcrossTurns (T1): a pi subprocess started by the first
// turn stays alive with the same PID for the second turn. Before the Phase 1
// ctx decouple, the turn-end cancel SIGKILLed the process every turn, so this
// would fail (and the second turn would respawn a new PID).
func TestPiSession_SurvivesAcrossTurns(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")

	res1 := runTurn(t, sess, cfg, "cmd-1")
	require.Equal(t, int32(0), res1.ExitCode, "first turn should succeed")
	require.True(t, sess.Alive(), "session must be alive after turn 1")
	pid := sessPID(t, sess)

	res2 := runTurn(t, sess, cfg, "cmd-2")
	require.Equal(t, int32(0), res2.ExitCode, "second turn should succeed")
	require.True(t, sess.Alive(), "session must be alive after turn 2")
	require.Equal(t, pid, sessPID(t, sess), "subprocess PID must be unchanged across turns")
}

// TestPiSession_CancelKeepsProcessAlive (T5): cancelling an in-flight turn
// tears down only the turn ctx; the session ctx is independent, so the
// subprocess survives and the next turn reuses the same PID. Before Phase 1
// the Cancel path cancelled the turn ctx which WAS the session ctx, killing the
// process (the "stays alive for the next turn" comment was aspirational).
func TestPiSession_CancelKeepsProcessAlive(t *testing.T) {
	sess, cfg := newFakePiSession(t, "wait")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-1",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	// Wait for the first sign of turn activity (the usage event fires at turn
	// start), then cancel mid-turn. By then the lazy Start has completed, so
	// the PID is stable and observable.
	select {
	case <-rt.EventChannel():
	case <-rt.OutputChannel():
	case <-rt.Done():
		t.Fatal("turn ended before cancel could fire")
	case <-time.After(10 * time.Second):
		t.Fatal("no turn activity before cancel")
	}
	require.True(t, sess.Alive(), "session must be alive before cancel")
	pid := sessPID(t, sess)

	rt.Cancel()
	_ = drainTurn(t, rt)

	require.True(t, sess.Alive(), "session must survive Cancel")
	require.Equal(t, pid, sessPID(t, sess), "PID must be unchanged after Cancel")

	// A subsequent turn reuses the same process (settle so it completes).
	writeFakePiMode(t, cfg.WorkingDir, "settle")
	res2 := runTurn(t, sess, cfg, "cmd-2")
	require.Equal(t, int32(0), res2.ExitCode, "second turn should succeed after cancel")
	require.Equal(t, pid, sessPID(t, sess), "PID must be unchanged after the post-cancel turn")
}

// TestPiSession_MidTurnDeathFailsFast (T3): when the subprocess dies mid-turn,
// waitPump closes the active turn channel so the drain loop's !ok branch fires
// immediately and the turn fails in seconds with "session exited mid-turn" —
// not a 30-minute (or turn-timeout) hang. Before Phase 2 the channel was never
// closed, so the drain loop blocked until the turn ctx timed out.
func TestPiSession_MidTurnDeathFailsFast(t *testing.T) {
	sess, cfg := newFakePiSession(t, "die")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-1",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		// Generous turn timeout so a regression (hang) is bounded by the test
		// deadline below, not masked by this value.
		TimeoutSeconds: 30,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	start := time.Now()
	result := drainTurn(t, rt)
	elapsed := time.Since(start)

	require.NotZero(t, result.ExitCode, "turn must fail when the process dies mid-turn")
	require.Contains(t, result.ErrorMessage, "session exited mid-turn",
		"failure must come from the closed turn channel, not the turn timeout")
	require.Less(t, elapsed, 10*time.Second, "mid-turn death must fail fast, not hang to the turn timeout")
}

// TestPiSession_WedgedStartupFailsFast (T2): when the pi subprocess spawns but
// never answers the startup RPC (get_state) within StartupTimeout, the turn must
// fail at ~StartupTimeout — not hang to the turn timeout (MaxTimeoutSeconds).
// Before Phase 5 the startup RPC used the 30-min turn ctx, so a wedged startup
// (bad config, stuck download) hung for the whole turn. Phase 5 bounds it with a
// dedicated StartupTimeout, kills the wedged process (so the next turn respawns),
// and surfaces the error immediately.
func TestPiSession_WedgedStartupFailsFast(t *testing.T) {
	sess, cfg := newFakePiSession(t, "stuck")
	// Shrink the startup timeout so the test is fast; keep the turn timeout
	// generous so a regression (no startup timeout) hangs to the test deadline,
	// not masked by the turn timeout.
	cfg.StartupTimeout = 300 * time.Millisecond

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-1",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   30,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	start := time.Now()
	result := drainTurn(t, rt)
	elapsed := time.Since(start)

	require.NotZero(t, result.ExitCode, "a wedged startup must fail the turn")
	require.Contains(t, result.ErrorMessage, "startup",
		"failure must come from the startup timeout, not the turn timeout")
	// ~StartupTimeout (300ms) plus drain/kill slack; well under the 30s turn
	// timeout a pre-Phase-5 regression would hit.
	require.Less(t, elapsed, 5*time.Second, "wedged startup must fail at ~StartupTimeout, not the turn timeout")

	// The wedged process must have been killed so the next turn respawns (a live
	// wedged process would make the next turn reuse it and hang again).
	require.False(t, sess.Alive(), "wedged process must be killed after a startup timeout")

	// A subsequent turn on a fresh (settle) fake-pi respawns and succeeds,
	// proving the session ctx survived the kill and can re-spawn.
	writeFakePiMode(t, cfg.WorkingDir, "settle")
	res2 := runTurn(t, sess, cfg, "cmd-2")
	require.Equal(t, int32(0), res2.ExitCode, "next turn must respawn after the wedged-startup kill")
	require.True(t, sess.Alive(), "respawned session must be alive")
}

// TestPiSession_TerminalDeliveredUnderBackpressure (T4): when the per-turn event
// buffer is full (backpressure), the terminal agent_settled must NOT be dropped.
// Before Phase 3 sendEvent used a non-blocking send with a default drop for every
// event, so a settled arriving while the buffer was full was dropped and the
// drain loop hung to the turn timeout. Phase 3 routes the terminal through a
// blocking send: it blocks until the drain loop drains the buffer (or the session
// is torn down), so the terminal is always delivered. This is a white-box test of
// sendEvent against a manually filled channel — no subprocess needed.
func TestPiSession_TerminalDeliveredUnderBackpressure(t *testing.T) {
	sess, _ := newFakePiSession(t, "settle")
	events := sess.beginTurn()

	// Fill the 256-buffer so the next send would drop under the pre-Phase-3 code.
	filler := &event{Type: eventMessageUpdate, AssistantMessageEvent: &assistantMessageEvent{
		Type:  assistantEventTextDelta,
		Delta: "x",
	}}
	for range turnEventBuffer {
		events <- filler
	}

	// sendEvent the terminal in a goroutine. Under Phase 3 it blocks (buffer
	// full) holding turnMu; under the pre-Phase-3 default-drop it would return
	// immediately and the terminal would be silently lost.
	delivered := make(chan struct{})
	go func() {
		sess.sendEvent(&event{Type: eventAgentSettled})
		close(delivered)
	}()

	// It must be blocked, not yet delivered (the buffer is still full).
	select {
	case <-delivered:
		t.Fatal("terminal send returned before the buffer had room; backpressure not simulated")
	case <-time.After(100 * time.Millisecond):
	}

	// Drain one event: the blocking terminal send now has room and delivers.
	<-events

	select {
	case <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal send did not deliver once the buffer drained")
	}

	// The channel is FIFO, so the terminal was appended after the fillers. Drain
	// the rest and assert the terminal is the last event delivered — i.e. it was
	// not dropped under backpressure.
	var last *event
	empty := false
	for !empty {
		select {
		case ev := <-events:
			last = ev
		default:
			empty = true
		}
	}
	require.Equal(t, eventAgentSettled, last.Type,
		"the terminal event must be delivered, not dropped under backpressure")

	// endTurn explicitly (not deferred) so a t.Fatal above cannot wedge on
	// turnMu held by the still-blocked sendEvent; sess.Stop in t.Cleanup cancels
	// s.ctx and unblocks the goroutine on any failure path.
	sess.endTurn()
}
