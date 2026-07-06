package chattools

import (
	"context"
	"errors"
	"testing"

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
