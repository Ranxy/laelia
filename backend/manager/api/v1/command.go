package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type CommandService struct {
	v1connect.UnimplementedCommandServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
	acpEnabled bool
}

func NewCommandService(s *store.Store, d *dispatcher.Dispatcher) *CommandService {
	return &CommandService{store: s, dispatcher: d, acpEnabled: true}
}

func (s *CommandService) SetACPEnabled(enabled bool) {
	s.acpEnabled = enabled
}

func (s *CommandService) SendCommand(ctx context.Context, req *connect.Request[v1pb.SendCommandRequest]) (*connect.Response[v1pb.Command], error) {
	executorKind := req.Msg.ExecutorKind
	if executorKind == v1pb.ExecutorKind_EXECUTOR_KIND_UNSPECIFIED {
		executorKind = v1pb.ExecutorKind_ACP
	}
	if executorKind == v1pb.ExecutorKind_ACP {
		if req.Msg.Instruction == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("instruction must not be empty for ACP tasks"))
		}
	}

	commandText := req.Msg.Command
	instruction := req.Msg.Instruction

	if executorKind == v1pb.ExecutorKind_SHELL && commandText == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("command must not be empty"))
	}

	agentResourceID := parseAgentResourceID(req.Msg.Agent)
	agent, err := s.store.GetAgentByResourceID(ctx, agentResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get agent"))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", agentResourceID))
	}

	user, _ := GetUserFromContext(ctx)
	principalID := 1 // system bot default
	principalName := "system"
	if user != nil {
		principalID = user.ID
		principalName = user.Name
	}

	if err := s.validateACPCapability(ctx, agent, executorKind, req.Msg.Profile, req.Msg.AllowDiff, req.Msg.TimeoutSeconds, user); err != nil {
		return nil, err
	}

	var conversationID *uuid.UUID
	if req.Msg.Source == v1pb.CommandSource_CHAT && instruction != "" {
		if req.Msg.ConversationId != "" {
			if cid, cidErr := uuid.Parse(req.Msg.ConversationId); cidErr == nil {
				conversationID = &cid
			}
		}
		if conversationID == nil {
			conv, convErr := s.store.GetOrCreateDirectConversation(ctx, agent.ID, principalID)
			if convErr != nil {
				slog.Warn("failed to get or create conversation", "error", convErr)
			} else {
				conversationID = &conv.ID
			}
		}
		if conversationID != nil {
			if _, msgErr := s.store.CreateChatMessage(ctx, &store.ChatMessage{
				ConversationID: *conversationID,
				PrincipalID:    principalID,
				Role:           1, // USER
				Content:        instruction,
			}); msgErr != nil {
				slog.Warn("failed to create user chat message", "error", msgErr)
			}
			if recent, recentErr := s.store.GetRecentChatMessages(ctx, *conversationID, 6); recentErr == nil && len(recent) > 0 {
				if chatCtx := buildLightChatContext(recent); chatCtx != "" {
					instruction = chatCtx + "\n---\n" + instruction
				}
			}
		}
	}

	envBytes, err := json.Marshal(req.Msg.Env)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to marshal env"))
	}

	cmd := &store.CommandMessage{
		AgentID:        agent.ID,
		PrincipalID:    principalID,
		Command:        commandText,
		Instruction:    instruction,
		ExecutorKind:   int32(executorKind),
		AllowDiff:      req.Msg.AllowDiff,
		Status:         1, // PENDING
		Env:            string(envBytes),
		WorkingDir:     req.Msg.WorkingDir,
		TimeoutSeconds: req.Msg.TimeoutSeconds,
		SourceType:     int32(req.Msg.Source),
		ConversationID: conversationID,
	}

	created, err := s.store.CreateCommand(ctx, cmd)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create command"))
	}

	if _, inboxErr := s.store.CreateInboxItem(ctx, agent.ID, created.ID, 0, buildInboxSummary(created)); inboxErr != nil {
		slog.Warn("failed to create inbox item", "commandID", created.ID, "error", inboxErr)
	} else {
		s.dispatcher.NotifyInboxUpdated(ctx, agent.ID)
	}

	created.PrincipalName = principalName
	created.AgentResourceID = agent.ResourceID

	return connect.NewResponse(convertToV1Command(created)), nil
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

	if cmd.Status != 1 && cmd.Status != 2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("command is not in pending or running state"))
	}

	if err := s.dispatcher.CancelCommand(ctx, cmd.AgentID, cmd.ID.String()); err != nil {
		// agent may not be connected, still proceed to cancel in DB
		slog.Warn("failed to send cancel to agent", "commandID", cmd.ID, "error", err)
	}

	if inboxErr := s.store.DeleteInboxItemByCommandID(ctx, cmd.ID); inboxErr != nil {
		slog.Warn("failed to delete inbox item for cancelled command", "error", inboxErr)
	}

	status := int32(v1pb.CommandStatus_CANCELLED)
	if err := s.store.UpdateCommandStatus(ctx, cmd.ID, status, nil, nil, nil, nil, ""); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update command status"))
	}

	cmd.Status = status
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

	if cmd.Status != 1 && cmd.Status != 2 {
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

	if cmd.Status != 1 && cmd.Status != 2 {
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

func (s *CommandService) RespondPermission(ctx context.Context, req *connect.Request[v1pb.RespondPermissionRequest]) (*connect.Response[emptypb.Empty], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if cmd == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("command %s not found", req.Msg.Name))
	}

	if cmd.Status != 2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("command is not running"))
	}

	if req.Msg.OptionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("option_id must not be empty"))
	}

	if err := s.dispatcher.RespondPermission(ctx, cmd.AgentID, cmd.ID.String(), req.Msg.OptionId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to respond to permission"))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
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
		ExecutorKind:  v1pb.ExecutorKind(cmd.ExecutorKind),
		AllowDiff:     cmd.AllowDiff,
		Status:        v1pb.CommandStatus(cmd.Status),
		CreatedAt:     timestamppb.New(cmd.CreatedAt),
		ErrorMessage:  cmd.ErrorMessage,
		FinalSummary:  cmd.FinalSummary,
		WorkingDir:    cmd.WorkingDir,
		Source:        v1pb.CommandSource(cmd.SourceType),
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
		case v1pb.CommandEventType_PERMISSION_REQUESTED:
			p := &v1pb.PermissionRequestedPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_PermissionRequested{PermissionRequested: p}
			}
		case v1pb.CommandEventType_PERMISSION_TIMED_OUT:
			p := &v1pb.PermissionTimedOutPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_PermissionTimedOut{PermissionTimedOut: p}
			}
		case v1pb.CommandEventType_PERMISSION_DECIDED:
			p := &v1pb.PermissionDecidedPayload{}
			if err := common.ProtojsonUnmarshaler.Unmarshal(data, p); err == nil {
				v1Event.Payload = &v1pb.CommandEvent_PermissionDecided{PermissionDecided: p}
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

func (s *CommandService) validateACPCapability(ctx context.Context, agent *store.AgentMessage, kind v1pb.ExecutorKind, _ string, allowDiff bool, timeoutSeconds int32, user *store.UserMessage) error {
	if kind != v1pb.ExecutorKind_ACP {
		return nil
	}

	capability := agent.Info.GetCapability()
	if capability == nil || !capability.SupportsAcp {
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("agent %s does not support ACP tasks", agent.ResourceID))
	}

	if timeoutSeconds > 0 && capability.MaxTimeoutSeconds > 0 && timeoutSeconds > capability.MaxTimeoutSeconds {
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("timeout %ds exceeds agent max %ds", timeoutSeconds, capability.MaxTimeoutSeconds))
	}

	if allowDiff && !capability.SupportsDiff {
		return connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("agent %s does not support diff events", agent.ResourceID))
	}

	if user == nil {
		return nil
	}

	if !s.acpEnabled || allowDiff {
		isAdmin, checkErr := isUserWorkspaceAdmin(ctx, s.store, user)
		if checkErr != nil {
			slog.Warn("failed to check workspace admin for ACP task", "error", checkErr, "user", user.Email)
			return connect.NewError(connect.CodeInternal, errors.New("failed to verify permissions"))
		}
		if !isAdmin {
			if allowDiff {
				return connect.NewError(connect.CodePermissionDenied,
					errors.New("only workspace admins can send ACP tasks with diff support"))
			}
			return connect.NewError(connect.CodePermissionDenied,
				errors.New("ACP tasks are currently restricted to workspace admins"))
		}
	}

	return nil
}

func (s *CommandService) validateRawEventAccess(ctx context.Context, user *store.UserMessage) error {
	if user == nil {
		return nil
	}

	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		slog.Warn("failed to check workspace admin for raw events", "error", err, "user", user.Email)
		return connect.NewError(connect.CodeInternal, errors.New("failed to verify permissions"))
	}
	if !isAdmin {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("only workspace admins can view structured command events"))
	}

	return nil
}

func (s *CommandService) SearchChatHistory(ctx context.Context, req *connect.Request[v1pb.SearchChatHistoryRequest]) (*connect.Response[v1pb.SearchChatHistoryResponse], error) {
	var convID uuid.UUID

	if req.Msg.Conversation != "" {
		cid, err := parseConversationID(req.Msg.Conversation)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation"))
		}
		convID = cid
	} else {
		agentResourceID := parseAgentResourceID(req.Msg.Agent)
		agent, err := s.store.GetAgentByResourceID(ctx, agentResourceID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get agent"))
		}
		if agent == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", agentResourceID))
		}
		user, _ := GetUserFromContext(ctx)
		principalID := 1
		if user != nil {
			principalID = user.ID
		}
		conv, convErr := s.store.GetOrCreateDirectConversation(ctx, agent.ID, principalID)
		if convErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get conversation"))
		}
		convID = conv.ID
	}

	var since, until *time.Time
	if req.Msg.Since != nil {
		st := req.Msg.Since.AsTime()
		since = &st
	}
	if req.Msg.Until != nil {
		ut := req.Msg.Until.AsTime()
		until = &ut
	}

	limit := int(req.Msg.Limit)
	entries, err := s.store.SearchChatHistory(ctx, convID, req.Msg.Query, since, until, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to search chat history"))
	}

	var v1Entries []*v1pb.ChatHistoryEntry
	for _, e := range entries {
		v1Entries = append(v1Entries, &v1pb.ChatHistoryEntry{
			MessageId: e.MessageID,
			CommandId: e.CommandID,
			Role:      e.Role,
			Content:   e.Content,
			CreatedAt: timestamppb.New(e.CreatedAt),
		})
	}

	return connect.NewResponse(&v1pb.SearchChatHistoryResponse{Entries: v1Entries}), nil
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

func (s *CommandService) GetOrCreateConversation(ctx context.Context, req *connect.Request[v1pb.GetOrCreateConversationRequest]) (*connect.Response[v1pb.GetOrCreateConversationResponse], error) {
	agentResourceID := parseAgentResourceID(req.Msg.Agent)
	agent, err := s.store.GetAgentByResourceID(ctx, agentResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get agent"))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", agentResourceID))
	}

	user, _ := GetUserFromContext(ctx)
	principalID := 1
	if user != nil {
		principalID = user.ID
	}

	conv, err := s.store.GetOrCreateDirectConversation(ctx, agent.ID, principalID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get or create conversation"))
	}

	return connect.NewResponse(&v1pb.GetOrCreateConversationResponse{
		Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
	}), nil
}

func (s *CommandService) ListConversationMessages(ctx context.Context, req *connect.Request[v1pb.ListConversationMessagesRequest]) (*connect.Response[v1pb.ListConversationMessagesResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
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

	msgs, err := s.store.ListConversationMessages(ctx, convID, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
	}

	nextPageToken := ""
	if len(msgs) == limitPlusOne {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	var v1msgs []*v1pb.ChatMessage
	for _, msg := range msgs {
		senderName := msg.PrincipalName
		senderType := int32(1)
		if msg.SenderAgentID.Valid {
			senderName = msg.AgentName
			senderType = 2
		}
		v1m := &v1pb.ChatMessage{
			Name:          msg.ID.String(),
			Conversation:  msg.ConversationID.String(),
			PrincipalName: msg.PrincipalName,
			Role:          msg.Role,
			Content:       msg.Content,
			CreatedAt:     timestamppb.New(msg.CreatedAt),
			SenderName:    senderName,
			SenderType:    senderType,
		}
		if msg.CommandID.Valid {
			v1m.CommandId = msg.CommandID.UUID.String()
		}
		v1msgs = append(v1msgs, v1m)
	}

	return connect.NewResponse(&v1pb.ListConversationMessagesResponse{
		Messages:      v1msgs,
		NextPageToken: nextPageToken,
	}), nil
}

func buildInboxSummary(cmd *store.CommandMessage) string {
	switch cmd.ExecutorKind {
	case int32(v1pb.ExecutorKind_SHELL):
		if cmd.Command != "" {
			return cmd.Command
		}
		return "shell command"
	case int32(v1pb.ExecutorKind_ACP):
		if cmd.Instruction != "" {
			return cmd.Instruction
		}
		return "acp task"
	default:
		return "task"
	}
}

func buildLightChatContext(msgs []*store.ChatMessage) string {
	var b strings.Builder
	_, _ = b.WriteString("## Recent conversation (use search_chat_history for older messages)\n")
	count := 0
	for i := len(msgs) - 1; i >= 0 && count < 6; i-- {
		msg := msgs[i]
		if msg.Role == 1 {
			sender := msg.PrincipalName
			if sender == "" {
				sender = "User"
			}
			_, _ = fmt.Fprintf(&b, "- %s: %s\n", sender, msg.Content)
		} else {
			sender := msg.AgentResourceID
			if sender == "" {
				sender = "Assistant"
			}
			_, _ = fmt.Fprintf(&b, "- %s: %s\n", sender, msg.Content)
		}
		count++
	}
	return b.String()
}
