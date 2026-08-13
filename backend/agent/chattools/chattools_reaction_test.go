package chattools

import (
	"testing"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestFormatReactionResultAdd(t *testing.T) {
	got := formatReactionResult(nil, "'#general:550e8400-e29b-41d4-a716-446655440000'", "👍", true)
	want := "Reaction 👍 added to '#general:550e8400-e29b-41d4-a716-446655440000'."
	if got != want {
		t.Fatalf("formatReactionResult(add) = %q, want %q", got, want)
	}
}

func TestFormatReactionResultRemove(t *testing.T) {
	got := formatReactionResult(nil, "'#general:550e8400-e29b-41d4-a716-446655440000'", "✅", false)
	want := "Reaction ✅ removed from '#general:550e8400-e29b-41d4-a716-446655440000'."
	if got != want {
		t.Fatalf("formatReactionResult(remove) = %q, want %q", got, want)
	}
}

// TestFormatReactionResultDMHandle verifies a dm: handle (no '#', no quotes)
// round-trips unchanged.
func TestFormatReactionResultDMHandle(t *testing.T) {
	got := formatReactionResult(nil, "dm:@rei-agent-1:550e8400-e29b-41d4-a716-446655440000", "👍", true)
	want := "Reaction 👍 added to dm:@rei-agent-1:550e8400-e29b-41d4-a716-446655440000."
	if got != want {
		t.Fatalf("formatReactionResult(dm) = %q, want %q", got, want)
	}
}

func TestFormatReactionsLineEmpty(t *testing.T) {
	if got := formatReactionsLine(nil); got != "" {
		t.Fatalf("formatReactionsLine(nil) = %q, want empty", got)
	}
	if got := formatReactionsLine([]*v1pb.Reaction{}); got != "" {
		t.Fatalf("formatReactionsLine(empty) = %q, want empty", got)
	}
}

func TestFormatReactionsLine(t *testing.T) {
	got := formatReactionsLine([]*v1pb.Reaction{
		{Emoji: "👍", Count: 2, Reactors: []string{"alice", "rei-agent-1"}},
		{Emoji: "✅", Count: 1, Reactors: []string{"bob"}},
	})
	want := "  reactions: 👍 ×2 (alice, rei-agent-1), ✅ (bob)\n"
	if got != want {
		t.Fatalf("formatReactionsLine = %q, want %q", got, want)
	}
}
