package provider

import (
	"github.com/coder/acp-go-sdk"
	"google.golang.org/protobuf/types/known/structpb"
)

// ToolCallSink is the executor surface a tool-call adapter uses to emit laelia
// command events. It is implemented by *executor.ACPExecutor and lives in the
// provider package so adapters can be registered per provider without importing
// the executor.
type ToolCallSink interface {
	// BeginStarted claims the first STARTED for a toolCallId, returning the
	// title recorded at the create and ok=true; ok=false if a STARTED was
	// already emitted (subsequent updates must not re-emit).
	BeginStarted(id string) (storedTitle string, ok bool)
	// EmitStarted emits a TOOL_CALL_STARTED event. source is the originating
	// frame ("create"/"update"/"status_update") for debug logging only.
	EmitStarted(title string, rawIn *structpb.Struct, source string)
	// EmitFinished emits a TOOL_CALL_FINISHED event with the terminal status
	// and raw output.
	EmitFinished(status string, rawOut *structpb.Struct)
	// Payload converts an ACP RawInput/RawOutput value into the protobuf Struct
	// stored on the event, or nil when the value is empty/{}.
	Payload(in any) *structpb.Struct
	// ToolTracesEnabled reports whether the session opts into tool-call tracing.
	ToolTracesEnabled() bool
}

// ToolCallAdapter maps an agent's ACP ToolCall create/update frames to laelia
// TOOL_CALL_STARTED/FINISHED events. The executor calls OnCreate for the create
// frame, OnContentUpdate for a Status==nil update, and OnStatusUpdate for a
// Status!=nil update — in that order, around the shared content-block buffering
// — so event seq order is preserved.
type ToolCallAdapter interface {
	OnCreate(s ToolCallSink, call *acp.SessionUpdateToolCall)
	OnContentUpdate(s ToolCallSink, upd *acp.SessionToolCallUpdate)
	OnStatusUpdate(s ToolCallSink, upd *acp.SessionToolCallUpdate)
}

// isTerminalStatus reports whether a ToolCallStatus is a terminal state that
// should emit a TOOL_CALL_FINISHED. in_progress/pending are progress updates
// and must not close the tool call.
func isTerminalStatus(status string) bool {
	return status == string(acp.ToolCallStatusCompleted) ||
		status == string(acp.ToolCallStatusFailed)
}

// DefaultAdapter handles agents that carry full RawInput at the create or a
// content-only update (claude-code and any unclassified agent). STARTED is
// emitted at the create when RawInput is present, otherwise deferred to the
// first content-only update that carries it; a status update that arrives with
// no STARTED yet emits a late one using its own RawInput. FINISHED is emitted
// only on terminal status, so repeated in_progress updates no longer orphan the
// real completed event.
type DefaultAdapter struct{}

func (DefaultAdapter) OnCreate(s ToolCallSink, call *acp.SessionUpdateToolCall) {
	if !s.ToolTracesEnabled() {
		return
	}
	rawIn := s.Payload(call.RawInput)
	if rawIn == nil {
		return
	}
	if _, ok := s.BeginStarted(string(call.ToolCallId)); ok {
		s.EmitStarted(call.Title, rawIn, "create")
	}
}

func (DefaultAdapter) OnContentUpdate(s ToolCallSink, upd *acp.SessionToolCallUpdate) {
	if !s.ToolTracesEnabled() {
		return
	}
	rawIn := s.Payload(upd.RawInput)
	if rawIn == nil {
		return
	}
	storedTitle, ok := s.BeginStarted(string(upd.ToolCallId))
	if !ok {
		return
	}
	title := storedTitle
	if upd.Title != nil && *upd.Title != "" {
		title = *upd.Title
	}
	s.EmitStarted(title, rawIn, "update")
}

func (DefaultAdapter) OnStatusUpdate(s ToolCallSink, upd *acp.SessionToolCallUpdate) {
	if !s.ToolTracesEnabled() {
		return
	}
	status := ""
	if upd.Status != nil {
		status = string(*upd.Status)
	}
	if storedTitle, ok := s.BeginStarted(string(upd.ToolCallId)); ok {
		title := storedTitle
		if upd.Title != nil && *upd.Title != "" {
			title = *upd.Title
		}
		s.EmitStarted(title, s.Payload(upd.RawInput), "status_update")
	}
	if isTerminalStatus(status) {
		s.EmitFinished(status, s.Payload(upd.RawOutput))
	}
}

// OpenCodeAdapter handles opencode. Its create carries only partial metadata
// ({cwd} for bash; empty for read/edit) under a generic title ("bash"/"read"/
// "edit"); the real command and full RawInput arrive in the first in_progress
// status update. OnCreate is a no-op so STARTED is deferred to that update;
// OnContentUpdate and OnStatusUpdate are inherited from DefaultAdapter.
type OpenCodeAdapter struct{ DefaultAdapter }

func (OpenCodeAdapter) OnCreate(ToolCallSink, *acp.SessionUpdateToolCall) {}
