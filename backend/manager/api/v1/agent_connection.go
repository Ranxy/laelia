package v1

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/store"
)

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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a holder of laelia.agents.edit can force-disconnect this agent"))
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
	patch.Info.Capability = convertToStoreAgentCapability(buildCapabilityForACPConfig(convertToV1AgentACPConfig(storedAcpConfig)))
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

	// The daemon receives the concrete config: a global-provider reference is
	// resolved to api_provider/api_key/model here, never in the v1 API surface.
	resolvedAcp, resolveErr := resolveAcpConfigForDaemon(ctx, s.store, convertToV1AgentACPConfig(storedAcpConfig))
	if resolveErr != nil {
		slog.Info("failed to resolve acp config for agent connect", "agent", agent.ResourceID, log.WithError(resolveErr))
	}
	resp := &v1pb.ConnectAgentResponse{
		SessionId:     sessionID,
		NextNonce:     nonce,
		InitialStatus: convertToV1AgentStatus(updated.Status, updated.Deleted, true, updated.Enabled),
		AcpConfig:     resolvedAcp,
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

	// The per-heartbeat agent.status rewrite used to cost a full JSONB marshal
	// + UPDATE + cache refill per agent per heartbeat. The HeartbeatBuffer now
	// batches last_heartbeat_at (session touch + status jsonb_set) for every
	// agent in one multi-row UPDATE per flush window; the immediate
	// TouchAgentSession above keeps the session row fresh on the request path.
	nowSec := time.Now().Unix()
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

// agentReachable reports whether an agent should present as online. Under the
// machine-hosts-many model the machine — not the agent — heartbeats, so an
// agent's liveness is NOT derived from a per-agent heartbeat timestamp
// (which is no longer written and would always read as offline). An agent is
// online when its own runner has a live AgentChannel (the precise signal) OR
// the machine it is bound to is connected (the machine app hosts the agent and
// will pick it up over its MachineChannel). The second clause matches the
// product model — "a connected machine's agents are online" — and covers the
// brief window before a freshly created agent's runner opens its stream, so
// the agent is online the moment it is created on a connected machine. Both
// go false when the machine disconnects (UnregisterMachine detaches every
// owned agent session), so the agent reports offline with its machine.
func agentReachable(d *dispatcher.Dispatcher, agentID, machineID int) bool {
	if d == nil {
		return false
	}
	return d.IsAgentConnected(agentID) || d.IsMachineConnected(machineID)
}
