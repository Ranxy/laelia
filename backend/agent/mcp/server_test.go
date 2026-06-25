package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeConversationName(t *testing.T) {
	assert.Equal(t, "", normalizeConversationName(""))
	assert.Equal(t, "conversations/abc-123", normalizeConversationName("abc-123"))
	assert.Equal(t, "conversations/abc-123", normalizeConversationName("conversations/abc-123"))
}

func TestResolveConversationName(t *testing.T) {
	t.Run("input preferred over url", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ctxKeyConversationID, "from-url")
		assert.Equal(t, "conversations/from-input", resolveConversationName(ctx, "from-input"))
	})

	t.Run("falls back to url context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ctxKeyConversationID, "from-url")
		assert.Equal(t, "conversations/from-url", resolveConversationName(ctx, ""))
	})

	t.Run("empty when neither present", func(t *testing.T) {
		assert.Equal(t, "", resolveConversationName(context.Background(), ""))
	})
}
