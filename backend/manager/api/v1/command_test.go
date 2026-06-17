package v1

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestTruncateText(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"hello world", 11, "hello world"},
		{"hello world", 0, "..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := truncateText(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateText(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestBuildChatContext(t *testing.T) {
	entries := []*store.ChatHistoryEntry{
		{Instruction: "What's the weather?", FinalSummary: "It's sunny."},
		{Instruction: "Hello", FinalSummary: "Hi there!"},
	}

	result := buildChatContext(entries)

	if !strings.Contains(result, "Conversation history:") {
		t.Error("expected context to contain header")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("expected context to contain older instruction")
	}
	if !strings.Contains(result, "Hi there!") {
		t.Error("expected context to contain older summary")
	}
	if !strings.Contains(result, "What's the weather?") {
		t.Error("expected context to contain newer instruction")
	}
	if !strings.Contains(result, "It's sunny.") {
		t.Error("expected context to contain newer summary")
	}

	firstPos := strings.Index(result, entries[1].Instruction)
	secondPos := strings.Index(result, entries[0].Instruction)
	if firstPos == -1 || secondPos == -1 {
		t.Fatal("entries not found in context")
	}
	if firstPos > secondPos {
		t.Error("expected entries in chronological order (oldest first)")
	}
}

func TestBuildChatContextEmpty(t *testing.T) {
	result := buildChatContext(nil)
	if !strings.Contains(result, "Conversation history:") {
		t.Error("expected header even for empty entries")
	}
	if strings.Contains(result, "- User:") {
		t.Error("expected no user entries for empty input")
	}
}

func TestBuildChatContextMissingSummary(t *testing.T) {
	entries := []*store.ChatHistoryEntry{
		{Instruction: "Hello", FinalSummary: ""},
	}
	result := buildChatContext(entries)
	if !strings.Contains(result, "Hello") {
		t.Error("expected instruction in context")
	}
	if strings.Contains(result, "- Assistant:") {
		t.Error("expected no assistant entry when final_summary is empty")
	}
}

func TestBuildChatContextTruncation(t *testing.T) {
	long := strings.Repeat("a", 3000)
	entries := []*store.ChatHistoryEntry{
		{Instruction: long, FinalSummary: long},
	}
	result := buildChatContext(entries)
	if !strings.Contains(result, "...") {
		t.Error("expected truncation for long text")
	}
	if strings.Contains(result, long) {
		t.Error("expected long text to be truncated")
	}
}

func TestFormatCommandName(t *testing.T) {
	got := formatCommandName("my-agent", uuid.Nil)
	want := "agents/my-agent/commands/00000000-0000-0000-0000-000000000000"
	if got != want {
		t.Errorf("formatCommandName() = %q, want %q", got, want)
	}
}

func TestFormatAgentName(t *testing.T) {
	got := formatAgentName("my-agent")
	want := "agents/my-agent"
	if got != want {
		t.Errorf("formatAgentName() = %q, want %q", got, want)
	}
}

func TestParseAgentResourceID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"agents/foo", "foo"},
		{"agents/bar/commands/123", "bar"},
		{"agents/abc/commands/def", "abc"},
		{"just-a-name", "just-a-name"},
	}
	for _, tt := range tests {
		got := parseAgentResourceID(tt.input)
		if got != tt.want {
			t.Errorf("parseAgentResourceID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
