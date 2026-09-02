package v1

import (
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/api/auth"
)

type refreshPrincipal struct {
	ID           int
	Name         string
	ResourceID   string
	TokenVersion int
	Deleted      bool
}

type refreshStoredToken struct {
	ID          int
	PrincipalID int
	Family      string
	State       int32
	ExpiresAt   time.Time
	Fingerprint string
}

// validateRefreshToken performs the shared validation/security checks for
// AgentService.RefreshAgentToken and MachineService.RefreshMachineToken:
// JWT parsing, hash lookup, reuse/revoke decision, expiry, fingerprint,
// principal existence/deletion, and token-version binding. Callers supply the
// entity-specific lookups and revoke actions, then perform their own
// access/refresh-token generation.
func validateRefreshToken(
	tokenStr string,
	fingerprint string,
	parse func(token string) (tokenVersion int, tokenType string, err error),
	lookup func(hash string) (refreshStoredToken, error),
	reuseAction func(state int32) refreshAction,
	revokeFamily func(family string) error,
	loadPrincipal func(id int) (refreshPrincipal, error),
) (refreshPrincipal, refreshStoredToken, error) {
	if tokenStr == "" {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	tokenVersion, tokenType, err := parse(tokenStr)
	if err != nil {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.Wrap(err, "invalid refresh token"))
	}
	if tokenType != auth.TokenTypeRefresh {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected refresh token, got %s", tokenType))
	}

	stored, err := lookup(hashToken(tokenStr))
	if err != nil {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up refresh token, error: %v", err))
	}
	if stored.ID == 0 {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token"))
	}

	switch action := reuseAction(stored.State); action {
	case refreshActionProceed:
	case refreshActionRevokeFamily:
		if err := revokeFamily(stored.Family); err != nil {
			return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke token family, error: %v", err))
		}
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodePermissionDenied, errors.New("refresh token reuse detected, token family revoked"))
	default:
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token state"))
	}

	if time.Now().After(stored.ExpiresAt) {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token expired"))
	}
	if fingerprint != "" && stored.Fingerprint != "" && fingerprint != stored.Fingerprint {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodePermissionDenied, errors.New("fingerprint mismatch, possible token theft detected"))
	}

	principal, err := loadPrincipal(stored.PrincipalID)
	if err != nil {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeInternal, errors.Errorf("failed to load principal, error: %v", err))
	}
	if principal.Deleted {
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.New("principal not found or deactivated"))
	}
	if tokenVersion != principal.TokenVersion {
		if err := revokeFamily(stored.Family); err != nil {
			return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke stale token family, error: %v", err))
		}
		return refreshPrincipal{}, refreshStoredToken{}, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token version mismatch"))
	}

	return principal, stored, nil
}
