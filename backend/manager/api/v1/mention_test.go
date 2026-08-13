package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTokenizeMentions guards the `@<handle>` extraction that
// parseContentMentions relies on: bare single-token handles, CJK handles,
// boundary handling so emails are not mistaken for mentions, and
// deduplication of the raw token stream. Only the bare form exists — display
// names and the legacy `@"name"` quoted form are not parsed.
func TestTokenizeMentions(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"bare single token", "please review @alice-user-1 when ready", []string{"alice-user-1"}},
		{"multiple bare tokens", "@alice-user-1 please ask @bob-user-2", []string{"alice-user-1", "bob-user-2"}},
		{"agent handle", "ask @rei-agent-1 to handle it", []string{"rei-agent-1"}},
		// CJK handles need a leading space so the `@` is at a token boundary;
		// otherwise "name@name" would be indistinguishable from an email.
		{"CJK bare handle with space", "问一下 @张三-user-1 这个问题", []string{"张三-user-1"}},
		{"dashed and dotted handle", "escalate to @backend-bot-agent-1 or @team.lead-user-1", []string{"backend-bot-agent-1", "team.lead-user-1"}},
		{"email is not a mention", "reach me at alice@example.com", []string{}},
		{"no leading boundary still a mention at start", "@alice-user-1 hi", []string{"alice-user-1"}},
		{"empty content", "", []string{}},
		{"no mentions", "just a normal message with no at-signs", []string{}},
		{"trailing @ with no name", "see you @", []string{}},
		{"duplicate tokens preserved in order", "@alice-user-1 @alice-user-1 @bob-user-2", []string{"alice-user-1", "alice-user-1", "bob-user-2"}},
		{"punctuation stops bare token", "hey @alice-user-1, can you?", []string{"alice-user-1"}},
		// A bare display name without a handle suffix is not a mention token
		// only if it fails the name-rune scan; "alice" alone is still a token
		// (resolution decides whether it names a member).
		{"bare name without suffix still tokenizes", "ping @alice", []string{"alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenizeMentions(tc.content)
			assert.Equal(t, len(tc.want), len(got), "token count")
			for i := 0; i < len(got) && i < len(tc.want); i++ {
				assert.Equal(t, tc.want[i], got[i])
			}
		})
	}
}

func TestMentionTypeString(t *testing.T) {
	assert.Equal(t, "agent", mentionTypeString(2))
	assert.Equal(t, "user", mentionTypeString(1))
	assert.Equal(t, "user", mentionTypeString(0))
}

func TestNormalizeMentionName(t *testing.T) {
	assert.Equal(t, "alice-user-1", normalizeMentionName("  Alice-User-1 "))
	assert.Equal(t, "rei-agent-1", normalizeMentionName("REI-AGENT-1"))
}
