package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const agentOfflineThresholdSeconds = 60

type AgentService struct {
	v1connect.UnimplementedAgentServiceHandler
	store    *store.Store
	secret   string
	profile  *config.Profile
	stateCfg *state.State
}

func NewAgentService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State) *AgentService {
	return &AgentService{
		store:    store,
		secret:   secret,
		profile:  profile,
		stateCfg: stateCfg,
	}
}

func (s *AgentService) CreateAgent(ctx context.Context, req *connect.Request[v1pb.CreateAgentRequest]) (*connect.Response[v1pb.Agent], error) {
	if req.Msg.Agent == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent must be set"))
	}
	if req.Msg.Agent.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent title must be set"))
	}

	agentMessage := &store.AgentMessage{
		Name:         req.Msg.Agent.Title,
		TokenVersion: 1,
		Info: &storepb.AgentInfo{
			Labels: req.Msg.Agent.Labels,
		},
		Status: &storepb.AgentStatus{},
	}

	created, err := s.store.CreateAgent(ctx, agentMessage)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create agent, error: %v", err))
	}

	resourceID := common.FormatAgentUID(created.ID)
	_, err = s.store.UpdateAgent(ctx, created, &store.UpdateAgentMessage{
		Name: &resourceID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent resource_id, error: %v", err))
	}
	created.ResourceID = resourceID

	token, err := auth.GenerateAgentToken(created.Name, created.ID, created.TokenVersion, s.profile.Mode, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate agent token, error: %v", err))
	}

	response := convertToAgent(created)
	response.Token = token
	return connect.NewResponse(response), nil
}

func (s *AgentService) ListAgents(ctx context.Context, req *connect.Request[v1pb.ListAgentsRequest]) (*connect.Response[v1pb.ListAgentsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindAgentMessage{
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
		ShowDeleted: req.Msg.ShowDeleted,
	}

	agents, err := s.store.ListAgents(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list agents, error: %v", err))
	}

	nextPageToken := ""
	if len(agents) == limitPlusOne {
		agents = agents[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to marshal next page token, error: %v", err))
		}
	}

	response := &v1pb.ListAgentsResponse{
		NextPageToken: nextPageToken,
	}
	for _, agent := range agents {
		response.Agents = append(response.Agents, convertToAgent(agent))
	}
	return connect.NewResponse(response), nil
}

func (s *AgentService) GetAgent(ctx context.Context, req *connect.Request[v1pb.GetAgentRequest]) (*connect.Response[v1pb.Agent], error) {
	agentID, err := common.GetAgentID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %d not found", agentID))
	}
	return connect.NewResponse(convertToAgent(agent)), nil
}

func (s *AgentService) DeleteAgent(ctx context.Context, req *connect.Request[v1pb.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	agentID, err := common.GetAgentID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.DeleteAgent(ctx, agentID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to delete agent, error: %v", err))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) ConnectAgent(ctx context.Context, req *connect.Request[v1pb.ConnectAgentRequest]) (*connect.Response[v1pb.ConnectAgentResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	now := time.Now().Unix()
	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			ConnectedAt:     now,
			LastHeartbeatAt: now,
		},
	}

	if req.Msg.Info != nil {
		patch.Info = convertToStoreAgentInfo(req.Msg.Info)
	}

	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent on connect, error: %v", err))
	}

	return connect.NewResponse(&v1pb.ConnectAgentResponse{}), nil
}

func (s *AgentService) AgentHeartbeat(ctx context.Context, _ *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	now := time.Now().Unix()
	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			LastHeartbeatAt: now,
			ConnectedAt:     agent.Status.GetConnectedAt(),
		},
	}

	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent heartbeat, error: %v", err))
	}

	return connect.NewResponse(&v1pb.AgentHeartbeatResponse{}), nil
}

func (s *AgentService) Hello(_ context.Context, _ *connect.Request[v1pb.HelloRequest]) (*connect.Response[v1pb.HelloResponse], error) {
	return connect.NewResponse(&v1pb.HelloResponse{
		CurrentTime: time.Now().Unix(),
	}), nil
}

func convertToAgent(agent *store.AgentMessage) *v1pb.Agent {
	name := agent.ResourceID
	if name == "" {
		name = common.FormatAgentUID(agent.ID)
	}

	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}

	status := convertToV1AgentStatus(agent.Status, agent.Deleted)

	return &v1pb.Agent{
		Name:      name,
		State:     state,
		Title:     agent.Name,
		Info:      convertToV1AgentInfo(agent.Info),
		Status:    status,
		CreatedAt: timestamppb.New(agent.CreatedAt),
	}
}

func convertToV1AgentInfo(info *storepb.AgentInfo) *v1pb.AgentInfo {
	if info == nil {
		return nil
	}
	return &v1pb.AgentInfo{
		AgentType: info.AgentType,
		Hostname:  info.Hostname,
		Os:        info.Os,
		Arch:      info.Arch,
		Ip:        info.Ip,
		Version:   info.Version,
		Labels:    info.Labels,
	}
}

func convertToStoreAgentInfo(info *v1pb.AgentInfo) *storepb.AgentInfo {
	if info == nil {
		return nil
	}
	return &storepb.AgentInfo{
		AgentType: info.AgentType,
		Hostname:  info.Hostname,
		Os:        info.Os,
		Arch:      info.Arch,
		Ip:        info.Ip,
		Version:   info.Version,
		Labels:    info.Labels,
	}
}

func convertToV1AgentStatus(status *storepb.AgentStatus, deleted bool) *v1pb.AgentStatus {
	if status == nil {
		return nil
	}
	state := computeConnectionState(status, deleted)

	var lastHeartbeatTime *timestamppb.Timestamp
	if status.LastHeartbeatAt > 0 {
		lastHeartbeatTime = timestamppb.New(time.Unix(status.LastHeartbeatAt, 0))
	}
	var connectedTime *timestamppb.Timestamp
	if status.ConnectedAt > 0 {
		connectedTime = timestamppb.New(time.Unix(status.ConnectedAt, 0))
	}

	return &v1pb.AgentStatus{
		State:             state,
		LastHeartbeatTime: lastHeartbeatTime,
		ConnectedTime:     connectedTime,
		ErrorMessage:      status.ErrorMessage,
	}
}

func computeConnectionState(status *storepb.AgentStatus, deleted bool) v1pb.AgentStatus_ConnectionState {
	if status.State == storepb.AgentStatus_ERROR {
		return v1pb.AgentStatus_ERROR
	}
	if deleted {
		return v1pb.AgentStatus_OFFLINE
	}
	threshold := time.Now().Unix() - agentOfflineThresholdSeconds
	if status.LastHeartbeatAt >= threshold {
		return v1pb.AgentStatus_ONLINE
	}
	if status.LastHeartbeatAt > 0 {
		return v1pb.AgentStatus_OFFLINE
	}
	return v1pb.AgentStatus_OFFLINE
}
