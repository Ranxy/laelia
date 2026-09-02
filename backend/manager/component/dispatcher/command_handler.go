package dispatcher

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (d *Dispatcher) CancelCommand(_ context.Context, agentID int, commandID string) error {
	sess, ok := d.registry.getAgent(agentID)

	if !ok {
		return errors.New("agent not connected")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_Cancel{
			Cancel: &v1pb.CancelMessage{
				CommandId: commandID,
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Error("failed to send cancel to agent", "error", err)
		return errors.Wrapf(err, "failed to send cancel to agent")
	}

	slog.Info("cancel sent to agent", "commandID", commandID, "agentID", agentID)
	return nil
}

// SteerCommand injects a follow-up message into the in-flight turn of a
// running command. It is best-effort: executors without mid-turn steering
// support ignore the message.
func (d *Dispatcher) SteerCommand(_ context.Context, agentID int, commandID, text string) error {
	sess, ok := d.registry.getAgent(agentID)

	if !ok {
		return errors.New("agent not connected")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_Steer{
			Steer: &v1pb.SteerMessage{
				CommandId: commandID,
				Text:      text,
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Error("failed to send steer to agent", "error", err)
		return errors.Wrapf(err, "failed to send steer to agent")
	}

	slog.Info("steer sent to agent", "commandID", commandID, "agentID", agentID)
	return nil
}

func (d *Dispatcher) HandleProgress(ctx context.Context, _ int, progress *v1pb.CommandProgress) error {
	commanID, err := uuid.Parse(progress.GetCommandId())
	if err != nil {
		return errors.Wrap(err, "progress commandId parse failed")
	}

	// Prefer the agent-side timestamp carried in the progress; fall back to
	// arrival time for older agents that do not send one.
	ts := progress.GetTimestamp()
	if ts == nil {
		ts = timestamppb.Now()
	}
	createdAt := ts.AsTime()

	if err := d.store.AppendCommandOutput(ctx, commanID, progress.SeqNo, int32(progress.Type), progress.Content, createdAt); err != nil {
		return errors.Wrapf(err, "failed to store command output")
	}

	output := &v1pb.CommandOutput{
		CommandId: progress.CommandId,
		Type:      progress.Type,
		Content:   progress.Content,
		SeqNo:     progress.SeqNo,
		Timestamp: ts,
	}

	d.broadcast(progress.CommandId, output)
	return nil
}

func (d *Dispatcher) HandleEvent(ctx context.Context, event *v1pb.CommandEvent) error {
	cmdID, err := uuid.Parse(event.CommandId)
	if err != nil {
		return errors.Wrapf(err, "invalid command ID in event")
	}

	payloadJSON := "{}"
	data, err := marshalEventPayload(event)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal command event payload")
	}
	if data != nil {
		payloadJSON = string(data)
	}

	if err := d.store.AppendCommandEvent(ctx, &store.CommandEventMessage{
		CommandID:   cmdID,
		SeqNo:       event.SeqNo,
		EventType:   int32(event.Type),
		Summary:     event.Summary,
		PayloadJSON: payloadJSON,
	}); err != nil {
		return errors.Wrapf(err, "failed to store command event")
	}

	// TOKEN_USAGE is additionally denormalized into command_token_usage so
	// agent/principal/time aggregates stay cheap. Failure must not break the
	// event stream: the standalone table is derived data, the event row above
	// is the source of truth.
	if event.Type == v1pb.CommandEventType_TOKEN_USAGE {
		if usage := event.GetTokenUsage(); usage != nil {
			if err := d.store.RecordCommandTokenUsage(ctx, &store.CommandTokenUsageMessage{
				CommandID:        cmdID,
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
				TotalTokens:      usage.TotalTokens,
			}); err != nil {
				slog.Error("failed to record command token usage", "commandID", event.CommandId, "error", err)
			}
		}
	}

	if err := d.store.UpdateCommandAckSeq(ctx, cmdID, event.SeqNo); err != nil {
		slog.Error("failed to update command ack seq from event", "commandID", event.CommandId, "error", err)
	}

	d.broadcastEvent(event.CommandId, event)
	return nil
}

func (d *Dispatcher) HandleResult(ctx context.Context, agentID int, result *v1pb.CommandResult) error {
	cmdID, err := uuid.Parse(result.CommandId)
	if err != nil {
		return errors.Wrapf(err, "invalid command ID in result")
	}

	sess, ok := d.registry.getAgent(agentID)

	if ok {
		sess.mu.Lock()
		if sess.currentCmdID == result.CommandId {
			sess.currentCmdID = ""
		}
		sess.mu.Unlock()
	}

	status := int32(v1pb.CommandStatus_COMPLETED)
	errorMsg := result.ErrorMessage
	if result.ExitCode != 0 {
		status = int32(v1pb.CommandStatus_FAILED)
	}

	now := time.Now()
	completedAt := &now
	durationMs := result.DurationMs
	exitCode := result.ExitCode

	if err := d.store.UpdateCommandStatus(ctx, cmdID, status, nil, completedAt, &exitCode, &durationMs, errorMsg); err != nil {
		return errors.Wrapf(err, "failed to update command result")
	}

	if err := d.store.UpdateCommandAckSeq(ctx, cmdID, result.LastSeqNo); err != nil {
		slog.Error("failed to update ack seq", "commandID", cmdID, "error", err)
	}

	resultJSON := ""
	if result.Result != nil {
		data, err := protojson.Marshal(result.Result)
		if err != nil {
			slog.Error("failed to marshal command result struct", "commandID", result.CommandId, "error", err)
		} else {
			resultJSON = string(data)
		}
	}
	if err := d.store.UpdateCommandResultSummary(ctx, cmdID, result.FinalSummary, resultJSON); err != nil {
		slog.Error("failed to update command result summary", "commandID", cmdID, "error", err)
	}

	output := &v1pb.CommandOutput{
		CommandId: result.CommandId,
		Type:      v1pb.CommandOutput_SYSTEM,
		Content:   formatResultMessage(result),
		SeqNo:     result.LastSeqNo + 1,
		Timestamp: timestamppb.Now(),
	}
	d.broadcast(result.CommandId, output)

	d.wgMu.Lock()
	d.wg.Add(1)
	d.wgMu.Unlock()
	go func() {
		defer d.wg.Done()
		select {
		case <-d.lifecycleCtx.Done():
			return
		case <-time.After(100 * time.Millisecond):
			d.closeWatchers(result.CommandId)
			d.closeEventWatchers(result.CommandId)
		}
	}()

	slog.Info("command completed", "commandID", result.CommandId, "exitCode", result.ExitCode, "duration_ms", result.DurationMs)

	// The agent's autonomous drain loop decides whether to open another
	// session (BeginSession will report idle if no channel has updates), so
	// the manager no longer pushes the next command here.
	return nil
}

func formatResultMessage(result *v1pb.CommandResult) string {
	if result.ErrorMessage != "" {
		return result.ErrorMessage
	}
	return ""
}

func ConvertChatMessageToV1(m *store.ChatMessage) *v1pb.ChatMessage {
	cm := &v1pb.ChatMessage{
		Name:          m.ID.String(),
		Conversation:  m.ConversationID.String(),
		PrincipalName: m.PrincipalName,
		Role:          m.Role,
		Content:       m.Content,
		CreatedAt:     timestamppb.New(m.CreatedAt),
		SenderName:    m.AgentName,
		SenderType:    v1pb.SenderType(m.SenderType),
		PrincipalId:   strconv.Itoa(m.PrincipalID),
		RoomVersion:   m.RoomVersion,
		Mentions:      m.Mentions,
	}
	if m.CommandID.Valid {
		cm.CommandId = m.CommandID.UUID.String()
	}
	if m.SenderType != store.SenderTypeAgent {
		cm.SenderName = m.PrincipalName
	}
	return cm
}

func marshalEventPayload(event *v1pb.CommandEvent) ([]byte, error) {
	switch event.Type {
	case v1pb.CommandEventType_LIFECYCLE:
		return protojson.Marshal(event.GetLifecycle())
	case v1pb.CommandEventType_TEXT_DELTA:
		return protojson.Marshal(event.GetTextDelta())
	case v1pb.CommandEventType_TOOL_CALL_STARTED:
		return protojson.Marshal(event.GetToolCallStarted())
	case v1pb.CommandEventType_TOOL_CALL_FINISHED:
		return protojson.Marshal(event.GetToolCallFinished())
	case v1pb.CommandEventType_DIFF_EMITTED:
		return protojson.Marshal(event.GetDiffEmitted())
	case v1pb.CommandEventType_WARNING:
		return protojson.Marshal(event.GetWarning())
	case v1pb.CommandEventType_RAW_ACP:
		return protojson.Marshal(event.GetRawAcp())
	case v1pb.CommandEventType_FINAL_SUMMARY:
		return protojson.Marshal(event.GetFinalSummary())
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		return protojson.Marshal(event.GetContextCompaction())
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		return protojson.Marshal(event.GetContextUsage())
	case v1pb.CommandEventType_TOKEN_USAGE:
		return protojson.Marshal(event.GetTokenUsage())
	default:
		return nil, nil
	}
}
