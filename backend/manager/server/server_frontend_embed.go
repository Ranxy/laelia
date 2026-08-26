//go:build embed_frontend

package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func init() {
	// .webmanifest is not in Go's default mime table on all platforms; without
	// this the PWA manifest would be served as application/octet-stream and
	// browsers would refuse to treat the app as installable.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

//go:embed dist
var embeddedFrontend embed.FS

// frontendStaticSkipper keeps API, health, and hashed-asset paths out of the
// SPA static middleware so they keep their structured responses instead of
// falling back to index.html.
func frontendStaticSkipper(c *echo.Context) bool {
	p := c.Request().URL.Path
	return strings.HasPrefix(p, "/v1") || strings.HasPrefix(p, "/machine/") || p == "/metrics" || p == "/healthz" || p == "/api/version" || strings.HasPrefix(p, "/assets/")
}

// pwaStaticHeadersMiddleware sets correct cache headers for PWA static files.
// The SW and manifest must never be cached for long (so updates propagate),
// SPA HTML fallbacks must revalidate every load, and icons can be cached
// aggressively since they are immutable by convention.
func pwaStaticHeadersMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		p := c.Request().URL.Path
		switch {
		case p == "/sw.js" || p == "/manifest.webmanifest":
			c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
		case strings.HasPrefix(p, "/icons/"):
			c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=31536000")
		default:
			// SPA HTML fallback (index.html) or any non-asset path: revalidate
			// each navigation so new hashed asset references are picked up.
			if isHtmlPath(p) {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
			}
		}
		return next(c)
	}
}

// isHtmlPath reports whether a request path is served as the SPA index.html
// (i.e. it has no file extension and is not an API/asset path).
func isHtmlPath(p string) bool {
	if p == "/" || strings.HasSuffix(p, "/") {
		return true
	}
	last := p[strings.LastIndex(p, "/")+1:]
	return !strings.Contains(last, ".")
}

func embedFrontend(e *echo.Echo) {
	distFS, err := fs.Sub(embeddedFrontend, "dist")
	if err != nil {
		slog.Error("embedded frontend dist is missing; run the frontend build before building with embed_frontend", "error", err)
		panic(err)
	}

	e.Use(pwaStaticHeadersMiddleware)
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Skipper:    frontendStaticSkipper,
		HTML5:      true,
		Filesystem: distFS,
	}))

	assetsFS, err := fs.Sub(distFS, "assets")
	if err != nil {
		slog.Error("embedded frontend assets are missing", "error", err)
		panic(err)
	}
	// Hashed assets are immutable: serve them with a long cache lifetime. The
	// cache header is only set when the file actually exists so a stale
	// browser cache never pins a 404 during a rolling deploy.
	e.Match(
		[]string{http.MethodGet, http.MethodHead},
		"/assets/*",
		echo.StaticDirectoryHandler(assetsFS, false),
		func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error {
				p := c.Param("*")
				if unescaped, err := url.PathUnescape(p); err == nil {
					p = unescaped
				}
				name := path.Clean(strings.TrimPrefix(p, "/"))
				if info, err := fs.Stat(assetsFS, name); err == nil && !info.IsDir() {
					c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=31536000, immutable")
				}
				return next(c)
			}
		},
	)
}
