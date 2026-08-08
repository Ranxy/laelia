// Package pi drives the built-in non-ACP pi coding agent (github.com/earendil-works/pi)
// over its RPC mode: a long-lived `pi --mode rpc` subprocess speaking JSONL over
// stdin/stdout. This is the non-ACP parallel to backend/agent/executor's ACP path;
// the runner picks this runtime when an agent's provider is "builtin-pi".
//
// The protocol is LF-delimited JSONL: one JSON object per line on each direction.
// Commands (stdin) carry an optional `id` for request/response correlation; events
// (stdout) stream asynchronously and carry no `id` (except bash_execution_update).
// See vendor docs at docs/rpc.md in the pi distribution (pinned to v0.82.1).
package pi

import "encoding/json"

// BuiltinPiProvider is the AgentACPConfig.provider value that selects this
// runtime. It is a known provider id (accepted by the manager validation) but is
// NOT a host-detected provider.Provider — pi is bundled, not installed on PATH.
const BuiltinPiProvider = "builtin-pi"

// command envelopes written to the pi subprocess stdin. Each is one JSON line.

type promptCommand struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

type steerCommand struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

type abortCommand struct {
	Type string `json:"type"`
}

type getStateCommand struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type getSessionStatsCommand struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type switchSessionCommand struct {
	Type        string `json:"type"`
	ID          string `json:"id,omitempty"`
	SessionPath string `json:"sessionPath"`
}

// response is the envelope pi writes back for a command that carried an id.
type response struct {
	Type    string          `json:"type"` // always "response"
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// getStateData is the `data` payload of a get_state response. We only need the
// session file path + id to persist for resume across machine restarts.
type getStateData struct {
	SessionFile string `json:"sessionFile"`
	SessionID   string `json:"sessionId"`
}

// sessionStatsData is the `data` payload of a get_session_stats response. It
// carries cumulative token/cost statistics plus the current context-window
// estimate pi uses for compaction and footer display.
type sessionStatsData struct {
	SessionFile  string               `json:"sessionFile,omitempty"`
	SessionID    string               `json:"sessionId,omitempty"`
	ContextUsage *sessionContextUsage `json:"contextUsage,omitempty"`
}

// sessionContextUsage is the current context-window estimate. tokens/percent
// are null immediately after a compaction until a fresh assistant response
// provides valid usage; the whole object is omitted when no model/context
// window is available.
type sessionContextUsage struct {
	Tokens        *int64   `json:"tokens"`
	ContextWindow *int64   `json:"contextWindow"`
	Percent       *float64 `json:"percent"`
}

// event is the raw envelope for a streamed stdout line. Type discriminates the
// rest; callers re-decode the relevant fields via typed accessors below.
type event struct {
	Type string `json:"type"`

	// message_update
	AssistantMessageEvent *assistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// tool_execution_*
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// agent_end
	Messages  json.RawMessage `json:"messages,omitempty"`
	WillRetry bool            `json:"willRetry,omitempty"`

	// compaction_*, auto_retry_* carry diagnostics we surface as warnings.
	Reason       string `json:"reason,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// bash_execution_update (not used by the drain loop, ignored).
	Raw json.RawMessage `json:"-"`
}

// assistantMessageEvent is the `assistantMessageEvent` of a message_update. The
// delta types that matter for the chat stream: text_delta, thinking_delta.
type assistantMessageEvent struct {
	Type         string `json:"type"`
	ContentIndex int    `json:"contentIndex"`
	Delta        string `json:"delta,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

const (
	assistantEventTextDelta     = "text_delta"
	assistantEventThinkingDelta = "thinking_delta"
	assistantEventDone          = "done"
	assistantEventError         = "error"
)

// event type strings the executor switches on.
const (
	eventAgentStart         = "agent_start"
	eventAgentEnd           = "agent_end"
	eventAgentSettled       = "agent_settled"
	eventMessageUpdate      = "message_update"
	eventToolExecutionStart = "tool_execution_start"
	eventToolExecutionEnd   = "tool_execution_end"
	eventCompactionStart    = "compaction_start"
	eventCompactionEnd      = "compaction_end"
	eventAutoRetryStart     = "auto_retry_start"
	eventAutoRetryEnd       = "auto_retry_end"
	eventExtensionError     = "extension_error"
)
