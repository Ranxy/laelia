package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionFingerprint_StableAndDistinguishing guards the cold/warm gate: the
// fingerprint must be identical for identical (provider,model,workingDir) and
// differ when any of the three changes, or a config change would resume a
// session the provider no longer recognizes.
func TestSessionFingerprint_StableAndDistinguishing(t *testing.T) {
	a := sessionFingerprint("opencode", "gpt-5", "/work")
	assert.Equal(t, a, sessionFingerprint("opencode", "gpt-5", "/work"), "same inputs must hash identically")

	// Each input change must invalidate the fingerprint.
	assert.NotEqual(t, a, sessionFingerprint("claude", "gpt-5", "/work"))
	assert.NotEqual(t, a, sessionFingerprint("opencode", "claude-4", "/work"))
	assert.NotEqual(t, a, sessionFingerprint("opencode", "gpt-5", "/elsewhere"))

	// Empty working dir is a valid distinct input, not a collapse to zero.
	assert.NotEqual(t, sessionFingerprint("opencode", "gpt-5", ""), a)
}

// TestLoadSaveClearACPSession_RoundTrip exercises the durable session file that
// makes a session survive between drain turns. A missing file is nil/nil (cold
// start), save→load round-trips, and clear drops back to nil. HOME is redirected
// to a temp dir so the test never touches the real ~/.laelia.
func TestLoadSaveClearACPSession_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const agentID = "test-agent-session-roundtrip"

	// Missing file => cold start, not an error.
	got, err := loadACPSession(agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "missing session file should yield nil, nil")

	want := &acpSessionState{SessionID: "sess-123", Fingerprint: "fp-abc", CreatedAt: 1700000000}
	require.NoError(t, saveACPSession(agentID, want))

	got, err = loadACPSession(agentID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.SessionID, got.SessionID)
	assert.Equal(t, want.Fingerprint, got.Fingerprint)
	assert.Equal(t, want.CreatedAt, got.CreatedAt)

	// The file is written under the per-agent dir, sibling of command-state.json.
	info, statErr := os.Stat(filepath.Join(os.Getenv("HOME"), ".laelia", agentID, "acp-session.json"))
	require.NoError(t, statErr)
	assert.True(t, info.Mode().Perm() <= 0o600, "session file must be owner-only")

	clearACPSession(agentID)
	got, err = loadACPSession(agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "clear must drop the session back to cold-start")

	// Clearing a missing file is a no-op, not an error.
	clearACPSession(agentID)
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
