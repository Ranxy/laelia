package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
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

func (m *mergedText) flush(stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], commandID string, state *executor.LocalState) error {
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
		Timestamp:  time.Now(),
		Payload: map[string]any{
			"stream_type": m.streamType.String(),
			"content":     text,
		},
	}
	return sendCommandEvent(stream, commandID, &event)
}

type commandStream struct {
	client     v1connect.AgentCommandServiceClient
	managerURL string
	backoff    *ExponentialBackoff
	getToken   func() string
	getSessID  func() string
	acpConfig  *executor.ACPConfig
}

func newCommandStream(httpClient *http.Client, managerURL string, acpConfig *executor.ACPConfig) *commandStream {
	return &commandStream{
		client:     v1connect.NewAgentCommandServiceClient(httpClient, managerURL),
		managerURL: managerURL,
		backoff:    NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		acpConfig:  acpConfig,
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

	stream := c.client.CommandChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	ready := &v1pb.AgentCommandMessage{
		Message: &v1pb.AgentCommandMessage_AgentReady{
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
			case *v1pb.ManagerCommandMessage_CommandRequest:
				req := m.CommandRequest
				slog.Info("received command", "commandID", req.CommandId, "command", req.Command)

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
					continue
				}

				currentExecutor = runtime
				go c.runCommand(ctx, runtime, stream, req)

			case *v1pb.ManagerCommandMessage_Cancel:
				if currentExecutor != nil {
					slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
					currentExecutor.Cancel()
				}

			case *v1pb.ManagerCommandMessage_Pong:
				// pong received, link acknowledged

			case *v1pb.ManagerCommandMessage_PermissionDecision:
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
			ping := &v1pb.AgentCommandMessage{
				Message: &v1pb.AgentCommandMessage_Ping{
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
	stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage],
	req *v1pb.CommandRequest,
) {
	commandID := req.CommandId
	state := &executor.LocalState{
		CommandID:        commandID,
		ExecutorKind:     req.ExecutorKind.String(),
		Profile:          req.Profile,
		Status:           "running",
		StartedAt:        time.Now().UnixMilli(),
		LastSeqSent:      0,
		LastEventSeqSent: 0,
	}
	if err := executor.SaveLocalState(state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

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
		SeqNo:     startSeq,
		Type:      v1pb.CommandEventType_LIFECYCLE,
		Summary:   "command started",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"executor_kind": req.ExecutorKind.String(),
			"profile":       req.Profile,
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

			// Drain any remaining output chunks before reading the result.
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

func drainOutput(runtime executor.Runtime, stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], commandID string, lastSeqNo int32, lastEventSeqNo int32, merged *mergedText) (int32, int32) {
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
	kind := req.ExecutorKind
	if kind == v1pb.ExecutorKind_EXECUTOR_KIND_UNSPECIFIED {
		kind = v1pb.ExecutorKind_ACP
	}

	switch kind {
	case v1pb.ExecutorKind_SHELL:
		return executor.New(req.Command, req.Env, req.WorkingDir, req.TimeoutSeconds), nil
	case v1pb.ExecutorKind_ACP:
		return executor.NewACP(executor.Request{
			CommandID:      req.CommandId,
			Command:        req.Command,
			Instruction:    req.Instruction,
			Profile:        req.Profile,
			WorkingDir:     req.WorkingDir,
			Env:            req.Env,
			TimeoutSeconds: req.TimeoutSeconds,
			ExecutorKind:   kind,
			AllowDiff:      req.AllowDiff,
		}, c.acpConfig)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, io.ErrUnexpectedEOF)
	}
}

func sendCommandProgress(stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], commandID string, chunk executor.OutputChunk) error {
	return stream.Send(&v1pb.AgentCommandMessage{
		Message: &v1pb.AgentCommandMessage_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				Type:      chunk.StreamType,
				Content:   chunk.Content,
				SeqNo:     chunk.SeqNo,
			},
		},
	})
}

func sendCommandEvent(stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], commandID string, event *executor.Event) error {
	payload, err := structpb.NewStruct(normalizePayload(event.Payload))
	if err != nil {
		return err
	}
	return stream.Send(&v1pb.AgentCommandMessage{
		Message: &v1pb.AgentCommandMessage_Event{
			Event: &v1pb.CommandEvent{
				CommandId: commandID,
				SeqNo:     event.SeqNo,
				Type:      event.Type,
				Summary:   event.Summary,
				Payload:   payload,
				Timestamp: timestamppb.New(event.Timestamp),
			},
		},
	})
}

func normalizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	for k, v := range payload {
		payload[k] = normalizeValue(v)
	}
	return payload
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizePayload(t)
	case []map[string]any:
		converted := make([]any, len(t))
		for i, m := range t {
			converted[i] = normalizePayload(m)
		}
		return converted
	case []any:
		for i, item := range t {
			t[i] = normalizeValue(item)
		}
		return t
	default:
		return v
	}
}

func sendCommandResult(stream *connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], result *v1pb.CommandResult) error {
	return stream.Send(&v1pb.AgentCommandMessage{
		Message: &v1pb.AgentCommandMessage_Result{
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
