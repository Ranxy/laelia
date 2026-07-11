package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTokenizeMentions guards the `@<name>` / `@"name"` extraction that
// parseContentMentions relies on: bare single-token names, quoted multi-word
// names, CJK names, boundary handling so emails are not mistaken for mentions,
// and deduplication of the raw token stream.
func TestTokenizeMentions(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"bare single token", "please review @alice when ready", []string{"alice"}},
		{"multiple bare tokens", "@alice please ask @bob", []string{"alice", "bob"}},
		{"quoted multi-word", `ping @"UI UX" team`, []string{"UI UX"}},
		{"quoted is trimmed", `assign @" backend engineer "`, []string{"backend engineer"}},
		// CJK mentions need a leading space (or quotes) so the `@` is at a token
		// boundary; otherwise "name@name" would be indistinguishable from an email.
		{"CJK bare name with space", "问一下 @张三 这个问题", []string{"张三"}},
		{"CJK quoted name", `转给@"张三"处理`, []string{"张三"}},
		{"dashed and dotted name", "escalate to @backend-bot or @team.lead", []string{"backend-bot", "team.lead"}},
		{"email is not a mention", "reach me at alice@example.com", []string{}},
		{"no leading boundary still a mention at start", "@alice hi", []string{"alice"}},
		{"empty content", "", []string{}},
		{"no mentions", "just a normal message with no at-signs", []string{}},
		{"trailing @ with no name", "see you @", []string{}},
		{"duplicate tokens preserved in order", "@alice @alice @bob", []string{"alice", "alice", "bob"}},
		{"punctuation stops bare token", "hey @alice, can you?", []string{"alice"}},
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
	assert.Equal(t, "alice", normalizeMentionName("  Alice "))
	assert.Equal(t, "ui ux", normalizeMentionName("UI UX"))
}
