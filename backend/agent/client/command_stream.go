package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	cmdPingInterval = 15 * time.Second

	mergedTextDeltaFlushBytes = 4096

	// minSessionGap is the hard floor between drain sessions for one agent. It
	// prevents two agents from tight-looping each other into a wake storm —
	// the LLM's "silence is valid" guidance is the soft brake, this is the hard
	// one. A session that finishes faster than this gap waits out the remainder
	// before opening the next.
	minSessionGap = 1 * time.Second
)

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

func (m *mergedText) flush(stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], commandID string, state *executor.LocalState) error {
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

type commandStream struct {
	client       v1connect.AgentStreamServiceClient
	managerURL   string
	backoff      *ExponentialBackoff
	getToken     func() string
	getSessID    func() string
	getAcpConfig func() *executor.ACPConfig
	socketPath   string
	sessionToken string
	binaryDir    string
	// agentName is the agent's full resource name (agents/{agent}), carried
	// in-stream as AgentReady.agent_name so the manager can bind this AgentChannel
	// to the agent. It is NOT used as LAELIA_AGENT — that is the bare agentID.
	agentName string
	// agentID is the agent's bare server-assigned UUID (the agents/{agent} tail).
	// It keys the per-agent working dir and local state file under the machine's
	// namespace, is passed to the executor as Request.AgentID, and — as
	// Request.AgentResourceID — becomes LAELIA_AGENT, which the daemon and
	// chattools use as a bare id (e.g. agents/<id>/commands/<id>).
	agentID string
	// machineID is the bare UUID of the machine hosting this agent. It namespaces
	// the agent's on-disk state (~/.laelia/<machineID>/<agentID>/) and is passed
	// to the executor as Request.MachineID.
	machineID   string
	isExecuting atomic.Bool

	// drain loop coordination. wakeCh is buffered(1): a wake while one is
	// already pending is coalesced. beginRespCh carries the manager's reply
	// to a BeginSession. currentExecutor is the in-flight session runtime, set
	// by the drain loop and read by the receive goroutine for Cancel/permission.
	wakeCh            chan struct{}
	beginRespCh       chan *v1pb.BeginSessionResponse
	currentExecutor   executor.Runtime
	currentExecutorMu sync.Mutex

	// newSessionRuntime builds the runtime for a drain session. It defaults to
	// buildRuntime (real ACP) and is overridable in tests.
	newSessionRuntime func(req *v1pb.CommandRequest) (executor.Runtime, error)
	// buildTurnBatch renders the "New messages received:" batch that opens a
	// drain turn, using the auth-bearing CommandServiceClient the daemon exposes.
	// Nil in tests (the test supplies TurnPrompt directly on the request).
	buildTurnBatch func(ctx context.Context) (string, error)
}

func newCommandStream(httpClient *http.Client, managerURL, socketPath, sessionToken, binaryDir, agentName, agentID, machineID string) *commandStream {
	c := &commandStream{
		client:       v1connect.NewAgentStreamServiceClient(httpClient, managerURL),
		managerURL:   managerURL,
		backoff:      NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		socketPath:   socketPath,
		sessionToken: sessionToken,
		binaryDir:    binaryDir,
		agentName:    agentName,
		agentID:      agentID,
		machineID:    machineID,
		wakeCh:       make(chan struct{}, 1),
		beginRespCh:  make(chan *v1pb.BeginSessionResponse, 1),
	}
	c.newSessionRuntime = c.buildRuntime
	return c
}

// wake signals the drain loop that new messages may be available. It is
// best-effort and non-blocking: the durable per-channel cursor is the source of
// truth, so a dropped wake just means the next BeginSession discovers the work.
func (c *commandStream) wake() {
	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

// resetCrossConnectionState clears stale in-flight session bookkeeping left
// over from a previous connection so a BeginSessionResponse that arrived but
// was never consumed (the drain loop's ctx cancelled mid-begin) cannot persist
// into the next connection and be consumed by its first beginSession. The
// caller guarantees the prior connection's receive pump and drain loop have
// exited, so replacing the channel fields is safe.
func (c *commandStream) resetCrossConnectionState() {
	c.setCurrentExecutor(nil)
	c.isExecuting.Store(false)
	c.beginRespCh = make(chan *v1pb.BeginSessionResponse, 1)
	c.wakeCh = make(chan struct{}, 1)
}

// Start runs one command-stream lifecycle (mainLoop) and returns its terminal
// error. It deliberately does NOT retry internally: a dead bidi stream must
// surface to the caller (Client.Run's heartbeat loop, the "death fuse") so the
// whole agent connection is torn down and reconnected rather than the agent
// going deaf while its heartbeat stays healthy. The caller owns reconnect
// backoff.
func (c *commandStream) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return c.mainLoop(ctx)
}

func (c *commandStream) mainLoop(ctx context.Context) error {
	token := c.getToken()
	if token == "" {
		_ = c.backoff.Wait(ctx)
		return nil
	}

	stream := c.client.AgentChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	ready := &v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_AgentReady{
			AgentReady: &v1pb.AgentReady{
				AgentName: c.agentName,
				SessionId: c.getSessID(),
			},
		},
	}
	if state, err := executor.LoadLocalState(c.machineID, c.agentID); err != nil {
		slog.Warn("failed to load local command state", "error", err)
	} else if state != nil {
		ready.GetAgentReady().LastCommandId = state.CommandID
		ready.GetAgentReady().LastAckSeq = state.LastSeqSent
		ready.GetAgentReady().LastEventSeq = state.LastEventSeqSent
	}
	if err := stream.Send(ready); err != nil {
		return err
	}

	// Reset any stale in-flight session bookkeeping from a previous connection.
	// The previous connection's receive pump and drain loop have exited
	// (doneCh close / drainCancel) before this point, so replacing the fields
	// is safe.
	c.resetCrossConnectionState()

	pingTicker := time.NewTicker(cmdPingInterval)
	defer pingTicker.Stop()

	var pingSeq int64

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	// Receive pump: dispatches manager messages. BeginSessionResponse goes to
	// the drain loop; NewMessages kicks the drain loop; Cancel/permission act
	// on the in-flight session.
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					select {
					case errCh <- err:
					case <-doneCh:
					}
				}
				return
			}

			switch m := msg.Message.(type) {
			case *v1pb.ManagerStreamMessage_BeginSessionResponse:
				resp := m.BeginSessionResponse
				select {
				case c.beginRespCh <- resp:
				case <-doneCh:
				}

			case *v1pb.ManagerStreamMessage_NewMessages:
				// Best-effort wake; the durable cursor recovers anything missed.
				c.wake()

			case *v1pb.ManagerStreamMessage_Cancel:
				if ex := c.getCurrentExecutor(); ex != nil {
					slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
					ex.Cancel()
				}

			case *v1pb.ManagerStreamMessage_Pong:
				// pong received, link acknowledged

			case *v1pb.ManagerStreamMessage_PermissionDecision:
				d := m.PermissionDecision
				slog.Info("received permission decision", "commandID", d.CommandId, "optionID", d.OptionId)
				if ex := c.getCurrentExecutor(); ex != nil {
					if resolver, ok := ex.(executor.PermissionResolver); ok {
						resolver.ResolvePermission(d.OptionId)
					}
				}

			default:
				slog.Warn("unknown message type from manager")
			}
		}
	}()

	// Drain loop: the agent-first autonomous engine. On wake it opens sessions
	// (BeginSession) and runs each until the manager reports idle. Wakes that
	// arrive during a session are coalesced — the post-session BeginSession
	// picks up anything new via the server-side cursor comparison.
	drainCtx, drainCancel := context.WithCancel(ctx)
	defer drainCancel()
	go c.drainLoop(drainCtx, stream, doneCh)

	// Kick the drain loop once on connect so missed-offline messages are
	// discovered immediately (AgentReady already told the manager we're back).
	c.wake()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case <-pingTicker.C:
			pingSeq++
			ping := &v1pb.AgentStreamMessage{
				Message: &v1pb.AgentStreamMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}
			if err := stream.Send(ping); err != nil {
				return err
			}
		}
	}
}

// drainLoop is the agent-first autonomous engine. It waits for a wake, then
// repeatedly opens a session (BeginSession) and runs it until the manager
// reports no channel has updates (idle), at which point it waits for the next
// wake. One session processes one channel; the outer loop opens the next.
func (c *commandStream) drainLoop(ctx context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], doneCh <-chan struct{}) {
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
				slog.Warn("drain loop: begin session failed, will retry on next wake", "error", err)
				return
			}
			if resp.Idle {
				goto START
			}

			lastSessionStart = time.Now()
			c.runSession(ctx, stream, resp.CommandId, resp.AgentDisplayName)
		}
	}
}

// beginSession sends a BeginSession message and waits for the manager's reply.
// Returns a non-idle response with a command_id to run, or idle=true when no
// channel has updates.
func (c *commandStream) beginSession(ctx context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], doneCh <-chan struct{}) (*v1pb.BeginSessionResponse, error) {
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
	}
}

// runSession executes one drain session: it builds the agent-first runtime
// (fixed prompt) and pumps progress/events/result over the bidi stream via
// runCommand. The agent itself decides which channel to process and how, by
// shelling out to the `laelia-agent` CLI over the local daemon. Blocking:
// returns when the session finishes.
func (c *commandStream) runSession(ctx context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], commandID string, agentDisplayName string) {
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

	req := &v1pb.CommandRequest{
		CommandId:        commandID,
		Instruction:      turnPrompt,
		AgentDisplayName: agentDisplayName,
		TimeoutSeconds:   0,
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
		return
	}

	c.setCurrentExecutor(runtime)
	defer c.setCurrentExecutor(nil)
	c.isExecuting.Store(true)
	defer c.isExecuting.Store(false)

	c.runCommand(ctx, runtime, stream, req)
}

func (c *commandStream) setCurrentExecutor(ex executor.Runtime) {
	c.currentExecutorMu.Lock()
	c.currentExecutor = ex
	c.currentExecutorMu.Unlock()
}

func (c *commandStream) getCurrentExecutor() executor.Runtime {
	c.currentExecutorMu.Lock()
	defer c.currentExecutorMu.Unlock()
	return c.currentExecutor
}

func (c *commandStream) runCommand(
	ctx context.Context,
	runtime executor.Runtime,
	stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage],
	req *v1pb.CommandRequest,
) {
	commandID := req.CommandId
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

	resultSent := false
	defer func() {
		if resultSent {
			return
		}
		runtime.Cancel()
		_ = executor.ClearLocalState(c.machineID, c.agentID)
		_ = sendCommandResult(stream, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: "agent stream send failure",
			LastSeqNo:    state.LastSeqSent,
		})
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
		return
	}
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

	var merged mergedText

	for {
		select {
		case <-ctx.Done():
			return

		case <-runtime.Done():
			_ = merged.flush(stream, commandID, state)

			// DrainOutput flushes any output/events the runtime produced while
			// the consumer was busy sending the result, mutating state so
			// LastSeqSent/LastEventSeqSent reflect exactly what was forwarded.
			drainOutput(ctx, runtime, stream, commandID, state, &merged)

			_ = merged.flush(stream, commandID, state)

			result := <-runtime.ResultChannel()
			result.LastSeqNo = state.LastSeqSent
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
				slog.Error("failed to send command result", "commandID", commandID, "error", err)
			} else {
				slog.Info("command result sent", "commandID", commandID, "exitCode", result.ExitCode)
			}
			_ = executor.ClearLocalState(c.machineID, c.agentID)
			return

		case event, ok := <-runtime.EventChannel():
			if !ok {
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sendCommandEvent(stream, commandID, &event); err != nil {
				slog.Error("failed to send command event", "commandID", commandID, "error", err)
				return
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
				return
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)

			if merged.append(chunk.StreamType, chunk.Content) {
				if err := merged.flush(stream, commandID, state); err != nil {
					slog.Error("failed to send merged text delta", "commandID", commandID, "error", err)
					return
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
	stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage],
	commandID string,
	state *executor.LocalState,
	merged *mergedText,
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
		}
	}
	_ = merged.flush(stream, commandID, state)
}

func (c *commandStream) buildRuntime(req *v1pb.CommandRequest) (executor.Runtime, error) {
	return executor.NewACP(executor.Request{
		CommandID:        req.CommandId,
		TurnPrompt:       req.Instruction,
		Profile:          req.Profile,
		WorkingDir:       req.WorkingDir,
		Env:              req.Env,
		TimeoutSeconds:   req.TimeoutSeconds,
		AllowDiff:        req.AllowDiff,
		ConversationID:   req.ConversationId,
		AgentResourceID:  c.agentID,
		AgentDisplayName: req.AgentDisplayName,
		AgentID:          c.agentID,
		MachineID:        c.machineID,
		DaemonSocket:     c.socketPath,
		SessionToken:     c.sessionToken,
		BinaryDir:        c.binaryDir,
	}, c.getAcpConfig())
}

func sendCommandProgress(stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], commandID string, chunk executor.OutputChunk) error {
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				Type:      chunk.StreamType,
				Content:   chunk.Content,
				SeqNo:     chunk.SeqNo,
			},
		},
	})
}

func sendCommandEvent(stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], commandID string, event *executor.Event) error {
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
	case v1pb.CommandEventType_PERMISSION_REQUESTED:
		ce.Payload = &v1pb.CommandEvent_PermissionRequested{PermissionRequested: event.PermissionRequested}
	case v1pb.CommandEventType_PERMISSION_TIMED_OUT:
		ce.Payload = &v1pb.CommandEvent_PermissionTimedOut{PermissionTimedOut: event.PermissionTimedOut}
	case v1pb.CommandEventType_PERMISSION_DECIDED:
		ce.Payload = &v1pb.CommandEvent_PermissionDecided{PermissionDecided: event.PermissionDecided}
	default:
	}

	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Event{Event: ce},
	})
}

func sendCommandResult(stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], result *v1pb.CommandResult) error {
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Result{
			Result: result,
		},
	})
}

func maxSeq(current int32, next int32) int32 {
	if next > current {
		return next
	}
	return current
}

func nextEventSeq(state *executor.LocalState) int32 {
	state.LastEventSeqSent++
	return state.LastEventSeqSent
}
