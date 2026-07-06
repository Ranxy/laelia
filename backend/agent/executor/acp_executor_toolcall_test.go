package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBeginToolCallStartedDedup(t *testing.T) {
	e := &ACPExecutor{toolCallStates: map[string]*toolCallState{}}

	// First sighting of an id claims it and returns the (empty) stored title.
	title, ok := e.beginToolCallStarted("tc-1")
	assert.True(t, ok)
	assert.Equal(t, "", title)
	// Subsequent sightings are no-ops.
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)

	// A different id is still claimed on first sight.
	_, ok = e.beginToolCallStarted("tc-2")
	assert.True(t, ok)
	_, ok = e.beginToolCallStarted("tc-2")
	assert.False(t, ok)

	// Empty id never claims.
	_, ok = e.beginToolCallStarted("")
	assert.False(t, ok)
}

func TestRecordToolCallTitleCarriesTitleToDeferredStart(t *testing.T) {
	e := &ACPExecutor{toolCallStates: map[string]*toolCallState{}}

	// A ToolCall create event records the title but does NOT mark started, so a
	// later ToolCallUpdate carrying RawInput can still emit the STARTED with the
	// stored title (this is the claude-code flow: empty-input create then a
	// content-only update with the command).
	e.recordToolCallTitle("tc-1", "Terminal")
	title, ok := e.beginToolCallStarted("tc-1")
	assert.True(t, ok)
	assert.Equal(t, "Terminal", title)
	// The STARTED has now been emitted; a later status update must not emit a
	// second STARTED.
	_, ok = e.beginToolCallStarted("tc-1")
	assert.False(t, ok)

	// Empty id is a no-op and does not pollute state.
	e.recordToolCallTitle("", "Whatever")
	_, ok = e.beginToolCallStarted("")
	assert.False(t, ok)
}

func TestToolPayloadStruct(t *testing.T) {
	// nil/empty -> nil (frontend "Input not captured" fallback).
	assert.Nil(t, toolPayloadStruct(nil))
	assert.Nil(t, toolPayloadStruct(map[string]any{}))

	// Map value passes through unchanged.
	s := toolPayloadStruct(map[string]any{"command": "ls", "description": "list files"})
	assert.NotNil(t, s)
	assert.Equal(t, "ls", s.AsMap()["command"])

	// Scalar (string) output is wrapped under "value" so structpb.NewStruct
	// accepts it (rawOutput is frequently a JSON string).
	out := toolPayloadStruct("Channels with unread messages")
	assert.NotNil(t, out)
	assert.Equal(t, "Channels with unread messages", out.AsMap()["value"])
}
