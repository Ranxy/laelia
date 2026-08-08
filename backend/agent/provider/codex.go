package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/coder/acp-go-sdk"

	"github.com/Ranxy/laelia/backend/agent/acp2"
)

// CodexProvider discovers and launches the codex CLI's ACP v2 app-server. It
// is the first provider speaking the v2 thread protocol; the thread executor
// drives it through the ThreadProvider capability.
type CodexProvider struct{}

func (*CodexProvider) ID() string          { return "codex" }
func (*CodexProvider) DisplayName() string { return "Codex" }

// ToolCallAdapter returns DefaultAdapter: codex speaks the v2 thread protocol,
// so the v1 tool-call adapter is never used (the thread executor maps tool
// calls from the EventMapper instead). The method exists to satisfy Provider.
func (*CodexProvider) ToolCallAdapter() ToolCallAdapter { return DefaultAdapter{} }

// ThreadCommand launches the codex app-server over stdio. The executable is
// resolved on PATH at spawn time, matching the v1 providers.
func (*CodexProvider) ThreadCommand(_ string) (string, []string) {
	return "codex", []string{"app-server", "--listen", "stdio://"}
}

// BuildCommand returns the same app-server launch as ThreadCommand. Codex
// rejects the v1 session protocol, so the v1 executor never drives it; the
// method exists to satisfy Provider.
func (*CodexProvider) BuildCommand(_ string) (string, []string) {
	return "codex", []string{"app-server", "--listen", "stdio://"}
}

// ProbeModels returns no models: codex rejects the v1 session protocol, so
// model discovery goes through ProbeModelsV2. The method exists to satisfy
// Provider.
func (*CodexProvider) ProbeModels(context.Context, string) ([]ModelOption, bool, error) {
	return nil, false, nil
}

// ThreadMcpArgs converts managed MCP servers into codex CLI args
// (-c mcp_servers.<name>.url=<url>). Only HTTP servers are supported; the
// URL is JSON-quoted like the codex config format expects.
func (*CodexProvider) ThreadMcpArgs(servers []acp.McpServer) []string {
	var args []string
	for _, s := range servers {
		if s.Http == nil {
			continue
		}
		urlJSON, err := json.Marshal(s.Http.Url)
		if err != nil {
			continue
		}
		args = append(args, "-c", fmt.Sprintf("mcp_servers.%s.url=%s", s.Http.Name, urlJSON))
	}
	return args
}

// NewThreadMapper returns the codex notification -> neutral event mapper.
func (*CodexProvider) NewThreadMapper() acp2.EventMapper { return NewCodexEventMapper() }

// Detect reports whether a codex CLI with app-server support is installed.
// The candidate must pass `codex app-server --help`; a codex binary without
// the v2 app-server is treated as absent.
func (p *CodexProvider) Detect(ctx context.Context) (*Detected, bool, error) {
	exe, _ := resolveCodexCandidate(ctx)
	if exe == "" {
		return nil, false, nil
	}
	return &Detected{
		ProviderID:     p.ID(),
		DisplayName:    p.DisplayName(),
		Version:        codexVersion(ctx, exe),
		ExecutablePath: exe,
	}, true, nil
}

// CheckThreadCompat implements ThreadCompatChecker: it resolves the codex
// candidate (app-server probe + version check) and returns the executable to
// spawn, or a clear error when no compatible candidate exists so a stale agent
// config fails fast with an upgrade hint instead of a confusing handshake
// failure.
func (*CodexProvider) CheckThreadCompat(ctx context.Context) (string, error) {
	exe, rejected := resolveCodexCandidate(ctx)
	if exe != "" {
		return exe, nil
	}
	msg := "Cannot resolve a compatible Codex CLI app-server entry point."
	if len(rejected) > 0 {
		msg += " Rejected candidates: " + strings.Join(rejected, "; ") + "."
	}
	return "", errors.New(msg)
}

// ProbeModelsV2 returns the models codex advertises. The app-server model/list
// RPC is tried first; on failure or an empty result the local models cache
// (~/.codex/models_cache.json) is the fallback.
func (*CodexProvider) ProbeModelsV2(ctx context.Context, workspaceDir string) ([]ModelOption, error) {
	models, err := probeCodexModelsFromAppServer(ctx, workspaceDir)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	if cached := codexModelsFromCache(); len(cached) > 0 {
		return cached, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// codexCandidates returns the codex entry points in preference order: PATH
// lookup (excluding the sandbox helper codex-command-runner), the macOS
// desktop bundle, and on Windows the npm global install and desktop install.
func codexCandidates() []string {
	var candidates []string
	if path, err := exec.LookPath("codex"); err == nil && !isCodexSandboxRunner(path) {
		candidates = append(candidates, path)
	}
	if runtime.GOOS == "darwin" {
		const desktopBundle = "/Applications/Codex.app/Contents/Resources/codex"
		if _, err := os.Stat(desktopBundle); err == nil {
			candidates = append(candidates, desktopBundle)
		}
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, windowsCodexCandidates()...)
	}
	return candidates
}

// isCodexSandboxRunner reports whether the path is the codex-command-runner
// sandbox helper, which is not the codex CLI itself.
func isCodexSandboxRunner(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "codex-command-runner")
}

// windowsCodexCandidates resolves the npm global @openai/codex entry and the
// Codex Desktop install on Windows.
func windowsCodexCandidates() []string {
	var candidates []string
	if out, err := exec.Command("npm", "root", "-g").Output(); err == nil {
		candidate := filepath.Join(strings.TrimSpace(string(out)), "@openai", "codex", "bin", "codex.js")
		if _, err := os.Stat(candidate); err == nil {
			candidates = append(candidates, candidate)
		}
	}
	roots := []string{os.Getenv("LOCALAPPDATA")}
	if profile := os.Getenv("USERPROFILE"); profile != "" {
		roots = append(roots, filepath.Join(profile, "AppData", "Local"))
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, rel := range []string{
			filepath.Join("Programs", "OpenAI", "Codex", "bin", "codex.exe"),
			filepath.Join("OpenAI", "Codex", "bin", "codex.exe"),
		} {
			candidate := filepath.Join(root, rel)
			if _, err := os.Stat(candidate); err == nil {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

// resolveCodexCandidate returns the first candidate whose app-server probe
// succeeds and whose version is supported, or "" when none does. rejected
// carries one reason per rejected candidate for diagnostics.
func resolveCodexCandidate(ctx context.Context) (string, []string) {
	var rejected []string
	for _, exe := range codexCandidates() {
		if !codexSupportsAppServer(ctx, exe) {
			rejected = append(rejected, exe+" rejected: app-server probe failed")
			continue
		}
		if msg := unsupportedCodexVersionMessage(codexVersion(ctx, exe)); msg != "" {
			rejected = append(rejected, exe+" rejected: "+msg)
			continue
		}
		return exe, rejected
	}
	return "", rejected
}

// codexSupportsAppServer verifies `codex app-server --help` exits cleanly
// within 5s.
func codexSupportsAppServer(ctx context.Context, exe string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, exe, "app-server", "--help")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// codexVersion returns the first line of `codex --version`, or "" on failure.
func codexVersion(ctx context.Context, exe string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, exe, "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return line
}

// minCodexVersion is the oldest codex CLI the v2 thread integration supports.
// Codex 0.95.0 added the phase field on item/agentMessage/delta, which the
// event mapper relies on to surface only final_answer deltas as output; older
// builds cannot be driven correctly.
const minCodexVersion = "0.95.0"

// codexVersionRe matches the semver triple inside a codex --version line
// (e.g. "codex-cli 0.146.0").
var codexVersionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// parseCodexVersion extracts the semver triple from a codex --version line.
func parseCodexVersion(version string) ([3]int, bool) {
	m := codexVersionRe.FindStringSubmatch(version)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

// codexVersionSupported reports whether a codex --version line is at least
// minCodexVersion. An unparseable version is treated as supported so a probe
// failure never hides an otherwise working install.
func codexVersionSupported(version string) bool {
	actual, ok := parseCodexVersion(version)
	if !ok {
		return true
	}
	minimum, ok := parseCodexVersion(minCodexVersion)
	if !ok {
		return true
	}
	for i, part := range actual {
		if part > minimum[i] {
			return true
		}
		if part < minimum[i] {
			return false
		}
	}
	return true
}

// unsupportedCodexVersionMessage returns the upgrade hint for a codex
// --version line below minCodexVersion, or "" when the version is supported.
func unsupportedCodexVersionMessage(version string) string {
	if version == "" || codexVersionSupported(version) {
		return ""
	}
	return fmt.Sprintf("Codex CLI %s is unsupported; requires Codex >= %s. Upgrade codex before starting this runtime.", version, minCodexVersion)
}

// probeCodexModelsFromAppServer spawns the codex app-server, runs the
// initialize handshake, and walks model/list pages.
func probeCodexModelsFromAppServer(ctx context.Context, workspaceDir string) ([]ModelOption, error) {
	if workspaceDir == "" {
		workspaceDir = "."
	}
	exe, _ := resolveCodexCandidate(ctx)
	if exe == "" {
		exe = "codex"
	}
	cmd := exec.CommandContext(ctx, exe, "app-server", "--listen", "stdio://")
	cmd.Dir = workspaceDir
	cmd.Env = probeEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Drain stderr so codex logs do not fill its pipe and block.
	go func(r io.Reader) { _, _ = io.Copy(io.Discard, r) }(stderr)

	client := acp2.NewClient(acp2.NewTransport(stdin), stdout, nil)
	client.Start()
	defer func() {
		client.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := client.Initialize(ctx, "laelia-machine-probe", "0.1.0"); err != nil {
		return nil, err
	}
	if err := client.Initialized(); err != nil {
		return nil, err
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelOption, 0, len(models))
	for _, m := range models {
		if m.Hidden {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		out = append(out, ModelOption{Value: m.ID, Name: name})
	}
	return out, nil
}

// codexModelsCacheEntry is one entry of ~/.codex/models_cache.json.
type codexModelsCacheEntry struct {
	Slug           string `json:"slug"`
	DisplayName    string `json:"display_name"`
	Visibility     string `json:"visibility"`
	SupportedInAPI *bool  `json:"supported_in_api"`
}

// codexModelsFromCache reads the local codex models cache, filtering out
// hidden and API-unsupported entries. It returns nil when no cache is found.
func codexModelsFromCache() []ModelOption {
	var models []ModelOption
	for _, root := range codexStateRoots() {
		raw, err := os.ReadFile(filepath.Join(root, "models_cache.json"))
		if err != nil {
			continue
		}
		var cache struct {
			Models []codexModelsCacheEntry `json:"models"`
		}
		if err := json.Unmarshal(raw, &cache); err != nil {
			continue
		}
		for _, e := range cache.Models {
			if e.Slug == "" {
				continue
			}
			if e.Visibility != "" && e.Visibility != "public" && e.Visibility != "list" {
				continue
			}
			if e.SupportedInAPI != nil && !*e.SupportedInAPI {
				continue
			}
			name := e.DisplayName
			if name == "" {
				name = e.Slug
			}
			models = append(models, ModelOption{Value: e.Slug, Name: name})
		}
		if len(models) > 0 {
			return models
		}
	}
	return nil
}

// codexStateRoots returns the candidate codex state roots: CODEX_HOME (or
// ~/.codex) plus the nested <root>/.codex variant.
func codexStateRoots() []string {
	var roots []string
	if home := os.Getenv("CODEX_HOME"); home != "" {
		roots = append(roots, home)
	} else if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".codex"))
	}
	var out []string
	for _, r := range roots {
		out = append(out, r, filepath.Join(r, ".codex"))
	}
	return out
}

// NewCodexEventMapper returns an empty codex event mapper.
func NewCodexEventMapper() *CodexEventMapper {
	return &CodexEventMapper{
		streamedAgentMessageIDs: map[string]struct{}{},
		agentMessagePhases:      map[string]string{},
		streamedReasoningIDs:    map[string]struct{}{},
		reasoningTextBuffers:    map[string]*strings.Builder{},
		fileChangeCounts:        map[string]int{},
	}
}

// CodexEventMapper translates codex app-server notifications into neutral
// acp2 events. Agent message deltas are user-visible only in the final_answer
// phase, the raw per-token reasoning stream is buffered and surfaces as one
// thinking delta at item completion, reasoning summaries surface as thinking
// deltas, item started/completed frames become tool call boundaries, and
// everything unrecognized degrades to raw so nothing is silently dropped.
type CodexEventMapper struct {
	streamedAgentMessageIDs map[string]struct{}
	agentMessagePhases      map[string]string
	streamedReasoningIDs    map[string]struct{}
	reasoningTextBuffers    map[string]*strings.Builder
	fileChangeCounts        map[string]int
}

// Reset clears the per-turn tracking state. The executor calls it when a new
// turn starts on a reused mapper.
func (m *CodexEventMapper) Reset() {
	m.streamedAgentMessageIDs = map[string]struct{}{}
	m.agentMessagePhases = map[string]string{}
	m.streamedReasoningIDs = map[string]struct{}{}
	m.reasoningTextBuffers = map[string]*strings.Builder{}
	m.fileChangeCounts = map[string]int{}
}

// MapNotification implements acp2.EventMapper.
func (m *CodexEventMapper) MapNotification(n acp2.Notification) []acp2.Event {
	var params map[string]json.RawMessage
	_ = json.Unmarshal(n.Params, &params)
	turnID := codexTurnID(params)
	switch n.Method {
	case "turn/started":
		return []acp2.Event{{Type: acp2.EventLifecycle, TurnID: turnID, Text: "turn_started"}}
	case "turn/completed":
		return m.turnCompleted(params, turnID)
	case "item/agentMessage/delta":
		return m.agentMessageDelta(params, turnID)
	case "item/reasoning/summaryTextDelta":
		return m.reasoningSummaryDelta(params, turnID)
	case "item/reasoning/textDelta":
		return m.reasoningTextDelta(params, turnID)
	case "item/started", "item/completed":
		return m.itemEvent(n.Method, params, turnID)
	case "thread/tokenUsage/updated":
		return m.tokenUsage(params, turnID)
	case "configWarning", "warning", "guardianWarning", "deprecationNotice":
		return m.warning(n.Method, params, turnID)
	case "thread/status/changed":
		return m.threadStatusChanged(params, turnID)
	case "error":
		return m.errorEvent(params, turnID)
	default:
		return []acp2.Event{{Type: acp2.EventRaw, TurnID: turnID, Raw: n.Params}}
	}
}

// turnCompleted maps turn/completed: failed/interrupted turns surface an
// error, and every completion emits a lifecycle marker for the turn gate.
func (m *CodexEventMapper) turnCompleted(params map[string]json.RawMessage, turnID string) []acp2.Event {
	var events []acp2.Event
	var turn struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if raw, ok := params["turn"]; ok {
		_ = json.Unmarshal(raw, &turn)
	}
	switch turn.Status {
	case "failed":
		msg := "Codex turn failed"
		if turn.Error != nil && turn.Error.Message != "" {
			msg = turn.Error.Message
		}
		events = append(events, acp2.Event{Type: acp2.EventError, TurnID: turnID, Text: msg})
	case "interrupted":
		msg := "Codex turn interrupted"
		if turn.Error != nil && turn.Error.Message != "" {
			msg += ": " + turn.Error.Message
		}
		events = append(events, acp2.Event{Type: acp2.EventError, TurnID: turnID, Text: msg})
	default:
		// completed and other statuses carry no error event.
	}
	m.Reset()
	events = append(events, acp2.Event{Type: acp2.EventLifecycle, TurnID: turnID, Text: "turn_completed"})
	return events
}

// agentMessageDelta maps item/agentMessage/delta. Deltas outside the
// final_answer phase are internal progress and degrade to raw.
func (m *CodexEventMapper) agentMessageDelta(params map[string]json.RawMessage, turnID string) []acp2.Event {
	itemID := codexString(params, "itemId")
	phase := codexString(params, "phase")
	if phase == "" {
		if raw, ok := params["item"]; ok {
			var item struct {
				Phase string `json:"phase"`
			}
			_ = json.Unmarshal(raw, &item)
			phase = item.Phase
		}
	}
	if itemID != "" {
		m.streamedAgentMessageIDs[itemID] = struct{}{}
	}
	delta := codexString(params, "delta")
	if delta == "" {
		return nil
	}
	if isUserVisibleCodexPhase(phase) {
		return []acp2.Event{{Type: acp2.EventTextDelta, TurnID: turnID, Text: delta}}
	}
	return []acp2.Event{{Type: acp2.EventRaw, TurnID: turnID, Raw: rawParams(params)}}
}

// reasoningSummaryDelta maps item/reasoning/summaryTextDelta to thinking.
func (m *CodexEventMapper) reasoningSummaryDelta(params map[string]json.RawMessage, turnID string) []acp2.Event {
	itemID := codexString(params, "itemId")
	if itemID != "" {
		m.streamedReasoningIDs[itemID] = struct{}{}
	}
	delta := codexString(params, "delta")
	if delta == "" {
		return nil
	}
	if _, buffered := m.reasoningTextBuffers[itemID]; buffered {
		// The full reasoning stream is buffered and will be emitted as one
		// thinking delta at item completion; skip the redundant summary.
		return nil
	}
	return []acp2.Event{{Type: acp2.EventThinkingDelta, TurnID: turnID, Text: delta}}
}

// reasoningTextDelta buffers the raw per-token reasoning stream per item.
// The full text is emitted as a single thinking delta when the item
// completes, so a long reasoning stream does not fragment into hundreds of
// events.
func (m *CodexEventMapper) reasoningTextDelta(params map[string]json.RawMessage, _ string) []acp2.Event {
	itemID := codexString(params, "itemId")
	delta := codexString(params, "delta")
	if itemID == "" || delta == "" {
		return nil
	}
	buf := m.reasoningTextBuffers[itemID]
	if buf == nil {
		buf = &strings.Builder{}
		m.reasoningTextBuffers[itemID] = buf
	}
	_, _ = buf.WriteString(delta)
	return nil
}

// itemEvent maps item/started and item/completed frames by item type.
func (m *CodexEventMapper) itemEvent(method string, params map[string]json.RawMessage, turnID string) []acp2.Event {
	raw, ok := params["item"]
	if !ok {
		return nil
	}
	var item struct {
		ID      string   `json:"id"`
		Type    string   `json:"type"`
		Phase   string   `json:"phase"`
		Text    string   `json:"text"`
		Command string   `json:"command"`
		Summary []string `json:"summary"`
		Changes []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
		Tool      string          `json:"tool"`
		Server    string          `json:"server"`
		Arguments json.RawMessage `json:"arguments"`
		Query     string          `json:"query"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil
	}
	started := method == "item/started"
	completed := method == "item/completed"
	var events []acp2.Event
	switch item.Type {
	case "reasoning":
		if completed && item.ID != "" {
			if buf, ok := m.reasoningTextBuffers[item.ID]; ok {
				// The raw reasoning stream was buffered; surface it as one
				// thinking delta instead of the summary to avoid duplication.
				if text := strings.TrimSpace(buf.String()); text != "" {
					events = append(events, acp2.Event{Type: acp2.EventThinkingDelta, TurnID: turnID, Text: text})
				}
				delete(m.reasoningTextBuffers, item.ID)
			} else if _, streamed := m.streamedReasoningIDs[item.ID]; !streamed {
				if text := strings.TrimSpace(strings.Join(item.Summary, "\n")); text != "" {
					events = append(events, acp2.Event{Type: acp2.EventThinkingDelta, TurnID: turnID, Text: text})
				}
			}
			delete(m.streamedReasoningIDs, item.ID)
		}
	case "agentMessage":
		if (started || completed) && item.ID != "" && item.Phase != "" {
			m.agentMessagePhases[item.ID] = item.Phase
		}
		if completed && item.ID != "" {
			if _, streamed := m.streamedAgentMessageIDs[item.ID]; !streamed && item.Text != "" {
				if isUserVisibleCodexPhase(item.Phase) {
					events = append(events, acp2.Event{Type: acp2.EventTextDelta, TurnID: turnID, Text: item.Text})
				} else {
					events = append(events, acp2.Event{Type: acp2.EventRaw, TurnID: turnID, Raw: raw})
				}
			}
			delete(m.streamedAgentMessageIDs, item.ID)
			delete(m.agentMessagePhases, item.ID)
		}
	case "commandExecution":
		if started && item.Command != "" {
			input, _ := json.Marshal(map[string]any{"command": item.Command})
			events = append(events, toolCallEvent(turnID, "shell", item.ID, "started", item.Command, input))
		}
		if completed {
			events = append(events, toolCallEvent(turnID, "shell", item.ID, "completed", item.Command, raw))
		}
	case "fileChange":
		if started {
			count := 0
			for _, c := range item.Changes {
				input, _ := json.Marshal(map[string]any{"path": c.Path, "kind": c.Kind})
				events = append(events, toolCallEvent(turnID, "file_change", item.ID, "started", "", input))
				count++
			}
			if count > 0 && item.ID != "" {
				m.fileChangeCounts[item.ID] = count
			}
		}
		if completed {
			count := m.fileChangeCounts[item.ID]
			delete(m.fileChangeCounts, item.ID)
			if count == 0 {
				count = len(item.Changes)
			}
			for i := 0; i < count; i++ {
				events = append(events, toolCallEvent(turnID, "file_change", item.ID, "completed", "", raw))
			}
		}
	case "mcpToolCall":
		name := codexMcpToolName(item.Tool, item.Server)
		if started {
			events = append(events, toolCallEvent(turnID, name, item.ID, "started", "", item.Arguments))
		}
		if completed {
			events = append(events, toolCallEvent(turnID, name, item.ID, "completed", "", raw))
		}
	case "webSearch":
		if started {
			input, _ := json.Marshal(map[string]any{"query": item.Query})
			events = append(events, toolCallEvent(turnID, "web_search", item.ID, "started", "", input))
		}
		if completed {
			events = append(events, toolCallEvent(turnID, "web_search", item.ID, "completed", "", raw))
		}
	case "contextCompaction":
		if started {
			events = append(events, acp2.Event{Type: acp2.EventContextCompactionStarted, TurnID: turnID})
		}
		if completed {
			events = append(events, acp2.Event{Type: acp2.EventContextCompactionFinished, TurnID: turnID})
		}
	case "enteredReviewMode":
		if started {
			events = append(events, acp2.Event{Type: acp2.EventLifecycle, TurnID: turnID, Text: "review_started"})
		}
	case "exitedReviewMode":
		if completed {
			events = append(events, acp2.Event{Type: acp2.EventLifecycle, TurnID: turnID, Text: "review_finished"})
		}
	default:
		// Unknown item types carry no laelia event surface; ignore.
	}
	return events
}

// tokenUsage maps thread/tokenUsage/updated to a context usage event.
func (*CodexEventMapper) tokenUsage(params map[string]json.RawMessage, turnID string) []acp2.Event {
	raw, ok := params["tokenUsage"]
	if !ok {
		return nil
	}
	var usage struct {
		Total struct {
			InputTokens       int64 `json:"inputTokens"`
			CachedInputTokens int64 `json:"cachedInputTokens"`
			TotalTokens       int64 `json:"totalTokens"`
			OutputTokens      int64 `json:"outputTokens"`
		} `json:"total"`
		ModelContextWindow int64 `json:"modelContextWindow"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil
	}
	if usage.Total.TotalTokens == 0 && usage.Total.InputTokens == 0 && usage.Total.OutputTokens == 0 {
		return nil
	}
	return []acp2.Event{{
		Type:   acp2.EventContextUsageUpdate,
		TurnID: turnID,
		ContextUsage: &acp2.ContextUsageInfo{
			TotalTokens:        usage.Total.TotalTokens,
			InputTokens:        usage.Total.InputTokens,
			CachedInputTokens:  usage.Total.CachedInputTokens,
			OutputTokens:       usage.Total.OutputTokens,
			ModelContextWindow: usage.ModelContextWindow,
		},
	}}
}

// warning maps configWarning/warning/guardianWarning/deprecationNotice.
func (*CodexEventMapper) warning(method string, params map[string]json.RawMessage, turnID string) []acp2.Event {
	msg := ""
	switch method {
	case "configWarning", "deprecationNotice":
		msg = codexString(params, "summary")
		if msg == "" {
			msg = codexString(params, "details")
		}
	case "warning", "guardianWarning":
		msg = codexString(params, "message")
	default:
		msg = codexString(params, "message")
	}
	if msg == "" {
		msg = "Codex " + method
	}
	return []acp2.Event{{Type: acp2.EventWarning, TurnID: turnID, Text: msg}}
}

// threadStatusChanged maps thread/status/changed: systemError becomes an
// error event, waiting-on-approval/input flags become a warning.
func (*CodexEventMapper) threadStatusChanged(params map[string]json.RawMessage, turnID string) []acp2.Event {
	raw, ok := params["status"]
	if !ok {
		var thread struct {
			Status json.RawMessage `json:"status"`
		}
		if err := json.Unmarshal(params["thread"], &thread); err != nil || thread.Status == nil {
			return nil
		}
		raw = thread.Status
	}
	var status struct {
		Type        string   `json:"type"`
		Message     string   `json:"message"`
		ActiveFlags []string `json:"activeFlags"`
		Error       *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil
	}
	switch status.Type {
	case "systemError":
		msg := status.Message
		if msg == "" && status.Error != nil {
			msg = status.Error.Message
		}
		if msg == "" {
			msg = codexString(params, "message")
		}
		if msg == "" {
			msg = "Codex thread entered system error state"
		}
		return []acp2.Event{{Type: acp2.EventError, TurnID: turnID, Text: msg}}
	case "active":
		var wait []string
		for _, f := range status.ActiveFlags {
			if f == "waitingOnApproval" || f == "waitingOnUserInput" {
				wait = append(wait, f)
			}
		}
		if len(wait) == 0 {
			return nil
		}
		label := "user input"
		if containsString(wait, "waitingOnApproval") && containsString(wait, "waitingOnUserInput") {
			label = "approval and user input"
		} else if containsString(wait, "waitingOnApproval") {
			label = "approval"
		}
		return []acp2.Event{{Type: acp2.EventWarning, TurnID: turnID, Text: "Codex thread is waiting on " + label}}
	default:
		// Other status types carry no laelia event surface; ignore.
	}
	return nil
}

// errorEvent maps error notifications. Retryable errors are internal progress
// and degrade to raw.
func (*CodexEventMapper) errorEvent(params map[string]json.RawMessage, turnID string) []acp2.Event {
	if codexBool(params, "willRetry") {
		return []acp2.Event{{Type: acp2.EventRaw, TurnID: turnID, Raw: rawParams(params)}}
	}
	msg := codexString(params, "message")
	if msg == "" {
		if raw, ok := params["error"]; ok {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			msg = e.Message
		}
	}
	if msg == "" {
		msg = "Unknown Codex app-server error"
	}
	return []acp2.Event{{Type: acp2.EventError, TurnID: turnID, Text: msg}}
}

// toolCallEvent builds a tool call started/completed event. title is the
// display title (e.g. the executed command for shell calls); when empty the
// executor falls back to the kind.
func toolCallEvent(turnID, kind, id, status, title string, payload json.RawMessage) acp2.Event {
	ev := acp2.Event{
		Type:   acp2.EventToolCallStarted,
		TurnID: turnID,
		ToolCall: &acp2.ToolCallInfo{
			ID:     id,
			Kind:   kind,
			Title:  title,
			Status: status,
		},
	}
	if status == "completed" {
		ev.Type = acp2.EventToolCallFinished
		ev.ToolCall.Output = payload
	} else {
		ev.ToolCall.Input = payload
	}
	return ev
}

// codexMcpToolName builds the mcp_<server>_<tool> kind for an mcpToolCall.
func codexMcpToolName(tool, server string) string {
	if tool == "" {
		tool = "unknown"
	}
	if server != "" {
		return "mcp_" + server + "_" + tool
	}
	return "mcp_" + tool
}

// isUserVisibleCodexPhase reports whether an agent message phase is
// user-visible: no phase or the final_answer phase.
func isUserVisibleCodexPhase(phase string) bool {
	return phase == "" || phase == "final_answer"
}

// codexTurnID extracts the turn id (falling back to the thread id) from
// notification params, or "" when absent.
func codexTurnID(params map[string]json.RawMessage) string {
	if raw, ok := params["turn"]; ok {
		var turn struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &turn) == nil && turn.ID != "" {
			return turn.ID
		}
	}
	if id := codexString(params, "turnId"); id != "" {
		return id
	}
	if raw, ok := params["thread"]; ok {
		var thread struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &thread) == nil && thread.ID != "" {
			return thread.ID
		}
	}
	if id := codexString(params, "threadId"); id != "" {
		return id
	}
	return codexString(params, "sessionId")
}

// codexString reads a string field from notification params.
func codexString(params map[string]json.RawMessage, key string) string {
	raw, ok := params[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// codexBool reads a bool field from notification params.
func codexBool(params map[string]json.RawMessage, key string) bool {
	raw, ok := params[key]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// rawParams re-encodes notification params as raw JSON for Raw events.
func rawParams(params map[string]json.RawMessage) json.RawMessage {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return raw
}

// containsString reports whether s is in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
