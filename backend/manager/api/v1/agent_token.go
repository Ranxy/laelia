package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/store"
)

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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a holder of laelia.agents.edit can rotate this agent's token"))
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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a holder of laelia.agents.edit can revoke this agent's tokens"))
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

func (s *AgentService) RefreshAgentToken(ctx context.Context, req *connect.Request[v1pb.RefreshAgentTokenRequest]) (*connect.Response[v1pb.RefreshAgentTokenResponse], error) {
	principal, stored, err := validateRefreshToken(
		req.Msg.RefreshToken,
		req.Msg.Fingerprint,
		func(token string) (int, string, error) {
			claims, err := auth.ParseAgentToken(token, s.secret)
			if err != nil {
				return 0, "", err
			}
			return int(claims.TokenVersion), claims.TokenType, nil
		},
		func(hash string) (refreshStoredToken, error) {
			t, err := s.store.GetAgentTokenByHash(ctx, hash)
			if err != nil {
				return refreshStoredToken{}, err
			}
			if t == nil {
				return refreshStoredToken{}, nil
			}
			return refreshStoredToken{ID: t.ID, PrincipalID: t.AgentID, Family: t.TokenFamily, State: int32(t.State), ExpiresAt: t.ExpiresAt, Fingerprint: t.Fingerprint}, nil
		},
		func(state int32) refreshAction {
			return refreshReuseAction(storepb.AgentTokenState(state))
		},
		func(family string) error {
			return s.store.RevokeTokenFamily(ctx, family)
		},
		func(id int) (refreshPrincipal, error) {
			agent, err := s.store.GetAgent(ctx, id)
			if err != nil {
				return refreshPrincipal{}, err
			}
			if agent == nil {
				return refreshPrincipal{Deleted: true}, nil
			}
			return refreshPrincipal{ID: agent.ID, Name: agent.Name, ResourceID: agent.ResourceID, TokenVersion: agent.TokenVersion, Deleted: agent.Deleted}, nil
		},
	)
	if err != nil {
		return nil, err
	}

	if stored.State == int32(storepb.AgentTokenState_ACTIVE) {
		consumedAt := time.Now()
		if err := s.store.UpdateAgentTokenState(ctx, stored.ID, storepb.AgentTokenState_CONSUMED, &consumedAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to mark refresh token as consumed, error: %v", err))
		}
		s.scheduleTokenRevoke(stored.ID, stored.Family)
	}

	accessToken, err := auth.GenerateAgentTokenWithSession(principal.Name, principal.ResourceID, principal.TokenVersion, auth.TokenTypeAccess, "", s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	newRefreshToken, err := auth.GenerateAgentTokenWithSession(principal.Name, principal.ResourceID, principal.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
	}

	newTokenHash := hashToken(newRefreshToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     principal.ID,
		TokenHash:   newTokenHash,
		TokenType:   storepb.AgentTokenType_REFRESH,
		TokenFamily: stored.Family,
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
