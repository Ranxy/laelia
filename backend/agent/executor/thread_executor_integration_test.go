package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// defaultTestCodexHome is the writable CODEX_HOME used when the runner does
// not export one. The default ~/.codex often fails to initialize its sqlite
// state under sandboxes; a standalone home keeps the integration tests
// hermetic and leaves the user's real codex config untouched.
const defaultTestCodexHome = "/tmp/codex-home"

// testCodexHome returns the CODEX_HOME the integration tests drive, falling
// back to the standalone default when the runner does not export one.
func testCodexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	return defaultTestCodexHome
}

// requireCodexACP gates the real-codex ACP v2 integration tests: they need a
// local codex CLI with the app-server subcommand and a writable CODEX_HOME
// carrying login/config state. Skip (not fail) when either is absent so the
// suite still runs on machines without codex.
func requireCodexACP(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping codex ACP integration test in short mode")
	}
	if os.Getenv("LAELIA_RUN_CODEX_ACP_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_CODEX_ACP_TESTS=1 to run local codex ACP v2 integration tests")
	}
	bin := os.Getenv("LAELIA_CODEX_BIN")
	if bin == "" {
		lookedUp, err := exec.LookPath("codex")
		if err != nil {
			t.Skip("codex binary not found in PATH")
		}
		bin = lookedUp
	}
	home := testCodexHome()
	if _, err := os.Stat(home); err != nil {
		t.Skipf("no writable CODEX_HOME at %s; export CODEX_HOME to run codex ACP integration tests", home)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err != nil {
		t.Skipf("CODEX_HOME %s has no config.toml; export a codex home with login config to run codex ACP integration tests", home)
	}
	return bin
}

// codexTestProvider drives the real codex app-server through the real
// CodexProvider, overriding ThreadCommand with the absolute binary path so the
// subprocess spawn does not depend on the test runner's PATH.
type codexTestProvider struct {
	*provider.CodexProvider
	bin string
}

func (p *codexTestProvider) ThreadCommand(_ string) (string, []string) {
	return p.bin, []string{"app-server", "--listen", "stdio://"}
}

// newCodexTestProvider wraps the registered codex provider with the resolved
// absolute binary path.
func newCodexTestProvider(t *testing.T, bin string) provider.ThreadProvider {
	t.Helper()
	base, ok := provider.Default().Lookup("codex")
	require.True(t, ok, "codex provider must be registered")
	cp, ok := base.(*provider.CodexProvider)
	require.True(t, ok, "codex provider must be *provider.CodexProvider")
	return &codexTestProvider{CodexProvider: cp, bin: bin}
}

// newCodexTestConfig builds the ThreadConfig for the real-codex integration
// tests. HOME is redirected to a temp dir so the persisted thread session
// state stays isolated; CODEX_HOME points at a hermetic per-test copy of the
// runner's codex home so codex always finds writable config/login state and
// the test never touches the runner's real codex home.
func newCodexTestConfig(t *testing.T, workspace string) *ThreadConfig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &ThreadConfig{
		MaxTimeoutSeconds: 120,
		MaxEventCount:     2000,
		MaxOutputBytes:    256 * 1024,
		OutputFlushBytes:  4096,
		StartupTimeout:    30 * time.Second,
		Provider:          "codex",
		Model:             os.Getenv("LAELIA_CODEX_MODEL"),
		WorkingDir:        workspace,
		PersonaPrompt:     "You are a concise assistant. Reply in as few words as the task allows.",
		Env:               map[string]string{"CODEX_HOME": newCodexTestHome(t)},
		AllowEnv:          []string{"PATH", "HOME", "LANG", "TERM", "NO_COLOR"},
		SupportsRawEvents: true,
	}
}

// newCodexTestHome materializes a hermetic CODEX_HOME under a temp dir: the
// runner's config.toml + models.json are copied in and the model catalog path
// is rewritten to an absolute path so codex never depends on ~ expansion
// against the redirected HOME (a ~/.codex relative catalog breaks config
// loading when HOME points at a fresh temp dir).
func newCodexTestHome(t *testing.T) string {
	t.Helper()
	src := testCodexHome()
	home := t.TempDir()

	cfgData, err := os.ReadFile(filepath.Join(src, "config.toml"))
	if err != nil {
		t.Fatalf("read codex config.toml from %s: %v", src, err)
	}
	modelsDest := filepath.Join(home, "models.json")
	cfgData = regexp.MustCompile(`(?m)^model_catalog_json\s*=\s*"[^"]*"`).
		ReplaceAll(cfgData, []byte(`model_catalog_json = "`+modelsDest+`"`))
	if err := os.WriteFile(filepath.Join(home, "config.toml"), cfgData, 0o600); err != nil {
		t.Fatalf("write hermetic codex config.toml: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(src, "models.json")); err == nil {
		if err := os.WriteFile(modelsDest, data, 0o644); err != nil {
			t.Fatalf("write hermetic codex models.json: %v", err)
		}
	}
	return home
}

func newCodexTestRequest(workspace, machineID, agentID, turnPrompt string) Request {
	return Request{
		CommandID:        "codex-integration",
		AgentID:          agentID,
		MachineID:        machineID,
		AgentDisplayName: "codex-test-agent",
		WorkingDir:       workspace,
		TurnPrompt:       turnPrompt,
		TimeoutSeconds:   120,
	}
}

// TestThreadExecutorCodexColdThenWarmResumesThread is the v2 mirror of
// TestACPExecutorColdThenWarmResumesSession: a cold turn reads a secret file
// into the thread's conversation, the file is deleted, and a fresh subprocess
// resumes the same thread id so the warm turn can recall the secret from
// conversation history — proving the cold-start history survived the process
// death and that thread resume (not a new thread) ran.
func TestThreadExecutorCodexColdThenWarmResumesThread(t *testing.T) {
	bin := requireCodexACP(t)
	workspace := t.TempDir()
	const (
		machineID = "test-machine-codex-integration"
		agentID   = "test-agent-codex-integration"
	)
	clearACPSession(machineID, agentID)
	t.Cleanup(func() { clearACPSession(machineID, agentID) })

	secret := "ZEPHYR"
	seedPath := filepath.Join(workspace, "secret.txt")
	require.NoError(t, os.WriteFile(seedPath, []byte(secret), 0o644))

	cfg := newCodexTestConfig(t, workspace)
	p := newCodexTestProvider(t, bin)

	cold, err := NewThread(
		newCodexTestRequest(workspace, machineID, agentID,
			"Read the file secret.txt in the current workspace and reply with exactly its contents. Do not add quotes or any extra words."),
		cfg, p)
	require.NoError(t, err)
	coldObs := runACPTestRuntime(t, cold, 150*time.Second, 0)

	require.Zero(t, coldObs.result.ExitCode, "cold turn failed: outputs=%q err=%s", joinOutput(coldObs.outputs), coldObs.result.ErrorMessage)
	assert.False(t, coldObs.result.Resumed, "first turn must be cold (no persisted thread)")
	assert.NotEmpty(t, coldObs.result.SessionID, "cold turn must persist a thread id")
	assert.Contains(t, compactText(joinOutput(coldObs.outputs)), secret, "cold turn must read the secret into the conversation")

	// Remove the file so the warm turn cannot simply re-read it; it must recall
	// the secret from the resumed thread's conversation history.
	require.NoError(t, os.Remove(seedPath))

	warm, err := NewThread(
		newCodexTestRequest(workspace, machineID, agentID,
			"Reply with exactly the single secret word I told you earlier in this conversation, nothing else. Do not read any file."),
		cfg, p)
	require.NoError(t, err)
	warmObs := runACPTestRuntime(t, warm, 150*time.Second, 0)

	require.Zero(t, warmObs.result.ExitCode, "warm turn failed: outputs=%q err=%s", joinOutput(warmObs.outputs), warmObs.result.ErrorMessage)
	assert.True(t, warmObs.result.Resumed, "second turn must resume the persisted thread (warm)")
	assert.Equal(t, coldObs.result.SessionID, warmObs.result.SessionID, "warm turn must resume the same thread id")

	combined := strings.ToUpper(compactText(joinOutput(warmObs.outputs)) + compactText(warmObs.result.FinalSummary))
	assert.Contains(t, combined, secret,
		"warm turn must recall the secret from the resumed thread; outputs=%q summary=%q", joinOutput(warmObs.outputs), warmObs.result.FinalSummary)
}

// TestThreadExecutorCodexToolCallSurfacesEvents verifies the full event
// surface against a real codex turn that performs a shell tool call: lifecycle
// frames, tool call started/finished, token usage, and the final summary all
// surface through the thread executor.
func TestThreadExecutorCodexToolCallSurfacesEvents(t *testing.T) {
	bin := requireCodexACP(t)
	workspace := t.TempDir()
	// Seed one file so the directory has a deterministic, non-empty entry set.
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "seed.txt"), []byte("x"), 0o644))
	const (
		machineID = "test-machine-codex-tool"
		agentID   = "test-agent-codex-tool"
	)
	clearACPSession(machineID, agentID)
	t.Cleanup(func() { clearACPSession(machineID, agentID) })

	cfg := newCodexTestConfig(t, workspace)
	rt, err := NewThread(
		newCodexTestRequest(workspace, machineID, agentID,
			"Run `ls -la` in the current workspace and reply with how many entries the directory contains. Reply with the number only."),
		cfg, newCodexTestProvider(t, bin))
	require.NoError(t, err)
	obs := runACPTestRuntime(t, rt, 150*time.Second, 0)

	require.Zero(t, obs.result.ExitCode, "turn failed: outputs=%q err=%s", joinOutput(obs.outputs), obs.result.ErrorMessage)

	types := eventTypes(obs.events)
	assert.Contains(t, types, v1pb.CommandEventType_LIFECYCLE, "turn lifecycle must surface")
	assert.Contains(t, types, v1pb.CommandEventType_TOOL_CALL_STARTED, "shell tool call start must surface")
	assert.Contains(t, types, v1pb.CommandEventType_TOOL_CALL_FINISHED, "shell tool call finish must surface")
	assert.Contains(t, types, v1pb.CommandEventType_CONTEXT_USAGE_UPDATE, "token usage update must surface")
	assert.Contains(t, types, v1pb.CommandEventType_FINAL_SUMMARY, "final summary must surface")

	// The tool call must carry the actual shell command.
	for _, ev := range obs.events {
		if ev.Type == v1pb.CommandEventType_TOOL_CALL_STARTED {
			require.NotNil(t, ev.ToolCallStarted)
			cmd, ok := ev.ToolCallStarted.RawInput.Fields["command"]
			assert.True(t, ok, "tool call input must carry the command; got %v", ev.ToolCallStarted.RawInput.Fields)
			assert.Contains(t, cmd.GetStringValue(), "ls", "tool call command must be the ls invocation")
		}
	}

	// The raw channel must be active: per-token reasoning stream deltas
	// (item/reasoning/textDelta) and other internal frames degrade to RAW_ACP
	// events instead of user-visible thinking/text.
	assert.Contains(t, types, v1pb.CommandEventType_RAW_ACP, "internal frames must degrade to raw events")
	assert.NotEmpty(t, obs.result.FinalSummary, "final summary must carry the reply")
}
