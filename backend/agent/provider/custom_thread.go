package provider

import (
	"context"
	"encoding/json"

	"github.com/coder/acp-go-sdk"

	"github.com/Ranxy/laelia/backend/agent/acp2"
)

// CustomThreadProvider adapts a hand-configured executable/args pair to the
// ACP v2 thread protocol. It is the ThreadProvider for the "custom" provider
// when the admin declares protocol "acp-v2": the launch command comes from the
// raw config fields, MCP servers are not translated (the provider's CLI shape
// is unknown), and model discovery is unavailable. Notifications are mapped
// by a generic mapper that understands only the core thread frames
// (turn/started, turn/completed, error); everything else degrades to raw.
type CustomThreadProvider struct {
	executable string
	args       []string
}

// NewCustomThreadProvider builds a thread provider from a raw launch command.
func NewCustomThreadProvider(executable string, args []string) *CustomThreadProvider {
	return &CustomThreadProvider{executable: executable, args: args}
}

// ThreadCommand returns the configured executable + args unchanged.
func (p *CustomThreadProvider) ThreadCommand(_ string) (string, []string) {
	return p.executable, p.args
}

// NewThreadMapper returns the generic thread mapper (turn lifecycle only).
func (*CustomThreadProvider) NewThreadMapper() acp2.EventMapper { return NewGenericThreadMapper() }

// ThreadMcpArgs returns nil: the provider's MCP CLI shape is unknown, so
// managed MCP servers are not injected for a custom v2 agent.
func (*CustomThreadProvider) ThreadMcpArgs(_ []acp.McpServer) []string { return nil }

// ProbeModelsV2 returns no models: a custom provider's model surface is
// unknown.
func (*CustomThreadProvider) ProbeModelsV2(context.Context, string) ([]ModelOption, error) {
	return nil, nil
}

// GenericThreadMapper maps the core thread protocol frames every v2 server is
// expected to emit (turn/started, turn/completed, error) and degrades all
// other notification shapes to raw so nothing is silently dropped. It is the
// EventMapper for hand-configured custom providers whose wire shape is not
// built in; a custom agent with a richer notification surface needs a
// registered built-in provider with a dedicated mapper.
type GenericThreadMapper struct{}

// NewGenericThreadMapper returns an empty generic thread mapper.
func NewGenericThreadMapper() *GenericThreadMapper { return &GenericThreadMapper{} }

// Reset clears per-turn state (no-op for the generic mapper).
func (*GenericThreadMapper) Reset() {}

// MapNotification implements acp2.EventMapper.
func (*GenericThreadMapper) MapNotification(n acp2.Notification) []acp2.Event {
	var params map[string]json.RawMessage
	_ = json.Unmarshal(n.Params, &params)
	turnID := threadTurnID(params)
	switch n.Method {
	case "turn/started":
		return []acp2.Event{{Type: acp2.EventLifecycle, TurnID: turnID, Text: "turn_started"}}
	case "turn/completed":
		return genericTurnCompleted(params, turnID)
	case "error":
		return genericError(params, turnID)
	default:
		return []acp2.Event{{Type: acp2.EventRaw, TurnID: turnID, Raw: n.Params}}
	}
}

// genericTurnCompleted maps turn/completed: failed/interrupted turns surface
// an error, and every completion emits the lifecycle marker that drives the
// executor's turn gate.
func genericTurnCompleted(params map[string]json.RawMessage, turnID string) []acp2.Event {
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
		msg := "Turn failed"
		if turn.Error != nil && turn.Error.Message != "" {
			msg = turn.Error.Message
		}
		events = append(events, acp2.Event{Type: acp2.EventError, TurnID: turnID, Text: msg})
	case "interrupted":
		msg := "Turn interrupted"
		if turn.Error != nil && turn.Error.Message != "" {
			msg += ": " + turn.Error.Message
		}
		events = append(events, acp2.Event{Type: acp2.EventError, TurnID: turnID, Text: msg})
	default:
		// completed and other statuses carry no error event.
	}
	events = append(events, acp2.Event{Type: acp2.EventLifecycle, TurnID: turnID, Text: "turn_completed"})
	return events
}

// genericError maps error notifications (retryable errors degrade to raw).
func genericError(params map[string]json.RawMessage, turnID string) []acp2.Event {
	if genericBool(params, "willRetry") {
		return []acp2.Event{{Type: acp2.EventRaw, TurnID: turnID, Raw: rawParams(params)}}
	}
	msg := genericString(params, "message")
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
		msg = "Unknown agent error"
	}
	return []acp2.Event{{Type: acp2.EventError, TurnID: turnID, Text: msg}}
}

// threadTurnID extracts the turn id (falling back to the thread id) from
// notification params, or "" when absent.
func threadTurnID(params map[string]json.RawMessage) string {
	if raw, ok := params["turn"]; ok {
		var turn struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &turn) == nil && turn.ID != "" {
			return turn.ID
		}
	}
	if id := genericString(params, "turnId"); id != "" {
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
	return genericString(params, "threadId")
}

// genericString reads a string field from notification params.
func genericString(params map[string]json.RawMessage, key string) string {
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

// genericBool reads a bool field from notification params.
func genericBool(params map[string]json.RawMessage, key string) bool {
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
