package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/acp2"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// fakeThreadProvider launches the re-exec'd fake app-server and maps
// notifications with the real codex mapper, so the tests exercise the full
// executor + mapper path without a real codex binary.
type fakeThreadProvider struct{}

func (*fakeThreadProvider) ThreadCommand(_ string) (string, []string) {
	return os.Args[0], []string{threadFakeServerArg}
}

func (*fakeThreadProvider) NewThreadMapper() acp2.EventMapper {
	return provider.NewCodexEventMapper()
}

func (*fakeThreadProvider) ThreadMcpArgs(_ []acp.McpServer) []string { return nil }

func (*fakeThreadProvider) ProbeModelsV2(context.Context, string) ([]provider.ModelOption, error) {
	return nil, nil
}

// compatCheckProvider wraps fakeThreadProvider with a ThreadCompatChecker so
// the spawn path's compatibility gate can be exercised.
type compatCheckProvider struct {
	fakeThreadProvider
	exe string
	err error
}

func (p *compatCheckProvider) CheckThreadCompat(context.Context) (string, error) {
	return p.exe, p.err
}

func TestSpawnThreadAppServerCompatCheck(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")

	// A provider whose compat check fails must fail the spawn with its error.
	blocked := &compatCheckProvider{err: errors.New("codex too old; requires Codex >= 0.95.0")}
	_, _, _, err := spawnThreadAppServer(context.Background(), newThreadTestRequest(cfg.WorkingDir), cfg, blocked)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires Codex >= 0.95.0")

	// A provider whose compat check resolves an executable must spawn that
	// executable instead of ThreadCommand's.
	resolved := &compatCheckProvider{exe: "/nonexistent/codex"}
	_, _, _, err = spawnThreadAppServer(context.Background(), newThreadTestRequest(cfg.WorkingDir), cfg, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/codex")
}

// threadFakeServerArg re-execs the test binary as the fake ACP v2 app-server.
// Detected in init() before flag.Parse so the child never runs the test suite.
const threadFakeServerArg = "--executor-thread-fake-server"

func init() {
	if slices.Contains(os.Args[1:], threadFakeServerArg) {
		os.Exit(runThreadFakeServer())
	}
}

// runThreadFakeServer is the re-exec'd fake ACP v2 app-server. It speaks the
// codex wire shape (so the real CodexEventMapper is exercised) over NDJSON
// stdio. LAELIA_FAKE_THREAD_MODE selects the behavior:
//   - cold: thread/start + a completed turn
//   - resume-ok: thread/resume succeeds
//   - resume-fail: thread/resume errors, forcing the cold-start fallback
//   - error-turn: turn/completed reports status=failed
//   - steer-wait: the turn stays open after turn/started until a turn/steer
//     arrives, then emits a final_answer delta + turn/completed
//   - wedged: never completes the thread/start handshake
//   - session: stays resident across turns (the resident ThreadSession mode);
//     every turn/start gets a completed turn on a stable thread, and the
//     process keeps serving until it is killed
func runThreadFakeServer() int {
	mode := os.Getenv("LAELIA_FAKE_THREAD_MODE")
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	threadSeq := 0
	turnSeq := 0
	for scanner.Scan() {
		var msg acp2.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.IsNotification() {
			continue
		}
		switch msg.Method {
		case "initialize":
			writeResult(writer, msg.ID, map[string]any{
				"userAgent":       "fake-codex/1.0",
				"protocolVersion": "2.0",
				"version":         "1.0",
			})
		case "thread/start":
			if mode == "wedged" {
				continue // never answer: the startup handshake must time out
			}
			threadSeq++
			threadID := fmt.Sprintf("thread-%d", threadSeq)
			if mode == "session" {
				// The resident mode test asserts the same thread survives
				// process restarts (idle eviction + resume), so the id must
				// be stable across spawns, not process-local.
				threadID = "thread-resident"
			}
			writeResult(writer, msg.ID, map[string]any{
				"thread": map[string]any{"id": threadID},
			})
		case "thread/resume":
			if mode == "resume-fail" {
				writeError(writer, msg.ID, -32000, "thread not found")
				continue
			}
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			writeResult(writer, msg.ID, map[string]any{
				"thread": map[string]any{"id": params.ThreadID},
			})
		case "turn/start":
			if mode == "session" {
				turnSeq++
				turnID := fmt.Sprintf("turn-%d", turnSeq)
				writeResult(writer, msg.ID, map[string]any{
					"turn": map[string]any{"id": turnID},
				})
				writeFakeSessionTurnNotifications(writer, turnID)
				continue
			}
			writeResult(writer, msg.ID, map[string]any{
				"turn": map[string]any{"id": "turn-1"},
			})
			if mode != "wedged" {
				if mode == "steer-wait" {
					writeNotification(writer, "turn/started", map[string]any{"turn": map[string]any{"id": "turn-1"}})
				} else {
					writeFakeTurnNotifications(writer, mode)
				}
			}
		case "turn/steer":
			writeResult(writer, msg.ID, map[string]any{
				"turn": map[string]any{"id": "turn-1"},
			})
			if mode == "steer-wait" {
				writeNotification(writer, "item/agentMessage/delta", map[string]any{
					"itemId": "msg-2", "phase": "final_answer", "delta": "steered reply",
				})
				writeNotification(writer, "turn/completed", map[string]any{
					"turn": map[string]any{"id": "turn-1", "status": "completed"},
				})
			}
		case "model/list":
			writeResult(writer, msg.ID, map[string]any{"data": []any{}})
		default:
			writeError(writer, msg.ID, -32601, "method not found")
		}
	}
	return 0
}

func writeResult(writer *bufio.Writer, id json.RawMessage, result any) {
	_ = writeJSON(writer, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func writeError(writer *bufio.Writer, id json.RawMessage, code int, message string) {
	_ = writeJSON(writer, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}

func writeNotification(writer *bufio.Writer, method string, params any) {
	_ = writeJSON(writer, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func writeJSON(writer *bufio.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.WriteByte('\n'); err != nil {
		return err
	}
	return writer.Flush()
}

// writeFakeSessionTurnNotifications emits one completed turn for the resident
// "session" fake mode. The server stays up between turns, so each turn/start
// gets its own sequence number; the thread id is stable ("thread-resident").
func writeFakeSessionTurnNotifications(writer *bufio.Writer, turnID string) {
	writeNotification(writer, "turn/started", map[string]any{"turn": map[string]any{"id": turnID}})
	writeNotification(writer, "item/reasoning/summaryTextDelta", map[string]any{
		"itemId": "reason-" + turnID, "delta": "thinking about turn " + turnID,
	})
	writeNotification(writer, "item/agentMessage/delta", map[string]any{
		"itemId": "msg-" + turnID, "phase": "final_answer", "delta": "reply for " + turnID,
	})
	writeNotification(writer, "thread/tokenUsage/updated", map[string]any{
		"thread": map[string]any{"id": "thread-resident"},
		"tokenUsage": map[string]any{
			"total":              map[string]any{"inputTokens": 100, "cachedInputTokens": 50, "totalTokens": 150, "outputTokens": 50},
			"modelContextWindow": 200000,
		},
	})
	writeNotification(writer, "turn/completed", map[string]any{
		"turn": map[string]any{"id": turnID, "status": "completed"},
	})
}

// writeFakeTurnNotifications emits the codex-shaped notification sequence for
// one completed turn: lifecycle, reasoning + agent message deltas, a shell
// tool call, token usage, and the terminal turn/completed frame.
func writeFakeTurnNotifications(writer *bufio.Writer, mode string) {
	writeNotification(writer, "turn/started", map[string]any{"turn": map[string]any{"id": "turn-1"}})
	writeNotification(writer, "item/reasoning/summaryTextDelta", map[string]any{
		"itemId": "reason-1", "delta": "thinking about the task",
	})
	writeNotification(writer, "item/agentMessage/delta", map[string]any{
		"itemId": "msg-1", "phase": "final_answer", "delta": "Hello from fake codex",
	})
	writeNotification(writer, "item/started", map[string]any{
		"item": map[string]any{"id": "tool-1", "type": "commandExecution", "command": "ls -la"},
	})
	writeNotification(writer, "item/completed", map[string]any{
		"item": map[string]any{"id": "tool-1", "type": "commandExecution", "command": "ls -la", "summary": []string{"done"}},
	})
	writeNotification(writer, "thread/tokenUsage/updated", map[string]any{
		"thread": map[string]any{"id": "thread-1"},
		"tokenUsage": map[string]any{
			"total":              map[string]any{"inputTokens": 800, "cachedInputTokens": 100, "totalTokens": 1000, "outputTokens": 200},
			"modelContextWindow": 200000,
		},
	})
	turn := map[string]any{"id": "turn-1", "status": "completed"}
	if mode == "error-turn" {
		turn["status"] = "failed"
		turn["error"] = map[string]any{"message": "boom"}
	}
	writeNotification(writer, "turn/completed", map[string]any{"turn": turn})
}

// newThreadTestConfig builds a ThreadConfig driving the fake app-server with
// the given mode. HOME is redirected to a temp dir so the persisted thread
// session state stays isolated per test.
func newThreadTestConfig(t *testing.T, mode string) *ThreadConfig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LAELIA_FAKE_THREAD_MODE", mode)
	return &ThreadConfig{
		MaxTimeoutSeconds: 30,
		MaxEventCount:     2000,
		MaxOutputBytes:    256 * 1024,
		OutputFlushBytes:  4096,
		StartupTimeout:    5 * time.Second,
		Provider:          "codex",
		Model:             "gpt-5.2-codex",
		WorkingDir:        t.TempDir(),
		PersonaPrompt:     "You are a test agent.",
		AllowEnv:          []string{"LAELIA_FAKE_THREAD_MODE"},
		SupportsRawEvents: true,
	}
}

func newThreadTestRequest(workspace string) Request {
	return Request{
		CommandID:        "thread-test",
		AgentID:          "test-agent-thread",
		MachineID:        "test-machine-thread",
		AgentDisplayName: "thread-agent",
		WorkingDir:       workspace,
		TurnPrompt:       "New messages received:\n\nhello",
		TimeoutSeconds:   30,
	}
}

func TestThreadExecutor_ColdTurnMapsEvents(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode, "completed turn must exit 0: %s", obs.result.ErrorMessage)
	assert.False(t, obs.result.Resumed, "cold turn must not resume")
	assert.Equal(t, "thread-1", obs.result.SessionID)
	assert.Equal(t, threadSessionFingerprint(cfg), obs.result.Fingerprint)

	// The persisted session must carry the thread id so the next turn resumes.
	state, err := loadACPSession("test-machine-thread", "test-agent-thread")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "thread-1", state.ThreadID)

	// Output: the final_answer delta is user-visible text.
	output := joinOutput(obs.outputs)
	assert.Contains(t, output, "Hello from fake codex")

	// Events: lifecycle, thinking, tool call started/finished, usage, and the
	// final summary.
	types := eventTypes(obs.events)
	assert.Contains(t, types, v1pb.CommandEventType_LIFECYCLE)
	assert.Contains(t, types, v1pb.CommandEventType_TOOL_CALL_STARTED)
	assert.Contains(t, types, v1pb.CommandEventType_TOOL_CALL_FINISHED)
	assert.Contains(t, types, v1pb.CommandEventType_CONTEXT_USAGE_UPDATE)
	assert.Contains(t, types, v1pb.CommandEventType_FINAL_SUMMARY)
	assert.Contains(t, obs.result.FinalSummary, "Hello from fake codex")

	var toolStarted *Event
	for i := range obs.events {
		if obs.events[i].Type == v1pb.CommandEventType_TOOL_CALL_STARTED {
			toolStarted = &obs.events[i]
			break
		}
	}
	require.NotNil(t, toolStarted)
	assert.Equal(t, "ls -la", toolStarted.Summary, "the executed command is the tool title")
	require.NotNil(t, toolStarted.ToolCallStarted)
	assert.Equal(t, "ls -la", toolStarted.ToolCallStarted.Title)
	require.NotNil(t, toolStarted.ToolCallStarted.RawInput)
	assert.Equal(t, "ls -la", toolStarted.ToolCallStarted.RawInput.Fields["command"].GetStringValue())

	var usage *Event
	for i := range obs.events {
		if obs.events[i].Type == v1pb.CommandEventType_CONTEXT_USAGE_UPDATE {
			usage = &obs.events[i]
			break
		}
	}
	require.NotNil(t, usage)
	require.NotNil(t, usage.ContextUsage)
	assert.Equal(t, int64(200000), usage.ContextUsage.Size)
	assert.Equal(t, int64(1000), usage.ContextUsage.Used)
}

func TestThreadExecutor_WarmTurnResumesThread(t *testing.T) {
	cfg := newThreadTestConfig(t, "resume-ok")
	req := newThreadTestRequest(cfg.WorkingDir)
	// Persist a thread id with a matching fingerprint so the executor resumes.
	require.NoError(t, saveACPSession(req.MachineID, req.AgentID, &acpSessionState{
		ThreadID:    "thread-7",
		Fingerprint: threadSessionFingerprint(cfg),
		CreatedAt:   time.Now().Unix(),
	}))

	rt, err := NewThread(req, cfg, &fakeThreadProvider{})
	require.NoError(t, err)
	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode, "resumed turn must exit 0: %s", obs.result.ErrorMessage)
	assert.True(t, obs.result.Resumed, "warm turn must resume the persisted thread")
	assert.Equal(t, "thread-7", obs.result.SessionID, "resume must keep the persisted thread id")
	assert.Contains(t, obs.result.FinalSummary, "Hello from fake codex")
}

func TestThreadExecutor_ResumeFailureFallsBackToColdStart(t *testing.T) {
	cfg := newThreadTestConfig(t, "resume-fail")
	req := newThreadTestRequest(cfg.WorkingDir)
	require.NoError(t, saveACPSession(req.MachineID, req.AgentID, &acpSessionState{
		ThreadID:    "dead-thread",
		Fingerprint: threadSessionFingerprint(cfg),
		CreatedAt:   time.Now().Unix(),
	}))

	rt, err := NewThread(req, cfg, &fakeThreadProvider{})
	require.NoError(t, err)
	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode, "fallback cold start must exit 0: %s", obs.result.ErrorMessage)
	assert.False(t, obs.result.Resumed, "failed resume must fall back to a cold start")
	assert.Equal(t, "thread-1", obs.result.SessionID, "fallback must start a fresh thread")
	assert.Equal(t, 1, obs.result.ResumeFailures)

	// The dead thread id must be cleared so the next turn cold-starts again.
	state, err := loadACPSession(req.MachineID, req.AgentID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "thread-1", state.ThreadID)
}

func TestThreadExecutor_ErrorTurnFailsResult(t *testing.T) {
	cfg := newThreadTestConfig(t, "error-turn")
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	assert.Equal(t, int32(1), obs.result.ExitCode, "failed turn must exit 1")
	assert.Contains(t, obs.result.ErrorMessage, "boom")
	assert.Contains(t, eventTypes(obs.events), v1pb.CommandEventType_WARNING)
}

func TestThreadExecutor_StartupTimeoutFailsFast(t *testing.T) {
	cfg := newThreadTestConfig(t, "wedged")
	cfg.StartupTimeout = 500 * time.Millisecond
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	start := time.Now()
	obs := runACPTestRuntime(t, rt, 8*time.Second, 0)
	elapsed := time.Since(start)

	require.NotZero(t, obs.result.ExitCode, "a wedged handshake must fail the turn")
	require.NotEmpty(t, obs.result.ErrorMessage)
	require.Less(t, elapsed, 5*time.Second, "wedged startup must fail at ~StartupTimeout, not the turn timeout")
}

func TestThreadExecutor_OutputLimitTruncates(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")
	// The thinking delta is exactly 23 bytes, so it fills the budget without
	// tripping the notice; the following agent message delta then trips the
	// limit and surfaces the notice.
	cfg.MaxOutputBytes = 23
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode)
	output := joinOutput(obs.outputs)
	assert.Contains(t, output, "output limit reached", "the limit notice must surface")
	assert.NotContains(t, output, "Hello from fake codex", "text past the byte limit must be dropped")
}

func TestThreadExecutor_EventLimitDropsStructuredEvents(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")
	cfg.MaxEventCount = 2
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode)
	assert.LessOrEqual(t, len(obs.events), 2, "structured events must be capped")
	assert.Contains(t, joinOutput(obs.outputs), "event limit reached")
}

func TestThreadExecutor_NoTurnPromptPersistsThread(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")
	req := newThreadTestRequest(cfg.WorkingDir)
	req.TurnPrompt = ""
	rt, err := NewThread(req, cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode, "an idle turn must finish cleanly")
	assert.Equal(t, "thread-1", obs.result.SessionID, "the thread must still be created and persisted")
	state, err := loadACPSession(req.MachineID, req.AgentID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "thread-1", state.ThreadID)
}

func TestThreadExecutor_ThinkingDeltaSurfacesAsAssistantOutput(t *testing.T) {
	cfg := newThreadTestConfig(t, "cold")
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)

	require.Zero(t, obs.result.ExitCode)
	var assistantChunks []string
	for _, chunk := range obs.outputs {
		if chunk.StreamType == v1pb.CommandOutput_ASSISTANT {
			assistantChunks = append(assistantChunks, chunk.Content)
		}
	}
	assert.NotEmpty(t, assistantChunks, "thinking deltas must surface as assistant output")
	assert.True(t, strings.Contains(strings.Join(assistantChunks, ""), "thinking about the task"))
}

func TestThreadExecutor_SteerInjectsIntoRunningTurn(t *testing.T) {
	cfg := newThreadTestConfig(t, "steer-wait")
	rt, err := NewThread(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	require.NoError(t, err)

	// The fake server keeps the turn open until a turn/steer arrives, so the
	// steer fires from a separate goroutine while the executor pumps the
	// turn — the same shape as the command stream's receive pump.
	steered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(steered)
		exec, ok := rt.(*ThreadExecutor)
		if !ok {
			return
		}
		for {
			select {
			case <-done:
				return
			default:
			}
			if exec.gate.State() == acp2.TurnStarted && exec.gate.CanSteerBusy() {
				exec.Steer("follow up")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	obs := runACPTestRuntime(t, rt, 30*time.Second, 0)
	close(done)
	<-steered

	require.Zero(t, obs.result.ExitCode, "steered turn must exit 0: %s", obs.result.ErrorMessage)
	assert.Contains(t, obs.result.FinalSummary, "steered reply", "the steer delta must land in the final summary")
	assert.Contains(t, joinOutput(obs.outputs), "steered reply", "the steer delta must surface as output")
}

// newResidentSession builds a resident ThreadSession over the fake app-server
// in "session" mode (stays alive across turns). idleTimeout controls the
// session's idle-eviction window; the returned session must be Stop()ped by
// the caller.
func newResidentSession(t *testing.T, idleTimeout time.Duration) *ThreadSession {
	t.Helper()
	cfg := newThreadTestConfig(t, "session")
	cfg.IdleTimeout = idleTimeout
	ctx, cancel := context.WithCancel(context.Background())
	sess := NewThreadSession(ctx, cancel, newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{})
	t.Cleanup(sess.Stop)
	return sess
}

// runResidentTurn drives one full turn over a resident ThreadSession through
// the real executor path (NewThreadWithSession), returning the observation.
func runResidentTurn(t *testing.T, sess *ThreadSession) acpTestObservation {
	t.Helper()
	cfg := sess.cfg
	rt, err := NewThreadWithSession(newThreadTestRequest(cfg.WorkingDir), cfg, &fakeThreadProvider{}, sess)
	require.NoError(t, err)
	return runACPTestRuntime(t, rt, 30*time.Second, 0)
}

// TestThreadSession_ResidentStaysAliveAcrossTurns covers the resident mode
// (LAELIA_ACP2_SESSION=1) happy path: one long-lived subprocess serves turn
// after turn on the same thread, so the second turn is warm and starts in
// seconds instead of a cold app-server boot.
func TestThreadSession_ResidentStaysAliveAcrossTurns(t *testing.T) {
	sess := newResidentSession(t, 0) // 0 disables idle eviction
	require.NoError(t, sess.Start(nil))

	obs1 := runResidentTurn(t, sess)
	require.Zero(t, obs1.result.ExitCode, "first resident turn must exit 0: %s", obs1.result.ErrorMessage)
	assert.False(t, obs1.result.Resumed, "the first resident turn cold-starts")
	assert.Equal(t, "thread-resident", obs1.result.SessionID)
	assert.Contains(t, obs1.result.FinalSummary, "reply for turn-1")
	require.True(t, sess.Alive(), "the subprocess must stay up after the first turn")
	require.True(t, sess.Warm(), "the session must be warm after the first turn")

	obs2 := runResidentTurn(t, sess)
	require.Zero(t, obs2.result.ExitCode, "second resident turn must exit 0: %s", obs2.result.ErrorMessage)
	assert.True(t, obs2.result.Resumed, "a later resident turn is warm (same thread, same process)")
	assert.Equal(t, "thread-resident", obs2.result.SessionID, "both turns share the resident thread")
	assert.Contains(t, obs2.result.FinalSummary, "reply for turn-2")
	require.True(t, sess.Alive(), "the subprocess must still be up after the second turn")
}

// TestThreadSession_IdleEvictionRespawnsAndResumes covers the idle-eviction
// path: after idleTimeout of turn inactivity the resident subprocess is freed,
// and the next turn respawns it and resumes the persisted thread id.
func TestThreadSession_IdleEvictionRespawnsAndResumes(t *testing.T) {
	sess := newResidentSession(t, 300*time.Millisecond)
	require.NoError(t, sess.Start(nil))

	obs1 := runResidentTurn(t, sess)
	require.Zero(t, obs1.result.ExitCode, "first resident turn must exit 0: %s", obs1.result.ErrorMessage)

	// EndTurn (called by the executor) arms the idle timer; wait for the
	// eviction to reap the subprocess instead of racing it.
	require.Eventually(t, func() bool { return !sess.Alive() }, 5*time.Second, 50*time.Millisecond,
		"the idle-eviction timer must free the resident subprocess")

	obs2 := runResidentTurn(t, sess)
	require.Zero(t, obs2.result.ExitCode, "post-eviction turn must exit 0: %s", obs2.result.ErrorMessage)
	assert.True(t, obs2.result.Resumed, "after eviction the next turn must resume the persisted thread")
	assert.Equal(t, "thread-resident", obs2.result.SessionID, "the thread id survives idle eviction")
	// The respawned process restarts its local turn sequence, so the exact
	// reply text is process-local; the resumed marker + thread id are the
	// cross-process invariants.
	assert.NotEmpty(t, obs2.result.FinalSummary)
}
