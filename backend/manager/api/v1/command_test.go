package v1

import (
	"strings"
	"testing"

	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestBuildLightChatContext(t *testing.T) {
	entries := []*store.ChatMessage{
		{Role: 1, Content: "Hello"},
		{Role: 2, Content: "Hi there!"},
		{Role: 1, Content: "What's the weather?"},
		{Role: 2, Content: "It's sunny."},
	}

	result := buildLightChatContext(entries)

	if !strings.Contains(result, "## Recent conversation") {
		t.Error("expected context to contain header")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("expected context to contain older user message")
	}
	if !strings.Contains(result, "Hi there!") {
		t.Error("expected context to contain older assistant message")
	}
	if !strings.Contains(result, "What's the weather?") {
		t.Error("expected context to contain newer user message")
	}
	if !strings.Contains(result, "It's sunny.") {
		t.Error("expected context to contain newer assistant message")
	}
}

func TestBuildLightChatContextEmpty(t *testing.T) {
	result := buildLightChatContext(nil)
	if !strings.Contains(result, "## Recent conversation") {
		t.Error("expected header even for empty entries")
	}
}

func TestBuildLightChatContextLimit(t *testing.T) {
	var entries []*store.ChatMessage
	for i := 0; i < 20; i++ {
		entries = append(entries, &store.ChatMessage{Role: 1, Content: "msg"})
	}
	result := buildLightChatContext(entries)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > 7 { // header + max 6 messages
		t.Errorf("expected at most 7 lines, got %d", len(lines))
	}
}
