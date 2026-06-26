package chattools

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
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
