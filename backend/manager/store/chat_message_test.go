package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListConversationMessagesMutualExclusion guards the before/after version
// contract: both bounds must not be set at once. The guard runs before any DB
// access, so a zero-value Store is enough to exercise it.
func TestListConversationMessagesMutualExclusion(t *testing.T) {
	s := &Store{}
	_, _, err := s.ListConversationMessages(context.Background(), uuid.New(), 1, 1, 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}
