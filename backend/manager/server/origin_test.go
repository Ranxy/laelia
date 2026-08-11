package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

func newOriginTestEcho(profile *config.Profile) *echo.Echo {
	e := echo.New()
	e.Use(originValidationMiddleware(profile))
	e.GET("/v1/test", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })
	return e
}

func doOriginRequest(e *echo.Echo, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Host = "app.example.com"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestOriginValidation_SameOriginAllowed(t *testing.T) {
	e := newOriginTestEcho(&config.Profile{Mode: common.ReleaseModeProd})
	rec := doOriginRequest(e, "https://app.example.com")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOriginValidation_NoOriginAllowed(t *testing.T) {
	e := newOriginTestEcho(&config.Profile{Mode: common.ReleaseModeProd})
	rec := doOriginRequest(e, "")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOriginValidation_ForeignOriginRejected(t *testing.T) {
	e := newOriginTestEcho(&config.Profile{Mode: common.ReleaseModeProd})
	rec := doOriginRequest(e, "https://evil.example")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOriginValidation_AllowlistedOriginAllowed(t *testing.T) {
	profile := &config.Profile{
		Mode:           common.ReleaseModeProd,
		AllowedOrigins: []string{"https://front.example.com"},
	}
	e := newOriginTestEcho(profile)
	rec := doOriginRequest(e, "https://front.example.com")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = doOriginRequest(e, "https://evil.example")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOriginValidation_DevAllowsLocalhostOnly(t *testing.T) {
	e := newOriginTestEcho(&config.Profile{Mode: common.ReleaseModeDev})
	rec := doOriginRequest(e, "http://localhost:5173")
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = doOriginRequest(e, "https://evil.example")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOriginValidation_ProdRejectsLocalhost(t *testing.T) {
	e := newOriginTestEcho(&config.Profile{Mode: common.ReleaseModeProd})
	rec := doOriginRequest(e, "http://localhost:5173")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCorsMiddleware_DevReflectsLocalhostOnly(t *testing.T) {
	e := echo.New()
	mw := corsMiddleware(&config.Profile{Mode: common.ReleaseModeDev})
	require.NotNil(t, mw)
	e.Use(mw)
	e.GET("/v1/test", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doOriginRequest(e, "http://localhost:5173")
	assert.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))

	rec = doOriginRequest(e, "https://evil.example")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_DevReflectsAllowlistToo(t *testing.T) {
	profile := &config.Profile{
		Mode:           common.ReleaseModeDev,
		AllowedOrigins: []string{"https://laeliapage.metaxisdata.com"},
	}
	e := echo.New()
	mw := corsMiddleware(profile)
	require.NotNil(t, mw)
	e.Use(mw)
	e.GET("/v1/test", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doOriginRequest(e, "https://laeliapage.metaxisdata.com")
	assert.Equal(t, "https://laeliapage.metaxisdata.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))

	rec = doOriginRequest(e, "https://evil.example")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_ProdAllowlist(t *testing.T) {
	profile := &config.Profile{
		Mode:           common.ReleaseModeProd,
		AllowedOrigins: []string{"https://front.example.com"},
	}
	e := echo.New()
	mw := corsMiddleware(profile)
	require.NotNil(t, mw)
	e.Use(mw)
	e.GET("/v1/test", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doOriginRequest(e, "https://front.example.com")
	assert.Equal(t, "https://front.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))

	rec = doOriginRequest(e, "https://evil.example")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_ProdEmptyAllowlistDisabled(t *testing.T) {
	assert.Nil(t, corsMiddleware(&config.Profile{Mode: common.ReleaseModeProd}))
}

func TestIsLocalhostOrigin(t *testing.T) {
	assert.True(t, isLocalhostOrigin("http://localhost:5173"))
	assert.True(t, isLocalhostOrigin("https://127.0.0.1:8181"))
	assert.True(t, isLocalhostOrigin("http://[::1]:5173"))
	assert.False(t, isLocalhostOrigin("https://app.example.com"))
	assert.False(t, isLocalhostOrigin("not a url"))
}

func TestNormalizeOrigin(t *testing.T) {
	assert.Equal(t, "https://app.example.com", normalizeOrigin("https://App.Example.com/"))
	assert.Equal(t, "http://localhost:5173", normalizeOrigin("http://localhost:5173"))
}
