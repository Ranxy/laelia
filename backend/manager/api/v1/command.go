package v1

import (
	"context"
	"database/sql"
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
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type CommandService struct {
	v1connect.UnimplementedCommandServiceHandler
	store           *store.Store
	dispatcher      *dispatcher.Dispatcher
	s3clientManager *s3client.Client
	iam             *iam.Manager
}

func NewCommandService(s *store.Store, d *dispatcher.Dispatcher, s3clientManager *s3client.Client, iamManager *iam.Manager) *CommandService {
	return &CommandService{store: s, dispatcher: d, s3clientManager: s3clientManager, iam: iamManager}
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
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return nil, err
	}

	return connect.NewResponse(convertToV1Command(cmd)), nil
}

func (s *CommandService) CancelCommand(ctx context.Context, req *connect.Request[v1pb.CancelCommandRequest]) (*connect.Response[v1pb.Command], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return nil, err
	}

	if cmd.Status != 1 && cmd.Status != 2 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("command is not in pending or running state"))
	}

	if err := s.dispatcher.CancelCommand(ctx, cmd.AgentID, cmd.ID.String()); err != nil {
		// agent may not be connected, still proceed to cancel in DB
		slog.Warn("failed to send cancel to agent", "commandID", cmd.ID, "error", err)
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
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return err
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
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return err
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
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return nil, err
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

func (s *CommandService) SearchChatHistory(ctx context.Context, req *connect.Request[v1pb.SearchChatHistoryRequest]) (*connect.Response[v1pb.SearchChatHistoryResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
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

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.Limit),
		maximum: 50,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	msgs, err := s.store.SearchChatHistory(ctx, convID, req.Msg.Query, since, until, limitPlusOne, offset.offset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to search chat history"))
	}

	nextPageToken := ""
	if len(msgs) == limitPlusOne {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	var v1Entries []*v1pb.ChatMessage
	callerAgent, _ := GetAgentFromContext(ctx)
	for _, msg := range msgs {
		v1m := storeToV1ChatMessage(msg)
		v1m.IsOwn = callerAgent != nil && msg.SenderAgentID.Valid && int(msg.SenderAgentID.Int32) == callerAgent.ID
		v1Entries = append(v1Entries, v1m)
	}

	return connect.NewResponse(&v1pb.SearchChatHistoryResponse{
		Entries:       v1Entries,
		NextPageToken: nextPageToken,
	}), nil
}

func (s *CommandService) GetCommandContext(ctx context.Context, req *connect.Request[v1pb.GetCommandContextRequest]) (*connect.Response[v1pb.GetCommandContextResponse], error) {
	cmd, err := s.store.GetCommandByName(ctx, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err := s.requireCommandAccess(ctx, cmd); err != nil {
		return nil, err
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

	// GetOrCreateConversation starts a direct conversation between the caller
	// and an agent. It is a user-facing action; an agent token must not create a
	// direct conversation owned by the system principal.
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("GetOrCreateConversation is for authenticated users"))
	}

	conv, err := s.store.GetOrCreateDirectConversation(ctx, agent.ID, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get or create conversation"))
	}

	return connect.NewResponse(&v1pb.GetOrCreateConversationResponse{
		Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
	}), nil
}

func (s *CommandService) GetOrCreateUserUserDM(ctx context.Context, req *connect.Request[v1pb.GetOrCreateUserUserDMRequest]) (*connect.Response[v1pb.GetOrCreateUserUserDMResponse], error) {
	// GetOrCreateUserUserDM starts a 1:1 DM between the caller and another
	// user. It is a user-facing action; an agent token must not create a
	// user-user DM owned by the system principal.
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("GetOrCreateUserUserDM is for authenticated users"))
	}
	if req.Msg.PeerUser == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("peer_user must not be empty"))
	}

	peerID, err := common.GetUserID(req.Msg.PeerUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid peer_user"))
	}
	peer, err := s.store.GetUserByID(ctx, peerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve peer user"))
	}
	if peer == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %s not found", req.Msg.PeerUser))
	}
	if peer.ID == user.ID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a user cannot open a DM with themselves"))
	}

	conv, err := s.store.GetOrCreateUserUserDM(ctx, user.ID, peer.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get or create user DM"))
	}

	return connect.NewResponse(&v1pb.GetOrCreateUserUserDMResponse{
		Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
	}), nil
}

func (s *CommandService) ListConversationMessages(ctx context.Context, req *connect.Request[v1pb.ListConversationMessagesRequest]) (*connect.Response[v1pb.ListConversationMessagesResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	if req.Msg.AfterVersion > 0 && req.Msg.BeforeVersion > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("after_version and before_version are mutually exclusive"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}

	// Three read modes, all returned in chronological (oldest -> newest) order:
	//   - before_version: one-shot history lookup of the `limit` messages
	//     immediately before the pivot (no backward page token).
	//   - after_version: incremental delta (room_version > after_version),
	//     paginated with a forward page token.
	//   - neither (default): one-shot latest-N — the newest `limit` messages,
	//     so opening a conversation shows recent history, not the oldest page.
	var msgs []*store.ChatMessage
	var currentVersion int64
	switch {
	case req.Msg.BeforeVersion > 0:
		msgs, currentVersion, err = s.store.ListConversationMessages(ctx, convID, 0, req.Msg.BeforeVersion, offset.limit, 0)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
	case req.Msg.AfterVersion > 0:
		limitPlusOne := offset.limit + 1
		msgs, currentVersion, err = s.store.ListConversationMessages(ctx, convID, req.Msg.AfterVersion, 0, limitPlusOne, offset.offset)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
	default:
		msgs, currentVersion, err = s.store.ListConversationMessages(ctx, convID, 0, 0, offset.limit, 0)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
	}

	// Forward pagination only applies to the after_version delta path: the store
	// returned one extra (limit+1) as a "has more" indicator, which we trim. The
	// before_version and default latest-N paths are one-shot (no +1), so len(msgs)
	// never reaches offset.limit+1 and this is a no-op for them.
	nextPageToken := ""
	if req.Msg.BeforeVersion == 0 && req.Msg.AfterVersion > 0 && len(msgs) == offset.limit+1 {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	// is_own is caller-relative: only the agent itself (authenticated via the MCP
	// server's bearer token) can mark its own messages. Frontend/user callers get
	// is_own=false for every message.
	callerAgent, _ := GetAgentFromContext(ctx)

	// An agent reading a channel is committing to work on it. Link the session's
	// running command to this conversation now (not at ack time, which runs after
	// the work is done) so FetchConversationActivity can surface the agent's live
	// status while it works — without this the channel status bar stays "idle"
	// throughout execution. User/frontend callers have no session command and are
	// skipped. LinkCommandConversation is an idempotent insert (+ first-wins
	// column set) so a multi-channel turn links its command to every channel.
	if callerAgent != nil {
		if cmdID := s.dispatcher.CurrentCommandID(callerAgent.ID); cmdID != "" {
			if cid, parseErr := uuid.Parse(cmdID); parseErr == nil {
				if linkErr := s.store.LinkCommandConversation(ctx, cid, convID); linkErr != nil {
					slog.Warn("failed to link command to conversation on read", "commandID", cmdID, "conversationID", convID, "error", linkErr)
				}
			}
		}
	}

	var v1msgs []*v1pb.ChatMessage
	for _, msg := range msgs {
		v1m := storeToV1ChatMessage(msg)
		v1m.IsOwn = callerAgent != nil && msg.SenderAgentID.Valid && int(msg.SenderAgentID.Int32) == callerAgent.ID
		v1msgs = append(v1msgs, v1m)
	}

	return connect.NewResponse(&v1pb.ListConversationMessagesResponse{
		Messages:       v1msgs,
		NextPageToken:  nextPageToken,
		CurrentVersion: currentVersion,
	}), nil
}

// ListThreadMessages returns the root message of a thread followed by its
// replies in room_version order, with the same cursor model as
// ListConversationMessages. The caller must be a member of the conversation.
func (s *CommandService) ListThreadMessages(ctx context.Context, req *connect.Request[v1pb.ListThreadMessagesRequest]) (*connect.Response[v1pb.ListThreadMessagesResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	if req.Msg.ThreadRoot == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root must not be empty"))
	}
	rootID, err := uuid.Parse(req.Msg.ThreadRoot)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid thread_root"))
	}
	if req.Msg.AfterVersion > 0 && req.Msg.BeforeVersion > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("after_version and before_version are mutually exclusive"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}

	var msgs []*store.ChatMessage
	var currentVersion int64
	switch {
	case req.Msg.BeforeVersion > 0:
		msgs, currentVersion, err = s.store.ListThreadMessages(ctx, convID, rootID, 0, req.Msg.BeforeVersion, offset.limit, 0)
	case req.Msg.AfterVersion > 0:
		limitPlusOne := offset.limit + 1
		msgs, currentVersion, err = s.store.ListThreadMessages(ctx, convID, rootID, req.Msg.AfterVersion, 0, limitPlusOne, offset.offset)
	default:
		msgs, currentVersion, err = s.store.ListThreadMessages(ctx, convID, rootID, 0, 0, offset.limit, 0)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list thread messages"))
	}

	nextPageToken := ""
	if req.Msg.BeforeVersion == 0 && req.Msg.AfterVersion > 0 && len(msgs) == offset.limit+1 {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	callerAgent, _ := GetAgentFromContext(ctx)
	// Link the session's command to this conversation on read, same as
	// ListConversationMessages, so FetchConversationActivity surfaces the
	// agent's live status while it works on the thread.
	if callerAgent != nil {
		if cmdID := s.dispatcher.CurrentCommandID(callerAgent.ID); cmdID != "" {
			if cid, parseErr := uuid.Parse(cmdID); parseErr == nil {
				if linkErr := s.store.LinkCommandConversation(ctx, cid, convID); linkErr != nil {
					slog.Warn("failed to link command to conversation on thread read", "commandID", cmdID, "conversationID", convID, "error", linkErr)
				}
			}
		}
	}

	var v1msgs []*v1pb.ChatMessage
	for _, msg := range msgs {
		v1m := storeToV1ChatMessage(msg)
		v1m.IsOwn = callerAgent != nil && msg.SenderAgentID.Valid && int(msg.SenderAgentID.Int32) == callerAgent.ID
		v1msgs = append(v1msgs, v1m)
	}

	return connect.NewResponse(&v1pb.ListThreadMessagesResponse{
		Messages:       v1msgs,
		NextPageToken:  nextPageToken,
		CurrentVersion: currentVersion,
	}), nil
}

// ListChannelThreads returns a summary (root id, reply count, latest reply
// version/time) for every active thread in a conversation. The channel page
// polls this to keep root-message reply-count badges fresh — including replies
// that arrive while the thread panel is closed (e.g. an async agent reply),
// which the message watcher cannot observe because ListConversationMessages
// excludes thread replies.
func (s *CommandService) ListChannelThreads(ctx context.Context, req *connect.Request[v1pb.ListChannelThreadsRequest]) (*connect.Response[v1pb.ListChannelThreadsResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	threads, err := s.store.ListChannelThreads(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channel threads"))
	}
	v1threads := make([]*v1pb.ChannelThread, 0, len(threads))
	for _, t := range threads {
		ct := &v1pb.ChannelThread{
			RootMessage:        t.RootMessageID.String(),
			ReplyCount:         t.ReplyCount,
			LatestReplyVersion: t.LatestVersion,
		}
		if !t.LatestAt.IsZero() {
			ct.LatestReplyAt = timestamppb.New(t.LatestAt)
		}
		v1threads = append(v1threads, ct)
	}
	return connect.NewResponse(&v1pb.ListChannelThreadsResponse{Threads: v1threads}), nil
}

func (s *CommandService) PostMessage(ctx context.Context, req *connect.Request[v1pb.PostMessageRequest]) (*connect.Response[v1pb.PostMessageResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	convUUID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
	}

	conv, err := s.store.GetConversation(ctx, convUUID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// An agent may only post to a conversation it is a member of.
	isMember, err := s.store.IsConversationMember(ctx, convUUID, store.MemberTypeAgent, agent.ResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}
	if !isMember {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("not a conversation member"))
	}

	currentVersion := conv.Version
	if req.Msg.BaseVersion == currentVersion {
		principalID := 1
		if conv.OwnerID > 0 {
			principalID = conv.OwnerID
		}

		var commandID uuid.NullUUID
		if req.Msg.CommandId != "" {
			if cid, parseErr := uuid.Parse(req.Msg.CommandId); parseErr == nil {
				commandID = uuid.NullUUID{UUID: cid, Valid: true}
			}
		}

		attachments, err := s.resolveAttachments(ctx, convUUID, req.Msg.Attachments)
		if err != nil {
			return nil, err
		}

		// thread_root, when set, makes this agent reply a message in an
		// existing thread rooted at the given message id. Validate the root
		// belongs to this conversation and is itself a root.
		var threadRoot uuid.NullUUID
		if req.Msg.ThreadRoot != "" {
			rootID, parseErr := uuid.Parse(req.Msg.ThreadRoot)
			if parseErr != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid thread_root"))
			}
			isRoot, rootErr := s.store.IsThreadRoot(ctx, convUUID, rootID)
			if rootErr != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(rootErr, "failed to validate thread root"))
			}
			if !isRoot {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root is not a root message in this conversation"))
			}
			threadRoot = uuid.NullUUID{UUID: rootID, Valid: true}
		}

		// The agent addresses other agents/users by typing content-only
		// `@someone`; the manager parses those tokens into structured Mentions
		// (the agent never sets Mentions itself). Parsed mentions are persisted on
		// the message and drive thread subscription / wake routing below.
		mentions := s.parseContentMentions(ctx, convUUID, req.Msg.Content, agent.ResourceID)

		msg, newVersion, createErr := s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
			ConversationID:      convUUID,
			PrincipalID:         principalID,
			SenderAgentID:       toNullInt32(int32(agent.ID)),
			Role:                2,
			Content:             req.Msg.Content,
			CommandID:           commandID,
			SenderType:          store.SenderTypeAgent,
			Attachments:         attachments,
			Mentions:            mentions,
			ThreadRootMessageID: threadRoot,
		})
		if createErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(createErr, "failed to create assistant message"))
		}

		if threadRoot.Valid {
			// Thread reply: subscribe the posting agent and any @mentioned
			// agents, then wake every subscriber except the poster. The poster
			// is excluded so its own reply does not re-wake itself. There is no
			// posting user here, so posterUserID is nil — user_thread_participant
			// subscriptions come only from the parsed @mentions of users.
			s.subscribeAndNotifyThread(ctx, convUUID, threadRoot.UUID, newVersion, mentions, &agent.ID, nil)
		} else {
			// Agent-first: wake every OTHER agent member of this conversation so
			// they can pull this agent's reply. The posting agent is excluded (it
			// just acked past its own post and must not re-wake itself). This is
			// the single change that enables agent→agent conversation.
			s.notifyConversationAgents(ctx, convUUID, newVersion, &agent.ID)
		}

		// The posting agent has produced this message, so it has processed up to
		// and including newVersion. Advance its durable cursor now so its own
		// reply is never re-surfaced as "new" on the next BeginSession (which
		// would make the agent re-read and mistake its own message for another
		// agent's). UpsertCursor is monotonic (GREATEST), so a later explicit
		// ack_processed_version from the agent cannot rewind it.
		if _, err := s.store.UpsertCursor(ctx, agent.ID, convUUID, newVersion); err != nil {
			slog.Warn("failed to advance posting agent cursor", "agentID", agent.ID, "conversationID", convUUID, "version", newVersion, "error", err)
		}

		// Generate per-user activity (mention/thread; agents carry no TASK/REMINDER
		// root kind here — task/reminder root messages are created via their own
		// handlers). A thread reply inherits the root's task/reminder kind.
		rootIsTask, rootIsReminder := false, false
		if threadRoot.Valid {
			rootIsTask, rootIsReminder, err = s.store.RootMessageKinds(ctx, threadRoot.UUID)
			if err != nil {
				slog.Warn("failed to resolve thread root kinds for activity", "rootID", threadRoot.UUID, "error", err)
			}
		}
		s.store.GenerateActivityForMessage(msg, rootIsTask, rootIsReminder)

		return connect.NewResponse(&v1pb.PostMessageResponse{
			Committed:      true,
			Message:        storeToV1ChatMessage(msg),
			CurrentVersion: newVersion,
		}), nil
	}

	newMsgs, _, listErr := s.store.ListConversationMessages(ctx, convUUID, req.Msg.BaseVersion, 0, 50, 0)
	if listErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(listErr, "failed to list new messages"))
	}

	var v1NewMsgs []*v1pb.ChatMessage
	for _, m := range newMsgs {
		v1m := storeToV1ChatMessage(m)
		v1m.IsOwn = m.SenderAgentID.Valid && int(m.SenderAgentID.Int32) == agent.ID
		v1NewMsgs = append(v1NewMsgs, v1m)
	}

	return connect.NewResponse(&v1pb.PostMessageResponse{
		Committed:           false,
		CurrentVersion:      currentVersion,
		NewMessages:         v1NewMsgs,
		ConflictDescription: fmt.Sprintf("Version conflict: base_version=%d, current=%d. %d new messages arrived.", req.Msg.BaseVersion, currentVersion, len(v1NewMsgs)),
	}), nil
}

func storeToV1ChatMessage(msg *store.ChatMessage) *v1pb.ChatMessage {
	senderName := msg.PrincipalName
	senderType := v1pb.SenderType(msg.SenderType)
	if msg.SenderType == store.SenderTypeAgent && msg.SenderAgentID.Valid {
		senderName = msg.AgentName
	}
	v1m := &v1pb.ChatMessage{
		Name:             msg.ID.String(),
		Conversation:     msg.ConversationID.String(),
		PrincipalName:    msg.PrincipalName,
		Role:             msg.Role,
		Content:          msg.Content,
		CreatedAt:        timestamppb.New(msg.CreatedAt),
		SenderName:       senderName,
		SenderType:       senderType,
		PrincipalId:      formatPrincipalID(msg.PrincipalID),
		RoomVersion:      msg.RoomVersion,
		Mentions:         msg.Mentions,
		Attachments:      msg.Attachments,
		ThreadReplyCount: msg.ThreadReplyCount,
		Task:             storeToV1TaskInfo(msg.TaskInfo),
	}
	if msg.CommandID.Valid {
		v1m.CommandId = msg.CommandID.UUID.String()
	}
	v1m.AgentId = msg.AgentResourceID
	if msg.ThreadRootMessageID.Valid {
		v1m.ThreadRoot = msg.ThreadRootMessageID.UUID.String()
	}
	return v1m
}

// storeToV1TaskInfo converts the store-layer TaskInfo join into the proto
// TaskInfo carried on a ChatMessage. Returns nil when the message is not a
// task, so non-task messages omit the field.
func storeToV1TaskInfo(ti *store.TaskInfo) *v1pb.TaskInfo {
	if ti == nil {
		return nil
	}
	return &v1pb.TaskInfo{
		TaskNumber:         ti.TaskNumber,
		Status:             v1pb.TaskStatus(ti.Status),
		AssigneeName:       ti.AssigneeName,
		AssigneeResourceId: ti.AssigneeResourceID,
	}
}

func toNullInt32(v int32) sql.NullInt32 {
	return sql.NullInt32{Int32: v, Valid: true}
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
