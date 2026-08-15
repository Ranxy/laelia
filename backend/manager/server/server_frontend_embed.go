//go:build embed_frontend

package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

//go:embed dist
var embeddedFrontend embed.FS

// frontendStaticSkipper keeps API, health, and hashed-asset paths out of the
// SPA static middleware so they keep their structured responses instead of
// falling back to index.html.
func frontendStaticSkipper(c *echo.Context) bool {
	p := c.Request().URL.Path
	return strings.HasPrefix(p, "/v1") || strings.HasPrefix(p, "/machine/") || p == "/metrics" || p == "/healthz" || strings.HasPrefix(p, "/assets/")
}

func embedFrontend(e *echo.Echo) {
	distFS, err := fs.Sub(embeddedFrontend, "dist")
	if err != nil {
		slog.Error("embedded frontend dist is missing; run the frontend build before building with embed_frontend", "error", err)
		panic(err)
	}

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
