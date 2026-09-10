package client

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestACPCommandStreamReadFile(t *testing.T) {
	bin := requireOpencodeBinary(t)
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()
	want := "LAELIA_CS_INTEGRATION_READ"
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "target.txt"), []byte(want), 0o644))

	sink := newMemoryTurnSink()

	acpConfig := newOpencodeCSConfig(bin, workspace, false)
	cs := &commandStream{getAcpConfig: func() *executor.ACPConfig { return acpConfig }}

	req := executor.Request{
		CommandID:      "acp-cs-read",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		TurnPrompt:     "Read the file target.txt in the current workspace and reply with exactly its contents. Do not add quotes or any extra words.",
	}

	runtime, err := cs.buildRuntime(req)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.runCommand(ctx, runtime, sink, req, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		runtime.Cancel()
		t.Fatal("timed out waiting for ACP command stream")
	}

	state, stateErr := executor.LoadLocalState("", "")
	require.NoError(t, stateErr)
	assert.Nil(t, state, "local state should be cleared after completion")

	entries := sink.Entries()
	require.NotEmpty(t, entries, "should have at least one record from ACP command stream")

	assertACPCSLifecycle(t, entries)
	assertACPCSProgressOutput(t, entries)
	assertACPCSReadFileResult(t, entries, want)
	assertACPCSEvents(t, entries)
}

func TestACPCommandStreamWriteFile(t *testing.T) {
	bin := requireOpencodeBinary(t)
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()
	targetPath := filepath.Join(workspace, "note.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("before"), 0o644))

	sink := newMemoryTurnSink()

	acpConfig := newOpencodeCSConfig(bin, workspace, true)
	cs := &commandStream{getAcpConfig: func() *executor.ACPConfig { return acpConfig }}

	req := executor.Request{
		CommandID:      "acp-cs-write",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		TurnPrompt:     "Use your file editing tool to replace the entire contents of note.txt with exactly LAELIA_CS_WRITE_OK. After the write succeeds, reply with exactly DONE.",
		AllowDiff:      true,
	}

	runtime, err := cs.buildRuntime(req)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.runCommand(ctx, runtime, sink, req, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Minute):
		runtime.Cancel()
		t.Fatal("timed out waiting for ACP command stream")
	}

	content, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "LAELIA_CS_WRITE_OK", strings.TrimSpace(string(content)))

	entries := sink.Entries()
	require.NotEmpty(t, entries)
	assertACPCSLifecycle(t, entries)
	assertACPCSProgressOutput(t, entries)
	assertACPCSToolCalls(t, entries)
	assertACPCSEvents(t, entries)
}

func TestACPCommandStreamCancel(t *testing.T) {
	bin := requireOpencodeBinary(t)
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()

	sink := newMemoryTurnSink()

	acpConfig := newOpencodeCSConfig(bin, workspace, false)
	cs := &commandStream{getAcpConfig: func() *executor.ACPConfig { return acpConfig }}

	req := executor.Request{
		CommandID:      "acp-cs-cancel",
		WorkingDir:     workspace,
		TimeoutSeconds: 120,
		TurnPrompt:     "Create a file named big_report.txt. Write a detailed 500-word markdown report about the history of computing, including sections on early mechanical computers, the transistor era, the microprocessor revolution, the internet age, and modern AI computing. After writing, read back the file and count the words.",
	}

	runtime, err := cs.buildRuntime(req)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.runCommand(ctx, runtime, sink, req, nil)
		close(done)
	}()

	time.Sleep(15 * time.Second)
	runtime.Cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("timed out waiting for cancelled ACP command stream")
	}

	entries := sink.Entries()
	require.NotEmpty(t, entries)

	result := findACPCSResult(entries)
	require.NotNil(t, result, "expected a CommandResult record")
	assert.NotZero(t, result.ExitCode, "cancelled ACP task should have non-zero exit code")
}

func assertACPCSLifecycle(t *testing.T, entries []*outbox.Entry) {
	t.Helper()
	firstEvent := findACPCSEvent(entries, v1pb.CommandEventType_LIFECYCLE)
	require.NotNil(t, firstEvent, "first message should be a LIFECYCLE event")
	assert.Equal(t, int32(1), firstEvent.SeqNo)
	assert.Equal(t, "command started", firstEvent.Summary)
	if lc := firstEvent.GetLifecycle(); lc != nil {
		assert.Equal(t, "ACP", lc.GetExecutorKind())
	}
}

func assertACPCSProgressOutput(t *testing.T, entries []*outbox.Entry) {
	t.Helper()
	var hasProgress bool
	for _, entry := range entries {
		if entry.GetProgress() != nil {
			hasProgress = true
			break
		}
	}
	assert.True(t, hasProgress, "should have at least one progress record")
}

func assertACPCSReadFileResult(t *testing.T, entries []*outbox.Entry, want string) {
	t.Helper()
	result := findACPCSResult(entries)
	require.NotNil(t, result, "expected a CommandResult record")
	assert.Equal(t, int32(0), result.ExitCode, "error_message=%q final_summary=%q", result.ErrorMessage, result.FinalSummary)
	assert.Empty(t, result.ErrorMessage)

	combined := result.FinalSummary
	for _, entry := range entries {
		if event := entry.GetEvent(); event != nil && event.Type == v1pb.CommandEventType_TEXT_DELTA {
			combined += "\n" + event.Summary
		}
	}
	assert.Contains(t, compactText(combined), want, "ACP output should contain the file content %q; got summary=%q", want, result.FinalSummary)
}

func assertACPCSToolCalls(t *testing.T, entries []*outbox.Entry) {
	t.Helper()
	started := findACPCSEvent(entries, v1pb.CommandEventType_TOOL_CALL_STARTED)
	finished := findACPCSEvent(entries, v1pb.CommandEventType_TOOL_CALL_FINISHED)
	assert.True(t, started != nil || finished != nil, "should have at least one tool call event")
}

func assertACPCSEvents(t *testing.T, entries []*outbox.Entry) {
	t.Helper()
	summary := findACPCSEvent(entries, v1pb.CommandEventType_FINAL_SUMMARY)
	assert.NotNil(t, summary, "should have a FINAL_SUMMARY event")

	textDelta := findACPCSEvent(entries, v1pb.CommandEventType_TEXT_DELTA)
	assert.NotNil(t, textDelta, "should have at least one TEXT_DELTA event")
}

func findACPCSResult(entries []*outbox.Entry) *v1pb.CommandResult {
	for _, entry := range entries {
		if r := entry.GetResult(); r != nil {
			return r
		}
	}
	return nil
}

func findACPCSEvent(entries []*outbox.Entry, wantType v1pb.CommandEventType) *v1pb.CommandEvent {
	for _, entry := range entries {
		if e := entry.GetEvent(); e != nil && e.Type == wantType {
			return e
		}
	}
	return nil
}

func compactText(input string) string {
	return strings.Join(strings.Fields(input), "")
}

func requireOpencodeBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping opencode ACP integration test in short mode")
	}
	if os.Getenv("LAELIA_RUN_OPENCODE_ACP_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_OPENCODE_ACP_TESTS=1 to run local opencode ACP integration tests")
	}
	bin := os.Getenv("LAELIA_OPENCODE_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("opencode")
		if err != nil {
			t.Skip("opencode binary not found in PATH")
		}
	}
	return bin
}

func newOpencodeCSConfig(bin string, workspace string, writable bool) *executor.ACPConfig {
	args := []string{"acp", "--pure", "--cwd", workspace}
	if model := os.Getenv("LAELIA_OPENCODE_MODEL"); model != "" {
		args = append(args, "--model", model)
	}
	if agent := os.Getenv("LAELIA_OPENCODE_AGENT"); agent != "" {
		args = append(args, "--agent", agent)
	}

	return &executor.ACPConfig{
		Limits: executor.Limits{
			MaxTimeoutSeconds: 120,
			MaxEventCount:     4000,
			MaxOutputBytes:    512 * 1024,
		},
		Executable:            bin,
		Args:                  args,
		WorkingDir:            workspace,
		AdditionalDirectories: []string{workspace},
		AllowEnv: []string{
			"PATH",
			"HOME",
			"LANG",
			"TERM",
			"XDG_CONFIG_HOME",
			"XDG_DATA_HOME",
			"XDG_CACHE_HOME",
			"ANTHROPIC_API_KEY",
			"OPENAI_API_KEY",
			"GOOGLE_API_KEY",
			"OPENROUTER_API_KEY",
		},
		ReadTextFiles:      true,
		WriteTextFiles:     writable,
		SupportsDiff:       writable,
		SupportsRawEvents:  true,
		SupportsToolTraces: true,
	}
}
