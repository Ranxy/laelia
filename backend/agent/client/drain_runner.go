package client

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
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
)

// beginSessionTimeout bounds the unary BeginSession RPC. The manager handles
// it synchronously (cursor checks + mint); a timeout means the manager is
// wedged and the drain loop backs off.
const beginSessionTimeout = 30 * time.Second

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

func (m *mergedText) flush(sink turnSink, commandID string, state *executor.LocalState) error {
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
	return sink.appendEvent(commandID, &event)
}

// drainLoop is the agent-first autonomous engine, living on the runner's
// long-lived ctx: it waits for a wake, then repeatedly pulls work through the
// unary BeginSession RPC and runs each session until the manager reports no
// channel has updates (idle). A dead manager only backs the pull off; a
// running turn is never interrupted by a reconnect.
func (c *commandStream) drainLoop(ctx context.Context) {
	if c.backoff == nil {
		c.backoff = NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait)
	}
	var lastSessionStart time.Time
	for {
	START:
		select {
		case <-ctx.Done():
			return
		case <-c.wakeCh:
		}

		// Drain until idle: each BeginSession that reports a channel opens a
		// session; an idle response ends this drain pass.
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			if !lastSessionStart.IsZero() {
				if gap := time.Since(lastSessionStart); gap < minSessionGap {
					select {
					case <-time.After(minSessionGap - gap):
					case <-ctx.Done():
						return
					}
				}
			}

			resp, err := c.beginSession(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				// Do NOT exit the drain loop: a transient BeginSession failure
				// (manager restart, DB hiccup, control stream down) would
				// otherwise deafen the agent until the whole machine
				// reconnects. Back off and retry proactively — the wake that
				// started this pass already fired and won't re-fire.
				slog.Warn("drain loop: begin session failed, backing off before retry", "error", err)
				if werr := c.backoff.Wait(ctx); werr != nil {
					return
				}
				continue
			}
			c.backoff.Reset()
			if resp.Idle {
				goto START
			}

			lastSessionStart = time.Now()
			c.runSession(ctx, resp.CommandId, resp.AgentDisplayName, resp.OwnerDisplayName, resp.Team, resp.PromptVersion, resp.PromptReleaseNotice)
		}
	}
}

// beginSession pulls the drain loop's next unit of work through the unary
// BeginSession RPC on MachineStreamService (the per-agent stream is retired).
// agent_name binds the pull to this agent; the reply carries the command to
// run, or idle=true when no conversation has updates beyond the agent's
// durable cursor.
func (c *commandStream) beginSession(ctx context.Context) (*v1pb.BeginSessionResponse, error) {
	token := c.getToken()
	if token == "" {
		return nil, errors.New("no machine access token for begin session")
	}
	callCtx, cancel := context.WithTimeout(ctx, beginSessionTimeout)
	defer cancel()
	req := connect.NewRequest(&v1pb.BeginSessionRequest{AgentName: c.agentName})
	req.Header().Set("Authorization", "Bearer "+token)
	resp, err := c.client.BeginSession(callCtx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// runSession executes one drain session: it starts with the turn-start
// barrier (the outbox must hold no other command's records), then builds the
// agent-first runtime (fixed prompt) and pumps progress/events/result into the
// per-agent outbox via runCommand — the uploader drains them to the manager.
// The agent itself decides which channel to process and how, by shelling out
// to the `laelia-machine` CLI over the local daemon. Blocking: returns when
// the session finishes.
func (c *commandStream) runSession(ctx context.Context, commandID string, agentDisplayName, ownerDisplayName string, team *v1pb.TeamContext, promptVersion string, promptNotice *v1pb.PromptReleaseNotice) {
	c.setCurrentCommand(commandID)
	defer c.setCurrentCommand("")

	// Turn-start barrier (§3.4). No-op without an uploader (turn-loop tests).
	if err := c.waitTurnClear(ctx, commandID); err != nil {
		if ctx.Err() == nil {
			slog.Error("turn aborted at the outbox barrier", "commandID", commandID, "error", err)
		}
		return
	}

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
		c.ackPromptNotice(notice)
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
		if !c.deliverTerminal(ctx, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: err.Error(),
			LastSeqNo:    -1,
		}) {
			slog.Error("failed to deliver drain session failure result", "commandID", commandID)
		}
		c.persistContextState(ctxState, nil)
		return
	}

	c.setCurrentExecutor(runtime)
	defer c.setCurrentExecutor(nil)
	c.beginInFlight()
	defer c.endInFlight()

	result := c.runCommand(ctx, runtime, c.sink, req, ctxState)
	c.persistContextState(ctxState, result)
}

// ackPromptNotice reports to the manager (over the machine control stream)
// that a prompt release notice was injected into this turn, so it stops
// re-pushing it. Best-effort: a machine that is offline re-sends the notice on
// the next BeginSession.
func (c *commandStream) ackPromptNotice(notice *v1pb.PromptReleaseNotice) {
	if c.sendMachine == nil || notice == nil {
		return
	}
	_ = c.sendMachine(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_PromptReleaseNoticeAck{
			PromptReleaseNoticeAck: &v1pb.PromptReleaseNoticeAck{
				AgentName:     c.agentName,
				NoticeKey:     notice.GetNoticeKey(),
				PromptVersion: notice.GetPromptVersion(),
			},
		},
	})
}

// initLocalState starts a turn's local state fresh: both seq counters (progress
// and events) count per-turn from 1. Seq spaces are scoped per command, and the
// manager only ever hands out a fresh command id (a leftover RUNNING row is
// never resumed), so there is nothing to continue from an interrupted turn.
func (*commandStream) initLocalState(commandID string) *executor.LocalState {
	return &executor.LocalState{
		CommandID:    commandID,
		ExecutorKind: "ACP",
		Status:       "running",
		StartedAt:    time.Now().UnixMilli(),
	}
}

// runCommand executes one turn: it records every progress chunk, event, and
// the terminal into the agent's outbox (turnSink). The manager's stream never
// carries command data; only record-append failures (a local WAL fault) can
// abort the turn, and the terminal is delivered via the §3.1 bypass when the
// log rejects it. The turn ends when its terminal is durably recorded — the
// uploader owns delivery.
//
// A turn interrupted by ctx (the runner or machine going away) records no
// terminal: the manager keeps the command RUNNING until the reaper closes it
// (machine lost past the grace) or a late turn result re-grades it.
func (c *commandStream) runCommand(
	ctx context.Context,
	runtime executor.Runtime,
	sink turnSink,
	req executor.Request,
	ctxState *executor.ContextState,
) *executor.Result {
	commandID := req.CommandID
	state := c.initLocalState(commandID)
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}
	observer := newContextObserver(ctxState, sink, commandID, state)
	defer observer.stopWatchdog()

	// terminalDelivered reports that the terminal record reached the manager's
	// path (durable in the outbox, or bypass-delivered): the local state can
	// then be cleared.
	terminalDelivered := false
	// pendingResult holds the runtime's real terminal when the record-append
	// failed, so the deferred bypass delivers the real outcome instead of a
	// synthetic failure.
	var pendingResult *v1pb.CommandResult
	defer func() {
		if terminalDelivered {
			return
		}
		runtime.Cancel()
		if ctx.Err() != nil {
			// Interrupted turn: no terminal. The per-command seq spaces make
			// the leftover records harmless (a later turn for the same command
			// id cannot exist — the manager only mints fresh ids).
			return
		}
		result := pendingResult
		if result == nil {
			result = &v1pb.CommandResult{
				CommandId:    commandID,
				ExitCode:     -1,
				ErrorMessage: "agent outbox write failure",
			}
		}
		if c.deliverTerminal(ctx, result) {
			_ = executor.ClearLocalState(c.machineID, c.agentID)
		}
	}()

	runtime.Start()
	if err := sink.appendEvent(commandID, &executor.Event{
		SeqNo:   nextEventSeq(state),
		Type:    v1pb.CommandEventType_LIFECYCLE,
		Summary: "command started",
		Lifecycle: &v1pb.LifecyclePayload{
			ExecutorKind: "ACP",
			Profile:      req.Profile,
		},
	}); err != nil {
		slog.Error("failed to append command start event", "commandID", commandID, "error", err)
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
			_ = merged.flush(sink, commandID, state)

			// drainOutput records any output/events the runtime produced while
			// the consumer was busy, mutating state so
			// LastSeqSent/LastEventSeqSent reflect exactly what was recorded.
			drainOutput(ctx, runtime, sink, commandID, state, &merged, observer)

			_ = merged.flush(sink, commandID, state)

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
			resultPayload := &v1pb.CommandResult{
				CommandId:    commandID,
				ExitCode:     result.ExitCode,
				DurationMs:   result.DurationMs,
				ErrorMessage: result.ErrorMessage,
				LastSeqNo:    result.LastSeqNo,
				FinalSummary: result.FinalSummary,
				Result:       result.Result,
			}
			if !c.deliverTerminal(ctx, resultPayload) {
				slog.Error("failed to record command result", "commandID", commandID)
				// The bypass failed too: the local state keeps the turn's
				// record of what happened, but there is no resume — the
				// manager's reaper owns the RUNNING row.
				pendingResult = resultPayload
				return &result
			}
			terminalDelivered = true
			slog.Info("command result recorded", "commandID", commandID, "exitCode", result.ExitCode)
			_ = executor.ClearLocalState(c.machineID, c.agentID)
			return &result

		case <-observer.watchdogCh:
			if err := observer.onWatchdog(); err != nil {
				slog.Error("failed to append compaction watchdog warning", "commandID", commandID, "error", err)
				return nil
			}

		case event, ok := <-runtime.EventChannel():
			if !ok {
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sink.appendEvent(commandID, &event); err != nil {
				slog.Error("failed to append command event", "commandID", commandID, "error", err)
				return nil
			}
			if err := observer.observe(&event); err != nil {
				slog.Error("failed to append derived context event", "commandID", commandID, "error", err)
				return nil
			}
			if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}

		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				continue
			}
			if err := sink.appendProgress(commandID, chunk); err != nil {
				slog.Error("failed to append command progress", "commandID", commandID, "error", err)
				return nil
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)

			if merged.append(chunk.StreamType, chunk.Content) {
				if err := merged.flush(sink, commandID, state); err != nil {
					slog.Error("failed to append merged text delta", "commandID", commandID, "error", err)
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

// drainOutput records any output chunks and events the runtime still has
// buffered after Done() fired, mutating state so LastSeqSent/LastEventSeqSent
// reflect exactly what was recorded. It drains until both channels close (the
// runtime closes them in its deferred teardown), with ctx as a backstop so a
// runtime that never closes cannot wedge the consumer. Previously it only
// drained OutputChannel via a non-blocking `default` (dropping queued events
// and any output produced after the peek) and wrote event seq numbers against
// a throwaway LocalState, leaving state.LastEventSeqSent stale/rolled back.
func drainOutput(
	ctx context.Context,
	runtime executor.Runtime,
	sink turnSink,
	commandID string,
	state *executor.LocalState,
	merged *mergedText,
	observer *contextObserver,
) {
	outputClosed, eventClosed := false, false
	for !outputClosed || !eventClosed {
		select {
		case <-ctx.Done():
			_ = merged.flush(sink, commandID, state)
			return
		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				outputClosed = true
				continue
			}
			if err := sink.appendProgress(commandID, chunk); err != nil {
				slog.Error("failed to append command progress", "commandID", commandID, "error", err)
				_ = merged.flush(sink, commandID, state)
				return
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)
			if merged.append(chunk.StreamType, chunk.Content) {
				_ = merged.flush(sink, commandID, state)
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
		case event, ok := <-runtime.EventChannel():
			if !ok {
				eventClosed = true
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sink.appendEvent(commandID, &event); err != nil {
				slog.Error("failed to append command event", "commandID", commandID, "error", err)
				_ = merged.flush(sink, commandID, state)
				return
			}
			if observer != nil {
				if err := observer.observe(&event); err != nil {
					slog.Error("failed to append derived context event", "commandID", commandID, "error", err)
					_ = merged.flush(sink, commandID, state)
					return
				}
			}
		}
	}
	_ = merged.flush(sink, commandID, state)
}

func (c *commandStream) buildRuntime(req executor.Request) (executor.Runtime, error) {
	return executor.NewACP(req, c.getAcpConfig())
}

// commandEventOf maps an executor event onto the wire CommandEvent. It lives
// next to the outbox envelopes: the payload shape is unchanged from the
// stream-era mapping, so the manager's persistence and UI are unaffected.
func commandEventOf(commandID string, event *executor.Event) *v1pb.CommandEvent {
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

	return ce
}
