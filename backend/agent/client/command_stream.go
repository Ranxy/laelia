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
	cmdPingTimeout  = 5 * time.Second

	mergedTextDeltaFlushBytes = 4096
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
	client          v1connect.AgentStreamServiceClient
	managerURL      string
	backoff         *ExponentialBackoff
	getToken        func() string
	getSessID       func() string
	getAcpConfig    func() *executor.ACPConfig
	mcpPort         int
	agentResourceID string
	isExecuting     atomic.Bool

	// Phase 2: per-conversation version cursors for autonomous message pull.
	conversationCursors   map[string]int64
	conversationCursorsMu sync.Mutex
}

func newCommandStream(httpClient *http.Client, managerURL string, mcpPort int, agentResourceID string) *commandStream {
	return &commandStream{
		client:              v1connect.NewAgentStreamServiceClient(httpClient, managerURL),
		managerURL:          managerURL,
		backoff:             NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		mcpPort:             mcpPort,
		agentResourceID:     agentResourceID,
		conversationCursors: make(map[string]int64),
	}
}

func (c *commandStream) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err := c.mainLoop(ctx); err != nil {
			slog.Error("command stream error, reconnecting", "error", err)
			if err := c.backoff.Wait(ctx); err != nil {
				return err
			}
			continue
		}

		c.backoff.Reset()
	}
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
				SessionId: c.getSessID(),
			},
		},
	}
	if state, err := executor.LoadLocalState(); err != nil {
		slog.Warn("failed to load local command state", "error", err)
	} else if state != nil {
		ready.GetAgentReady().LastCommandId = state.CommandID
		ready.GetAgentReady().LastAckSeq = state.LastSeqSent
		ready.GetAgentReady().LastEventSeq = state.LastEventSeqSent
	}
	if err := stream.Send(ready); err != nil {
		return err
	}

	pingTicker := time.NewTicker(cmdPingInterval)
	defer pingTicker.Stop()

	var pingSeq int64
	var currentExecutor executor.Runtime

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					errCh <- err
				}
				return
			}

			switch m := msg.Message.(type) {
			case *v1pb.ManagerStreamMessage_CommandRequest:
				req := m.CommandRequest
				slog.Info("received command", "commandID", req.CommandId)

				runtime, err := c.buildRuntime(req)
				if err != nil {
					slog.Error("failed to build runtime", "commandID", req.CommandId, "error", err)
					if sendErr := sendCommandResult(stream, &v1pb.CommandResult{
						CommandId:    req.CommandId,
						ExitCode:     -1,
						ErrorMessage: err.Error(),
						LastSeqNo:    -1,
					}); sendErr != nil {
						errCh <- sendErr
					}
					c.isExecuting.Store(false)
					continue
				}

				currentExecutor = runtime
				go func() {
					c.runCommand(ctx, runtime, stream, req)
					c.isExecuting.Store(false)
				}()

			case *v1pb.ManagerStreamMessage_NewMessages:
				c.handleNewMessages(ctx, stream, m.NewMessages)

			case *v1pb.ManagerStreamMessage_MessageSnapshot:
				c.handleMessageSnapshot(ctx, stream, m.MessageSnapshot)

			case *v1pb.ManagerStreamMessage_ActionResponse:
				// Phase 2: manager replies to SubmitAction. If held, resolve.
				// If committed, the CommandRequest follows in a subsequent message.
				c.handleActionResponse(ctx, stream, m.ActionResponse)

			case *v1pb.ManagerStreamMessage_Cancel:
				if currentExecutor != nil {
					slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
					currentExecutor.Cancel()
				}

			case *v1pb.ManagerStreamMessage_Pong:
				// pong received, link acknowledged

			case *v1pb.ManagerStreamMessage_PermissionDecision:
				d := m.PermissionDecision
				slog.Info("received permission decision", "commandID", d.CommandId, "optionID", d.OptionId)
				if resolver, ok := currentExecutor.(executor.PermissionResolver); ok {
					resolver.ResolvePermission(d.OptionId)
				}

			default:
				slog.Warn("unknown message type from manager")
			}
		}
	}()

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

func (*commandStream) runCommand(
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
	if err := executor.SaveLocalState(state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

	var mergedTextBuf mergedText
	defer func() {
		_ = mergedTextBuf.flush(stream, commandID, state)
	}()

	resultSent := false
	defer func() {
		if resultSent {
			return
		}
		runtime.Cancel()
		_ = executor.ClearLocalState()
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
	if err := executor.SaveLocalState(state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

	var merged mergedText

	for {
		select {
		case <-ctx.Done():
			return

		case <-runtime.Done():
			_ = merged.flush(stream, commandID, state)

			lastSeqNo, lastEventSeqNo := drainOutput(runtime, stream, commandID, state.LastSeqSent, state.LastEventSeqSent, &merged)
			state.LastSeqSent = lastSeqNo
			state.LastEventSeqSent = lastEventSeqNo

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
			_ = executor.ClearLocalState()
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
			if err := executor.SaveLocalState(state); err != nil {
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
			if err := executor.SaveLocalState(state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}
		}
	}
}

func drainOutput(runtime executor.Runtime, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], commandID string, lastSeqNo int32, lastEventSeqNo int32, merged *mergedText) (int32, int32) {
	for {
		select {
		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				_ = merged.flush(stream, commandID, &executor.LocalState{LastEventSeqSent: lastEventSeqNo})
				return lastSeqNo, lastEventSeqNo
			}
			if err := sendCommandProgress(stream, commandID, chunk); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				return lastSeqNo, lastEventSeqNo
			}
			lastSeqNo = maxSeq(lastSeqNo, chunk.SeqNo)

			if merged.append(chunk.StreamType, chunk.Content) {
				_ = merged.flush(stream, commandID, &executor.LocalState{})
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
		default:
			return lastSeqNo, lastEventSeqNo
		}
	}
}

func (c *commandStream) buildRuntime(req *v1pb.CommandRequest) (executor.Runtime, error) {
	return executor.NewACP(executor.Request{
		CommandID:            req.CommandId,
		Instruction:          req.Instruction,
		Profile:              req.Profile,
		WorkingDir:           req.WorkingDir,
		Env:                  req.Env,
		TimeoutSeconds:       req.TimeoutSeconds,
		AllowDiff:            req.AllowDiff,
		ConversationID:       req.ConversationId,
		ReplyToMessageID:     req.ReplyToMessageId,
		AgentResourceID:      c.agentResourceID,
		PrincipalID:          req.PrincipalId,
		MCPPort:              c.mcpPort,
		LastProcessedVersion: c.conversationCursor(req.ConversationId),
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

// ---- Phase 2: Agent Autonomy ----

// conversationCursor returns the last-seen room_version for a conversation,
// defaulting to 0 (pull all messages) on first access.
func (c *commandStream) conversationCursor(convID string) int64 {
	c.conversationCursorsMu.Lock()
	defer c.conversationCursorsMu.Unlock()
	return c.conversationCursors[convID]
}

func (c *commandStream) setConversationCursor(convID string, v int64) {
	c.conversationCursorsMu.Lock()
	defer c.conversationCursorsMu.Unlock()
	if v > c.conversationCursors[convID] {
		c.conversationCursors[convID] = v
	}
}

// handleNewMessages is the Phase 2 response to NewMessagesAvailable: for each
// conversation that has new messages, pull the delta since the last-seen
// version.
func (c *commandStream) handleNewMessages(_ context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], nm *v1pb.NewMessagesAvailable) {
	for i, convID := range nm.ConversationIds {
		afterVersion := c.conversationCursor(convID)
		version := int64(0)
		if i < len(nm.Versions) {
			version = nm.Versions[i]
		}
		slog.Info("pulling messages for conversation",
			"conversation_id", convID,
			"after_version", afterVersion,
			"latest_version", version)

		req := &v1pb.AgentStreamMessage{
			Message: &v1pb.AgentStreamMessage_PullMessages{
				PullMessages: &v1pb.PullMessages{
					ConversationId: convID,
					AfterVersion:   afterVersion,
				},
			},
		}
		if err := stream.Send(req); err != nil {
			slog.Error("failed to send PullMessages", "conversation_id", convID, "error", err)
		}
	}
}

// handleMessageSnapshot is the Phase 2 autonomy gate. It processes a
// PullMessages response: updates the cursor, finds the latest USER message, and
// if the agent is not already executing, submits an action for it.
func (c *commandStream) handleMessageSnapshot(_ context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], snap *v1pb.MessageSnapshot) {
	convID := "" // snapshot doesn't carry conversation_id; we track implicit context
	if len(snap.Messages) > 0 {
		convID = snap.Messages[0].Conversation
		c.setConversationCursor(convID, snap.CurrentVersion)
	}

	slog.Info("message snapshot received",
		"messages", len(snap.Messages),
		"current_version", snap.CurrentVersion)

	if c.isExecuting.Load() {
		slog.Info("skipping submit — agent is already executing")
		return
	}
	if convID == "" {
		return
	}

	// Find the latest USER message to act on.
	var latestUser *v1pb.ChatMessage
	for _, msg := range snap.Messages {
		if msg.SenderType == v1pb.SenderType_SENDER_TYPE_USER && msg.Content != "" {
			latestUser = msg
		}
	}
	if latestUser == nil {
		return
	}

	c.submitAction(stream, convID, latestUser, snap.CurrentVersion)
}

// handleActionResponse is the Phase 2 held-draft handler. Committed actions
// are followed by a CommandRequest (handled elsewhere). Held actions are
// resolved with REVISE by default: the agent re-pulls and re-submits with
// fresh context.
func (c *commandStream) handleActionResponse(_ context.Context, stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], resp *v1pb.ActionResponse) {
	if resp.Committed {
		slog.Info("action committed", "action_id", resp.ActionId, "command_id", resp.CommandId)
		return
	}

	slog.Info("action held — resolving with REVISE",
		"action_id", resp.ActionId,
		"current_version", resp.CurrentVersion,
		"new_messages", len(resp.NewMessages))

	// Update cursor from the held response so the next pull starts from here.
	if len(resp.NewMessages) > 0 {
		convID := resp.NewMessages[0].Conversation
		c.setConversationCursor(convID, resp.CurrentVersion)
	}

	// Resolve with REVISE: the agent will re-pull and re-decide.
	req := &v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_ResolveHeldAction{
			ResolveHeldAction: &v1pb.ResolveHeldAction{
				ActionId:   resp.ActionId,
				Resolution: v1pb.ActionResolution_REVISE,
			},
		},
	}
	if err := stream.Send(req); err != nil {
		slog.Error("failed to send ResolveHeldAction", "action_id", resp.ActionId, "error", err)
	}
}

// submitAction sends a SubmitAction for the given message. It is the
// autonomous execution trigger replacing the Phase 1 manager-driven dispatch.
func (c *commandStream) submitAction(stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], convID string, msg *v1pb.ChatMessage, baseVersion int64) {
	c.isExecuting.Store(true)

	req := &v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_SubmitAction{
			SubmitAction: &v1pb.SubmitAction{
				ConversationId:   convID,
				ReplyToMessageId: msg.Name,
				BaseVersion:      baseVersion,
				Instruction:      msg.Content,
			},
		},
	}
	if err := stream.Send(req); err != nil {
		slog.Error("failed to send SubmitAction", "conversation_id", convID, "error", err)
		c.isExecuting.Store(false)
		return
	}
	slog.Info("submit action sent", "conversation_id", convID, "base_version", baseVersion, "message_id", msg.Name)
}
