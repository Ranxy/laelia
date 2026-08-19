package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Ranxy/laelia/backend/manager/store"
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
		// A trailing '.' is sentence-ending punctuation, not part of the
		// handle. Without the trailing-dot strip the token would be
		// "para-agent-1." and the handle lookup would miss, silently dropping
		// the mention (the exact bug where agent messages with "@handle." at
		// the end of a sentence had no mentions field).
		{"trailing sentence period is stripped", "Waiting for my role from @para-agent-1.", []string{"para-agent-1"}},
		{"trailing period then space", "ping @para-agent-1. ok", []string{"para-agent-1"}},
		{"trailing double period stripped", "see @para-agent-1.. weird", []string{"para-agent-1"}},
		// An internal '.' (followed by a name rune) is preserved — it may be
		// part of a hypothetical handle like "team.lead-user-1".
		{"internal period preserved", "escalate to @team.lead-user-1 now", []string{"team.lead-user-1"}},
		{"internal period then trailing period", "ask @team.lead-user-1. please", []string{"team.lead-user-1"}},
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

// TestBuildDisplayNameIndexWithResolver guards the display-name fallback index
// that parseContentMentions uses when handle matching fails: unambiguous
// display names resolve, ambiguous ones (two members sharing a name) are
// excluded so a display name can never misroute, and empty names are skipped.
func TestBuildDisplayNameIndexWithResolver(t *testing.T) {
	members := []*store.ConversationMember{
		{MemberType: store.MemberTypeAgent, MemberID: "rei-agent-1"},
		{MemberType: store.MemberTypeUser, MemberID: "alice-user-1"},
		{MemberType: store.MemberTypeAgent, MemberID: "bot-agent-1"},
		{MemberType: store.MemberTypeUser, MemberID: "empty-user-1"},
	}
	resolve := func(_ int32, memberID string) string {
		switch memberID {
		case "rei-agent-1":
			return "Rei"
		case "alice-user-1":
			return "Alice"
		case "bot-agent-1":
			return "Rei" // same display name as rei → ambiguous
		case "empty-user-1":
			return "" // empty display name → skipped
		}
		return ""
	}
	idx := buildDisplayNameIndexWithResolver(members, resolve)

	// "rei" is ambiguous (Rei and bot both have display name "Rei") → excluded.
	_, ok := idx["rei"]
	assert.False(t, ok, "ambiguous display name must be excluded")

	// "alice" is unique → resolves to the alice member.
	m, ok := idx["alice"]
	assert.True(t, ok, "unique display name must resolve")
	assert.Equal(t, "alice-user-1", m.MemberID)

	// Empty display name → not in the index.
	_, ok = idx[""]
	assert.False(t, ok, "empty display name must be skipped")
}

// TestResolveMentionTokenFallback verifies the two-pass resolution order used
// by parseContentMentions: a token that matches a handle resolves by handle,
// and a token that does NOT match any handle but matches a display name
// resolves by display name (the case where an agent wrote @<display name>
// instead of @<handle>).
func TestResolveMentionTokenFallback(t *testing.T) {
	// Members: agent "rei-agent-1" (display name "Rei"), user "alice-user-1"
	// (display name "Alice").
	members := []*store.ConversationMember{
		{MemberType: store.MemberTypeAgent, MemberID: "rei-agent-1"},
		{MemberType: store.MemberTypeUser, MemberID: "alice-user-1"},
	}
	resolve := func(_ int32, memberID string) string {
		switch memberID {
		case "rei-agent-1":
			return "Rei"
		case "alice-user-1":
			return "Alice"
		}
		return ""
	}

	byHandle := make(map[string]*store.ConversationMember, len(members))
	for _, m := range members {
		byHandle[normalizeMentionName(m.MemberID)] = m
	}
	byDisplayName := buildDisplayNameIndexWithResolver(members, resolve)

	// Helper mirroring parseContentMentions' resolution order.
	resolveToken := func(token string) (*store.ConversationMember, bool) {
		key := normalizeMentionName(token)
		if m, ok := byHandle[key]; ok {
			return m, true
		}
		m, ok := byDisplayName[key]
		return m, ok
	}

	// Handle match: "rei-agent-1" is a known handle.
	m, ok := resolveToken("rei-agent-1")
	assert.True(t, ok)
	assert.Equal(t, store.MemberTypeAgent, m.MemberType)

	// Display-name fallback: "Rei" is NOT a handle, but it IS a display name.
	m, ok = resolveToken("Rei")
	assert.True(t, ok, "display name must resolve when handle match fails")
	assert.Equal(t, "rei-agent-1", m.MemberID)

	// Unknown token: neither handle nor display name.
	_, ok = resolveToken("nobody")
	assert.False(t, ok)

	// Case-insensitive: "ALICE" matches display name "Alice".
	m, ok = resolveToken("ALICE")
	assert.True(t, ok)
	assert.Equal(t, "alice-user-1", m.MemberID)
}

// TestBuildGlobalMentionIndex guards the global agent/user fallback directory
// used when a @mention does not match any member of the current conversation.
// It must index every active agent/user by canonical handle, and by display
// name only when that name is unambiguous across the whole directory.
func TestBuildGlobalMentionIndex(t *testing.T) {
	agents := []*store.AgentMessage{
		{ResourceID: "jane-agent-1", Name: "jane"},
		{ResourceID: "rei-agent-1", Name: "rei"},
		{ResourceID: "amao-agent-1", Name: "amao"},
		{ResourceID: "dup-agent-1", Name: "jane"}, // same display name as jane-agent-1
	}
	users := []*store.UserMessage{
		{Handle: "ran-user-1", Name: "Ran"},
		{Handle: "alice-user-1", Name: "Alice"},
	}

	idx := store.BuildGlobalMentionIndex(agents, users)

	// Every handle is present with the correct type and id.
	for handle, wantType := range map[string]string{
		"jane-agent-1": "agent",
		"rei-agent-1":  "agent",
		"amao-agent-1": "agent",
		"dup-agent-1":  "agent",
		"ran-user-1":   "user",
		"alice-user-1": "user",
	} {
		m, ok := idx.Get(handle)
		assert.True(t, ok, "handle %q must be in global index", handle)
		assert.Equal(t, wantType, m.Type)
		assert.Equal(t, handle, m.Id)
	}

	// The global index carries display names so the UI can show @name instead
	// of @handle for non-member mentions.
	if m, ok := idx.Get("jane-agent-1"); ok {
		assert.Equal(t, "jane", m.Name)
	}
	if m, ok := idx.Get("ran-user-1"); ok {
		assert.Equal(t, "Ran", m.Name)
	}

	// Unique display names resolve; ambiguous ones are excluded.
	m, ok := idx.Get("rei")
	assert.True(t, ok, "unique display name rei must resolve")
	assert.Equal(t, "rei-agent-1", m.Id)

	m, ok = idx.Get("ran")
	assert.True(t, ok, "unique display name ran must resolve")
	assert.Equal(t, "ran-user-1", m.Id)

	_, ok = idx.Get("jane")
	assert.False(t, ok, "ambiguous display name jane must be excluded")
}

func TestBuildMentionsWithDisplayNames(t *testing.T) {
	// Unique display names are shown as-is.
	got := buildMentionsWithDisplayNames([]mentionCandidate{
		{Type: "agent", ID: "jet-agent-1", DisplayName: "jet"},
		{Type: "user", ID: "ran-user-1", DisplayName: "Ran"},
	})
	assert.Len(t, got, 2)
	assert.Equal(t, "jet", got[0].Name)
	assert.Equal(t, "Ran", got[1].Name)

	// Two different ids sharing a display name fall back to handles.
	ambiguous := buildMentionsWithDisplayNames([]mentionCandidate{
		{Type: "agent", ID: "jane-agent-1", DisplayName: "jane"},
		{Type: "agent", ID: "jane-agent-2", DisplayName: "jane"},
	})
	assert.Len(t, ambiguous, 2)
	assert.Equal(t, "jane-agent-1", ambiguous[0].Name)
	assert.Equal(t, "jane-agent-2", ambiguous[1].Name)

	// Empty display name falls back to the handle.
	empty := buildMentionsWithDisplayNames([]mentionCandidate{
		{Type: "agent", ID: "x-agent-1", DisplayName: ""},
	})
	assert.Len(t, empty, 1)
	assert.Equal(t, "x-agent-1", empty[0].Name)
}
