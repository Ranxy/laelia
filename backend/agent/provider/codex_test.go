package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"

	"github.com/Ranxy/laelia/backend/agent/acp2"
)

func notif(method, params string) acp2.Notification {
	return acp2.Notification{Method: method, Params: json.RawMessage(params)}
}

func TestCodexEventMapperAgentMessageDelta(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("item/agentMessage/delta", `{"itemId":"a1","delta":"hello","phase":"final_answer"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventTextDelta || evs[0].Text != "hello" {
		t.Fatalf("final_answer delta: %+v", evs)
	}
	evs = m.MapNotification(notif("item/agentMessage/delta", `{"itemId":"a2","delta":"working...","phase":"planning"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("non-final delta should degrade to raw: %+v", evs)
	}
	evs = m.MapNotification(notif("item/agentMessage/delta", `{"itemId":"a3","delta":""}`))
	if len(evs) != 0 {
		t.Fatalf("empty delta should produce no events: %+v", evs)
	}
}

func TestCodexEventMapperReasoningDelta(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("item/reasoning/summaryTextDelta", `{"itemId":"r1","delta":"thinking..."}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventThinkingDelta || evs[0].Text != "thinking..." {
		t.Fatalf("reasoning delta: %+v", evs)
	}
	// Per-token reasoning stream (item/reasoning/textDelta) is internal
	// progress, not user-visible thinking — degrade to raw like raft does.
	evs = m.MapNotification(notif("item/reasoning/textDelta", `{"itemId":"r1","delta":"token by token","contentIndex":0}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("textDelta should degrade to raw: %+v", evs)
	}
}

func TestCodexEventMapperToolCalls(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("item/started", `{"item":{"id":"c1","type":"commandExecution","command":"ls -la"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventToolCallStarted || evs[0].ToolCall.Kind != "shell" {
		t.Fatalf("commandExecution started: %+v", evs)
	}
	if string(evs[0].ToolCall.Input) != `{"command":"ls -la"}` {
		t.Fatalf("commandExecution input: %s", evs[0].ToolCall.Input)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"c1","type":"commandExecution"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventToolCallFinished || evs[0].ToolCall.Kind != "shell" {
		t.Fatalf("commandExecution completed: %+v", evs)
	}

	evs = m.MapNotification(notif("item/started", `{"item":{"id":"f1","type":"fileChange","changes":[{"path":"a.txt","kind":"edit"},{"path":"b.txt","kind":"create"}]}}`))
	if len(evs) != 2 || evs[0].ToolCall.Kind != "file_change" || evs[1].ToolCall.Kind != "file_change" {
		t.Fatalf("fileChange started: %+v", evs)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"f1","type":"fileChange"}}`))
	if len(evs) != 2 || evs[0].Type != acp2.EventToolCallFinished {
		t.Fatalf("fileChange completed: %+v", evs)
	}

	evs = m.MapNotification(notif("item/started", `{"item":{"id":"m1","type":"mcpToolCall","server":"fs","tool":"read","arguments":{"path":"/x"}}}`))
	if len(evs) != 1 || evs[0].ToolCall.Kind != "mcp_fs_read" {
		t.Fatalf("mcpToolCall started: %+v", evs)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"m1","type":"mcpToolCall","server":"fs","tool":"read"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventToolCallFinished || evs[0].ToolCall.Kind != "mcp_fs_read" {
		t.Fatalf("mcpToolCall completed: %+v", evs)
	}

	evs = m.MapNotification(notif("item/started", `{"item":{"id":"w1","type":"webSearch","query":"golang"}}`))
	if len(evs) != 1 || evs[0].ToolCall.Kind != "web_search" {
		t.Fatalf("webSearch started: %+v", evs)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"w1","type":"webSearch"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventToolCallFinished {
		t.Fatalf("webSearch completed: %+v", evs)
	}
}

func TestCodexEventMapperCompactionAndReasoningItem(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("item/started", `{"item":{"id":"cc1","type":"contextCompaction"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventContextCompactionStarted {
		t.Fatalf("compaction started: %+v", evs)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"cc1","type":"contextCompaction"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventContextCompactionFinished {
		t.Fatalf("compaction finished: %+v", evs)
	}

	// Reasoning item completed with a summary that was never streamed.
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"r9","type":"reasoning","summary":["line1","line2"]}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventThinkingDelta || evs[0].Text != "line1\nline2" {
		t.Fatalf("reasoning item completed: %+v", evs)
	}
	// Streamed reasoning items must not re-emit on completion.
	evs = m.MapNotification(notif("item/reasoning/summaryTextDelta", `{"itemId":"r9","delta":"x"}`))
	if len(evs) != 1 {
		t.Fatalf("reasoning delta: %+v", evs)
	}
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"r9","type":"reasoning","summary":["line1"]}}`))
	if len(evs) != 0 {
		t.Fatalf("streamed reasoning should not re-emit: %+v", evs)
	}

	// Agent message item completed with text that was never streamed.
	evs = m.MapNotification(notif("item/completed", `{"item":{"id":"a9","type":"agentMessage","phase":"final_answer","text":"final text"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventTextDelta || evs[0].Text != "final text" {
		t.Fatalf("agentMessage item completed: %+v", evs)
	}
}

func TestCodexEventMapperTokenUsage(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("thread/tokenUsage/updated", `{"tokenUsage":{"total":{"inputTokens":100,"cachedInputTokens":20,"totalTokens":150,"outputTokens":50},"modelContextWindow":200000}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventContextUsageUpdate {
		t.Fatalf("token usage: %+v", evs)
	}
	u := evs[0].ContextUsage
	if u.TotalTokens != 150 || u.InputTokens != 100 || u.CachedInputTokens != 20 || u.OutputTokens != 50 || u.ModelContextWindow != 200000 {
		t.Fatalf("token usage fields: %+v", u)
	}
	evs = m.MapNotification(notif("thread/tokenUsage/updated", `{"tokenUsage":{"total":{}}}`))
	if len(evs) != 0 {
		t.Fatalf("empty usage should produce no events: %+v", evs)
	}
}

func TestCodexEventMapperWarningsAndErrors(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("configWarning", `{"summary":"bad config","details":"detail"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventWarning || evs[0].Text != "bad config" {
		t.Fatalf("configWarning: %+v", evs)
	}
	evs = m.MapNotification(notif("warning", `{"message":"disk full"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventWarning || evs[0].Text != "disk full" {
		t.Fatalf("warning: %+v", evs)
	}
	evs = m.MapNotification(notif("error", `{"message":"boom"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventError || evs[0].Text != "boom" {
		t.Fatalf("error: %+v", evs)
	}
	evs = m.MapNotification(notif("error", `{"willRetry":true,"message":"rate limited"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("retryable error should degrade to raw: %+v", evs)
	}
	evs = m.MapNotification(notif("error", `{"error":{"message":"nested"}}`))
	if len(evs) != 1 || evs[0].Text != "nested" {
		t.Fatalf("nested error: %+v", evs)
	}
}

func TestCodexEventMapperTurnLifecycle(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("turn/started", `{"turn":{"id":"t1"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventLifecycle || evs[0].Text != "turn_started" || evs[0].TurnID != "t1" {
		t.Fatalf("turn/started: %+v", evs)
	}
	evs = m.MapNotification(notif("turn/completed", `{"turn":{"id":"t1","status":"completed"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventLifecycle || evs[0].Text != "turn_completed" {
		t.Fatalf("turn/completed: %+v", evs)
	}
	evs = m.MapNotification(notif("turn/completed", `{"turn":{"id":"t2","status":"failed","error":{"message":"model error"}}}`))
	if len(evs) != 2 || evs[0].Type != acp2.EventError || evs[0].Text != "model error" || evs[1].Type != acp2.EventLifecycle {
		t.Fatalf("turn failed: %+v", evs)
	}
}

func TestCodexEventMapperThreadStatus(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("thread/status/changed", `{"status":{"type":"systemError","message":"disk error"}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventError || evs[0].Text != "disk error" {
		t.Fatalf("systemError: %+v", evs)
	}
	evs = m.MapNotification(notif("thread/status/changed", `{"status":{"type":"active","activeFlags":["waitingOnApproval"]}}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventWarning {
		t.Fatalf("waitingOnApproval: %+v", evs)
	}
	evs = m.MapNotification(notif("thread/status/changed", `{"status":{"type":"active","activeFlags":["running"]}}`))
	if len(evs) != 0 {
		t.Fatalf("running status should produce no events: %+v", evs)
	}
}

func TestCodexEventMapperUnknownDegradesToRaw(t *testing.T) {
	m := NewCodexEventMapper()
	evs := m.MapNotification(notif("process/outputDelta", `{"delta":"x"}`))
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("unknown notification should degrade to raw: %+v", evs)
	}
}

// writeFakeCodex writes an executable fake codex script into dir and returns
// its path. The script supports app-server --help, --version, and a minimal
// NDJSON app-server loop that answers initialize and model/list.
func writeFakeCodex(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const fakeCodexScript = `#!/bin/sh
if [ "$1" = "app-server" ] && [ "$2" = "--help" ]; then
  exit 0
fi
if [ "$1" = "--version" ]; then
  echo "codex-cli 0.146.0"
  exit 0
fi
while IFS= read -r line; do
  case "$line" in
    *\"method\":\"initialize\"*)
      echo '{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex/0.146.0","protocolVersion":"2.0"}}'
      ;;
    *\"method\":\"model/list\"*)
      echo '{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"gpt-5.2-codex","displayName":"GPT-5.2 Codex","hidden":false,"isDefault":true},{"id":"gpt-5.1-codex","displayName":"GPT-5.1 Codex","hidden":false},{"id":"hidden-model","displayName":"Hidden","hidden":true}]}}'
      exit 0
      ;;
  esac
done
`

func TestCodexDetect(t *testing.T) {
	dir := t.TempDir()
	writeFakeCodex(t, dir, fakeCodexScript)
	t.Setenv("PATH", dir)

	p := &CodexProvider{}
	info, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !present || info == nil {
		t.Fatalf("expected codex detected, present=%v info=%+v", present, info)
	}
	if info.Version != "codex-cli 0.146.0" {
		t.Errorf("version = %q", info.Version)
	}
	if info.ExecutablePath != filepath.Join(dir, "codex") {
		t.Errorf("executable = %q", info.ExecutablePath)
	}
}

func TestCodexDetectAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := &CodexProvider{}
	info, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if present || info != nil {
		t.Fatalf("expected absent, present=%v info=%+v", present, info)
	}
}

func TestCodexDetectRejectsNoAppServer(t *testing.T) {
	dir := t.TempDir()
	writeFakeCodex(t, dir, `#!/bin/sh
exit 1
`)
	t.Setenv("PATH", dir)
	p := &CodexProvider{}
	_, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("expected codex without app-server to be absent")
	}
}

func TestIsCodexSandboxRunner(t *testing.T) {
	if !isCodexSandboxRunner("/foo/codex-command-runner") {
		t.Error("codex-command-runner should be excluded")
	}
	if isCodexSandboxRunner("/foo/codex") {
		t.Error("codex should not be excluded")
	}
}

func TestCodexProbeModelsV2(t *testing.T) {
	dir := t.TempDir()
	writeFakeCodex(t, dir, fakeCodexScript)
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := &CodexProvider{}
	models, err := p.ProbeModelsV2(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelOption{
		{Value: "gpt-5.2-codex", Name: "GPT-5.2 Codex"},
		{Value: "gpt-5.1-codex", Name: "GPT-5.1 Codex"},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %+v, want %+v", models, want)
	}
}

func TestCodexProbeModelsV2FallsBackToCache(t *testing.T) {
	dir := t.TempDir()
	// A codex that starts but never answers the handshake.
	writeFakeCodex(t, dir, `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", dir)

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	cache := `{"models":[
		{"slug":"gpt-5.2-codex","display_name":"GPT-5.2 Codex","visibility":"public"},
		{"slug":"private-model","visibility":"private"},
		{"slug":"api-only","supported_in_api":false}
	]}`
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := &CodexProvider{}
	models, err := p.ProbeModelsV2(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelOption{{Value: "gpt-5.2-codex", Name: "GPT-5.2 Codex"}}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %+v, want %+v", models, want)
	}
}

func TestCodexThreadCommand(t *testing.T) {
	p := &CodexProvider{}
	exe, args := p.ThreadCommand("/ws")
	if exe != "codex" || !reflect.DeepEqual(args, []string{"app-server", "--listen", "stdio://"}) {
		t.Fatalf("ThreadCommand = %q %v", exe, args)
	}
}

func TestCodexThreadMcpArgs(t *testing.T) {
	p := &CodexProvider{}
	servers := []acp.McpServer{
		{Http: &acp.McpServerHttpInline{Name: "fs", Url: "http://localhost:9000"}},
		{Stdio: &acp.McpServerStdio{Command: "echo"}},
	}
	args := p.ThreadMcpArgs(servers)
	want := []string{"-c", `mcp_servers.fs.url="http://localhost:9000"`}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("ThreadMcpArgs = %v, want %v", args, want)
	}
}

type fakeThreadProvider struct {
	fakeProvider
	models   []ModelOption
	probeErr error
}

func (*fakeThreadProvider) ThreadCommand(string) (string, []string) { return "codex", nil }
func (*fakeThreadProvider) NewThreadMapper() acp2.EventMapper       { return nil }
func (*fakeThreadProvider) ThreadMcpArgs([]acp.McpServer) []string  { return nil }
func (f *fakeThreadProvider) ProbeModelsV2(context.Context, string) ([]ModelOption, error) {
	if f.probeErr != nil {
		return nil, f.probeErr
	}
	return f.models, nil
}

func TestRegistryDiscoverUsesV2ProbeForThreadProvider(t *testing.T) {
	r := New(&fakeThreadProvider{
		fakeProvider: fakeProvider{id: "codex", display: "Codex", present: true},
		models:       []ModelOption{{Value: "gpt-5.2-codex", Name: "GPT-5.2 Codex"}},
	})
	got := r.Discover(context.Background())
	if len(got) != 1 {
		t.Fatalf("expected 1 discovered provider, got %d", len(got))
	}
	if !got[0].SupportsModelConfigOption || len(got[0].Models) != 1 {
		t.Fatalf("expected v2 models reported, got %+v", got[0])
	}
}

func TestRegistryDiscoverThreadProviderProbeFailure(t *testing.T) {
	r := New(&fakeThreadProvider{
		fakeProvider: fakeProvider{id: "codex", display: "Codex", present: true},
		probeErr:     context.DeadlineExceeded,
	})
	got := r.Discover(context.Background())
	if len(got) != 1 {
		t.Fatalf("expected provider kept on probe failure, got %d", len(got))
	}
	if got[0].SupportsModelConfigOption || len(got[0].Models) != 0 {
		t.Fatalf("expected empty models on probe failure, got %+v", got[0])
	}
}
