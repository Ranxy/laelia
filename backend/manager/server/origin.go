package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
)

// originValidationMiddleware rejects requests whose Origin is neither
// same-origin with the request Host nor explicitly allowed. Browsers send
// Origin on every fetch/XHR request, so this closes the CSRF hole for
// credentialed cross-site calls (e.g. when the access-token cookie is
// SameSite=None). Requests without an Origin header (curl, machines, agents,
// top-level navigations) are not validated — those are covered by the cookie
// SameSite policy instead.
func originValidationMiddleware(profile *config.Profile) echo.MiddlewareFunc {
	allowed := make(map[string]struct{}, len(profile.AllowedOrigins))
	for _, origin := range profile.AllowedOrigins {
		allowed[normalizeOrigin(origin)] = struct{}{}
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			origin := c.Request().Header.Get("Origin")
			if origin == "" {
				return next(c)
			}
			if isSameOrigin(origin, c.Request()) {
				return next(c)
			}
			if _, ok := allowed[normalizeOrigin(origin)]; ok {
				return next(c)
			}
			// Dev mode: the vite dev server (localhost) may call a remote dev
			// API cross-site; the dev CORS middleware only reflects localhost
			// origins, so this does not open the door to arbitrary sites.
			if profile.Mode == common.ReleaseModeDev && isLocalhostOrigin(origin) {
				return next(c)
			}
			return c.NoContent(http.StatusForbidden)
		}
	}
}

// isSameOrigin reports whether origin (scheme://host[:port]) matches the
// request Host. The scheme is intentionally not compared: behind a
// TLS-terminating reverse proxy the server cannot reliably know the public
// scheme, and a host mismatch is what matters for CSRF.
func isSameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// isLocalhostOrigin reports whether origin is a localhost/loopback origin.
func isLocalhostOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// normalizeOrigin lowercases scheme and host and strips the path so allowlist
// entries compare consistently.
func normalizeOrigin(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return strings.ToLower(strings.TrimSuffix(origin, "/"))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
