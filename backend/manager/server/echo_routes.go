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

	connectcors "connectrpc.com/cors"
)

func configureEchoRouters(
	e *echo.Echo,
	profile *config.Profile,
) {
	e.Use(recoverMiddleware)

	if profile.Mode == common.ReleaseModeDev {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
			UnsafeAllowOriginFunc: func(_ *echo.Context, origin string) (string, bool, error) {
				return origin, true, nil
			},
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
			AllowHeaders:     connectcors.AllowedHeaders(),
			ExposeHeaders:    connectcors.ExposedHeaders(),
			AllowCredentials: true,
		}))
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

	// TODO we need to Embed frontend at future. for now, we just use frontend not embed for skip this
	embedFrontend(e)

	registerPprof(e, &profile.RuntimeDebug)

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

				// In Echo v5, send error response directly
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
