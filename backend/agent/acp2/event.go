package acp2

import "encoding/json"

// EventType classifies a neutral protocol event produced by an EventMapper.
type EventType string

// Event types. The set mirrors the laelia command event surface; the executor
// narrows each event onto its v1pb counterpart.
const (
	EventLifecycle                 EventType = "lifecycle"
	EventTextDelta                 EventType = "text_delta"
	EventToolCallStarted           EventType = "tool_call_started"
	EventToolCallFinished          EventType = "tool_call_finished"
	EventWarning                   EventType = "warning"
	EventContextCompactionStarted  EventType = "context_compaction_started"
	EventContextCompactionFinished EventType = "context_compaction_finished"
	EventContextUsageUpdate        EventType = "context_usage_update"
	EventError                     EventType = "error"
	EventRaw                       EventType = "raw"
)

// Event is a provider-neutral event derived from a protocol notification.
// Unknown notification shapes degrade to EventRaw so nothing is silently
// dropped.
type Event struct {
	Type         EventType
	Text         string
	Summary      string
	ToolCall     *ToolCallInfo
	ContextUsage *ContextUsageInfo
	Raw          json.RawMessage
}

// ToolCallInfo carries tool call activity observed in item events.
type ToolCallInfo struct {
	ID     string
	Kind   string // e.g. shell, file_change, mcp_<server>_<tool>, web_search
	Title  string
	Status string // started or completed
	Input  json.RawMessage
	Output json.RawMessage
}

// ContextUsageInfo carries cumulative token usage from token usage updates.
type ContextUsageInfo struct {
	TotalTokens        int64
	InputTokens        int64
	CachedInputTokens  int64
	OutputTokens       int64
	ModelContextWindow int64
}

// Notification is one protocol notification received from the agent server.
type Notification struct {
	Method string
	Params json.RawMessage
}

// EventMapper translates provider-specific notifications into neutral
// events. Providers implement it per wire shape; the client invokes it for
// every inbound notification.
type EventMapper interface {
	MapNotification(n Notification) []Event
}
