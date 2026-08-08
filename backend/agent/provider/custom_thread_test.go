package provider

import (
	"encoding/json"
	"testing"

	"github.com/Ranxy/laelia/backend/agent/acp2"
)

func TestGenericThreadMapperTurnLifecycle(t *testing.T) {
	m := NewGenericThreadMapper()

	evs := m.MapNotification(acp2.Notification{Method: "turn/started", Params: json.RawMessage(`{"turn":{"id":"t1"},"threadId":"th1"}`)})
	if len(evs) != 1 || evs[0].Type != acp2.EventLifecycle || evs[0].Text != "turn_started" || evs[0].TurnID != "t1" {
		t.Fatalf("turn/started: got %+v", evs)
	}

	evs = m.MapNotification(acp2.Notification{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"id":"t1","status":"completed"}}`)})
	if len(evs) != 1 || evs[0].Type != acp2.EventLifecycle || evs[0].Text != "turn_completed" {
		t.Fatalf("turn/completed: got %+v", evs)
	}
}

func TestGenericThreadMapperTurnFailed(t *testing.T) {
	m := NewGenericThreadMapper()
	evs := m.MapNotification(acp2.Notification{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"id":"t1","status":"failed","error":{"message":"boom"}}}`)})
	if len(evs) != 2 {
		t.Fatalf("expected error + lifecycle, got %+v", evs)
	}
	if evs[0].Type != acp2.EventError || evs[0].Text != "boom" {
		t.Fatalf("error event: got %+v", evs[0])
	}
	if evs[1].Type != acp2.EventLifecycle || evs[1].Text != "turn_completed" {
		t.Fatalf("lifecycle event: got %+v", evs[1])
	}
}

func TestGenericThreadMapperError(t *testing.T) {
	m := NewGenericThreadMapper()
	evs := m.MapNotification(acp2.Notification{Method: "error", Params: json.RawMessage(`{"message":"kaput"}`)})
	if len(evs) != 1 || evs[0].Type != acp2.EventError || evs[0].Text != "kaput" {
		t.Fatalf("error: got %+v", evs)
	}
	// retryable errors degrade to raw
	evs = m.MapNotification(acp2.Notification{Method: "error", Params: json.RawMessage(`{"message":"later","willRetry":true}`)})
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("retryable error: got %+v", evs)
	}
}

func TestGenericThreadMapperUnknownDegradesToRaw(t *testing.T) {
	m := NewGenericThreadMapper()
	params := json.RawMessage(`{"delta":"hi"}`)
	evs := m.MapNotification(acp2.Notification{Method: "item/agentMessage/delta", Params: params})
	if len(evs) != 1 || evs[0].Type != acp2.EventRaw {
		t.Fatalf("unknown method: got %+v", evs)
	}
}

func TestCustomThreadProviderCommand(t *testing.T) {
	p := NewCustomThreadProvider("my-agent", []string{"serve"})
	exe, args := p.ThreadCommand("/work")
	if exe != "my-agent" || len(args) != 1 || args[0] != "serve" {
		t.Fatalf("ThreadCommand: got (%q, %v)", exe, args)
	}
	if p.ThreadMcpArgs(nil) != nil {
		t.Fatal("custom provider must not inject MCP args")
	}
	models, err := p.ProbeModelsV2(t.Context(), "/work")
	if err != nil || models != nil {
		t.Fatalf("ProbeModelsV2: got (%v, %v)", models, err)
	}
}
