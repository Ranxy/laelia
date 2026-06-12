package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"

	"github.com/pkg/errors"
)

const gracefulShutdownPeriod = 10 * time.Second

type Server struct {
	runnerWG     sync.WaitGroup
	runnerCtx    context.Context
	runnerCancel context.CancelFunc
	profile      *config.Profile
	echoServer   *echo.Echo
	httpServer   *http.Server
	store        *store.Store
	startedTS    int64

	// PG server stoppers.
	stopper []func()

	// stateCfg is the shared in-momory state within the server.
	stateCfg *state.State

	// boot specifies that whether the server boot correctly
	cancel context.CancelFunc
}

// NewServer creates a server.
func NewServer(ctx context.Context, profile *config.Profile) (*Server, error) {
	s := &Server{
		profile:   profile,
		startedTS: time.Now().Unix(),
	}

	// Display config
	slog.Info("-----Config BEGIN-----")
	slog.Info(fmt.Sprintf("mode=%s", profile.Mode))
	slog.Info("-----Config END-------")

	serverStarted := false
	defer func() {
		if !serverStarted {
			_ = s.Shutdown(ctx)
		}
	}()

	stores, err := store.New(ctx, profile.PgURL, false)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to new store")
	}
	s.store = stores
	s.runnerCtx, s.runnerCancel = context.WithCancel(ctx)

	stateCfg, err := state.New()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize state")
	}
	s.stateCfg = stateCfg

	if err := s.initializeSetting(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to init config")
	}
	// Configure echo server.
	s.echoServer = echo.New()

	if err := configureGrpcRouters(ctx, s.echoServer, s.store, s.profile, s.stateCfg); err != nil {
		return nil, errors.Wrapf(err, "failed to configure gRPC routers")
	}

	configureEchoRouters(s.echoServer, profile)

	for _, route := range s.echoServer.Router().Routes() {
		fmt.Printf("Path: %s, Method: %s\n", route.Path, route.Method)
	}

	serverStarted = true

	return s, nil
}

func (s *Server) Run(ctx context.Context, port int) error {
	_, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	address := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	s.httpServer = &http.Server{
		Addr:      address,
		Handler:   s.echoServer,
		Protocols: protocols,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("http server listen error", log.WithError(err))
			}
		}
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Stopping ...")
	slog.Info("Stopping web server...")

	ctx, cancel := context.WithTimeout(ctx, gracefulShutdownPeriod)
	defer cancel()

	// Cancel the worker
	if s.runnerCancel != nil {
		s.runnerCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}

	// Shutdown echo
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			// log.Fatal("failed to shutdown server", "error", err)
		}
	}

	s.runnerWG.Wait()

	// Close db connection
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			return err
		}
	}

	for _, stopper := range s.stopper {
		stopper()
	}

	return nil
}
