package client

import (
	"context"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// uploadRPCTimeout bounds one UploadCommandData call. The uploader's own
// retry/backoff owns reachability; this only stops a hung connection from
// holding a batch forever.
const uploadRPCTimeout = 30 * time.Second

// turnSink is the turn loop's only report action: append one command data
// record to the agent's durable outbox. The manager's bidi stream no longer
// carries command data — network sends vanish from the turn path (§3.3) and
// the uploader owns delivery.
type turnSink interface {
	appendProgress(commandID string, chunk executor.OutputChunk) error
	appendEvent(commandID string, event *executor.Event) error
	appendResult(commandID string, result *v1pb.CommandResult) error
}

// outboxSink is the production turnSink over the agent's outbox WAL.
type outboxSink struct {
	ob *outbox.Outbox
}

func (s outboxSink) appendProgress(commandID string, chunk executor.OutputChunk) error {
	if s.ob == nil {
		return errors.New("outbox unavailable")
	}
	return s.ob.Append(progressEnvelope(commandID, chunk))
}

func (s outboxSink) appendEvent(commandID string, event *executor.Event) error {
	if s.ob == nil {
		return errors.New("outbox unavailable")
	}
	return s.ob.Append(eventEnvelope(commandID, event))
}

func (s outboxSink) appendResult(_ string, result *v1pb.CommandResult) error {
	if s.ob == nil {
		return errors.New("outbox unavailable")
	}
	return s.ob.Append(resultEnvelope(result))
}

// progressEnvelope builds the durable progress record. The payload mirrors the
// wire CommandProgress the stream path used to carry, so the manager's
// broadcast shape is unchanged; the chunk's production timestamp (or arrival
// time) rides through so the manager orders and stores it without adding
// arrival delay.
func progressEnvelope(commandID string, chunk executor.OutputChunk) *outbox.Entry {
	timestamp := chunk.Timestamp
	if timestamp == nil {
		timestamp = timestamppb.New(time.Now())
	}
	return &outbox.Entry{
		CommandId:          commandID,
		Kind:               v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS,
		SeqNo:              chunk.SeqNo,
		AgentSideTimestamp: timestamp,
		Payload: &v1pb.UploadCommandDataEntry_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				Type:      chunk.StreamType,
				Content:   chunk.Content,
				SeqNo:     chunk.SeqNo,
				Timestamp: timestamp,
			},
		},
	}
}

// eventEnvelope builds the durable event record with a fresh agent-side
// timestamp (events are discrete moments, unlike streamed progress chunks that
// carry the executor's production time).
func eventEnvelope(commandID string, event *executor.Event) *outbox.Entry {
	return &outbox.Entry{
		CommandId:          commandID,
		Kind:               v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT,
		SeqNo:              event.SeqNo,
		AgentSideTimestamp: timestamppb.Now(),
		Payload: &v1pb.UploadCommandDataEntry_Event{
			Event: commandEventOf(commandID, event),
		},
	}
}

// resultEnvelope builds the durable terminal record. Terminal envelopes carry
// seq 1: the manager tracks the terminal as a per-command boolean watermark,
// not a sequence.
func resultEnvelope(result *v1pb.CommandResult) *outbox.Entry {
	return &outbox.Entry{
		CommandId: result.GetCommandId(),
		Kind:      v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT,
		SeqNo:     1,
		Payload: &v1pb.UploadCommandDataEntry_Result{
			Result: result,
		},
	}
}

// deliverTerminal puts the turn's terminal record where the manager will get
// it: the outbox when the WAL is healthy, a direct bypass upload when it is
// not (§3.1 terminal bypass). It reports delivery — a false return leaves the
// command to the next resume or the manager's reaper.
func (c *commandStream) deliverTerminal(ctx context.Context, result *v1pb.CommandResult) bool {
	if c.sink != nil {
		err := c.sink.appendResult(result.GetCommandId(), result)
		if err == nil {
			return true
		}
		slog.Warn("outbox rejected the terminal record; bypassing",
			"commandID", result.GetCommandId(), "error", err)
	}
	if c.uploader == nil {
		return false
	}
	return c.uploader.BypassUpload(ctx, resultEnvelope(result)) == nil
}

// recordTurnFailure reports a turn that ended without its real terminal (a
// record-append failure aborted it): a synthetic FAILED terminal via
// deliverTerminal. Returns delivery.
func (c *commandStream) recordTurnFailure(ctx context.Context, commandID string, state *executor.LocalState, message string) bool {
	return c.deliverTerminal(ctx, &v1pb.CommandResult{
		CommandId:    commandID,
		ExitCode:     -1,
		ErrorMessage: message,
		LastSeqNo:    state.LastSeqSent,
	})
}

// waitTurnClear is the turn-start barrier (§3.4): the outbox must hold no
// records for any other command before this turn appends. A barrier error with
// a live ctx is an outbox fault: the turn is failed fast (its records could
// not be reported either way) instead of running work that cannot be persisted.
// No-op without an uploader (direct turn-loop tests).
func (c *commandStream) waitTurnClear(ctx context.Context, commandID string) error {
	if c.uploader == nil {
		return nil
	}
	if err := c.uploader.WaitClearFor(ctx, commandID); err != nil {
		if ctx.Err() != nil {
			return err
		}
		slog.Warn("turn-start barrier failed", "commandID", commandID, "error", err)
		c.recordTurnFailure(ctx, commandID, &executor.LocalState{}, "outbox barrier failed before the turn")
		return err
	}
	return nil
}
