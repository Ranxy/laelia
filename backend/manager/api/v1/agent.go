package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
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

const (
	agentOfflineThresholdSeconds = 60
	accessTokenDuration          = 15 * time.Minute
	refreshTokenDuration         = 24 * time.Hour
	bootstrapTokenDuration       = 7 * 24 * time.Hour
	refreshTokenReuseWindow      = 30 * time.Second
	sessionIDLength              = 32
)

type AgentService struct {
	v1connect.UnimplementedAgentServiceHandler
	store          *store.Store
	secret         string
	profile        *config.Profile
	stateCfg       *state.State
	consumedTimers map[int]*time.Timer
	consumedMu     sync.Mutex
}

func NewAgentService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State) *AgentService {
	return &AgentService{
		store:          store,
		secret:         secret,
		profile:        profile,
		stateCfg:       stateCfg,
		consumedTimers: make(map[int]*time.Timer),
	}
}

func (s *AgentService) CreateAgent(ctx context.Context, req *connect.Request[v1pb.CreateAgentRequest]) (*connect.Response[v1pb.CreateAgentResponse], error) {
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

	bootstrapToken, err := auth.GenerateAgentToken(created.Name, created.ResourceID, created.TokenVersion, auth.TokenTypeBootstrap, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate agent token, error: %v", err))
	}

	tokenHash := hashToken(bootstrapToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     created.ID,
		TokenHash:   tokenHash,
		TokenType:   storepb.AgentTokenType_BOOTSTRAP,
		TokenFamily: created.ResourceID,
		State:       storepb.AgentTokenState_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store agent token, error: %v", err))
	}

	response := &v1pb.CreateAgentResponse{
		Agent:          convertToAgent(created),
		BootstrapToken: bootstrapToken,
	}
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
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	return connect.NewResponse(convertToAgent(agent)), nil
}

func (s *AgentService) DeleteAgent(ctx context.Context, req *connect.Request[v1pb.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.DeleteAgent(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to delete agent, error: %v", err))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) RotateAgentToken(ctx context.Context, req *connect.Request[v1pb.RotateAgentTokenRequest]) (*connect.Response[v1pb.RotateAgentTokenResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	newTokenVersion := agent.TokenVersion + 1
	nowRotated := time.Now()
	if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent token version, error: %v", err))
	}

	bootstrapToken, err := auth.GenerateAgentToken(agent.Name, agent.ResourceID, newTokenVersion, auth.TokenTypeBootstrap, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate agent token, error: %v", err))
	}

	tokenHash := hashToken(bootstrapToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   tokenHash,
		TokenType:   storepb.AgentTokenType_BOOTSTRAP,
		TokenFamily: agent.ResourceID,
		State:       storepb.AgentTokenState_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store agent token, error: %v", err))
	}

	return connect.NewResponse(&v1pb.RotateAgentTokenResponse{
		BootstrapToken: bootstrapToken,
	}), nil
}

func (s *AgentService) RevokeAgentToken(ctx context.Context, req *connect.Request[v1pb.RevokeAgentTokenRequest]) (*connect.Response[v1pb.RevokeAgentTokenResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	newTokenVersion := agent.TokenVersion + 1
	nowRotated := time.Now()
	if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent token version, error: %v", err))
	}

	if err := s.store.RevokeAllAgentTokens(ctx, agent.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke agent tokens, error: %v", err))
	}

	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, "token_revoked"); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent sessions, error: %v", err))
	}

	return connect.NewResponse(&v1pb.RevokeAgentTokenResponse{}), nil
}

func (s *AgentService) ForceDisconnectAgent(ctx context.Context, req *connect.Request[v1pb.ForceDisconnectAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	reason := "admin_forced"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, reason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent sessions, error: %v", err))
	}

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State: storepb.AgentStatus_OFFLINE,
		},
	}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent status, error: %v", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) ListAgentSessions(ctx context.Context, req *connect.Request[v1pb.ListAgentSessionsRequest]) (*connect.Response[v1pb.ListAgentSessionsResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	sessions, err := s.store.ListAgentSessions(ctx, agent.ID, req.Msg.IncludeTerminated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list agent sessions, error: %v", err))
	}

	response := &v1pb.ListAgentSessionsResponse{}
	for _, session := range sessions {
		response.Sessions = append(response.Sessions, convertToV1Session(session))
	}
	return connect.NewResponse(response), nil
}

func (s *AgentService) ConnectAgent(ctx context.Context, req *connect.Request[v1pb.ConnectAgentRequest]) (*connect.Response[v1pb.ConnectAgentResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		if req.Msg.BootstrapToken == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated and no bootstrap token provided"))
		}
		var err error
		agent, err = s.authenticateBootstrapToken(req.Msg.BootstrapToken)
		if err != nil {
			return nil, err
		}
	}

	sessionID := generateRandomString(sessionIDLength)
	nonce := s.stateCfg.NonceManager.GenerateNonce(agent.ResourceID, sessionID)

	now := time.Now()
	nowSec := now.Unix()

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			ConnectedAt:     nowSec,
			LastHeartbeatAt: nowSec,
			ActiveSessionId: sessionID,
		},
	}
	if req.Msg.Info != nil {
		patch.Info = convertToStoreAgentInfo(req.Msg.Info)
	}

	updated, err := s.store.UpdateAgent(ctx, agent, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent on connect, error: %v", err))
	}

	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, "replaced"); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate existing sessions, error: %v", err))
	}

	sourceIP := ""
	if ip, ok := common.GetSourceIPFromContext(ctx); ok {
		sourceIP = ip
	}

	reportedIP := ""
	if req.Msg.Info != nil {
		reportedIP = req.Msg.Info.Ip
	}
	if err := auth.ValidateAgentIP(reportedIP, sourceIP, auth.IPValidationWarn); err != nil {
		return nil, err
	}

	if err := s.store.CreateAgentSession(ctx, &store.AgentSessionMessage{
		SessionID:    sessionID,
		AgentID:      agent.ID,
		TokenFamily:  agent.ResourceID,
		State:        "ACTIVE",
		SourceIP:     sourceIP,
		Fingerprint:  req.Msg.Fingerprint,
		AgentVersion: "",
		ConnectedAt:  now,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create agent session, error: %v", err))
	}

	accessToken, err := auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeAccess, sessionID, s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	refreshToken, err := auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
	}

	refreshTokenHash := hashToken(refreshToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   refreshTokenHash,
		TokenType:   storepb.AgentTokenType_REFRESH,
		TokenFamily: agent.ResourceID,
		State:       storepb.AgentTokenState_ACTIVE,
		Fingerprint: req.Msg.Fingerprint,
		ExpiresAt:   time.Now().Add(refreshTokenDuration),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store refresh token, error: %v", err))
	}

	return connect.NewResponse(&v1pb.ConnectAgentResponse{
		AccessToken:          accessToken,
		RefreshToken:         refreshToken,
		SessionId:            sessionID,
		NextNonce:            nonce,
		AccessTokenExpiresAt: timestamppb.New(time.Now().Add(accessTokenDuration)),
		InitialStatus:        convertToV1AgentStatus(updated.Status, updated.Deleted),
	}), nil
}

func (s *AgentService) AgentHeartbeat(ctx context.Context, req *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	if req.Msg.SessionId != "" {
		session, err := s.store.GetAgentSession(ctx, req.Msg.SessionId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent session, error: %v", err))
		}
		if session == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session not found"))
		}
		if session.State == "KICKED" {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session has been replaced by a new connection"))
		}

		if !s.stateCfg.NonceManager.VerifyNonce(req.Msg.PreviousNonce, agent.ResourceID, req.Msg.SessionId) {
			if req.Msg.PreviousNonce != "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid nonce"))
			}
		}

		if err := s.store.TouchAgentSession(ctx, req.Msg.SessionId); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to touch agent session, error: %v", err))
		}
	}

	nonce := s.stateCfg.NonceManager.GenerateNonce(agent.ResourceID, req.Msg.SessionId)

	nowSec := time.Now().Unix()
	activeSessionID := agent.Status.GetActiveSessionId()
	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			LastHeartbeatAt: nowSec,
			ConnectedAt:     agent.Status.GetConnectedAt(),
			ActiveSessionId: activeSessionID,
		},
	}

	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent heartbeat, error: %v", err))
	}

	if s.stateCfg.HeartbeatBuffer != nil {
		s.stateCfg.HeartbeatBuffer.Record(&state.HeartbeatUpdate{
			AgentID:         agent.ID,
			LastHeartbeatAt: nowSec,
			SessionID:       req.Msg.SessionId,
		})
	}

	resp := &v1pb.AgentHeartbeatResponse{
		NextNonce:       nonce,
		NextHeartbeatAt: timestamppb.New(time.Now().Add(30 * time.Second)),
	}

	if expiresAt, ok := common.GetAccessTokenExpiresAtFromContext(ctx); ok && expiresAt > 0 {
		if time.Now().Unix() >= expiresAt-int64(accessTokenDuration.Seconds()/3) {
			newAccessToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeAccess, req.Msg.SessionId, s.profile.Mode, s.secret, accessTokenDuration)
			if err != nil {
				slog.Warn("failed to generate new access token during heartbeat", "error", err)
			} else {
				resp.AccessToken = newAccessToken
				resp.AccessTokenExpiresAt = timestamppb.New(time.Now().Add(accessTokenDuration))
			}
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *AgentService) AgentDisconnect(ctx context.Context, req *connect.Request[v1pb.AgentDisconnectRequest]) (*connect.Response[emptypb.Empty], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	reason := "agent_shutdown"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	sessionID := req.Msg.SessionId
	if sessionID != "" {
		if err := s.store.TerminateAgentSession(ctx, sessionID, reason); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent session, error: %v", err))
		}
	}

	s.stateCfg.NonceManager.DeleteKey(agent.ResourceID)

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_OFFLINE,
			ActiveSessionId: "",
		},
	}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent status, error: %v", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) RefreshAgentToken(ctx context.Context, req *connect.Request[v1pb.RefreshAgentTokenRequest]) (*connect.Response[v1pb.RefreshAgentTokenResponse], error) {
	refreshTokenStr := req.Msg.RefreshToken
	if refreshTokenStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	tokenHash := hashToken(refreshTokenStr)
	storedToken, err := s.store.GetAgentTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up refresh token, error: %v", err))
	}
	if storedToken == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token"))
	}

	switch storedToken.State {
	case storepb.AgentTokenState_ACTIVE:
	case storepb.AgentTokenState_CONSUMED:
		if err := s.store.UpdateAgentTokenState(ctx, storedToken.ID, storepb.AgentTokenState_REVOKED, nil); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke consumed token, error: %v", err))
		}
	case storepb.AgentTokenState_REVOKED:
		if err := s.store.RevokeTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("refresh token has been revoked, possible security breach detected"))
	default:
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token state"))
	}

	if time.Now().After(storedToken.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token expired"))
	}

	if req.Msg.Fingerprint != "" && storedToken.Fingerprint != "" && req.Msg.Fingerprint != storedToken.Fingerprint {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("fingerprint mismatch, possible token theft detected"))
	}

	agent, err := s.store.GetAgent(ctx, storedToken.AgentID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil || agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not found or deactivated"))
	}

	if storedToken.State == storepb.AgentTokenState_ACTIVE {
		consumedAt := time.Now()
		if err := s.store.UpdateAgentTokenState(ctx, storedToken.ID, storepb.AgentTokenState_CONSUMED, &consumedAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to mark refresh token as consumed, error: %v", err))
		}
		s.scheduleTokenRevoke(storedToken.ID, storedToken.TokenFamily)
	}

	accessToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeAccess, "", s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	newRefreshToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
	}

	newTokenHash := hashToken(newRefreshToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   newTokenHash,
		TokenType:   storepb.AgentTokenType_REFRESH,
		TokenFamily: storedToken.TokenFamily,
		State:       storepb.AgentTokenState_ACTIVE,
		Fingerprint: req.Msg.Fingerprint,
		ExpiresAt:   time.Now().Add(refreshTokenDuration),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new refresh token, error: %v", err))
	}

	return connect.NewResponse(&v1pb.RefreshAgentTokenResponse{
		AccessToken:          accessToken,
		RefreshToken:         newRefreshToken,
		AccessTokenExpiresAt: timestamppb.New(time.Now().Add(accessTokenDuration)),
	}), nil
}

func (s *AgentService) scheduleTokenRevoke(tokenID int, _ string) {
	timer := time.AfterFunc(refreshTokenReuseWindow, func() {
		if err := s.store.UpdateAgentTokenState(context.Background(), tokenID, storepb.AgentTokenState_REVOKED, nil); err != nil {
			slog.Error("failed to revoke consumed token", "token_id", tokenID, "error", err)
		}
		s.consumedMu.Lock()
		delete(s.consumedTimers, tokenID)
		s.consumedMu.Unlock()
	})
	s.consumedMu.Lock()
	s.consumedTimers[tokenID] = timer
	s.consumedMu.Unlock()
}

func (*AgentService) Hello(_ context.Context, _ *connect.Request[v1pb.HelloRequest]) (*connect.Response[v1pb.HelloResponse], error) {
	return connect.NewResponse(&v1pb.HelloResponse{
		CurrentTime:   time.Now().Unix(),
		ServerVersion: "0.1.0",
	}), nil
}

type bootstrapClaims struct {
	Name         string `json:"name"`
	TokenVersion int    `json:"token_version"`
	TokenType    string `json:"token_type"`
	jwt.RegisteredClaims
}

func (s *AgentService) authenticateBootstrapToken(tokenStr string) (*store.AgentMessage, error) {
	claims := &bootstrapClaims{}
	parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, errors.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		if kid, ok := t.Header["kid"].(string); ok && kid == "v1" {
			return []byte(s.secret), nil
		}
		return nil, errors.Errorf("unexpected kid %v", t.Header["kid"])
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("invalid bootstrap token: %v", err))
	}
	if !parsedToken.Valid {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token is invalid"))
	}
	if claims.TokenType != auth.TokenTypeBootstrap {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected bootstrap token, got %s", claims.TokenType))
	}

	agent, err := s.store.GetAgentByResourceID(context.Background(), claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find agent: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("agent %s not found", claims.Subject))
	}
	if agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("agent %s has been deactivated", claims.Subject))
	}
	if agent.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent token version mismatch"))
	}

	tokenHash := hashToken(tokenStr)
	storedToken, err := s.store.GetAgentTokenByHash(context.Background(), tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up token: %v", err))
	}
	if storedToken == nil || storedToken.State != storepb.AgentTokenState_ACTIVE {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token is not active"))
	}
	if time.Now().After(storedToken.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token expired"))
	}

	return agent, nil
}

func convertToAgent(agent *store.AgentMessage) *v1pb.Agent {
	name := common.FormatAgentUID(agent.ResourceID)
	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}

	status := convertToV1AgentStatus(agent.Status, agent.Deleted)

	result := &v1pb.Agent{
		Name:         name,
		State:        state,
		Title:        agent.Name,
		Info:         convertToV1AgentInfo(agent.Info),
		Status:       status,
		CreatedAt:    timestamppb.New(agent.CreatedAt),
		TokenVersion: int32(agent.TokenVersion),
	}
	if !agent.LastTokenRotatedAt.IsZero() {
		result.LastTokenRotatedAt = timestamppb.New(agent.LastTokenRotatedAt)
	}
	return result
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
		ActiveSessionId:   status.ActiveSessionId,
	}
}

func computeConnectionState(status *storepb.AgentStatus, deleted bool) v1pb.AgentStatus_ConnectionState {
	if status.State == storepb.AgentStatus_ERROR {
		return v1pb.AgentStatus_ERROR
	}
	if status.State == storepb.AgentStatus_KICKED {
		return v1pb.AgentStatus_KICKED
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

func convertToV1Session(session *store.AgentSessionMessage) *v1pb.AgentSession {
	var connectedAt, lastHeartbeatAt, disconnectedAt *timestamppb.Timestamp
	if !session.ConnectedAt.IsZero() {
		connectedAt = timestamppb.New(session.ConnectedAt)
	}
	if !session.LastHeartbeatAt.IsZero() {
		lastHeartbeatAt = timestamppb.New(session.LastHeartbeatAt)
	}
	if !session.DisconnectedAt.IsZero() {
		disconnectedAt = timestamppb.New(session.DisconnectedAt)
	}

	var state v1pb.AgentStatus_ConnectionState
	switch session.State {
	case "ACTIVE":
		state = v1pb.AgentStatus_ONLINE
	case "KICKED":
		state = v1pb.AgentStatus_KICKED
	case "TERMINATED":
		state = v1pb.AgentStatus_OFFLINE
	default:
		state = v1pb.AgentStatus_CONNECTION_STATE_UNSPECIFIED
	}

	return &v1pb.AgentSession{
		SessionId:        session.SessionID,
		AgentName:        common.FormatAgentUID(session.AgentResourceID),
		SourceIp:         session.SourceIP,
		AgentVersion:     session.AgentVersion,
		Fingerprint:      session.Fingerprint,
		ConnectedAt:      connectedAt,
		LastHeartbeatAt:  lastHeartbeatAt,
		DisconnectedAt:   disconnectedAt,
		DisconnectReason: session.DisconnectReason,
		State:            state,
	}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:length]
}
