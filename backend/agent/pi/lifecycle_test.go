package pi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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

// readFakePiPrompts returns the prompt messages the fake pi logged (one per
// accepted prompt command), in send order. The fake pi appends each prompt's
// message to fakePiPromptsFile in its CWD (the session working dir).
func readFakePiPrompts(t *testing.T, work string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(work, fakePiPromptsFile))
	if err != nil {
		return nil
	}
	var prompts []string
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		prompts = append(prompts, rec.Message)
	}
	return prompts
}

// TestPiSession_PrimedResetOnExit_ColdRestartSendsInitPrompt (T8 / Phase 6): when
// the pi process dies and a subsequent turn cannot resume from disk (a
// switch_session failure or no resumable session), the next turn must re-send
// the cold init prompt — not just the batch. Before Phase 6, waitPump did NOT
// reset primed, so IsWarm() stayed true from the dead process and
// turnPromptText sent only the batch to a FRESH session that had never seen the
// init prompt → the agent lost its persona ("amnesia"). Phase 6 resets primed
// on exit so IsWarm() returns false and the restart is cold until its own init
// prompt. This is the hard prerequisite for Phase 7 idle eviction, which will
// restart processes frequently and thus hit this path often.
func TestPiSession_PrimedResetOnExit_ColdRestartSendsInitPrompt(t *testing.T) {
	sess, cfg := newFakePiSession(t, "die")

	// Turn 1: cold start (no saved session) → init prompt sent + MarkPrimed, then
	// "die" kills the process mid-turn. waitPump reaps it and (Phase 6) resets
	// primed. The turn fails fast as "session exited mid-turn".
	res1 := runTurn(t, sess, cfg, "cmd-1")
	require.NotZero(t, res1.ExitCode, "die-mode turn must fail")
	require.Contains(t, res1.ErrorMessage, "session exited mid-turn")

	// The process is gone and — the Phase 6 fix — IsWarm() is false (primed was
	// reset on exit, atomically with started). Before the fix IsWarm() stayed
	// true from the dead process.
	require.False(t, sess.Alive(), "process must be reaped after die")
	require.False(t, sess.IsWarm(), "primed must reset on process exit so a restart is cold")

	// Simulate a switch_session that cannot resume (the plan's cold-start
	// branch): drop the persisted session so resumeOrCapture skips the switch
	// and resumedFromDisk stays false on the next start.
	require.NoError(t, os.Remove(piSessionPath(cfg.MachineID, cfg.AgentID)))

	// Turn 2 on a fresh (settling) process. With primed reset, IsWarm()=false →
	// the turn re-sends the cold init prompt instead of just the batch.
	writeFakePiMode(t, cfg.WorkingDir, "settle")
	res2 := runTurn(t, sess, cfg, "cmd-2")
	require.Equal(t, int32(0), res2.ExitCode, "respawned cold turn must succeed")

	prompts := readFakePiPrompts(t, cfg.WorkingDir)
	require.Len(t, prompts, 2, "both turns must have sent a prompt")
	require.Contains(t, prompts[0], "autonomous AI agent in Laelia",
		"turn 1 was cold and must carry the init prompt")
	require.Contains(t, prompts[1], "autonomous AI agent in Laelia",
		"turn 2 must re-send the init prompt after the process died and the session could not resume (no amnesia)")
	require.Contains(t, prompts[1], "do the thing",
		"turn 2 must also carry the batch alongside the init prompt")
}

// waitForEviction polls until the session's subprocess is no longer alive (idle
// eviction reaped it), failing the test on timeout. The idle timer is armed by
// endTurn when the turn exits, so this is called after a turn completes.
func waitForEviction(t *testing.T, sess *Session, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if !sess.Alive() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("subprocess was not idle-evicted within %s", timeout)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPiSession_IdleEvictionPreservesConversation (T10 / Phase 7): after a turn
// ends and IdleTimeout elapses with no further turn, the subprocess is evicted
// (memory freed) but pi-session.json is preserved; the next turn re-spawns a new
// process, resumes via switch_session (warm — no init prompt), and the
// conversation continues. Before Phase 7 the process stayed resident forever.
func TestPiSession_IdleEvictionPreservesConversation(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")
	// Short idle timeout so the test is fast; the turn timeout stays generous so
	// a regression (no eviction) is caught by the wait deadline, not masked.
	sess.idleTimeout = 100 * time.Millisecond

	// Turn 1: cold start, sends the init prompt + batch, settles, succeeds.
	res1 := runTurn(t, sess, cfg, "cmd-1")
	require.Equal(t, int32(0), res1.ExitCode, "turn 1 must succeed")
	pid1 := sessPID(t, sess)
	require.True(t, sess.Alive(), "process must be alive after turn 1")

	// endTurn (run on turn exit) armed the idle timer; wait for eviction.
	waitForEviction(t, sess, 3*time.Second)
	require.False(t, sess.Alive(), "subprocess must be evicted after the idle timeout")

	// The resume state file is preserved (NOT deleted) so the next turn resumes.
	_, err := os.Stat(piSessionPath(cfg.MachineID, cfg.AgentID))
	require.NoError(t, err, "pi-session.json must survive eviction so the conversation resumes")

	// Turn 2: re-spawns a NEW process, resumes via switch_session (warm), and
	// succeeds. The conversation is continuous: no init prompt, only the batch.
	res2 := runTurn(t, sess, cfg, "cmd-2")
	require.Equal(t, int32(0), res2.ExitCode, "turn 2 must respawn and succeed after eviction")
	require.True(t, sess.Alive(), "respawned process must be alive")
	require.NotEqual(t, pid1, sessPID(t, sess), "turn 2 must spawn a new process after eviction")

	prompts := readFakePiPrompts(t, cfg.WorkingDir)
	require.Len(t, prompts, 2, "both turns must have sent a prompt")
	require.Contains(t, prompts[0], "autonomous AI agent in Laelia",
		"turn 1 was cold and must carry the init prompt")
	require.NotContains(t, prompts[1], "autonomous AI agent in Laelia",
		"turn 2 must resume warm (no init prompt) — conversation continuous")
	require.Contains(t, prompts[1], "do the thing", "turn 2 must carry the batch")
}

// TestPiSession_IdleEvictionNoDoubleSpawn (T11 / Phase 7): after eviction reaps
// the subprocess, a follow-up turn re-spawns exactly once, and the next turn
// reuses that live process — no double-spawn / thrash. Start's started-guard
// (under startMu) is the single source of truth, and a consecutive turn's
// beginTurn stops the idle timer so rapid turns never evict.
func TestPiSession_IdleEvictionNoDoubleSpawn(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")
	sess.idleTimeout = 100 * time.Millisecond

	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-1").ExitCode)
	waitForEviction(t, sess, 3*time.Second)

	// Turn 2 respawns once after the eviction.
	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-2").ExitCode)
	require.True(t, sess.Alive(), "turn 2 must respawn the subprocess")
	pid2 := sessPID(t, sess)

	// Turn 3 reuses turn 2's live process (no idle elapsed between them).
	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-3").ExitCode)
	require.True(t, sess.Alive(), "turn 3 must keep the subprocess alive")
	require.Equal(t, pid2, sessPID(t, sess), "turn 3 must reuse turn 2's process — no double-spawn")
}

// TestPiSession_IdleEvictionThenColdRestartSendsInitPrompt (T12 / Phase 6+7):
// eviction restarts the process frequently; when a post-eviction turn cannot
// resume (no resumable session → cold-start branch), Phase 6's primed reset
// ensures the turn re-sends the init prompt — no amnesia. This is the Phase 7
// hot-restart path exercising the Phase 6 invariant.
func TestPiSession_IdleEvictionThenColdRestartSendsInitPrompt(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")
	sess.idleTimeout = 100 * time.Millisecond

	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-1").ExitCode)
	waitForEviction(t, sess, 3*time.Second)
	require.False(t, sess.Alive(), "subprocess must be evicted")

	// Simulate an unresumable session (the plan's cold-start branch): drop
	// pi-session.json so resumeOrCapture skips switch_session and the next turn
	// is cold.
	require.NoError(t, os.Remove(piSessionPath(cfg.MachineID, cfg.AgentID)))

	// Turn 2 re-spawns cold and MUST re-send the init prompt (primed was reset on
	// the eviction-driven exit), not just the batch.
	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-2").ExitCode)

	prompts := readFakePiPrompts(t, cfg.WorkingDir)
	require.Len(t, prompts, 2)
	require.Contains(t, prompts[1], "autonomous AI agent in Laelia",
		"post-eviction cold turn must re-send the init prompt (no amnesia)")
	require.Contains(t, prompts[1], "do the thing", "turn 2 must also carry the batch")
}

// TestPiSession_TruncatedSessionFileFallsBackCold (T9 / Phase 0): a truncated
// pi-session.json (the half-written file a non-atomic write would leave behind
// on a crash mid-write) must NOT crash or hang the turn. loadPiSession returns a
// JSON decode error, resumeOrCapture surfaces it, and Start degrades to a clean
// cold start (init prompt) instead of failing. The atomic write (atomicfile)
// prevents truncation in the first place; this test guards the load-side
// fallback so a corrupt file from an older version or external corruption still
// degrades gracefully.
func TestPiSession_TruncatedSessionFileFallsBackCold(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")

	// Write a truncated pi-session.json — valid prefix, cut mid-field, exactly
	// what a crashed non-atomic write would leave.
	path := piSessionPath(cfg.MachineID, cfg.AgentID)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"session_path":"/tmp/fake-pi-session.jsonl","finger`), 0o600))

	// The turn must succeed (cold start), not return the decode error or hang.
	res := runTurn(t, sess, cfg, "cmd-1")
	require.Equal(t, int32(0), res.ExitCode, "truncated session file must degrade to a successful cold start, not fail the turn")

	// Despite a "saved" session existing, the decode error forced a cold start,
	// so the init prompt was sent (no amnesia from trying to resume a corrupt
	// file).
	prompts := readFakePiPrompts(t, cfg.WorkingDir)
	require.Len(t, prompts, 1)
	require.Contains(t, prompts[0], "autonomous AI agent in Laelia",
		"a truncated session file must fall back to the cold init prompt")
	require.Contains(t, prompts[0], "do the thing", "the batch must still be carried")
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
	// context.Background() has no Done channel, so the terminal send's
	// turn-ctx bound (added so a cancelled turn unblocks it) does not fire
	// here — the send blocks on the full buffer until drained, exactly the
	// backpressure behavior this test asserts.
	events := sess.beginTurn(context.Background())

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

// TestPiSession_TerminalUnblocksOnTurnCancel (F1 regression): a turn cancelled
// while the per-turn event buffer is full must not leave the terminal
// agent_settled send blocked on turnMu — that would wedge the turn's deferred
// endTurn and every later turn's beginTurn until an explicit Stop (a silent wedge
// no turn timeout escapes). The terminal send is bounded by the turn ctx (set in
// beginTurn), so cancelling the turn unblocks it promptly WITHOUT draining the
// buffer. Before the fix the send was bounded only by s.ctx (Stop), so a
// cancelled-but-not-stopped session wedged indefinitely.
func TestPiSession_TerminalUnblocksOnTurnCancel(t *testing.T) {
	sess, _ := newFakePiSession(t, "settle")
	ctx, cancel := context.WithCancel(context.Background())
	events := sess.beginTurn(ctx)

	// Fill the 256-buffer so the terminal send would block on a full channel.
	filler := &event{Type: eventMessageUpdate, AssistantMessageEvent: &assistantMessageEvent{
		Type:  assistantEventTextDelta,
		Delta: "x",
	}}
	for range turnEventBuffer {
		events <- filler
	}

	// The terminal send blocks (buffer full) holding turnMu.
	delivered := make(chan struct{})
	go func() {
		sess.sendEvent(&event{Type: eventAgentSettled})
		close(delivered)
	}()

	// Confirmed blocked while the turn is alive.
	select {
	case <-delivered:
		t.Fatal("terminal send returned before cancel with a full buffer")
	case <-time.After(100 * time.Millisecond):
	}

	// Cancel the TURN (not the session). The send must unblock via turnCtx.Done
	// without the buffer being drained. Before the fix this hung until Stop.
	cancel()
	select {
	case <-delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal send did not unblock on turn cancel — turn-ctx bound missing (wedge)")
	}

	// turnMu must be free for endTurn now (the send released it); a regression
	// that kept it held would deadlock this call until the test deadline.
	done := make(chan struct{})
	go func() {
		sess.endTurn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("endTurn blocked on turnMu — the terminal send never released it (wedge)")
	}
}

// --- same-turn steering ---

// waitForFakePiPrompts polls the fake pi's prompts log until n prompts have
// been accepted (the fake pi is then blocked waiting for a steer in the
// steer/steer-fail modes), or fails the test.
func waitForFakePiPrompts(t *testing.T, work string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(readFakePiPrompts(t, work)) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake pi did not log %d prompt(s)", n)
}

// readFakePiSteers returns the steer messages the fake pi logged (one per
// accepted steer command), in send order.
func readFakePiSteers(t *testing.T, work string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(work, fakePiSteersFile))
	if err != nil {
		return nil
	}
	var steers []string
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		steers = append(steers, rec.Message)
	}
	return steers
}

// TestPiExecutor_SteerDeliveredMidTurn proves a same-turn steer reaches the
// running turn: the fake pi accepts the prompt, blocks until a steer command
// arrives, then settles only after responding to it — so a successful turn
// with exactly one logged steer shows the notice was delivered mid-turn and
// the turn extended until the steered work was processed.
func TestPiExecutor_SteerDeliveredMidTurn(t *testing.T) {
	sess, cfg := newFakePiSession(t, "steer")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-steer",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	// Wait until the prompt is accepted (the fake pi is now blocked waiting for
	// a steer), then steer a notice into the running turn.
	waitForFakePiPrompts(t, cfg.WorkingDir, 1)
	steerer, ok := rt.(interface{ Steer(string) error })
	require.True(t, ok, "PiExecutor must implement Steer")
	require.NoError(t, steerer.Steer("[Laelia inbox notice: new messages arrived]"))

	result := drainTurn(t, rt)
	require.Equal(t, int32(0), result.ExitCode, "steered turn should succeed")

	steers := readFakePiSteers(t, cfg.WorkingDir)
	require.Len(t, steers, 1, "exactly one steer must reach the subprocess")
	require.Equal(t, "[Laelia inbox notice: new messages arrived]", steers[0])
}

// TestPiExecutor_SteerFailureDoesNotBlockTurn proves a rejected steer is
// best-effort: the fake pi answers success:false, and the turn still settles
// cleanly (the post-turn BeginSession wake is the fallback).
func TestPiExecutor_SteerFailureDoesNotBlockTurn(t *testing.T) {
	sess, cfg := newFakePiSession(t, "steer-fail")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-steer-fail",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	waitForFakePiPrompts(t, cfg.WorkingDir, 1)
	steerer, ok := rt.(interface{ Steer(string) error })
	require.True(t, ok)
	require.NoError(t, steerer.Steer("notice"))

	result := drainTurn(t, rt)
	require.Equal(t, int32(0), result.ExitCode, "a rejected steer must not fail the turn")

	steers := readFakePiSteers(t, cfg.WorkingDir)
	require.Len(t, steers, 1, "the steer must still reach the subprocess")
}

// TestPiExecutor_SteerSuppressedDuringCompaction proves steering is suppressed
// while a context compaction is in flight: the fake pi emits compaction_start
// and waits for a steer that must never arrive; the turn completes and the
// steer log stays empty (the post-turn wake recovers the notice).
func TestPiExecutor_SteerSuppressedDuringCompaction(t *testing.T) {
	sess, cfg := newFakePiSession(t, "compact")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-compact",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	// Wait until compaction_start is observed: the executor sets compacting
	// BEFORE emitting the event, so once this event is seen the drain loop is
	// guaranteed to suppress any steer queued afterwards.
	deadline := time.After(10 * time.Second)
	seen := false
	for !seen {
		select {
		case ev, ok := <-rt.EventChannel():
			require.True(t, ok, "event channel closed before compaction_start")
			if ev.Type == v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED {
				seen = true
			}
		case <-deadline:
			t.Fatal("compaction_start event not observed")
		}
	}
	steerer, ok := rt.(interface{ Steer(string) error })
	require.True(t, ok)
	require.NoError(t, steerer.Steer("notice"))

	result := drainTurn(t, rt)
	require.Equal(t, int32(0), result.ExitCode, "compacted turn should succeed")

	steers := readFakePiSteers(t, cfg.WorkingDir)
	require.Empty(t, steers, "steer must be suppressed during compaction")
}

// TestPiExecutor_SteerAfterSettledNoBlock covers the steer/agent_settled race
// the other way: a notice steered AFTER the turn already settled (the drain
// loop exited). Steer is best-effort by contract, so it must return
// immediately without panicking, the notice must not reach the (gone)
// subprocess, and the post-turn BeginSession wake — the durable fallback —
// recovers the messages next turn. A follow-up turn on the same session must
// be unaffected.
func TestPiExecutor_SteerAfterSettledNoBlock(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-settle",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()
	require.Equal(t, int32(0), drainTurn(t, rt).ExitCode, "turn must settle cleanly")

	// drainTurn returned ⇒ agent_settled was processed and the drain loop
	// exited. steerCh is never closed (a close would race the drain loop), so
	// Steer must be safe here: no panic, no block.
	steerer, ok := rt.(interface{ Steer(string) error })
	require.True(t, ok, "PiExecutor must implement Steer")
	done := make(chan error, 1)
	go func() { done <- steerer.Steer("[Laelia inbox notice: new messages arrived]") }()
	select {
	case err := <-done:
		require.NoError(t, err, "a post-settle Steer must not fail (queue has room; best-effort)")
	case <-time.After(2 * time.Second):
		t.Fatal("Steer blocked after the turn settled — must be non-blocking")
	}
	require.Empty(t, readFakePiSteers(t, cfg.WorkingDir),
		"a post-settle steer must not reach the subprocess (the drain loop is gone)")

	// The stale notice must not poison the next turn.
	require.Equal(t, int32(0), runTurn(t, sess, cfg, "cmd-2").ExitCode, "next turn must be unaffected")
}

// TestPiExecutor_SteerQueueFullRejectsNonBlocking proves Steer never blocks
// the receive pump: with the 8-slot queue full (no drain loop running), the
// ninth steer is rejected immediately with "queue full" instead of blocking,
// and draining one slot restores acceptance.
func TestPiExecutor_SteerQueueFullRejectsNonBlocking(t *testing.T) {
	sess, cfg := newFakePiSession(t, "settle")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-queue",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	e, ok := rt.(*PiExecutor)
	require.True(t, ok)

	// No drain loop is running (the turn is not started), so the queue only
	// fills. Steer must accept exactly cap entries...
	for i := 0; i < cap(e.steerCh); i++ {
		require.NoError(t, e.Steer("notice"), "queue must accept up to capacity")
	}

	// ...and reject the (cap+1)-th immediately, without blocking.
	done := make(chan error, 1)
	go func() { done <- e.Steer("overflow") }()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "queue full", "overflow steer must be rejected")
	case <-time.After(2 * time.Second):
		t.Fatal("Steer blocked on a full queue — must be non-blocking")
	}

	// Room is restored once the queue drains (e.g. the drain loop consumes a
	// notice).
	<-e.steerCh
	require.NoError(t, e.Steer("after-drain"), "a drained slot must accept a steer again")
}

// TestPiExecutor_SteerAfterCancelNoBlock covers the cancelled-turn variant of
// the post-turn race: the drain loop exited via Cancel (not settle), and a
// late Steer must still be safe — no panic, no block, nothing delivered to the
// subprocess. The messages are recovered by the next turn's wake.
func TestPiExecutor_SteerAfterCancelNoBlock(t *testing.T) {
	sess, cfg := newFakePiSession(t, "wait")

	rt, err := NewPi(executor.Request{
		CommandID:        "cmd-cancel",
		TurnPrompt:       "do the thing",
		AgentDisplayName: "TestPi",
		TimeoutSeconds:   10,
	}, sess, cfg)
	require.NoError(t, err)
	rt.Start()

	// Wait for turn activity, then cancel mid-turn and drain to completion.
	select {
	case <-rt.EventChannel():
	case <-rt.OutputChannel():
	case <-rt.Done():
		t.Fatal("turn ended before cancel could fire")
	case <-time.After(10 * time.Second):
		t.Fatal("no turn activity before cancel")
	}
	rt.Cancel()
	_ = drainTurn(t, rt)

	steerer, ok := rt.(interface{ Steer(string) error })
	require.True(t, ok)
	done := make(chan error, 1)
	go func() { done <- steerer.Steer("notice") }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Steer blocked after Cancel — must be non-blocking")
	}
	require.Empty(t, readFakePiSteers(t, cfg.WorkingDir),
		"a post-cancel steer must not reach the subprocess")
}
