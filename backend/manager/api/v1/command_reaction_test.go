package v1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReactionCallerFromContextNoIdentity guards that reaction identity
// resolution degrades to nil/nil when neither a user nor an agent is in the
// context — the store then scopes reactions to no caller and the `reacted`
// flag is false for every reactor. Runs without a live database.
func TestReactionCallerFromContextNoIdentity(t *testing.T) {
	p, a := reactionCallerFromContext(context.Background())
	assert.Nil(t, p)
	assert.Nil(t, a)
}

// TestFillReactionsEmpty guards that filling reactions over an empty message
// page is a no-op that never touches the store. Runs without a live database.
func TestFillReactionsEmpty(t *testing.T) {
	s := &CommandService{}
	require.NoError(t, s.fillReactions(context.Background(), nil, nil))
}
