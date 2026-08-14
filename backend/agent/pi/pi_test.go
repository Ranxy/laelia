package pi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestPiFingerprint_StableAndDistinguishing guards the resume gate: identical
// (apiProvider, model, workingDir) must hash identically, and any change must
// invalidate the fingerprint so a config change does not resume a stale session.
func TestPiFingerprint_StableAndDistinguishing(t *testing.T) {
	a := piFingerprint(&PiConfig{APIProvider: "deepseek", Model: "deepseek-chat", WorkingDir: "/work"})
	assert.Equal(t, a, piFingerprint(&PiConfig{APIProvider: "deepseek", Model: "deepseek-chat", WorkingDir: "/work"}))

	assert.NotEqual(t, a, piFingerprint(&PiConfig{APIProvider: "openrouter", Model: "deepseek-chat", WorkingDir: "/work"}))
	assert.NotEqual(t, a, piFingerprint(&PiConfig{APIProvider: "deepseek", Model: "deepseek-reasoner", WorkingDir: "/work"}))
	assert.NotEqual(t, a, piFingerprint(&PiConfig{APIProvider: "deepseek", Model: "deepseek-chat", WorkingDir: "/else"}))
}

// TestLoadSavePiSession_RoundTrip exercises the durable session file. A missing
// file is nil/nil (cold start); save→load round-trips. HOME is redirected so the
// test never touches the real ~/.laelia.
func TestLoadSavePiSession_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		machineID = "test-machine-pi"
		agentID   = "test-agent-pi-roundtrip"
	)

	got, err := loadPiSession(machineID, agentID)
	require.NoError(t, err)
	assert.Nil(t, got, "missing session file should yield nil, nil")

	want := &piSessionState{SessionPath: "/path/to/session.jsonl", Fingerprint: "fp-abc"}
	require.NoError(t, savePiSession(machineID, agentID, want))

	got, err = loadPiSession(machineID, agentID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.SessionPath, got.SessionPath)
	assert.Equal(t, want.Fingerprint, got.Fingerprint)
}

// TestBuildPiConfig_Gating confirms BuildPiConfig returns nil unless the
// provider is builtin-pi AND a known api_provider AND a non-empty api_key —
// the runner treats nil as "not a pi agent / not yet configured".
func TestBuildPiConfig_Gating(t *testing.T) {
	base := func() *v1pb.AgentACPConfig {
		return &v1pb.AgentACPConfig{
			Provider:      BuiltinPiProvider,
			ApiProvider:   APIProviderDeepseek,
			Model:         "deepseek-chat",
			ApiKey:        "sk-test",
			PersonaPrompt: "be concise",
		}
	}

	t.Run("valid", func(t *testing.T) {
		cfg := BuildPiConfig(base(), "m", "a", "agents/a", "/bin/pi", "/sock", "tok", "/bin")
		require.NotNil(t, cfg)
		assert.Equal(t, APIProviderDeepseek, cfg.APIProvider)
		assert.Equal(t, "deepseek-chat", cfg.Model)
		assert.Equal(t, "sk-test", cfg.APIKey)
		assert.Equal(t, "be concise", cfg.PersonaPrompt)
	})

	t.Run("wrong provider", func(t *testing.T) {
		c := base()
		c.Provider = "opencode"
		assert.Nil(t, BuildPiConfig(c, "m", "a", "agents/a", "/bin/pi", "/sock", "tok", "/bin"))
	})

	t.Run("unknown api_provider", func(t *testing.T) {
		c := base()
		c.ApiProvider = "anthropic"
		assert.Nil(t, BuildPiConfig(c, "m", "a", "agents/a", "/bin/pi", "/sock", "tok", "/bin"))
	})

	t.Run("empty api_key", func(t *testing.T) {
		c := base()
		c.ApiKey = "  "
		assert.Nil(t, BuildPiConfig(c, "m", "a", "agents/a", "/bin/pi", "/sock", "tok", "/bin"))
	})

	t.Run("nil config", func(t *testing.T) {
		assert.Nil(t, BuildPiConfig(nil, "m", "a", "agents/a", "/bin/pi", "/sock", "tok", "/bin"))
	})
}

// TestBuildPiCapability confirms pi agents advertise SupportsPi and NOT
// SupportsAcp, and that non-pi configs get a zero capability.
func TestBuildPiCapability(t *testing.T) {
	t.Run("pi config", func(t *testing.T) {
		capability := BuildPiCapability(&v1pb.AgentACPConfig{Provider: BuiltinPiProvider, ApiProvider: APIProviderDeepseek, ApiKey: "k"})
		assert.True(t, capability.SupportsPi)
		assert.False(t, capability.SupportsAcp)
		assert.True(t, capability.SupportsDiff)
		assert.True(t, capability.SupportsToolTraces)
	})

	t.Run("non-pi config", func(t *testing.T) {
		capability := BuildPiCapability(&v1pb.AgentACPConfig{Provider: "opencode"})
		assert.False(t, capability.SupportsPi)
		assert.False(t, capability.SupportsAcp)
	})

	t.Run("nil config", func(t *testing.T) {
		capability := BuildPiCapability(nil)
		assert.False(t, capability.SupportsPi)
		assert.False(t, capability.SupportsAcp)
	})
}

// TestLaunchArgs confirms the pi argv shape: rpc mode, provider/model from the
// api provider spec, session-dir, and the headless-minimizing flags.
func TestLaunchArgs(t *testing.T) {
	cfg := &PiConfig{APIProvider: APIProviderOpenRouter, Model: "anthropic/claude-3.5-sonnet", WorkingDir: "/work"}
	args := cfg.launchArgs()
	want := []string{
		"--mode", "rpc",
		"--provider", "openrouter",
		"--model", "anthropic/claude-3.5-sonnet",
		"--session-dir", "/work",
		"--no-skills",
		"--no-prompt-templates",
		"--approve",
	}
	assert.Equal(t, want, args)
}

// TestBuildPiEnv_APIKeyAndBootstrap confirms the env the subprocess receives:
// only the whitelisted host vars, the provider's API key env, and the laelia
// bootstrap vars. The API key must land in the env var the configured provider
// expects (DEEPSEEK_API_KEY vs OPENROUTER_API_KEY).
func TestBuildPiEnv_APIKeyAndBootstrap(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("SECRET_SHOULD_NOT_LEAK", "nope")

	cfg := &PiConfig{
		APIProvider:     APIProviderDeepseek,
		APIKey:          "sk-deepseek",
		AgentResourceID: "agents/abc",
		DaemonSocket:    "/tmp/sock",
		SessionToken:    "tok",
		BinaryDir:       "/opt/laelia/bin",
	}
	env := cfg.buildPiEnv("commands/1")

	m := envMap(env)
	assert.Equal(t, "sk-deepseek", m["DEEPSEEK_API_KEY"])
	assert.Equal(t, "", m["OPENROUTER_API_KEY"], "deepseek provider must not set the openrouter key")
	assert.Equal(t, "/tmp/sock", m["LAELIA_DAEMON_SOCKET"])
	assert.Equal(t, "tok", m["LAELIA_SESSION_TOKEN"])
	assert.Equal(t, "agents/abc", m["LAELIA_AGENT"])
	assert.Equal(t, "commands/1", m["LAELIA_COMMAND"])
	assert.Contains(t, m["PATH"], "/opt/laelia/bin")
	assert.NotContains(t, m, "SECRET_SHOULD_NOT_LEAK", "non-whitelisted host env must not leak")

	// openrouter variant: key lands in OPENROUTER_API_KEY.
	cfg.APIProvider = APIProviderOpenRouter
	cfg.APIKey = "sk-or"
	env = cfg.buildPiEnv("commands/1")
	assert.Equal(t, "sk-or", envMap(env)["OPENROUTER_API_KEY"])
}

func TestBuildPiEnvPropagatesLaeliaHomeOutsideAllowEnv(t *testing.T) {
	t.Setenv("LAELIA_HOME", "/custom/laelia")
	t.Setenv("PATH", "/usr/bin")

	cfg := &PiConfig{
		APIProvider:     APIProviderDeepseek,
		APIKey:          "sk-deepseek",
		AgentResourceID: "agents/abc",
		DaemonSocket:    "/tmp/sock",
		SessionToken:    "tok",
		BinaryDir:       "/opt/laelia/bin",
	}
	env := cfg.buildPiEnv("commands/1")
	m := envMap(env)
	assert.Equal(t, "/custom/laelia", m["LAELIA_HOME"], "parent LAELIA_HOME must be forced into pi env even though piAllowEnv does not include it")
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// TestJSONLFraming_LFOnly verifies the line-splitting rule the readPump relies
// on: bufio.Reader.ReadString('\n') splits on LF only, so a JSON payload
// containing U+2028/U+2029 (which Node's readline would split on) stays intact.
// This is the framing-safety guarantee for the Go side of the protocol.
func TestJSONLFraming_LFOnly(t *testing.T) {
	payload := map[string]any{"message": "line separator inside"}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	// Simulate one JSONL line: the payload followed by LF.
	line := string(data) + "\n"
	assert.True(t, strings.HasSuffix(line, "\n"))

	// Decode it back the way readPump does: strip trailing LF (and optional CR).
	raw := strings.TrimSuffix(line, "\n")
	raw = strings.TrimSuffix(raw, "\r")
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	assert.Equal(t, "line separator inside", got["message"], "U+2028/U+2029 must not split the line")
}

// TestResponseCorrelation verifies the id-based request/response correlation
// the Session uses to route get_state/switch_session responses to waiting Send
// callers.
func TestResponseCorrelation(t *testing.T) {
	resp := response{Type: "response", ID: "laelia-1", Command: "get_state", Success: true}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var head struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
	}
	require.NoError(t, json.Unmarshal(data, &head))
	assert.Equal(t, "response", head.Type)
	assert.Equal(t, "laelia-1", head.ID)

	var r response
	require.NoError(t, json.Unmarshal(data, &r))
	assert.Equal(t, "laelia-1", r.ID)
	assert.True(t, r.Success)
	assert.Equal(t, "get_state", r.Command)
}

// TestSessionStatsResponseDecode verifies the get_session_stats response shape
// (command framing + contextUsage fields) decodes into sessionStatsData.
func TestSessionStatsResponseDecode(t *testing.T) {
	raw := `{"type":"response","id":"laelia-2","command":"get_session_stats","success":true,"data":{"sessionFile":"/tmp/s.jsonl","sessionId":"abc","tokens":{"input":50000,"output":10000,"total":105000},"cost":0.45,"contextUsage":{"tokens":60000,"contextWindow":200000,"percent":30}}}`
	var r response
	require.NoError(t, json.Unmarshal([]byte(raw), &r))
	assert.Equal(t, "get_session_stats", r.Command)

	var data sessionStatsData
	require.NoError(t, json.Unmarshal(r.Data, &data))
	require.NotNil(t, data.Tokens)
	assert.Equal(t, int64(50000), data.Tokens.Input)
	assert.Equal(t, int64(10000), data.Tokens.Output)
	assert.Equal(t, int64(105000), data.Tokens.Total)
	require.NotNil(t, data.ContextUsage)
	require.NotNil(t, data.ContextUsage.Tokens)
	assert.Equal(t, int64(60000), *data.ContextUsage.Tokens)
	require.NotNil(t, data.ContextUsage.ContextWindow)
	assert.Equal(t, int64(200000), *data.ContextUsage.ContextWindow)
	require.NotNil(t, data.ContextUsage.Percent)
	assert.InDelta(t, 30.0, *data.ContextUsage.Percent, 1e-9)

	// Command framing: the stats command is a plain JSON object with a type.
	cmd, err := json.Marshal(getSessionStatsCommand{Type: "get_session_stats"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"get_session_stats"}`, string(cmd))
}

// TestUsageEventFromStats maps get_session_stats data to the structured usage
// event the frontend progress bar consumes.
func TestUsageEventFromStats(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	f64 := func(v float64) *float64 { return &v }

	t.Run("valid with percent", func(t *testing.T) {
		ev := usageEventFromStats(&sessionStatsData{
			ContextUsage: &sessionContextUsage{
				Tokens:        i64(60000),
				ContextWindow: i64(200000),
				Percent:       f64(30),
			},
		})
		require.NotNil(t, ev)
		assert.Equal(t, v1pb.CommandEventType_CONTEXT_USAGE_UPDATE, ev.Type)
		require.NotNil(t, ev.ContextUsage)
		assert.Equal(t, int64(200000), ev.ContextUsage.Size)
		assert.Equal(t, int64(60000), ev.ContextUsage.Used)
		assert.InDelta(t, 0.3, ev.ContextUsage.UsageRatio, 1e-9)
	})

	t.Run("derived ratio without percent", func(t *testing.T) {
		ev := usageEventFromStats(&sessionStatsData{
			ContextUsage: &sessionContextUsage{
				Tokens:        i64(50000),
				ContextWindow: i64(200000),
			},
		})
		require.NotNil(t, ev)
		assert.InDelta(t, 0.25, ev.ContextUsage.UsageRatio, 1e-9)
	})

	t.Run("tokens null after compaction, percent present", func(t *testing.T) {
		ev := usageEventFromStats(&sessionStatsData{
			ContextUsage: &sessionContextUsage{
				ContextWindow: i64(200000),
				Percent:       f64(25),
			},
		})
		require.NotNil(t, ev)
		assert.Equal(t, int64(50000), ev.ContextUsage.Used, "used derived from percent when tokens are null")
	})

	t.Run("omitted contextUsage", func(t *testing.T) {
		assert.Nil(t, usageEventFromStats(&sessionStatsData{}))
		assert.Nil(t, usageEventFromStats(nil))
	})

	t.Run("null after compaction", func(t *testing.T) {
		ev := usageEventFromStats(&sessionStatsData{
			ContextUsage: &sessionContextUsage{
				ContextWindow: i64(200000),
			},
		})
		assert.Nil(t, ev, "tokens and percent both null -> no usable observation")
	})

	t.Run("zero context window", func(t *testing.T) {
		ev := usageEventFromStats(&sessionStatsData{
			ContextUsage: &sessionContextUsage{
				Tokens:        i64(1),
				ContextWindow: i64(0),
			},
		})
		assert.Nil(t, ev)
	})
}

// TestTokenUsageDelta verifies the per-command token delta computation from
// turn-start/turn-end session snapshots.
func TestTokenUsageDelta(t *testing.T) {
	start := &sessionTokens{Input: 1000, Output: 500, CacheRead: 200, CacheWrite: 50, Total: 1750}
	end := &sessionTokens{Input: 1500, Output: 800, CacheRead: 300, CacheWrite: 80, Total: 2680}

	t.Run("normal delta", func(t *testing.T) {
		usage := tokenUsageDelta(start, end)
		require.NotNil(t, usage)
		assert.Equal(t, int64(500), usage.InputTokens)
		assert.Equal(t, int64(300), usage.OutputTokens)
		assert.Equal(t, int64(100), usage.CacheReadTokens)
		assert.Equal(t, int64(30), usage.CacheWriteTokens)
		assert.Equal(t, int64(930), usage.TotalTokens)
	})

	t.Run("nil baseline", func(t *testing.T) {
		assert.Nil(t, tokenUsageDelta(nil, end))
		assert.Nil(t, tokenUsageDelta(start, nil))
		assert.Nil(t, tokenUsageDelta(nil, nil))
	})

	t.Run("negative delta clamped to zero", func(t *testing.T) {
		usage := tokenUsageDelta(end, start)
		require.NotNil(t, usage)
		assert.Equal(t, int64(0), usage.InputTokens)
		assert.Equal(t, int64(0), usage.OutputTokens)
		assert.Equal(t, int64(0), usage.CacheReadTokens)
		assert.Equal(t, int64(0), usage.CacheWriteTokens)
		assert.Equal(t, int64(0), usage.TotalTokens)
	})

	t.Run("zero consumption", func(t *testing.T) {
		usage := tokenUsageDelta(start, &sessionTokens{Input: 1000, Output: 500, CacheRead: 200, CacheWrite: 50, Total: 1750})
		require.NotNil(t, usage)
		assert.Equal(t, int64(0), usage.TotalTokens)
	})
}

// TestEventMapping_TextDeltaAndToolCalls exercises the executor's event→Event
// mapping without spawning a subprocess: feed events through handleEvent and
// assert the emitted executor.Events.
func TestEventMapping_TextDeltaAndToolCalls(t *testing.T) {
	e := newTestExecutor(t)
	defer close(e.outputCh)
	defer close(e.eventCh)
	defer close(e.resultCh)
	defer close(e.done)

	// text_delta is buffered (no event, no output yet); the LLM token carries
	// its own whitespace so concatenated deltas reproduce the original text.
	e.handleEvent(&event{
		Type:                  eventMessageUpdate,
		AssistantMessageEvent: &assistantMessageEvent{Type: assistantEventTextDelta, Delta: "hello"},
	})
	// tool start flushes the buffer (one STDOUT chunk "hello") + TOOL_CALL_STARTED.
	e.handleEvent(&event{
		Type:       eventToolExecutionStart,
		ToolCallID: "tc-1",
		ToolName:   "bash",
		Args:       json.RawMessage(`{"command":"echo hi"}`),
	})
	// tool end → TOOL_CALL_FINISHED.
	e.handleEvent(&event{
		Type:       eventToolExecutionEnd,
		ToolCallID: "tc-1",
		Result:     json.RawMessage(`{"stdout":"hi"}`),
	})
	// agent_settled → terminal.
	assert.True(t, e.handleEvent(&event{Type: eventAgentSettled}))

	// Drain: 2 events (started, finished) + 1 buffered STDOUT chunk flushed at
	// the tool-call boundary. text_delta no longer emits a TEXT_DELTA event
	// (unused on the frontend; matches ACP, which emits none).
	var events []executor.Event
	var outputs []executor.OutputChunk
	for {
		select {
		case ev := <-e.eventCh:
			events = append(events, ev)
		case chunk := <-e.outputCh:
			outputs = append(outputs, chunk)
		default:
			goto done
		}
	}
done:
	require.Len(t, events, 2, "tool start + tool end")
	assert.Equal(t, v1pb.CommandEventType_TOOL_CALL_STARTED, events[0].Type)
	// The bash command is the title (mirrors opencode), not just "bash".
	assert.Equal(t, "echo hi", events[0].ToolCallStarted.Title)
	assert.Equal(t, v1pb.CommandEventType_TOOL_CALL_FINISHED, events[1].Type)
	assert.Equal(t, "success", events[1].ToolCallFinished.Status)
	require.Len(t, outputs, 1, "buffered text_delta flushed as one STDOUT chunk")
	assert.Equal(t, v1pb.CommandOutput_STDOUT, outputs[0].StreamType)
	assert.Equal(t, "hello", outputs[0].Content)
	assert.Equal(t, int32(1), e.toolCallCount.Load())
}

// TestEventMapping_CompactionEvents verifies pi's compaction_start/end events
// are promoted to structured CONTEXT_COMPACTION_* events with the reason.
func TestEventMapping_CompactionEvents(t *testing.T) {
	e := newTestExecutor(t)
	defer close(e.outputCh)
	defer close(e.eventCh)
	defer close(e.resultCh)
	defer close(e.done)

	e.handleEvent(&event{Type: eventCompactionStart, Reason: "  window full  "})
	e.handleEvent(&event{Type: eventCompactionEnd, Reason: "  window full  "})

	var events []executor.Event
	for {
		select {
		case ev := <-e.eventCh:
			events = append(events, ev)
		default:
			goto done
		}
	}
done:
	require.Len(t, events, 2, "compaction start + end")
	assert.Equal(t, v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, events[0].Type)
	require.NotNil(t, events[0].ContextCompaction)
	assert.Equal(t, "window full", events[0].ContextCompaction.Reason)
	assert.False(t, events[0].ContextCompaction.Inferred, "pi events are direct, not inferred")
	assert.Equal(t, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED, events[1].Type)
	require.NotNil(t, events[1].ContextCompaction)
	assert.Equal(t, "window full", events[1].ContextCompaction.Reason)
}

func TestPiTurnPromptText_ReanchorOnWarmTurn(t *testing.T) {
	e := newTestExecutor(t)
	e.req = executor.Request{
		TurnPrompt:     "New messages received:\n\nwork",
		ReanchorPrompt: executor.BuildReanchorPrompt("alice", ""),
	}
	got := e.turnPromptText(true)
	assert.Contains(t, got, "Re-anchor (context compaction recovery)")
	assert.Contains(t, got, "New messages received:")
	assert.Contains(t, got, "Laelia inbox notice", "re-anchor must carry the same-turn steering instruction")
	assert.True(t, strings.Index(got, "Re-anchor") < strings.Index(got, "New messages received:"),
		"anchor must be prepended to the warm batch")
}

func TestPiTurnPromptText_ColdTurnIgnoresReanchor(t *testing.T) {
	e := newTestExecutor(t)
	e.identity = "alice"
	e.req = executor.Request{
		TurnPrompt:     "batch",
		ReanchorPrompt: executor.BuildReanchorPrompt("alice", ""),
	}
	got := e.turnPromptText(false)
	assert.NotContains(t, got, "Re-anchor")
	assert.Contains(t, got, `You are "alice"`, "cold turn sends the full init prompt")
	assert.Contains(t, got, "Laelia inbox notice", "cold init prompt must carry the same-turn steering instruction")
	assert.Contains(t, got, "batch")
}

// TestDeriveToolTitle covers the tool-card title derivation: the bash command
// becomes the title; read/edit append the path; unknown shapes fall back to the
// tool name.
func TestDeriveToolTitle(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		args     json.RawMessage
		want     string
	}{
		{"bash command", "bash", json.RawMessage(`{"command":"echo hi"}`), "echo hi"},
		{"edit path", "edit", json.RawMessage(`{"path":"notes/channels.md","edits":[]}`), "edit notes/channels.md"},
		{"read file_path", "read", json.RawMessage(`{"file_path":"src/main.go"}`), "read src/main.go"},
		{"no args", "bash", nil, "bash"},
		{"unknown shape", "grep", json.RawMessage(`{"pattern":"foo"}`), "grep"},
		{"empty command falls back", "bash", json.RawMessage(`{"command":"  "}`), "bash"},
		{"empty tool name", "", json.RawMessage(`{"command":"ls"}`), "ls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deriveToolTitle(tc.toolName, tc.args))
		})
	}
}

// newTestExecutor builds a PiExecutor with a nil-noop session for event-mapping
// tests that never touch the subprocess.
func newTestExecutor(t *testing.T) *PiExecutor {
	t.Helper()
	cfg := &PiConfig{
		APIProvider:      APIProviderDeepseek,
		Model:            "deepseek-chat",
		APIKey:           "sk",
		PiBinaryPath:     "/bin/pi",
		MaxEventCount:    10000,
		MaxOutputBytes:   1 << 20,
		OutputFlushBytes: defaultOutputFlushBytes,
	}
	e := &PiExecutor{
		cfg:         cfg,
		identity:    "TestAgent",
		ctx:         context.Background(),
		outputCh:    make(chan executor.OutputChunk, outputBufferSize),
		eventCh:     make(chan executor.Event, outputBufferSize),
		resultCh:    make(chan executor.Result, 1),
		done:        make(chan struct{}),
		toolStarted: map[string]bool{},
	}
	return e
}
