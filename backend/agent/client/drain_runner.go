package client

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	mergedTextDeltaFlushBytes = 4096

	// minSessionGap is the hard floor between drain sessions for one agent. It
	// prevents two agents from tight-looping each other into a wake storm —
	// the LLM's "silence is valid" guidance is the soft brake, this is the hard
	// one. A session that finishes faster than this gap waits out the remainder
	// before opening the next.
	minSessionGap = 1 * time.Second

	// beginSessionRetryWait is the backoff after a transient BeginSession
	// failure (e.g. a manager-side DB hiccup returning Internal). The pending
	// messages that triggered the wake are still queued server-side and the
	// manager will not re-wake, so the drain loop must retry BeginSession
	// proactively rather than wait for the next wake. A truly dead stream
	// surfaces via the receive pump and triggers a full reconnect independently.
	beginSessionRetryWait = 2 * time.Second
)

// beginSessionResponseTimeout bounds how long the drain loop waits for the
// manager's BeginSessionResponse. The manager handles BeginSession
// synchronously on the stream, so a reply is normally immediate; a timeout
// means the manager failed to reply (e.g. a DB hiccup: it logs the failure and
// never sends a response) — without this bound the drain loop would wait on
// beginRespCh forever and the agent would never run again. The drain loop
// retries with backoff; any command the timed-out attempt minted manager-side
// is reaped by that agent's next BeginSession or the stale-command reaper.
// Var (not const) so tests can shorten the wait.
var beginSessionResponseTimeout = 30 * time.Second

type mergedText struct {
	builder    strings.Builder
	streamType v1pb.CommandOutput_StreamType
	started    bool
}

func (m *mergedText) append(streamType v1pb.CommandOutput_StreamType, text string) bool {
	if !m.started {
		m.started = true
		m.streamType = streamType
	}
	if streamType != m.streamType {
		return true
	}
	_, _ = m.builder.WriteString(text)
	return m.builder.Len() >= mergedTextDeltaFlushBytes
}

func (m *mergedText) flush(stream streamSender, commandID string, state *executor.LocalState) error {
	if !m.started {
		return nil
	}
	text := m.builder.String()
	m.builder.Reset()
	m.started = false
	if text == "" {
		return nil
	}
	event := executor.Event{
		SeqNo:      nextEventSeq(state),
		Type:       v1pb.CommandEventType_TEXT_DELTA,
		Summary:    text,
		Text:       text,
		StreamType: m.streamType,
		TextDelta: &v1pb.TextDeltaPayload{
			StreamType: m.streamType.String(),
			Content:    text,
		},
	}
	return sendCommandEvent(stream, commandID, &event)
}

// drainLoop is the agent-first autonomous engine. It waits for a wake, then
// repeatedly opens a session (BeginSession) and runs it until the manager
// reports no channel has updates (idle), at which point it waits for the next
// wake. One session processes one channel; the outer loop opens the next.
func (c *commandStream) drainLoop(ctx context.Context, stream streamSender, doneCh <-chan struct{}) {
	var lastSessionStart time.Time
	for {
	START:
		select {
		case <-ctx.Done():
			return
		case <-doneCh:
			return
		case <-c.wakeCh:
		}

		// Drain until idle: each BeginSession that reports a channel opens a
		// session; an idle response ends this drain pass.
		for {
			select {
			case <-ctx.Done():
				return
			case <-doneCh:
				return
			default:
			}

			if !lastSessionStart.IsZero() {
				if gap := time.Since(lastSessionStart); gap < minSessionGap {
					select {
					case <-time.After(minSessionGap - gap):
					case <-ctx.Done():
						return
					case <-doneCh:
						return
					}
				}
			}

			resp, err := c.beginSession(ctx, stream, doneCh)
			if err != nil {
				// Do NOT exit the drain loop: a transient BeginSession error
				// (e.g. a manager-side DB hiccup) would otherwise deafen the
				// agent until the whole machine reconnects. The wake that
				// started this pass already fired and won't re-fire, so back off
				// and retry BeginSession proactively. A dead stream is caught
				// separately by the receive pump and drives a full reconnect.
				slog.Warn("drain loop: begin session failed, backing off before retry", "error", err)
				select {
				case <-time.After(beginSessionRetryWait):
				case <-ctx.Done():
					return
				case <-doneCh:
					return
				}
				continue
			}
			if resp.Idle {
				goto START
			}

			lastSessionStart = time.Now()
			c.runSession(ctx, stream, resp.CommandId, resp.AgentDisplayName, resp.OwnerDisplayName, resp.Team, resp.PromptVersion, resp.PromptReleaseNotice)
		}
	}
}

// beginSession sends a BeginSession message and waits for the manager's reply.
// Returns a non-idle response with a command_id to run, or idle=true when no
// channel has updates. The wait is bounded: a manager that received the
// request but never replies must not wedge the drain loop forever.
func (c *commandStream) beginSession(ctx context.Context, stream streamSender, doneCh <-chan struct{}) (*v1pb.BeginSessionResponse, error) {
	// Discard any BeginSessionResponse queued by a previous attempt that gave
	// up waiting: the manager replies in stream order, so whatever is still
	// buffered here predates this send and would anchor this session to an
	// already-reaped command.
	select {
	case <-c.beginRespCh:
	default:
	}
	if err := stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_BeginSession{
			BeginSession: &v1pb.BeginSession{},
		},
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-c.beginRespCh:
		return resp, nil
	case <-doneCh:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(beginSessionResponseTimeout):
		return nil, errors.New("timed out waiting for BeginSession response")
	}
}

// runSession executes one drain session: it builds the agent-first runtime
// (fixed prompt) and pumps progress/events/result over the bidi stream via
// runCommand. The agent itself decides which channel to process and how, by
// shelling out to the `laelia-machine` CLI over the local daemon. Blocking:
// returns when the session finishes.
func (c *commandStream) runSession(ctx context.Context, stream streamSender, commandID string, agentDisplayName, ownerDisplayName string, team *v1pb.TeamContext, promptVersion string, promptNotice *v1pb.PromptReleaseNotice) {
	// Per-agent context state drives re-anchor / usage-warning decisions for
	// this turn and is updated from the events below. A load failure disables
	// context tracking for the turn (never blocks work).
	ctxState, err := executor.LoadContextState(c.machineID, c.agentID)
	if err != nil {
		slog.Warn("failed to load context state; context tracking disabled for turn", "commandID", commandID, "error", err)
		ctxState = nil
	} else if ctxState == nil {
		// First observed turn: start with an empty state so observations and
		// decisions below have a place to accumulate.
		ctxState = &executor.ContextState{}
	}

	// Owner-change force re-anchor: a warm session's init prompt (which names the
	// owner) lives in the session history, so an ownership transfer is invisible
	// to the agent until a cold start or re-anchor. Comparing the manager's fresh
	// owner against the last one this session re-anchored with catches the change
	// on the very next warm turn, so the old owner's authority ends promptly.
	if ctxState != nil && ownerDisplayName != "" && ctxState.OwnerDisplayName != ownerDisplayName {
		ctxState.NeedsReanchor = true
	}
	if ctxState != nil {
		ctxState.OwnerDisplayName = ownerDisplayName
	}

	// Build the "New messages received:" bounded batch that opens this turn. It
	// is the user message the LLM is prompted with (the init prompt is sent only
	// once, at cold start, and inherited via session resume on warm turns).
	turnPrompt := ""
	if c.buildTurnBatch != nil {
		if batch, err := c.buildTurnBatch(ctx); err != nil {
			slog.Warn("failed to build turn batch; proceeding with empty batch", "commandID", commandID, "error", err)
		} else {
			turnPrompt = batch
		}
	}
	turnPrompt = appendContextWarning(turnPrompt, ctxState)

	// Consume a manager-pushed prompt release notice that could not be steered
	// into the previous in-flight turn, or one re-sent via BeginSession because
	// the agent was offline. Prepend it to this turn and ack.
	notice := c.takePendingPromptNotice()
	if notice == nil {
		notice = promptNotice
	}
	if notice != nil {
		// A real prompt-version notice (persona/team/owner change) marks the new
		// version as confirmed and forces a re-anchor. A stale-machine notice
		// carries no prompt_version: it only tells the agent to upgrade and must
		// not overwrite the locally confirmed version (which would mask the
		// staleness signal).
		if ctxState != nil && notice.GetPromptVersion() != "" {
			ctxState.PromptVersion = notice.GetPromptVersion()
			ctxState.NeedsReanchor = true
		}
		if msg := strings.TrimSpace(notice.GetMessage()); msg != "" {
			if strings.TrimSpace(turnPrompt) == "" {
				turnPrompt = msg
			} else {
				turnPrompt = msg + "\n\n" + turnPrompt
			}
		}
		_ = sendPromptReleaseNoticeAck(stream, notice)
	}

	// A notice successfully steered into the previous in-flight turn is already
	// seen; mark it confirmed so applyPromptVersion does not inject a duplicate.
	if ctxState != nil {
		if v := c.takeSteeredPromptVersion(); v != "" {
			ctxState.PromptVersion = v
		}
	}

	// Prompt-version change detection: the manager's composite prompt version
	// changes when the static prompt bundle or the dynamic persona/team/owner
	// changes. The agent injects a notice / forces re-anchor so the change is
	// perceived on the very next turn.
	if ctxState != nil && promptVersion != "" {
		turnPrompt = c.applyPromptVersion(ctxState, promptVersion, turnPrompt)
	}

	name := agentDisplayName
	if name == "" {
		name = c.agentID
	}
	teamPrompt := ""
	if team != nil {
		teamPrompt = team.TeamPrompt
	}
	req := executor.Request{
		CommandID:        commandID,
		TurnPrompt:       turnPrompt,
		AgentDisplayName: agentDisplayName,
		OwnerDisplayName: ownerDisplayName,
		TeamPrompt:       teamPrompt,
		ReanchorPrompt:   reanchorPrompt(ctxState, name, ownerDisplayName, teamPrompt),
	}

	runtime, err := c.newSessionRuntime(req)
	if err != nil {
		slog.Error("failed to build drain session runtime", "commandID", commandID, "error", err)
		if sendErr := sendCommandResult(stream, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: err.Error(),
			LastSeqNo:    -1,
		}); sendErr != nil {
			slog.Error("failed to send drain session failure result", "commandID", commandID, "error", sendErr)
		}
		c.persistContextState(ctxState, nil)
		return
	}

	c.setCurrentExecutor(runtime)
	defer c.setCurrentExecutor(nil)
	c.beginInFlight()
	defer c.endInFlight()

	result := c.runCommand(ctx, runtime, stream, req, ctxState)
	c.persistContextState(ctxState, result)
}

func (c *commandStream) runCommand(
	ctx context.Context,
	runtime executor.Runtime,
	stream streamSender,
	req executor.Request,
	ctxState *executor.ContextState,
) *executor.Result {
	commandID := req.CommandID
	state := &executor.LocalState{
		CommandID:        commandID,
		ExecutorKind:     "ACP",
		Status:           "running",
		StartedAt:        time.Now().UnixMilli(),
		LastSeqSent:      0,
		LastEventSeqSent: 0,
	}
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}
	observer := newContextObserver(ctxState, stream, commandID, state)
	defer observer.stopWatchdog()

	resultSent := false
	defer func() {
		if resultSent {
			return
		}
		runtime.Cancel()
		// Send the failure result BEFORE touching the local state: when the
		// stream is dead the send fails, and the surviving CommandID is what
		// the reconnecting AgentReady.lastCommandId carries so the manager can
		// reap the orphaned RUNNING command. Clearing first would destroy that
		// key and leave the command RUNNING forever (the manager's reconnect
		// reap and its 60s grace timer are both keyed off it).
		if err := sendCommandResult(stream, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: "agent stream send failure",
			LastSeqNo:    state.LastSeqSent,
		}); err == nil {
			_ = executor.ClearLocalState(c.machineID, c.agentID)
		}
	}()

	runtime.Start()
	startSeq := nextEventSeq(state)
	if err := sendCommandEvent(stream, commandID, &executor.Event{
		SeqNo:   startSeq,
		Type:    v1pb.CommandEventType_LIFECYCLE,
		Summary: "command started",
		Lifecycle: &v1pb.LifecyclePayload{
			ExecutorKind: "ACP",
			Profile:      req.Profile,
		},
	}); err != nil {
		slog.Error("failed to send command start event", "commandID", commandID, "error", err)
		return nil
	}
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

	var merged mergedText

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-runtime.Done():
			_ = merged.flush(stream, commandID, state)

			// DrainOutput flushes any output/events the runtime produced while
			// the consumer was busy sending the result, mutating state so
			// LastSeqSent/LastEventSeqSent reflect exactly what was forwarded.
			drainOutput(ctx, runtime, stream, commandID, state, &merged, observer)

			_ = merged.flush(stream, commandID, state)

			result := <-runtime.ResultChannel()
			result.LastSeqNo = state.LastSeqSent
			// A coordinated cancel (e.g. config hot-reload) overrides the
			// runtime's generic cancellation error with an explicit cause so
			// the manager reports the reload, not "context canceled". Only
			// override a FAILED turn: a turn that finished successfully
			// (ExitCode 0) before the cancel took effect must not be mislabeled
			// as a reload failure (which could trigger a retry and duplicate
			// side effects).
			if reason := c.takeCancelReason(); reason != "" && result.ExitCode != 0 {
				result.ErrorMessage = reason
			}
			resultSent = true
			if err := sendCommandResult(stream, &v1pb.CommandResult{
				CommandId:    commandID,
				ExitCode:     result.ExitCode,
				DurationMs:   result.DurationMs,
				ErrorMessage: result.ErrorMessage,
				LastSeqNo:    result.LastSeqNo,
				FinalSummary: result.FinalSummary,
				Result:       result.Result,
			}); err != nil {
				// Keep the local state: the reconnecting AgentReady.lastCommandId
				// must carry this command so the manager reaps the orphaned
				// RUNNING row instead of losing it.
				slog.Error("failed to send command result", "commandID", commandID, "error", err)
			} else {
				slog.Info("command result sent", "commandID", commandID, "exitCode", result.ExitCode)
				_ = executor.ClearLocalState(c.machineID, c.agentID)
			}
			return &result

		case <-observer.watchdogCh:
			if err := observer.onWatchdog(); err != nil {
				slog.Error("failed to send compaction watchdog warning", "commandID", commandID, "error", err)
				return nil
			}

		case event, ok := <-runtime.EventChannel():
			if !ok {
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sendCommandEvent(stream, commandID, &event); err != nil {
				slog.Error("failed to send command event", "commandID", commandID, "error", err)
				return nil
			}
			if err := observer.observe(&event); err != nil {
				slog.Error("failed to send derived context event", "commandID", commandID, "error", err)
				return nil
			}
			if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}

		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				continue
			}
			if err := sendCommandProgress(stream, commandID, chunk); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				return nil
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)

			if merged.append(chunk.StreamType, chunk.Content) {
				if err := merged.flush(stream, commandID, state); err != nil {
					slog.Error("failed to send merged text delta", "commandID", commandID, "error", err)
					return nil
				}
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
			if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}
		}
	}
}

// drainOutput forwards any output chunks and events the runtime still has
// buffered after Done() fired, mutating state so LastSeqSent/LastEventSeqSent
// reflect exactly what was sent. It drains until both channels close (the
// runtime closes them in its deferred teardown), with ctx as a backstop so a
// runtime that never closes cannot wedge the consumer. Previously it only
// drained OutputChannel via a non-blocking `default` (dropping queued events
// and any output produced after the peek) and wrote event seq numbers against
// a throwaway LocalState, leaving state.LastEventSeqSent stale/rolled back.
func drainOutput(
	ctx context.Context,
	runtime executor.Runtime,
	stream streamSender,
	commandID string,
	state *executor.LocalState,
	merged *mergedText,
	observer *contextObserver,
) {
	outputClosed, eventClosed := false, false
	for !outputClosed || !eventClosed {
		select {
		case <-ctx.Done():
			_ = merged.flush(stream, commandID, state)
			return
		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				outputClosed = true
				continue
			}
			if err := sendCommandProgress(stream, commandID, chunk); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				_ = merged.flush(stream, commandID, state)
				return
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)
			if merged.append(chunk.StreamType, chunk.Content) {
				_ = merged.flush(stream, commandID, state)
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
		case event, ok := <-runtime.EventChannel():
			if !ok {
				eventClosed = true
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sendCommandEvent(stream, commandID, &event); err != nil {
				slog.Error("failed to send command event", "commandID", commandID, "error", err)
				_ = merged.flush(stream, commandID, state)
				return
			}
			if observer != nil {
				if err := observer.observe(&event); err != nil {
					slog.Error("failed to send derived context event", "commandID", commandID, "error", err)
					_ = merged.flush(stream, commandID, state)
					return
				}
			}
		}
	}
	_ = merged.flush(stream, commandID, state)
}

func (c *commandStream) buildRuntime(req executor.Request) (executor.Runtime, error) {
	return executor.NewACP(req, c.getAcpConfig())
}

func sendCommandProgress(stream streamSender, commandID string, chunk executor.OutputChunk) error {
	// Carry the agent-side timestamp through so the manager can order and store
	// it without adding its own arrival delay.
	timestamp := chunk.Timestamp
	if timestamp == nil {
		timestamp = timestamppb.New(time.Now())
	}
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				Type:      chunk.StreamType,
				Content:   chunk.Content,
				SeqNo:     chunk.SeqNo,
				Timestamp: timestamp,
			},
		},
	})
}

func sendCommandEvent(stream streamSender, commandID string, event *executor.Event) error {
	ce := &v1pb.CommandEvent{
		CommandId: commandID,
		SeqNo:     event.SeqNo,
		Type:      event.Type,
		Summary:   event.Summary,
		Timestamp: timestamppb.New(time.Now()),
	}

	switch event.Type {
	case v1pb.CommandEventType_LIFECYCLE:
		ce.Payload = &v1pb.CommandEvent_Lifecycle{Lifecycle: event.Lifecycle}
	case v1pb.CommandEventType_TEXT_DELTA:
		ce.Payload = &v1pb.CommandEvent_TextDelta{TextDelta: event.TextDelta}
	case v1pb.CommandEventType_TOOL_CALL_STARTED:
		ce.Payload = &v1pb.CommandEvent_ToolCallStarted{ToolCallStarted: event.ToolCallStarted}
	case v1pb.CommandEventType_TOOL_CALL_FINISHED:
		ce.Payload = &v1pb.CommandEvent_ToolCallFinished{ToolCallFinished: event.ToolCallFinished}
	case v1pb.CommandEventType_DIFF_EMITTED:
		ce.Payload = &v1pb.CommandEvent_DiffEmitted{DiffEmitted: event.DiffEmitted}
	case v1pb.CommandEventType_WARNING:
		ce.Payload = &v1pb.CommandEvent_Warning{Warning: event.Warning}
	case v1pb.CommandEventType_RAW_ACP:
		ce.Payload = &v1pb.CommandEvent_RawAcp{RawAcp: event.RawAcp}
	case v1pb.CommandEventType_FINAL_SUMMARY:
		ce.Payload = &v1pb.CommandEvent_FinalSummary{FinalSummary: event.FinalSummary}
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		ce.Payload = &v1pb.CommandEvent_ContextCompaction{ContextCompaction: event.ContextCompaction}
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		ce.Payload = &v1pb.CommandEvent_ContextUsage{ContextUsage: event.ContextUsage}
	case v1pb.CommandEventType_TOKEN_USAGE:
		ce.Payload = &v1pb.CommandEvent_TokenUsage{TokenUsage: event.TokenUsage}
	default:
	}

	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Event{Event: ce},
	})
}

func sendCommandResult(stream streamSender, result *v1pb.CommandResult) error {
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Result{
			Result: result,
		},
	})
}
