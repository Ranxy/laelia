package server

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo-contrib/v5/echoprometheus"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"

	connectcors "connectrpc.com/cors"
)

func configureEchoRouters(
	e *echo.Echo,
	profile *config.Profile,
	stores *store.Store,
) {
	e.Use(recoverMiddleware)
	e.Use(securityHeadersMiddleware())
	e.Use(originValidationMiddleware(profile))

	if mw := corsMiddleware(profile); mw != nil {
		e.Use(mw)
	}

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogMethod: true,
		LogStatus: true,
		LogValuesFunc: func(_ *echo.Context, values middleware.RequestLoggerValues) error {
			if values.Error != nil {
				slog.Error("echo request logger", "method", values.Method, "uri", values.URI, "status", values.Status, log.WithError(values.Error))
			}
			return nil
		},
	}))

	// Machine binary download routes must be registered before the SPA static
	// middleware so they are not swallowed by the HTML5 fallback.
	registerMachineDownloadRoutes(e, stores)

	embedFrontend(e)

	// Prometheus metrics - use custom registry to avoid duplicate registration in tests
	registry := prometheus.NewRegistry()
	e.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
		Subsystem:  "api",
		Registerer: registry,
	}))
	// Fold the local echo registry with the default registry at scrape
	// time. The local registry isolates echo HTTP middleware metrics from
	// duplicate-registration errors in tests; the default registry catches
	// promauto-registered metrics from other packages (e.g. db_metrics,
	// the tidb dispatcher fallback counter, and Go runtime metrics auto-
	// registered by client_golang). Without this fold, those metrics are
	// registered but never exposed at /metrics.
	//
	// Why bypass echoprometheus.NewHandlerWithConfig: that helper only
	// applies promhttp.InstrumentMetricHandler when its Gatherer also
	// implements prometheus.Registerer (echoprometheus/prometheus.go:129).
	// prometheus.Gatherers (slice type) does not implement Registerer,
	// so passing the fold there silently drops scrape-health
	// self-instrumentation (promhttp_metric_handler_requests_total etc.).
	// Use promhttp directly: pass the local registry as the Registerer
	// for self-instrumentation; pass the Gatherers fold as the gather
	// source. Both observability surfaces preserved.
	e.GET("/metrics", echo.WrapHandler(promhttp.InstrumentMetricHandler(
		registry,
		promhttp.HandlerFor(
			prometheus.Gatherers{registry, prometheus.DefaultGatherer},
			promhttp.HandlerOpts{},
		),
	)))

	e.GET("/healthz", func(c *echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
}

func recoverMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = errors.Errorf("%v", r)
				}
				slog.Error("Middleware PANIC RECOVER", log.WithError(err), log.Stack("panic-stack"))

				resp, unwrapErr := echo.UnwrapResponse(c.Response())
				if unwrapErr == nil && !resp.Committed {
					_ = c.JSON(http.StatusInternalServerError, map[string]string{
						"error": "Internal server error",
					})
				}
			}
		}()
		return next(c)
	}
}

func securityHeadersMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h := c.Response().Header()
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-XSS-Protection", "1; mode=block")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			return next(c)
		}
	}
}

// corsMiddleware returns the CORS middleware for the profile, or nil when no
// cross-origin access is configured. Dev mode reflects localhost origins (the
// vite dev server) plus explicitly configured origins; production only allows
// configured origins. Same-origin requests never need CORS headers, so an
// empty allowlist means no CORS middleware at all.
func corsMiddleware(profile *config.Profile) echo.MiddlewareFunc {
	config := middleware.CORSConfig{
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
		AllowHeaders:     connectcors.AllowedHeaders(),
		ExposeHeaders:    connectcors.ExposedHeaders(),
		AllowCredentials: true,
	}
	if profile.Mode == common.ReleaseModeDev {
		allowed := make(map[string]struct{}, len(profile.AllowedOrigins))
		for _, origin := range profile.AllowedOrigins {
			allowed[normalizeOrigin(origin)] = struct{}{}
		}
		config.UnsafeAllowOriginFunc = func(_ *echo.Context, origin string) (string, bool, error) {
			if isLocalhostOrigin(origin) {
				return origin, true, nil
			}
			if _, ok := allowed[normalizeOrigin(origin)]; ok {
				return origin, true, nil
			}
			return "", false, nil
		}
		return middleware.CORSWithConfig(config)
	}
	if len(profile.AllowedOrigins) == 0 {
		return nil
	}
	config.AllowOrigins = profile.AllowedOrigins
	return middleware.CORSWithConfig(config)
}
