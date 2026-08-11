package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestGetTokenCookie_SameSiteDefaultsToLax(t *testing.T) {
	profile := &config.Profile{Mode: common.ReleaseModeProd}
	for _, origin := range []string{"https://app.example.com", "http://app.example.com"} {
		cookie := GetTokenCookie(context.Background(), &store.Store{}, profile, origin, "token")
		assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		assert.Equal(t, "token", cookie.Value)
		assert.True(t, cookie.HttpOnly)
	}
}

func TestGetTokenCookie_SameSiteStrict(t *testing.T) {
	profile := &config.Profile{Mode: common.ReleaseModeProd, CookieSameSite: "strict"}
	cookie := GetTokenCookie(context.Background(), &store.Store{}, profile, "https://app.example.com", "token")
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
}

func TestGetTokenCookie_SameSiteNoneOnlyOverHTTPS(t *testing.T) {
	profile := &config.Profile{Mode: common.ReleaseModeProd, CookieSameSite: "none"}
	httpsCookie := GetTokenCookie(context.Background(), &store.Store{}, profile, "https://app.example.com", "token")
	assert.Equal(t, http.SameSiteNoneMode, httpsCookie.SameSite)
	assert.True(t, httpsCookie.Secure)

	// Browsers reject SameSite=None without Secure; fall back to Lax over HTTP.
	httpCookie := GetTokenCookie(context.Background(), &store.Store{}, profile, "http://app.example.com", "token")
	assert.Equal(t, http.SameSiteLaxMode, httpCookie.SameSite)
	assert.False(t, httpCookie.Secure)
}

func TestGetTokenCookie_UnsetCookieMatchesPolicy(t *testing.T) {
	profile := &config.Profile{Mode: common.ReleaseModeProd}
	cookie := GetTokenCookie(context.Background(), &store.Store{}, profile, "https://app.example.com", "")
	assert.Equal(t, "", cookie.Value)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.True(t, cookie.Secure)
}
