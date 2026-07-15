package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/provider"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
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

// envVarNameRegex matches a valid environment variable name.
var envVarNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AgentService struct {
	v1connect.UnimplementedAgentServiceHandler
	store          *store.Store
	secret         string
	profile        *config.Profile
	stateCfg       *state.State
	dispatcher     *dispatcher.Dispatcher
	iam            *iam.Manager
	consumedTimers map[int]*time.Timer
	consumedMu     sync.Mutex
}

func NewAgentService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State, d *dispatcher.Dispatcher, iamManager *iam.Manager) *AgentService {
	return &AgentService{
		store:          store,
		secret:         secret,
		profile:        profile,
		stateCfg:       stateCfg,
		dispatcher:     d,
		iam:            iamManager,
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

	// Record the creator so the agentEditor IAM binding can be seeded for them
	// (seedAgentEditorBindingTx), granting agents.edit to the creator. CreateAgent
	// is admin-tier, so a user is always present; guard defensively against a
	// missing one.
	creatorID := 0
	if user, _ := GetUserFromContext(ctx); user != nil {
		creatorID = user.ID
	}

	agentMessage := &store.AgentMessage{
		Name:         req.Msg.Agent.Title,
		TokenVersion: 1,
		Info: &storepb.AgentInfo{
			Labels: req.Msg.Agent.Labels,
			AcpConfig: &storepb.AgentACPConfig{
				AllowEnv: executor.DefaultAllowEnv,
			},
		},
		Status:    &storepb.AgentStatus{},
		CreatedBy: creatorID,
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
	caller, _ := GetUserFromContext(ctx)
	for _, agent := range agents {
		a := convertToAgent(agent)
		a.CanEdit = s.canEditAgent(ctx, caller, a.Name)
		response.Agents = append(response.Agents, a)
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
	out := convertToAgent(agent)
	caller, _ := GetUserFromContext(ctx)
	out.CanEdit = s.canEditAgent(ctx, caller, out.Name)
	return connect.NewResponse(out), nil
}

// canEditAgent reports whether the caller holds laelia.agents.edit on the agent
// (creator via the agentEditor binding, or any workspace admin via the
// all-permissions union). A lookup failure is treated as not-editable
// (fail-closed) so a stale can_edit never grants modification. Agent-daemon
// callers and unauthenticated requests get false.
func (s *AgentService) canEditAgent(ctx context.Context, user *store.UserMessage, agentName string) bool {
	if user == nil || s.iam == nil {
		return false
	}
	ok, err := s.iam.CheckPermission(ctx, permission.AgentsEdit, user, nil, &iam.ResourceRef{
		ResourceType: storepb.Policy_AGENT,
		Name:         agentName,
	})
	if err != nil {
		slog.Error("failed to resolve agents.edit", slog.String("agent", agentName), slog.Any("err", err))
		return false
	}
	return ok
}

func (s *AgentService) DeleteAgent(ctx context.Context, req *connect.Request[v1pb.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
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
	newTokenFamily := fmt.Sprintf("%s:v%d", agent.ResourceID, newTokenVersion)

	bootstrapToken, err := auth.GenerateAgentTokenWithFamily(agent.Name, agent.ResourceID, newTokenVersion, auth.TokenTypeBootstrap, newTokenFamily, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate agent token, error: %v", err))
	}

	nowRotated := time.Now()
	if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent token version, error: %v", err))
	}

	if err := s.store.RevokeAllAgentTokens(ctx, agent.ID); err != nil {
		// Abort the rotation: leaving old tokens live while minting a new
		// bootstrap token means a previously-issued (possibly leaked) refresh
		// token would keep working under the old token_version until it
		// expired. Failing closed forces the admin to retry and keeps the
		// "rotation revokes everything" invariant intact. The version bump
		// above is contained: old refresh tokens embed the prior version and
		// RefreshAgentToken rejects version mismatches.
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke old tokens during rotation, error: %v", err))
	}

	tokenHash := hashToken(bootstrapToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   tokenHash,
		TokenType:   storepb.AgentTokenType_BOOTSTRAP,
		TokenFamily: newTokenFamily,
		State:       storepb.AgentTokenState_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		slog.Error("failed to store new token after rotation — agent has no valid bootstrap token", "agent", resourceID, "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new agent token, error: %v", err))
	}

	terminateReason := "token_rotated"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, terminateReason); err != nil {
		slog.Warn("failed to terminate agent sessions after rotation", "agent", resourceID, "error", err)
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

	terminateReason := "token_revoked"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, terminateReason); err != nil {
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
	tokenFamily := ""
	bootstrapTokenID := 0
	if !ok || agent == nil {
		if req.Msg.BootstrapToken == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated and no bootstrap token provided"))
		}
		authResult, err := s.authenticateBootstrapToken(req.Msg.BootstrapToken)
		if err != nil {
			return nil, err
		}
		agent = authResult.agent
		tokenFamily = authResult.tokenFamily
		bootstrapTokenID = authResult.tokenID
	}
	if tokenFamily == "" {
		tokenFamily = agent.ResourceID
	}

	sessionID := generateRandomString(sessionIDLength)
	nonce := s.stateCfg.NonceManager.GenerateNonce(agent.ResourceID, sessionID)

	now := time.Now()
	nowSec := now.Unix()

	// ACP config is owned by the server (set by the admin via
	// UpdateAgentACPConfig). Always echo it back to the agent and derive the
	// capability from it, regardless of what the agent reports.
	var storedAcpConfig *storepb.AgentACPConfig
	if agent.Info != nil {
		storedAcpConfig = agent.Info.GetAcpConfig()
	}

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
	} else {
		patch.Info = &storepb.AgentInfo{}
	}
	patch.Info.Capability = convertToStoreAgentCapability(executor.BuildCapability(convertToV1AgentACPConfig(storedAcpConfig)))
	patch.Info.AcpConfig = storedAcpConfig

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
		TokenFamily:  tokenFamily,
		State:        "ACTIVE",
		SourceIP:     sourceIP,
		Fingerprint:  req.Msg.Fingerprint,
		AgentVersion: "",
		ConnectedAt:  now,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create agent session, error: %v", err))
	}

	// Mint the agent's initial access + refresh tokens only on the bootstrap
	// (first connect) path. On a reconnect the agent authenticated via an
	// access token it already holds from RefreshAgentToken, which also minted
	// and persisted the current refresh token — so minting another refresh
	// token here is redundant and was the source of both the
	// idx_agent_token_hash collision (a second identical-token insert in the
	// same second) and the unbounded growth of the refresh-token table.
	accessToken := ""
	refreshToken := ""
	accessTokenExpiresAt := time.Time{}
	if bootstrapTokenID != 0 {
		accessToken, err = auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeAccess, sessionID, s.profile.Mode, s.secret, accessTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
		}

		refreshToken, err = auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
		}

		refreshTokenHash := hashToken(refreshToken)
		if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
			AgentID:     agent.ID,
			TokenHash:   refreshTokenHash,
			TokenType:   storepb.AgentTokenType_REFRESH,
			TokenFamily: tokenFamily,
			State:       storepb.AgentTokenState_ACTIVE,
			Fingerprint: req.Msg.Fingerprint,
			ExpiresAt:   time.Now().Add(refreshTokenDuration),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store refresh token, error: %v", err))
		}

		accessTokenExpiresAt = time.Now().Add(accessTokenDuration)

		// The bootstrap token is single-use: once the connection has fully
		// succeeded (agent updated, old sessions terminated, new session + tokens
		// persisted), mark it CONSUMED so a leaked bootstrap token cannot be
		// replayed within its validity window to kick the legitimate agent off.
		consumedAt := time.Now()
		if err := s.store.UpdateAgentTokenState(ctx, bootstrapTokenID, storepb.AgentTokenState_CONSUMED, &consumedAt); err != nil {
			slog.Warn("failed to consume bootstrap token after connect", "agent", agent.ResourceID, "error", err)
		}
	}

	resp := &v1pb.ConnectAgentResponse{
		SessionId:     sessionID,
		NextNonce:     nonce,
		InitialStatus: convertToV1AgentStatus(updated.Status, updated.Deleted),
		AcpConfig:     convertToV1AgentACPConfig(storedAcpConfig),
	}
	if accessToken != "" {
		resp.AccessToken = accessToken
		resp.RefreshToken = refreshToken
		resp.AccessTokenExpiresAt = timestamppb.New(accessTokenExpiresAt)
	}
	return connect.NewResponse(resp), nil
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
		if session.AgentID != agent.ID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session does not belong to this agent"))
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

	if s.dispatcher != nil && !s.dispatcher.IsAgentConnected(agent.ID) {
		pending, err := s.store.GetNextPendingCommand(ctx, agent.ID)
		if err != nil {
			slog.Warn("failed to check pending commands during heartbeat", "error", err)
		} else if pending != nil {
			resp.CommandStreamRequired = true
			resp.PendingCommandHint = &v1pb.PendingCommandHint{
				CommandId:      pending.ID.String(),
				Command:        pending.Command,
				WorkingDir:     pending.WorkingDir,
				TimeoutSeconds: pending.TimeoutSeconds,
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

	// Verify the JWT signature before trusting any claim. Without this the
	// handler relied solely on a hash lookup: a refresh token whose
	// token_version was forged (or minted under a since-rotated secret) could
	// pass the hash check and be "upgraded" to the current token_version.
	claims, err := auth.ParseAgentToken(refreshTokenStr, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Wrap(err, "invalid refresh token"))
	}
	if claims.TokenType != auth.TokenTypeRefresh {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected refresh token, got %s", claims.TokenType))
	}

	tokenHash := hashToken(refreshTokenStr)
	storedToken, err := s.store.GetAgentTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up refresh token, error: %v", err))
	}
	if storedToken == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token"))
	}

	switch action := refreshReuseAction(storedToken.State); action {
	case refreshActionProceed:
		// Fresh token: proceed to rotate it below.
	case refreshActionRevokeFamily:
		// Reuse detected: a refresh token that was already exchanged (CONSUMED)
		// or revoked is being presented again. Revoke the entire family so
		// every token derived from the same bootstrap/rotation is invalidated,
		// then reject. Reusing a CONSUMED token previously only revoked the
		// single row and still issued a new token — i.e. it silently
		// succeeded, defeating reuse detection.
		if err := s.store.RevokeTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("refresh token reuse detected, token family revoked"))
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

	// Bind the token's version to the agent's current version. After a
	// RotateAgentToken/RevokeAgentToken the version increments and the old
	// family is revoked; if a refresh token from the old family survived the
	// revoke (e.g. a partial failure), its embedded version no longer matches
	// and we reject rather than minting a new token under the current version.
	if claims.TokenVersion != agent.TokenVersion {
		if err := s.store.RevokeTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke stale token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token version mismatch"))
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

// refreshReuseAction is the pure decision for how RefreshAgentToken should
// treat a stored refresh token given its current state. Extracted so the
// reuse-detection matrix is unit-testable without a DB.
//
//   - ACTIVE        → proceed (rotate the token)
//   - CONSUMED/REVOKED → reuse: revoke the whole family and reject
//   - anything else → reject as invalid
//
// CONSUMED is treated as reuse (not as a valid second use) because a refresh
// token is single-use: once exchanged it must never be accepted again.
type refreshAction int

const (
	refreshActionProceed refreshAction = iota
	refreshActionRevokeFamily
	refreshActionInvalid
)

func refreshReuseAction(state storepb.AgentTokenState) refreshAction {
	switch state {
	case storepb.AgentTokenState_ACTIVE:
		return refreshActionProceed
	case storepb.AgentTokenState_CONSUMED, storepb.AgentTokenState_REVOKED:
		return refreshActionRevokeFamily
	default:
		return refreshActionInvalid
	}
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
	TokenFamily  string `json:"token_family"`
	jwt.RegisteredClaims
}

type bootstrapAuthResult struct {
	agent       *store.AgentMessage
	tokenFamily string
	// tokenID is the DB row id of the bootstrap token, so ConnectAgent can mark
	// it CONSUMED once the connection succeeds (making the bootstrap token
	// single-use). Zero when the agent authenticated via an access token.
	tokenID int
}

func (s *AgentService) authenticateBootstrapToken(tokenStr string) (*bootstrapAuthResult, error) {
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

	tokenFamily := claims.TokenFamily
	if tokenFamily == "" {
		tokenFamily = claims.Subject
	}

	return &bootstrapAuthResult{agent: agent, tokenFamily: tokenFamily, tokenID: storedToken.ID}, nil
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
	if agent.CreatedBy != 0 {
		// Creator's user resource name (users/{id}); empty for legacy agents.
		result.CreatedBy = common.FormatUserUID(agent.CreatedBy)
	}
	return result
}

// cloneStoreAgentInfo returns a deep copy of info safe to mutate before a
// partial UpdateAgent, or a fresh empty AgentInfo when info is nil. The plain
// type assertion after proto.Clone is unchecked; centralizing it here keeps
// revive's unchecked-type-assertion quiet without hiding the panic risk at
// each call site (the input is always *storepb.AgentInfo here).
func cloneStoreAgentInfo(info *storepb.AgentInfo) *storepb.AgentInfo {
	if info == nil {
		return &storepb.AgentInfo{}
	}
	cloned := proto.Clone(info)
	patchInfo, ok := cloned.(*storepb.AgentInfo)
	if !ok || patchInfo == nil {
		return &storepb.AgentInfo{}
	}
	return patchInfo
}

func convertToV1AgentInfo(info *storepb.AgentInfo) *v1pb.AgentInfo {
	if info == nil {
		return nil
	}
	return &v1pb.AgentInfo{
		AgentType:          info.AgentType,
		Hostname:           info.Hostname,
		Os:                 info.Os,
		Arch:               info.Arch,
		Ip:                 info.Ip,
		Version:            info.Version,
		Labels:             info.Labels,
		Capability:         convertToV1AgentCapability(info.Capability),
		AvailableProviders: convertToV1Providers(info.AvailableProviders),
		AcpConfig:          convertToV1AgentACPConfig(info.AcpConfig),
	}
}

func convertToStoreAgentInfo(info *v1pb.AgentInfo) *storepb.AgentInfo {
	if info == nil {
		return nil
	}
	return &storepb.AgentInfo{
		AgentType:  info.AgentType,
		Hostname:   info.Hostname,
		Os:         info.Os,
		Arch:       info.Arch,
		Ip:         info.Ip,
		Version:    info.Version,
		Labels:     info.Labels,
		Capability: convertToStoreAgentCapability(info.Capability),
		// available_providers is agent-owned: store exactly what the agent reported.
		AvailableProviders: convertToStoreProviders(info.AvailableProviders),
		// AcpConfig is server-owned; never overwrite it from agent-reported info.
		AcpConfig: nil,
	}
}

func convertToV1AgentACPConfig(cfg *storepb.AgentACPConfig) *v1pb.AgentACPConfig {
	if cfg == nil {
		return nil
	}
	return &v1pb.AgentACPConfig{
		Executable:    cfg.Executable,
		Args:          cfg.Args,
		AllowEnv:      cfg.AllowEnv,
		Provider:      cfg.Provider,
		Model:         cfg.Model,
		CustomEnv:     cfg.CustomEnv,
		PersonaPrompt: cfg.PersonaPrompt,
	}
}

func convertToStoreAgentACPConfig(cfg *v1pb.AgentACPConfig) *storepb.AgentACPConfig {
	if cfg == nil {
		return nil
	}
	return &storepb.AgentACPConfig{
		Executable:    cfg.Executable,
		Args:          cfg.Args,
		AllowEnv:      cfg.AllowEnv,
		Provider:      cfg.Provider,
		Model:         cfg.Model,
		CustomEnv:     cfg.CustomEnv,
		PersonaPrompt: cfg.PersonaPrompt,
	}
}

func convertToV1Providers(in []*storepb.AgentProviderInfo) []*v1pb.AgentProviderInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]*v1pb.AgentProviderInfo, 0, len(in))
	for _, p := range in {
		out = append(out, &v1pb.AgentProviderInfo{
			ProviderId:                p.ProviderId,
			DisplayName:               p.DisplayName,
			Version:                   p.Version,
			ExecutablePath:            p.ExecutablePath,
			Models:                    convertToV1Models(p.Models),
			SupportsModelConfigOption: p.SupportsModelConfigOption,
			DetectedAt:                p.DetectedAt,
		})
	}
	return out
}

func convertToStoreProviders(in []*v1pb.AgentProviderInfo) []*storepb.AgentProviderInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]*storepb.AgentProviderInfo, 0, len(in))
	for _, p := range in {
		out = append(out, &storepb.AgentProviderInfo{
			ProviderId:                p.ProviderId,
			DisplayName:               p.DisplayName,
			Version:                   p.Version,
			ExecutablePath:            p.ExecutablePath,
			Models:                    convertToStoreModels(p.Models),
			SupportsModelConfigOption: p.SupportsModelConfigOption,
			DetectedAt:                p.DetectedAt,
		})
	}
	return out
}

func convertToV1Models(in []*storepb.AgentModelOption) []*v1pb.AgentModelOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]*v1pb.AgentModelOption, 0, len(in))
	for _, m := range in {
		out = append(out, &v1pb.AgentModelOption{
			Value:       m.Value,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	return out
}

func convertToStoreModels(in []*v1pb.AgentModelOption) []*storepb.AgentModelOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]*storepb.AgentModelOption, 0, len(in))
	for _, m := range in {
		out = append(out, &storepb.AgentModelOption{
			Value:       m.Value,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	return out
}

func convertToV1AgentCapability(capability *storepb.AgentCapability) *v1pb.AgentCapability {
	if capability == nil {
		return nil
	}
	return &v1pb.AgentCapability{
		SupportsAcp:        capability.SupportsAcp,
		MaxTimeoutSeconds:  capability.MaxTimeoutSeconds,
		SupportsDiff:       capability.SupportsDiff,
		SupportsRawEvents:  capability.SupportsRawEvents,
		SupportsToolTraces: capability.SupportsToolTraces,
		MaxEventCount:      capability.MaxEventCount,
		MaxOutputBytes:     capability.MaxOutputBytes,
	}
}

func convertToStoreAgentCapability(capability *v1pb.AgentCapability) *storepb.AgentCapability {
	if capability == nil {
		return nil
	}
	return &storepb.AgentCapability{
		SupportsAcp:        capability.SupportsAcp,
		MaxTimeoutSeconds:  capability.MaxTimeoutSeconds,
		SupportsDiff:       capability.SupportsDiff,
		SupportsRawEvents:  capability.SupportsRawEvents,
		SupportsToolTraces: capability.SupportsToolTraces,
		MaxEventCount:      capability.MaxEventCount,
		MaxOutputBytes:     capability.MaxOutputBytes,
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

func (s *AgentService) UpdateAgentACPConfig(ctx context.Context, req *connect.Request[v1pb.UpdateAgentACPConfigRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := validateAgentACPConfig(req.Msg.AcpConfig); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	// Preserve the rest of AgentInfo (hostname/os/capability/available_providers/
	// labels); only AcpConfig is admin-owned and replaced here. Previously this
	// built a fresh Info{AcpConfig:...} and clobbered the agent-reported fields.
	patchInfo := cloneStoreAgentInfo(agent.Info)
	patchInfo.AcpConfig = convertToStoreAgentACPConfig(req.Msg.AcpConfig)

	patch := &store.UpdateAgentMessage{Info: patchInfo}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RefreshAgentProviders asks the agent daemon to re-probe its host for
// installed LLM agent providers + models, then persists the fresh result into
// agent.info.available_providers and returns it. Requires the agent to be
// online (the probe runs on the agent's host, reached via the bidi stream).
func (s *AgentService) RefreshAgentProviders(ctx context.Context, req *connect.Request[v1pb.RefreshAgentProvidersRequest]) (*connect.Response[v1pb.RefreshAgentProvidersResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	if !s.dispatcher.IsAgentConnected(agent.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent is not connected; cannot probe providers"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingDiscover(requestID)
	defer s.dispatcher.CancelPendingDiscover(requestID)

	if err := s.dispatcher.SendDiscoverProviders(agent.ID, requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request provider discovery"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("provider discovery returned no result"))
		}
		// Persist the fresh provider list into agent.info, preserving every
		// other AgentInfo field (only available_providers is agent-owned here).
		patchInfo := cloneStoreAgentInfo(agent.Info)
		patchInfo.AvailableProviders = convertToStoreProviders(msg.Providers)
		if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{Info: patchInfo}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to persist discovered providers"))
		}
		return connect.NewResponse(&v1pb.RefreshAgentProvidersResponse{
			Providers: convertToV1Providers(patchInfo.AvailableProviders),
		}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for provider discovery"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// validateAgentACPConfig checks the user-configurable ACP fields. A built-in
// provider (opencode, claude-code) supplies its own launch command, so
// executable is only required for the "custom"/empty provider. Every
// allow_env and custom_env key must be a valid env var name.
func validateAgentACPConfig(cfg *v1pb.AgentACPConfig) error {
	if cfg == nil {
		return errors.New("acp_config must be set")
	}
	if cfg.Provider != "" && !knownProviderID(cfg.Provider) {
		return errors.Errorf("invalid acp_config.provider %q: must be a built-in id or \"custom\"", cfg.Provider)
	}
	// A built-in provider derives its command from the registry; anything else
	// requires a raw executable.
	_, isBuiltin := provider.Default().Lookup(cfg.Provider)
	if !isBuiltin && cfg.Executable == "" {
		return errors.New("acp_config.executable must be set when provider is not a built-in")
	}
	for _, name := range cfg.AllowEnv {
		if !envVarNameRegex.MatchString(name) {
			return errors.Errorf("invalid allow_env entry %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", name)
		}
	}
	for key := range cfg.CustomEnv {
		if !envVarNameRegex.MatchString(key) {
			return errors.Errorf("invalid custom_env key %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", key)
		}
	}
	return nil
}

// knownProviderID reports whether id is a recognized provider id (a built-in or
// the "custom" escape hatch).
func knownProviderID(id string) bool {
	if id == "custom" {
		return true
	}
	_, ok := provider.Default().Lookup(id)
	return ok
}
