package chattools

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestNormalizeConversationName(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		"abc-123":           "conversations/abc-123",
		"conversations/x-1": "conversations/x-1",
	} {
		assert.Equal(t, want, normalizeConversationName(in), "input %q", in)
	}
}

func TestNormalizeThreadRoot(t *testing.T) {
	for in, want := range map[string]string{
		"":                                   "",
		"abc-123":                            "abc-123",
		"conversations/c-1/messages/m-2":     "m-2",
		"conversations/abc-123/messages/456": "456",
	} {
		assert.Equal(t, want, normalizeThreadRoot(in), "input %q", in)
	}
}

func TestWrapManagerErrorCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		code connect.Code
		want string
	}{
		{"not found", connect.CodeNotFound, "NOT_FOUND_FAILED"},
		{"permission denied", connect.CodePermissionDenied, "PERMISSION_FAILED"},
		{"invalid argument", connect.CodeInvalidArgument, "INVALID_ARGUMENT_FAILED"},
		{"unauthenticated", connect.CodeUnauthenticated, "AUTH_FAILED"},
		{"internal", connect.CodeInternal, "SERVER_5XX"},
		{"unavailable", connect.CodeUnavailable, "SERVER_5XX"},
		{"deadline exceeded", connect.CodeDeadlineExceeded, "SERVER_5XX"},
		{"already exists (default)", connect.CodeAlreadyExists, "REQUEST_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := connect.NewError(tc.code, errors.New("boom"))
			got := wrapManagerError(err)
			assert.NotNil(t, got)
			assert.Equal(t, tc.want, got.Code)
			assert.Contains(t, got.Message, "boom")
		})
	}
}

func TestWrapManagerErrorNil(t *testing.T) {
	assert.Nil(t, wrapManagerError(nil))
}

func TestLocalError(t *testing.T) {
	e := localError("MISSING_COMMAND", "no command", "pass --command-id")
	assert.Equal(t, "MISSING_COMMAND", e.Code)
	assert.Equal(t, "no command", e.Message)
	assert.Equal(t, "pass --command-id", e.NextAction)
	assert.Equal(t, "no command", e.Error())
}

func TestGetConversationMessagesRequiresConversation(t *testing.T) {
	// No client call is made when the conversation is missing; this is a local
	// bootstrap error surfaced as MISSING_CONVERSATION.
	_, err := GetConversationMessages(context.Background(), Deps{}, GetConversationMessagesInput{})
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

func TestGetConversationMessagesBadDirection(t *testing.T) {
	_, err := GetConversationMessages(context.Background(), Deps{}, GetConversationMessagesInput{
		Conversation: "conversations/c",
		Direction:    "sideways",
	})
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestAckProcessedVersionRequiresPositive(t *testing.T) {
	_, err := AckProcessedVersion(context.Background(), Deps{}, AckProcessedVersionInput{
		Conversation:     "conversations/c",
		ProcessedVersion: 0,
	})
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)
}

func TestGetCommandContextFallsBackToDepsCommand(t *testing.T) {
	// When CommandID is empty the session command id is used; with neither set
	// it is a MISSING_COMMAND local error (no manager call).
	_, err := GetCommandContext(context.Background(), Deps{}, GetCommandContextInput{})
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "MISSING_COMMAND", e.Code)
}

// TestFormatMessageLineAttachments locks the rendering that lets the agent tie
// a message like "test file" to the file it must `file download <id>`. The
// attachment id/name/size/mime must appear inline so the LLM can act on it
// without a second round-trip.
func TestFormatMessageLineAttachments(t *testing.T) {
	// No attachments and no message id: the line is unchanged.
	assert.Equal(t, "[2026-06-26T07:31:16Z] admin (USER): test file\n",
		formatMessageLine("2026-06-26T07:31:16Z", "admin", "USER", false, "", "", 0, "test file", nil))

	// Own message keeps the (YOU) tag.
	assert.Equal(t, "[2026-06-26T07:31:16Z] admin (USER, YOU): hi\n",
		formatMessageLine("2026-06-26T07:31:16Z", "admin", "USER", true, "", "", 0, "hi", nil))

	// With a message id: the full resource name and room version appear on an
	// indented line so the agent can pass it straight to `reminder convert` /
	// `task claim` without reconstructing it from the conversation id.
	got := formatMessageLine("2026-06-26T07:31:16Z", "admin", "USER", false,
		"0d8856c0-ed2d-476b-9a86-33c0c333f5b9", "11111111-2222-3333-4444-555555555555", 58,
		"每天3点分析github提交", nil)
	want := "[2026-06-26T07:31:16Z] admin (USER): 每天3点分析github提交\n" +
		"  message: conversations/0d8856c0-ed2d-476b-9a86-33c0c333f5b9/messages/11111111-2222-3333-4444-555555555555  version: 58\n"
	assert.Equal(t, want, got)

	// With attachments: the id appears so `file download <id>` is callable, in
	// the same id/name/size/mime shape `file list` uses. The message handle line
	// comes before the attachments block.
	got = formatMessageLine("2026-06-26T07:31:16Z", "admin", "USER", false,
		"0d8856c0-ed2d-476b-9a86-33c0c333f5b9", "11111111-2222-3333-4444-555555555555", 58, "test file",
		[]*v1pb.Attachment{{Id: "f-1", Name: "report.pdf", MimeType: "application/pdf", SizeBytes: 123456}})
	want = "[2026-06-26T07:31:16Z] admin (USER): test file\n" +
		"  message: conversations/0d8856c0-ed2d-476b-9a86-33c0c333f5b9/messages/11111111-2222-3333-4444-555555555555  version: 58\n" +
		"  attachments:\n" +
		"    - id=f-1  name=report.pdf  size=123456  mime=application/pdf\n"
	assert.Equal(t, want, got)

	// An anchored-comment attachment surfaces its section anchor and quoted
	// selection so the agent knows which span of the file the user is reacting
	// to, instead of seeing only a re-attached file.
	got = formatMessageLine("2026-06-26T07:31:16Z", "admin", "USER", false, "", "", 0, "为什么会这样?",
		[]*v1pb.Attachment{{
			Id:            "fa764496",
			Name:          "crystal_design_assessment.md",
			MimeType:      "text/plain; charset=utf-8",
			SizeBytes:     10289,
			SectionAnchor: "§ 2.1 Concurrency (worker pool)",
			QuotedText:    "the worker pool spawns unbounded goroutines",
		}})
	want = "[2026-06-26T07:31:16Z] admin (USER): 为什么会这样?\n" +
		"  attachments:\n" +
		"    - id=fa764496  name=crystal_design_assessment.md  size=10289  mime=text/plain; charset=utf-8\n" +
		"      commented on § 2.1 Concurrency (worker pool)\n" +
		"        > the worker pool spawns unbounded goroutines\n"
	assert.Equal(t, want, got)
}

func TestMemberTypeString(t *testing.T) {
	assert.Equal(t, "user", memberTypeString(1))
	assert.Equal(t, "agent", memberTypeString(2))
	assert.Equal(t, "unknown", memberTypeString(0))
	assert.Equal(t, "unknown", memberTypeString(7))
}

func TestMemberRoleString(t *testing.T) {
	assert.Equal(t, "owner", memberRoleString(1))
	assert.Equal(t, "member", memberRoleString(2))
	assert.Equal(t, "", memberRoleString(0)) // thread participants: role not meaningful
}

// TestFormatMemberLine locks the roster rendering the agent reads to decide whom
// to @mention: type, display name, agents/<id> handle for agents, role when
// meaningful, and the member's full description as an indented block — for users
// their self-description, for agents their complete persona_prompt (untruncated,
// so one roster call carries every co-agent's persona).
func TestFormatMemberLine(t *testing.T) {
	// User owner with a single-line description: header line + indented block.
	assert.Equal(t, "- [user] Alice (owner)\n  后端工程师, 专注 agent 构建\n",
		formatMemberLine(&v1pb.ChannelMember{
			MemberType: 1, DisplayName: "Alice", MemberRole: 1, Description: "后端工程师, 专注 agent 构建",
		}))

	// Agent member: the agents/<id> handle appears; a multi-line persona is
	// emitted in full, one indented line per source line — no truncation.
	got := formatMemberLine(&v1pb.ChannelMember{
		MemberType: 2, MemberId: "abc-123", DisplayName: "backend-bot", MemberRole: 2,
		Description: "精通后端, 专注构建 agent。\n前端任务请转给 @ui-expert。",
	})
	want := "- [agent] backend-bot [agents/abc-123] (member)\n" +
		"  精通后端, 专注构建 agent。\n" +
		"  前端任务请转给 @ui-expert。\n"
	assert.Equal(t, want, got)

	// Thread participant: role 0 → no role parenthetical.
	assert.Equal(t, "- [user] Bob\n",
		formatMemberLine(&v1pb.ChannelMember{MemberType: 1, DisplayName: "Bob", MemberRole: 0}))

	// No description → no indented block.
	assert.Equal(t, "- [agent] dev [agents/9] (member)\n",
		formatMemberLine(&v1pb.ChannelMember{MemberType: 2, MemberId: "9", DisplayName: "dev", MemberRole: 2}))

	// nil is safe.
	assert.Equal(t, "", formatMemberLine(nil))
}

func TestListMembersRequiresConversation(t *testing.T) {
	// No client call is made when the conversation is missing; this is a local
	// bootstrap error surfaced as MISSING_CONVERSATION.
	_, err := ListMembers(context.Background(), Deps{}, ListMembersInput{})
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "MISSING_CONVERSATION", e.Code)
}

// TestParseFireAtTime guards the one-shot fire_at parse: empty is an error, a
// bad timestamp is an error, and a valid RFC3339 value round-trips. This
// replaces the old parseFireAt+mustParseRFC3339 pair whose "Unreachable" fallback
// silently returned time.Now() on a logic slip.
func TestParseFireAtTime(t *testing.T) {
	_, err := parseFireAtTime("   ")
	e, ok := err.(*Error)
	assert.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT_FAILED", e.Code)

	_, err = parseFireAtTime("not-a-date")
	assert.Error(t, err)

	got, err := parseFireAtTime("2026-07-07T03:00:00Z")
	assert.NoError(t, err)
	want, _ := time.Parse(time.RFC3339, "2026-07-07T03:00:00Z")
	assert.True(t, got.Equal(want))
}
