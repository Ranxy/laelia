package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// token="" => unset
func GetTokenCookie(ctx context.Context, stores *store.Store, profile *config.Profile, origin, token string) *http.Cookie {
	isHTTPS := strings.HasPrefix(origin, "https")
	sameSite := cookieSameSite(profile, isHTTPS)
	if token == "" {
		return &http.Cookie{
			Name:     AccessTokenCookieName,
			Value:    "",
			Expires:  time.Unix(0, 0),
			Path:     "/",
			Secure:   isHTTPS,
			SameSite: sameSite,
		}
	}
	tokenDuration := GetTokenDuration(ctx, stores)
	return &http.Cookie{
		Name:  AccessTokenCookieName,
		Value: token,
		// CookieExpDuration expires slightly earlier than the jwt expiration. Client would be logged out if the user
		// cookie expires, thus the client would always logout first before attempting to make a request with the expired jwt.
		// Suppose we have a valid refresh token, we will refresh the token in 2 cases:
		// 1. The access token is about to expire in <<refreshThresholdDuration>>
		// 2. The access token has already expired, we refresh the token so that the ongoing request can pass through.
		Expires: time.Now().Add(tokenDuration - 1*time.Second),
		Path:    "/",
		// Http-only helps mitigate the risk of client side script accessing the protected cookie.
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: sameSite,
	}
}

// cookieSameSite returns the SameSite policy for the access-token cookie.
// Lax is the safe default: it blocks cross-site subresource requests (the
// CSRF vector) while keeping same-site deployments (including frontend and
// API on different subdomains) and top-level SSO redirects working. "strict"
// and "none" can be opted into via LAELIA_COOKIE_SAMESITE; "none" is only
// honored over HTTPS (browsers reject SameSite=None without Secure) and is
// meant for deployments that serve the frontend from a different site than
// the API.
func cookieSameSite(profile *config.Profile, isHTTPS bool) http.SameSite {
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(profile.CookieSameSite) {
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		if isHTTPS {
			sameSite = http.SameSiteNoneMode
		}
	default:
		sameSite = http.SameSiteLaxMode
	}
	return sameSite
}

func GetTokenDuration(_ context.Context, _ *store.Store) time.Duration {
	tokenDuration := DefaultTokenDuration
	// maybe we can add a setting for token duration in the future

	return tokenDuration
}
