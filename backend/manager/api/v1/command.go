package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/roomhub"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type CommandService struct {
	v1connect.UnimplementedCommandServiceHandler
	store           *store.Store
	dispatcher      *dispatcher.Dispatcher
	s3clientManager *s3client.Client
	iam             *iam.Manager
	roomhub         *roomhub.Hub
}

func NewCommandService(s *store.Store, d *dispatcher.Dispatcher, s3clientManager *s3client.Client, iamManager *iam.Manager, hub *roomhub.Hub) *CommandService {
	return &CommandService{store: s, dispatcher: d, s3clientManager: s3clientManager, iam: iamManager, roomhub: hub}
}

func (s *CommandService) ListCommands(ctx context.Context, req *connect.Request[v1pb.ListCommandsRequest]) (*connect.Response[v1pb.ListCommandsResponse], error) {
	agentResourceID := parseAgentResourceID(req.Msg.Agent)

	agent, err := s.store.GetAgentByResourceID(ctx, agentResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get agent"))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", agentResourceID))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindCommandMessage{
		AgentID: &agent.ID,
		Limit:   &limitPlusOne,
		Offset:  &offset.offset,
	}

	if req.Msg.Status != v1pb.CommandStatus_COMMAND_STATUS_UNSPECIFIED {
		status := int32(req.Msg.Status)
		find.Status = &status
	}

	commands, err := s.store.ListCommands(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list commands"))
	}

	nextPageToken := ""
	if len(commands) == limitPlusOne {
		commands = commands[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	var v1Commands []*v1pb.Command
	for _, cmd := range commands {
		v1Commands = append(v1Commands, convertToV1Command(cmd))
	}

	return connect.NewResponse(&v1pb.ListCommandsResponse{
		Commands:      v1Commands,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) GetCommand(ctx context.Context, req *connect.Request[v1pb.GetCommandRequest]) (*connect.Response[v1pb.Command], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(convertToV1Command(cmd)), nil
}

func (s *CommandService) CancelCommand(ctx context.Context, req *connect.Request[v1pb.CancelCommandRequest]) (*connect.Response[v1pb.Command], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if cmd.Status != store.CommandStatusPending && cmd.Status != store.CommandStatusRunning {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("command is not in pending or running state"))
	}

	if err := s.dispatcher.CancelCommand(ctx, cmd.AgentID, cmd.ID.String()); err != nil {
		// agent may not be connected, still proceed to cancel in DB
		slog.Warn("failed to send cancel to agent", "commandID", cmd.ID, "error", err)
	}

	status := store.CommandStatusCancelled
	if err := s.store.UpdateCommandStatus(ctx, cmd.ID, status, nil, nil, nil, nil, ""); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update command status"))
	}

	cmd.Status = status
	return connect.NewResponse(convertToV1Command(cmd)), nil
}

// SteerCommand injects a follow-up message into the in-flight turn of a
// running command. Unlike CancelCommand it does not change the command's DB
// state — it is a pure best-effort push to the agent; executors that do not
// support mid-turn steering ignore it. A non-running command or an agent that
// is not connected surfaces an error so the caller knows the steer did not go
// through.
func (s *CommandService) SteerCommand(ctx context.Context, req *connect.Request[v1pb.SteerCommandRequest]) (*connect.Response[v1pb.Command], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if cmd.Status != store.CommandStatusRunning {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("command is not in running state"))
	}
	text := strings.TrimSpace(req.Msg.Text)
	if text == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("steer text must not be empty"))
	}
	if err := s.dispatcher.SteerCommand(ctx, cmd.AgentID, cmd.ID.String(), text); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}
	return connect.NewResponse(convertToV1Command(cmd)), nil
}

func (s *CommandService) WatchCommand(ctx context.Context, req *connect.Request[v1pb.WatchCommandRequest], stream *connect.ServerStream[v1pb.CommandOutput]) error {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, err)
	}

	afterSeq := req.Msg.AfterSeqNo

	historicalOutputs, err := s.store.GetCommandOutput(ctx, cmd.ID, afterSeq)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get historical output"))
	}

	for _, o := range historicalOutputs {
		if err := stream.Send(&v1pb.CommandOutput{
			CommandId: o.CommandID.String(),
			Type:      v1pb.CommandOutput_StreamType(o.StreamType),
			Content:   o.Content,
			SeqNo:     o.SeqNo,
			Timestamp: timestamppb.New(o.CreatedAt),
		}); err != nil {
			return err
		}
	}

	if cmd.Status != store.CommandStatusPending && cmd.Status != store.CommandStatusRunning {
		return nil
	}

	ch, err := s.dispatcher.Subscribe(ctx, cmd.ID.String())
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to subscribe"))
	}
	defer s.dispatcher.Unsubscribe(cmd.ID.String(), ch)

	for {
		select {
		case <-ctx.Done():
			return nil
		case output, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(output); err != nil {
				return err
			}
		}
	}
}

func (s *CommandService) WatchCommandEvents(ctx context.Context, req *connect.Request[v1pb.WatchCommandEventsRequest], stream *connect.ServerStream[v1pb.CommandEvent]) error {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, err)
	}

	user, _ := GetUserFromContext(ctx)
	if err := s.validateRawEventAccess(ctx, user); err != nil {
		return err
	}

	historicalEvents, err := s.store.GetCommandEvents(ctx, cmd.ID, req.Msg.AfterSeqNo)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get historical command events"))
	}

	for _, event := range historicalEvents {
		if err := stream.Send(convertToV1CommandEvent(event)); err != nil {
			return err
		}
	}

	if cmd.Status != store.CommandStatusPending && cmd.Status != store.CommandStatusRunning {
		return nil
	}

	ch, err := s.dispatcher.SubscribeEvents(ctx, cmd.ID.String())
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to subscribe command events"))
	}
	defer s.dispatcher.UnsubscribeEvents(cmd.ID.String(), ch)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

func parseConversationID(conversation string) (uuid.UUID, error) {
	parts := strings.Split(conversation, "/")
	if len(parts) == 2 && parts[0] == "conversations" {
		return uuid.Parse(parts[1])
	}
	return uuid.Parse(conversation)
}

func parseAgentResourceID(agent string) string {
	parts := strings.Split(agent, "/")
	if len(parts) >= 4 && parts[0] == "agents" {
		return parts[1]
	}
	if len(parts) == 2 && parts[0] == "agents" {
		return parts[1]
	}
	return agent
}

func convertToV1Command(cmd *store.CommandMessage) *v1pb.Command {
	v1cmd := &v1pb.Command{
		Name:          formatCommandName(cmd.AgentResourceID, cmd.ID),
		Agent:         formatAgentName(cmd.AgentResourceID),
		PrincipalId:   formatPrincipalID(cmd.PrincipalID),
		PrincipalName: cmd.PrincipalName,
		Command:       cmd.Command,
		Instruction:   cmd.Instruction,
		Profile:       cmd.Profile,
		AllowDiff:     cmd.AllowDiff,
		Status:        v1pb.CommandStatus(cmd.Status),
		CreatedAt:     timestamppb.New(cmd.CreatedAt),
		ErrorMessage:  cmd.ErrorMessage,
		FinalSummary:  cmd.FinalSummary,
		WorkingDir:    cmd.WorkingDir,
	}

	if cmd.ConversationID != nil {
		v1cmd.ConversationId = cmd.ConversationID.String()
	}

	if cmd.ExitCode.Valid {
		v1cmd.ExitCode = cmd.ExitCode.Int32
	}
	if cmd.DurationMs.Valid {
		v1cmd.DurationMs = cmd.DurationMs.Int64
	}
	if cmd.StartedAt.Valid {
		v1cmd.StartedAt = timestamppb.New(cmd.StartedAt.Time)
	}
	if cmd.CompletedAt.Valid {
		v1cmd.CompletedAt = timestamppb.New(cmd.CompletedAt.Time)
	}

	if cmd.Env != "" && cmd.Env != "{}" {
		var envMap map[string]string
		if err := json.Unmarshal([]byte(cmd.Env), &envMap); err == nil {
			v1cmd.Env = envMap
		}
	}
	if cmd.ResultJSON != "" && cmd.ResultJSON != "{}" {
		result := &structpb.Struct{}
		if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(cmd.ResultJSON), result); err == nil {
			v1cmd.Result = result
		}
	}

	return v1cmd
}

func convertToV1CommandEvent(event *store.CommandEventMessage) *v1pb.CommandEvent {
	v1Event := &v1pb.CommandEvent{
		CommandId: event.CommandID.String(),
		SeqNo:     event.SeqNo,
		Type:      v1pb.CommandEventType(event.EventType),
		Summary:   event.Summary,
		Timestamp: timestamppb.New(event.CreatedAt),
	}
	if event.PayloadJSON != "" && event.PayloadJSON != "{}" {
		data := []byte(event.PayloadJSON)
		switch v1pb.CommandEventType(event.EventType) {
		case v1pb.CommandEventType_LIFECYCLE:
			p := &v1pb.LifecyclePayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_Lifecycle{Lifecycle: p}
			}
		case v1pb.CommandEventType_TEXT_DELTA:
			p := &v1pb.TextDeltaPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_TextDelta{TextDelta: p}
			}
		case v1pb.CommandEventType_TOOL_CALL_STARTED:
			p := &v1pb.ToolCallStartedPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_ToolCallStarted{ToolCallStarted: p}
			}
		case v1pb.CommandEventType_TOOL_CALL_FINISHED:
			p := &v1pb.ToolCallFinishedPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_ToolCallFinished{ToolCallFinished: p}
			}
		case v1pb.CommandEventType_DIFF_EMITTED:
			p := &v1pb.DiffEmittedPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_DiffEmitted{DiffEmitted: p}
			}
		case v1pb.CommandEventType_WARNING:
			p := &v1pb.WarningPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_Warning{Warning: p}
			}
		case v1pb.CommandEventType_RAW_ACP:
			p := &v1pb.RawAcpPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_RawAcp{RawAcp: p}
			}
		case v1pb.CommandEventType_FINAL_SUMMARY:
			p := &v1pb.FinalSummaryPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_FinalSummary{FinalSummary: p}
			}
		case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
			p := &v1pb.ContextCompactionPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_ContextCompaction{ContextCompaction: p}
			}
		case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
			p := &v1pb.ContextUsagePayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_ContextUsage{ContextUsage: p}
			}
		default:
		}
	}
	return v1Event
}

func formatCommandName(agentResourceID string, commandID uuid.UUID) string {
	return "agents/" + agentResourceID + "/commands/" + commandID.String()
}

func formatAgentName(agentResourceID string) string {
	return "agents/" + agentResourceID
}

func formatPrincipalID(id int) string {
	return fmt.Sprintf("%d", id)
}

func (s *CommandService) validateRawEventAccess(ctx context.Context, user *store.UserMessage) error {
	if user == nil {
		return nil
	}

	ok, err := s.iam.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil)
	if err != nil {
		slog.Warn("failed to check reviewAll for raw events", "error", err, "user", user.Email)
		return connect.NewError(connect.CodeInternal, errors.New("failed to verify permissions"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("only users with conversations.reviewAll can view structured command events"))
	}

	return nil
}

func (s *CommandService) GetCommandContext(ctx context.Context, req *connect.Request[v1pb.GetCommandContextRequest]) (*connect.Response[v1pb.GetCommandContextResponse], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	outputs, err := s.store.GetCommandOutput(ctx, cmd.ID, 0)
	if err != nil {
		slog.Warn("failed to get command outputs for context", "commandID", cmd.ID, "error", err)
	}

	events, err := s.store.GetCommandEvents(ctx, cmd.ID, 0)
	if err != nil {
		slog.Warn("failed to get command events for context", "commandID", cmd.ID, "error", err)
	}

	var v1Outputs []*v1pb.CommandOutput
	for _, o := range outputs {
		v1Outputs = append(v1Outputs, &v1pb.CommandOutput{
			CommandId: o.CommandID.String(),
			Type:      v1pb.CommandOutput_StreamType(o.StreamType),
			Content:   o.Content,
			SeqNo:     o.SeqNo,
			Timestamp: timestamppb.New(o.CreatedAt),
		})
	}

	var v1Events []*v1pb.CommandEvent
	for _, e := range events {
		v1Events = append(v1Events, convertToV1CommandEvent(e))
	}

	return connect.NewResponse(&v1pb.GetCommandContextResponse{
		Command: convertToV1Command(cmd),
		Outputs: v1Outputs,
		Events:  v1Events,
	}), nil
}

// ListChannelUpdates is the agent's "what's worth my context" discovery (AX
// Agent Inbox). It returns every conversation the authenticated agent is a
// member of whose room_version is beyond the agent's durable per-channel
// cursor, with the current version, the agent's processed version, and the
// count of new messages. The autonomous drain loop calls this first every
// session; an empty list means the agent is idle.
func (s *CommandService) ListChannelUpdates(ctx context.Context, _ *connect.Request[v1pb.ListChannelUpdatesRequest]) (*connect.Response[v1pb.ListChannelUpdatesResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	updates, err := s.store.ListChannelsWithUpdates(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channel updates"))
	}

	var v1Updates []*v1pb.ChannelUpdate
	for _, u := range updates {
		v1Updates = append(v1Updates, &v1pb.ChannelUpdate{
			Conversation:     fmt.Sprintf("conversations/%s", u.ConversationID.String()),
			CurrentVersion:   u.CurrentVersion,
			ProcessedVersion: u.ProcessedVersion,
			NewMessageCount:  u.NewMessageCount,
		})
	}

	return connect.NewResponse(&v1pb.ListChannelUpdatesResponse{Updates: v1Updates}), nil
}

// ListThreadUpdates is the agent's thread inbox. It returns every thread the
// authenticated agent is subscribed to (via @mention or having replied) that
// has replies beyond the agent's per-channel cursor for that conversation. The
// drain loop runs this after ListChannelUpdates and before acking, so a
// subscribed agent catches up on every thread it cares about.
func (s *CommandService) ListThreadUpdates(ctx context.Context, _ *connect.Request[v1pb.ListThreadUpdatesRequest]) (*connect.Response[v1pb.ListThreadUpdatesResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	updates, err := s.store.ListSubscribedThreadUpdates(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list thread updates"))
	}

	var v1Updates []*v1pb.ThreadUpdate
	for _, u := range updates {
		v1Updates = append(v1Updates, &v1pb.ThreadUpdate{
			Conversation:  fmt.Sprintf("conversations/%s", u.ConversationID.String()),
			ThreadRoot:    u.RootMessageID.String(),
			LatestVersion: u.LatestVersion,
			NewReplyCount: u.NewReplyCount,
		})
	}

	return connect.NewResponse(&v1pb.ListThreadUpdatesResponse{Updates: v1Updates}), nil
}

// AckProcessedVersion advances the agent's durable per-channel cursor to
// processed_version (monotonic — a stale ack cannot rewind progress). When
// command_id is supplied, it links the current session's command to this
// conversation so the frontend can associate execution events with the
// channel. The agent MUST call this after finishing a channel (reply or
// deliberate silence) so the next ListChannelUpdates no longer reports it.
func (s *CommandService) AckProcessedVersion(ctx context.Context, req *connect.Request[v1pb.AckProcessedVersionRequest]) (*connect.Response[v1pb.AckProcessedVersionResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	convUUID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
	}
	if req.Msg.ProcessedVersion <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("processed_version must be positive"))
	}

	result, err := s.store.UpsertCursor(ctx, agent.ID, convUUID, req.Msg.ProcessedVersion)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to ack processed version"))
	}

	// Link the session's command to this conversation. LinkCommandConversation is
	// an idempotent insert (+ first-wins column set) so a multi-channel turn that
	// acks in several conversations links its command to each of them. A
	// missing/empty command_id (e.g. an ack outside a session) is ignored.
	if req.Msg.CommandId != "" {
		if cid, parseErr := uuid.Parse(req.Msg.CommandId); parseErr == nil {
			if linkErr := s.store.LinkCommandConversation(ctx, cid, convUUID); linkErr != nil {
				slog.Warn("failed to link command to conversation", "commandID", req.Msg.CommandId, "conversationID", convUUID, "error", linkErr)
			}
		}
	}

	return connect.NewResponse(&v1pb.AckProcessedVersionResponse{ProcessedVersion: result}), nil
}
