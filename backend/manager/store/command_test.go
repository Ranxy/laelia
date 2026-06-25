package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCoerceEnvJSON guards the JSONB NOT NULL command.env column against the
// empty-string bug that silently killed agent BeginSession sessions: an empty
// env must become valid JSON, while a real env value is passed through.
func TestCoerceEnvJSON(t *testing.T) {
	assert.Equal(t, "{}", coerceEnvJSON(""))
	assert.Equal(t, `{"FOO":"bar"}`, coerceEnvJSON(`{"FOO":"bar"}`))
}
