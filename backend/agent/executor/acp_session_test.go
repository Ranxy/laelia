package executor

import (
	"os"
	"path/filepath"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionFingerprint_StableAndDistinguishing guards the cold/warm gate: the
// fingerprint must be identical for an identical config and differ when any
// session-defining input changes (provider, model, working dir, persona, env,
// MCP), or a config change would resume a session the provider no longer
// recognizes.
func TestSessionFingerprint_StableAndDistinguishing(t *testing.T) {
	cfg := &ACPConfig{Provider: "opencode", Model: "gpt-5"}
	a := sessionFingerprint(cfg, "/work")
	assert.Equal(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5"}, "/work"), "same inputs must hash identically")

	// Each session-defining input change must invalidate the fingerprint.
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "claude", Model: "gpt-5"}, "/work"))
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "claude-4"}, "/work"))
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5"}, "/elsewhere"))

	// Empty working dir is a valid distinct input, not a collapse to zero.
	assert.NotEqual(t, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5"}, ""), a)

	// Persona, env overlays and allow-env must invalidate.
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", PersonaPrompt: "be terse"}, "/work"))
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", CustomEnv: map[string]string{"FOO": "1"}}, "/work"))
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", AllowEnv: []string{"PATH"}}, "/work"))
	assert.NotEqual(t, a, sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", Env: map[string]string{"TMPDIR": "/x"}}, "/work"))

	// Env maps must hash deterministically regardless of iteration order.
	envA := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", CustomEnv: map[string]string{"A": "1", "B": "2", "C": "3"}}, "/work")
	envB := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", CustomEnv: map[string]string{"C": "3", "A": "1", "B": "2"}}, "/work")
	assert.Equal(t, envA, envB, "map iteration order must not change the fingerprint")

	// An MCP server entry participates in the fingerprint...
	mcp := []acp.McpServer{{Stdio: &acp.McpServerStdio{Name: "laelia-mcp", Command: "laelia-machine", Args: []string{"mcp-proxy"}}}}
	withMCP := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", McpServers: mcp}, "/work")
	assert.NotEqual(t, a, withMCP, "adding an MCP server must invalidate the session")
	otherMCP := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", McpServers: []acp.McpServer{{Stdio: &acp.McpServerStdio{Name: "other", Command: "x", Args: []string{"y"}}}}}, "/work")
	assert.NotEqual(t, withMCP, otherMCP, "changing an MCP server must invalidate the session")

	// ...but the per-turn session token must NOT: it rotates every turn and
	// would otherwise defeat session reuse entirely.
	tokA := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", McpServers: []acp.McpServer{{Stdio: &acp.McpServerStdio{
		Name: "laelia-mcp", Command: "laelia-machine", Args: []string{"mcp-proxy"},
		Env: []acp.EnvVariable{
			{Name: "LAELIA_SESSION_TOKEN", Value: "token-turn-1"},
			{Name: "LAELIA_DAEMON_SOCKET", Value: "/tmp/daemon.sock"},
		},
	}}}}, "/work")
	tokB := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", McpServers: []acp.McpServer{{Stdio: &acp.McpServerStdio{
		Name: "laelia-mcp", Command: "laelia-machine", Args: []string{"mcp-proxy"},
		Env: []acp.EnvVariable{
			{Name: "LAELIA_SESSION_TOKEN", Value: "token-turn-2"},
			{Name: "LAELIA_DAEMON_SOCKET", Value: "/tmp/daemon.sock"},
		},
	}}}}, "/work")
	assert.Equal(t, tokA, tokB, "a rotated session token must not invalidate the session")
	otherEnv := sessionFingerprint(&ACPConfig{Provider: "opencode", Model: "gpt-5", McpServers: []acp.McpServer{{Stdio: &acp.McpServerStdio{
		Name: "laelia-mcp", Command: "laelia-machine", Args: []string{"mcp-proxy"},
		Env: []acp.EnvVariable{{Name: "LAELIA_DAEMON_SOCKET", Value: "/tmp/other.sock"}},
	}}}}, "/work")
	assert.NotEqual(t, tokA, otherEnv, "a changed MCP env must invalidate the session")
}

// TestLoadSaveClearACPSession_RoundTrip exercises the durable session file that
// makes a session survive between drain turns. A missing file is nil/nil (cold
// start), save→load round-trips, and clear drops back to nil. HOME is redirected
// to a temp dir so the test never touches the real ~/.laelia.
func TestLoadSaveClearACPSession_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		machineID = "test-machine-session"
		agentID   = "test-agent-session-roundtrip"
	)

	// Missing file => cold start, not an error.
	got, err := loadACPSession(machineID, agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "missing session file should yield nil, nil")

	want := &acpSessionState{SessionID: "sess-123", Fingerprint: "fp-abc", CreatedAt: 1700000000}
	require.NoError(t, saveACPSession(machineID, agentID, want))

	got, err = loadACPSession(machineID, agentID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.SessionID, got.SessionID)
	assert.Equal(t, want.Fingerprint, got.Fingerprint)
	assert.Equal(t, want.CreatedAt, got.CreatedAt)

	// The file is written under the per-machine/per-agent dir, sibling of
	// command-state.json.
	info, statErr := os.Stat(filepath.Join(os.Getenv("HOME"), ".laelia", machineID, agentID, "acp-session.json"))
	require.NoError(t, statErr)
	assert.True(t, info.Mode().Perm() <= 0o600, "session file must be owner-only")

	clearACPSession(machineID, agentID)
	got, err = loadACPSession(machineID, agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "clear must drop the session back to cold-start")

	// Clearing a missing file is a no-op, not an error.
	clearACPSession(machineID, agentID)
}

// TestRecordResumeFailure_ThresholdAndReset guards the G8 resume-failure
// counter: each failure increments the context-state counter, and the third
// consecutive failure reports warned=true and resets it to 0 (the caller
// surfaces the WARNING).
func TestRecordResumeFailure_ThresholdAndReset(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// recordResumeFailure does not save (the drain loop persists the counter
	// at the end of the turn via Result.ResumeFailures), so persist between
	// calls the way the drain loop would.
	failures, warned := recordResumeFailure("m", "a")
	assert.Equal(t, 1, failures)
	assert.False(t, warned)
	require.NoError(t, SaveContextState("m", "a", &ContextState{Session: SessionHealth{ResumeFailures: failures}}))

	failures, warned = recordResumeFailure("m", "a")
	assert.Equal(t, 2, failures)
	assert.False(t, warned)
	require.NoError(t, SaveContextState("m", "a", &ContextState{Session: SessionHealth{ResumeFailures: failures}}))

	failures, warned = recordResumeFailure("m", "a")
	assert.Equal(t, 0, failures, "counter resets after the warning threshold")
	assert.True(t, warned)

	// The counter is read from the persisted context state (not a global), so
	// a fresh agent starts at 1 again.
	failures, warned = recordResumeFailure("other", "agent")
	assert.Equal(t, 1, failures)
	assert.False(t, warned)
}

// TestTurnPromptText_ColdVsWarm guards the core token-saving invariant:
//   - warm (resumed) turn sends ONLY the batch;
//   - cold turn prepends the init prompt (identity + persona) and appends the batch;
//   - a cold turn with no batch sends the init prompt alone;
//   - a warm turn with no batch sends nothing (the executor's empty-prompt guard
//     then finishes cleanly rather than prompting the LLM with whitespace).
func TestTurnPromptText_ColdVsWarm(t *testing.T) {
	const batch = "New messages received:\n\n[target=dm:@alice msg=1 time=2026-07-04 12:00:00 type=human] @alice: hi"

	// Warm: batch only.
	warm := &ACPExecutor{request: Request{TurnPrompt: batch, AgentDisplayName: "Rei"}}
	assert.Equal(t, batch, warm.turnPromptText(true))

	// Cold: init prompt + batch.
	cold := &ACPExecutor{request: Request{TurnPrompt: batch, AgentDisplayName: "Rei"}, config: &ACPConfig{PersonaPrompt: "be helpful"}}
	got := cold.turnPromptText(false)
	assert.Contains(t, got, "Rei", "cold prompt must carry the identity name")
	assert.Contains(t, got, "be helpful", "cold prompt must carry the persona")
	assert.True(t, len(got) > len(batch), "cold prompt must be longer than the batch alone")
	assert.True(t, endsWith(got, batch), "cold prompt must append the batch after the init prompt")

	// Cold with no batch: init prompt only.
	coldNoBatch := &ACPExecutor{request: Request{AgentDisplayName: "Rei"}, config: &ACPConfig{PersonaPrompt: "be helpful"}}
	got = coldNoBatch.turnPromptText(false)
	assert.Contains(t, got, "Rei")
	assert.Contains(t, got, "be helpful")

	// Cold with no display name falls back to the resource id for identity.
	coldNoName := &ACPExecutor{request: Request{TurnPrompt: batch, AgentResourceID: "agents/rei"}, config: &ACPConfig{}}
	got = coldNoName.turnPromptText(false)
	assert.Contains(t, got, "agents/rei")

	// Warm with no batch: empty (the executor guards against prompting empty text).
	warmNoBatch := &ACPExecutor{request: Request{AgentDisplayName: "Rei"}}
	assert.Empty(t, warmNoBatch.turnPromptText(true))
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
