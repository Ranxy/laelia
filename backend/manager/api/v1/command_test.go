package v1

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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

// TestConvertToV1CommandEvent_ContextPayloads guards the persisted-event
// round trip for the context event types: without these unmarshal cases the
// frontend receives events whose payload is unset, and the context usage bar /
// compaction rows render nothing.
func TestConvertToV1CommandEvent_ContextPayloads(t *testing.T) {
	cmdID := uuid.New()
	cases := []struct {
		name    string
		event   v1pb.CommandEventType
		payload proto.Message
		check   func(*testing.T, *v1pb.CommandEvent)
	}{
		{
			name:    "compaction",
			event:   v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
			payload: &v1pb.ContextCompactionPayload{Reason: "window full", Inferred: true},
			check: func(t *testing.T, ev *v1pb.CommandEvent) {
				require.NotNil(t, ev.GetContextCompaction())
				assert.Equal(t, "window full", ev.GetContextCompaction().GetReason())
				assert.True(t, ev.GetContextCompaction().GetInferred())
			},
		},
		{
			name:    "usage",
			event:   v1pb.CommandEventType_CONTEXT_USAGE_UPDATE,
			payload: &v1pb.ContextUsagePayload{Size: 200000, Used: 180000, UsageRatio: 0.9},
			check: func(t *testing.T, ev *v1pb.CommandEvent) {
				require.NotNil(t, ev.GetContextUsage())
				assert.Equal(t, int64(200000), ev.GetContextUsage().GetSize())
				assert.Equal(t, int64(180000), ev.GetContextUsage().GetUsed())
				assert.InDelta(t, 0.9, ev.GetContextUsage().GetUsageRatio(), 1e-9)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := protojson.Marshal(tc.payload)
			require.NoError(t, err)
			ev := convertToV1CommandEvent(&store.CommandEventMessage{
				CommandID:   cmdID,
				SeqNo:       1,
				EventType:   int32(tc.event),
				Summary:     "summary",
				PayloadJSON: string(data),
			})
			tc.check(t, ev)
		})
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

func TestSearchSnippet(t *testing.T) {
	long := strings.Repeat("a", 300)
	if got := searchSnippet(long, "b"); got != strings.Repeat("a", 200)+"…" {
		t.Errorf("expected leading excerpt for missing query, got %q", got)
	}
	content := strings.Repeat("前", 100) + "needle" + strings.Repeat("后", 100)
	got := searchSnippet(content, "needle")
	if !strings.Contains(got, "needle") {
		t.Errorf("expected snippet to contain the match, got %q", got)
	}
	if got == content {
		t.Error("expected a truncated snippet, got the full content")
	}

	multi := strings.Repeat("a", 100) + "rust" + strings.Repeat("b", 100) + "jdk"
	got = searchSnippet(multi, "jdk rust")
	if !strings.Contains(got, "rust") {
		t.Errorf("expected snippet to center on the earliest token match, got %q", got)
	}
}
