package v1

import (
	"context"
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

func (s *MachineService) RevokeMachineToken(ctx context.Context, req *connect.Request[v1pb.RevokeMachineTokenRequest]) (*connect.Response[v1pb.RevokeMachineTokenResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can revoke this machine's tokens"))
	}

	newTokenVersion := machine.TokenVersion + 1
	nowRotated := time.Now()
	if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine token version, error: %v", err))
	}

	if err := s.store.RevokeAllMachineTokens(ctx, machine.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke machine tokens, error: %v", err))
	}

	terminateReason := "token_revoked"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllMachineSessions(ctx, machine.ID, terminateReason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate machine sessions, error: %v", err))
	}
	if s.dispatcher != nil {
		s.dispatcher.UnregisterMachine(machine.ID)
	}

	return connect.NewResponse(&v1pb.RevokeMachineTokenResponse{}), nil
}
func (s *MachineService) RefreshMachineToken(ctx context.Context, req *connect.Request[v1pb.RefreshMachineTokenRequest]) (*connect.Response[v1pb.RefreshMachineTokenResponse], error) {
	principal, stored, err := validateRefreshToken(
		req.Msg.RefreshToken,
		req.Msg.Fingerprint,
		func(token string) (int, string, error) {
			claims, err := auth.ParseMachineToken(token, s.secret)
			if err != nil {
				return 0, "", err
			}
			return int(claims.TokenVersion), claims.TokenType, nil
		},
		func(hash string) (refreshStoredToken, error) {
			t, err := s.store.GetMachineTokenByHash(ctx, hash)
			if err != nil {
				return refreshStoredToken{}, err
			}
			if t == nil {
				return refreshStoredToken{}, nil
			}
			return refreshStoredToken{ID: t.ID, PrincipalID: t.MachineID, Family: t.TokenFamily, State: int32(t.State), ExpiresAt: t.ExpiresAt, Fingerprint: t.Fingerprint}, nil
		},
		func(state int32) refreshAction {
			return machineRefreshReuseAction(storepb.MachineTokenState(state))
		},
		func(family string) error {
			return s.store.RevokeMachineTokenFamily(ctx, family)
		},
		func(id int) (refreshPrincipal, error) {
			machine, err := s.store.GetMachine(ctx, id)
			if err != nil {
				return refreshPrincipal{}, err
			}
			if machine == nil {
				return refreshPrincipal{Deleted: true}, nil
			}
			return refreshPrincipal{ID: machine.ID, Name: machine.Name, ResourceID: machine.ResourceID, TokenVersion: machine.TokenVersion, Deleted: machine.Deleted}, nil
		},
	)
	if err != nil {
		return nil, err
	}

	accessToken, err := auth.GenerateMachineTokenWithSession(principal.Name, principal.ResourceID, principal.TokenVersion, auth.TokenTypeAccess, "", s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	// Multi-use: reuse the same refresh token across reconnects. Mint a
	// replacement only when the current one is within the rolling-renewal
	// window of expiry, so the credential self-renews without manual rotation
	// and the machine never dies from expiry as long as it reconnects within
	// the window. The old token is left ACTIVE and expires on its own within
	// the window (a pre-renewal thief is bounded by it); it is not consumed, so
	// a lost renewal response is safely retryable.
	newRefreshToken := ""
	if time.Until(stored.ExpiresAt) < machineRefreshRotateWindow {
		newRefreshToken, err = auth.GenerateMachineTokenWithSession(principal.Name, principal.ResourceID, principal.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, machineRefreshTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
		}
		if err := s.store.CreateMachineToken(ctx, &store.MachineTokenMessage{
			MachineID:   principal.ID,
			TokenHash:   hashToken(newRefreshToken),
			TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
			TokenFamily: stored.Family,
			State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
			Fingerprint: req.Msg.Fingerprint,
			ExpiresAt:   time.Now().Add(machineRefreshTokenDuration),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new refresh token, error: %v", err))
		}
	}

	return connect.NewResponse(&v1pb.RefreshMachineTokenResponse{
		AccessToken:          accessToken,
		RefreshToken:         newRefreshToken,
		AccessTokenExpiresAt: timestamppb.New(time.Now().Add(accessTokenDuration)),
	}), nil
}

func machineRefreshReuseAction(state storepb.MachineTokenState) refreshAction {
	switch state {
	case storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE:
		return refreshActionProceed
	case storepb.MachineTokenState_MACHINE_TOKEN_CONSUMED, storepb.MachineTokenState_MACHINE_TOKEN_REVOKED:
		return refreshActionRevokeFamily
	default:
		return refreshActionInvalid
	}
}
