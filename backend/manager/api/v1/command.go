package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type CommandService struct {
	v1connect.UnimplementedCommandServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
}

func NewCommandService(s *store.Store, d *dispatcher.Dispatcher) *CommandService {
	return &CommandService{store: s, dispatcher: d}
}

func (s *CommandService) SendCommand(ctx context.Context, req *connect.Request[v1pb.SendCommandRequest]) (*connect.Response[v1pb.Command], error) {
	if req.Msg.Command == "" {
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

	envBytes, err := json.Marshal(req.Msg.Env)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to marshal env"))
	}

	cmd := &store.CommandMessage{
		AgentID:        agent.ID,
		PrincipalID:    principalID,
		Command:        req.Msg.Command,
		Status:         1, // PENDING
		Env:            string(envBytes),
		WorkingDir:     req.Msg.WorkingDir,
		TimeoutSeconds: req.Msg.TimeoutSeconds,
	}

	created, err := s.store.CreateCommand(ctx, cmd)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create command"))
	}

	_ = s.dispatcher.DispatchCommand(ctx, created)

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
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to cancel command"))
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
		Status:        v1pb.CommandStatus(cmd.Status),
		CreatedAt:     timestamppb.New(cmd.CreatedAt),
		ErrorMessage:  cmd.ErrorMessage,
		WorkingDir:    cmd.WorkingDir,
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

	return v1cmd
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
