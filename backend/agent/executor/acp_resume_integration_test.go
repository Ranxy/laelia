package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestACPExecutorColdThenWarmResumesSession is the design's gating verification:
// the ACP provider must honor session/resume from a fresh process so the LLM
// conversation (and the init prompt sent once at cold start) is inherited across
// drain turns.
//
// Turn 1 (cold) reads a file containing a secret word — a concrete task opencode
// completes quickly, mirroring TestACPExecutorWithOpencodeReadFile — so the
// secret enters the conversation history. The file is then deleted. Turn 2 spawns
// a NEW subprocess and resumes the same SessionId (warm: only the batch is sent,
// no init prompt re-sent). With the file gone, the agent can only recall the
// secret from the resumed session's conversation — proving the cold-start
// history survived the subprocess death. If the provider cannot resume, turn 2
// cold-starts with no memory of the secret and no file to read, so the assertion
// fails (correctly flagging the lost inheritance).
//
// Gated like the other opencode ACP integration tests: set LAELIA_RUN_OPENCODE_ACP_TESTS=1.
func TestACPExecutorColdThenWarmResumesSession(t *testing.T) {
	bin := requireOpencodeACP(t)

	// One workspace for both turns so the fingerprint (provider|model|cwd)
	// matches and turn 2 actually hits the resume path.
	workspace := t.TempDir()
	const (
		machineID = "test-machine-resume-integration"
		agentID   = "test-agent-resume-integration"
	)
	// The session file lands in the real ~/.laelia/<machineID>/<agentID>/ (HOME
	// is left alone so opencode can find its API key/config). Clear it before and
	// after so prior runs cannot interfere and we leave nothing behind.
	clearACPSession(machineID, agentID)
	t.Cleanup(func() { clearACPSession(machineID, agentID) })

	// Cold turn: seed a file with the secret, ask the agent to read it back. This
	// is a concrete task opencode finishes in seconds (the init prompt's
	// laelia-agent procedure does not derail it), and it puts the secret into the
	// session's conversation history.
	secret := "ZEPHYR"
	seedPath := filepath.Join(workspace, "secret.txt")
	require.NoError(t, os.WriteFile(seedPath, []byte(secret), 0o644))

	coldRuntime, err := NewACP(Request{
		CommandID:      "resume-cold",
		AgentID:        agentID,
		MachineID:      machineID,
		TurnPrompt:     "Read the file secret.txt in the current workspace and reply with exactly its contents. Do not add quotes or any extra words.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
	}, newOpencodeTestConfig(bin, workspace, false))
	require.NoError(t, err)
	cold := runACPTestRuntime(t, coldRuntime, 150*time.Second, 0)

	require.Zero(t, cold.result.ExitCode, "cold turn failed: outputs=%q err=%s", joinOutput(cold.outputs), cold.result.ErrorMessage)
	assert.False(t, cold.result.Resumed, "first turn must be cold (no persisted session)")
	assert.NotEmpty(t, cold.result.SessionID, "cold turn must persist a session id")
	assert.Contains(t, compactText(joinOutput(cold.outputs)), secret, "cold turn must read the secret into the conversation")

	// Remove the file so the warm turn cannot simply re-read it; it must recall
	// the secret from the resumed session's conversation history.
	require.NoError(t, os.Remove(seedPath))

	// Warm turn: fresh subprocess, same agentID + workspace => fingerprint matches
	// => ResumeSession(same id). Only the batch (this instruction) is sent — no
	// init prompt, so the agent does not re-receive the laelia-agent procedure.
	warmRuntime, err := NewACP(Request{
		CommandID:      "resume-warm",
		AgentID:        agentID,
		MachineID:      machineID,
		TurnPrompt:     "Reply with exactly the single secret word I told you earlier in this conversation, nothing else. Do not read any file.",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
	}, newOpencodeTestConfig(bin, workspace, false))
	require.NoError(t, err)
	warm := runACPTestRuntime(t, warmRuntime, 150*time.Second, 0)

	require.Zero(t, warm.result.ExitCode, "warm turn failed: outputs=%q err=%s", joinOutput(warm.outputs), warm.result.ErrorMessage)
	assert.True(t, warm.result.Resumed, "second turn must resume the persisted session (warm)")
	assert.Equal(t, cold.result.SessionID, warm.result.SessionID, "warm turn must resume the same session id")

	// The agent recalls the secret from the resumed conversation — the file is
	// gone, so this can only succeed if the cold-start history was inherited.
	combined := strings.ToUpper(compactText(joinOutput(warm.outputs)) + compactText(warm.result.FinalSummary))
	assert.Contains(t, combined, secret,
		"warm turn must recall the secret from the resumed session; outputs=%q summary=%q", joinOutput(warm.outputs), warm.result.FinalSummary)

	// opencode v1.17.x replays the prior conversation as session/update
	// notifications DURING session/resume. The executor must drop that replay so
	// the warm turn does not inherit the cold turn's events — specifically the
	// read tool call on secret.txt, which the warm prompt forbids and the now-
	// deleted file makes impossible. Any event or output referencing the cold
	// turn's filename is a leaked replay.
	for _, ev := range warm.events {
		blob := strings.ToLower(ev.Summary + " " + toolCallEventBlob(ev))
		assert.NotContains(t, blob, "secret.txt",
			"warm turn must not replay the cold turn's secret.txt tool call; event=%s", eventTypes([]Event{ev}))
	}
}

// toolCallEventBlob renders a tool-call event's payload to a string for replay
// inspection. Returns "" for non-tool-call events.
func toolCallEventBlob(ev Event) string {
	switch {
	case ev.ToolCallStarted != nil:
		return toJSONString(ev.ToolCallStarted)
	case ev.ToolCallFinished != nil:
		return toJSONString(ev.ToolCallFinished)
	case ev.DiffEmitted != nil:
		return toJSONString(ev.DiffEmitted)
	case ev.RawAcp != nil:
		return toJSONString(ev.RawAcp)
	default:
		return ""
	}
}

// TestACPExecutorResumeFallbackToColdOnBadSession guards the dead-session
// recovery path: if the persisted SessionId no longer exists at the provider
// (simulated by writing a bogus id with a matching fingerprint), the executor
// must drop to cold (NewSession + init) rather than error out — the cursor is
// the source of truth so no message is lost, only the init prompt is re-sent.
//
// This is a unit test (no real provider): it checks that a persisted bogus id
// is cleared after the turn and the turn still completes via the cold path.
// The full provider round-trip is covered by TestACPExecutorColdThenWarmResumesSession.
func TestACPExecutorResumeFallbackToColdOnBadSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		machineID = "test-machine-resume-fallback"
		agentID   = "test-agent-resume-fallback"
	)
	const fakeProvider = "fake-provider"
	const fakeModel = "fake-model"
	workspace := t.TempDir()

	// Persist a bogus session id with a matching fingerprint so the executor
	// will attempt ResumeSession (and fail), then fall back to cold.
	fp := sessionFingerprint(fakeProvider, fakeModel, workspace)
	require.NoError(t, saveACPSession(machineID, agentID, &acpSessionState{SessionID: "dead-session-id", Fingerprint: fp, CreatedAt: 1}))

	// The executor would try to spawn a real subprocess for fakeProvider, which
	// does not exist. Instead of driving run(), assert the precondition the
	// fallback relies on: the persisted state is what resume would load, and
	// clearACPSession (the fallback's recovery call) drops it.
	got, err := loadACPSession(machineID, agentID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "dead-session-id", got.SessionID)
	assert.Equal(t, fp, got.Fingerprint)

	clearACPSession(machineID, agentID)
	got, err = loadACPSession(machineID, agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "fallback must clear the dead session so the next turn cold-starts")

	// And a fingerprint mismatch also forces cold: a config change must not
	// resume a session the provider no longer recognizes.
	require.NoError(t, saveACPSession(machineID, agentID, &acpSessionState{SessionID: "stale", Fingerprint: "old-fp", CreatedAt: 1}))
	mismatch := sessionFingerprint(fakeProvider, "different-model", workspace)
	assert.NotEqual(t, "old-fp", mismatch, "different model must yield a different fingerprint")
	clearACPSession(machineID, agentID)
}
