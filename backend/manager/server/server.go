package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/component/scheduler"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/migration"
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
	pprofServer  *http.Server
	store        *store.Store
	startedTS    int64

	// PG server stoppers.
	stopper []func()

	// stateCfg is the shared in-momory state within the server.
	stateCfg *state.State

	// s3clientManager is the shared S3 client used by the CommandService file RPCs and
	// the SettingService.
	s3clientManager *s3client.Client

	// dispatcher is the command dispatcher; Stop joins its ping monitor and
	// grace goroutines on shutdown.
	dispatcher *dispatcher.Dispatcher

	// scheduler fires reminders at their scheduled time; Stop joins its scan
	// loops on shutdown. It is stopped before the dispatcher so it stops waking
	// agents while the dispatcher tears down sessions.
	scheduler *scheduler.Scheduler

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
	// Migrate the metadata schema to the latest embedded version before any
	// subsystem reads from it. On failure, close the store and abort startup.
	// s.store is assigned only after migration succeeds so the serverStarted
	// defer does not double-close the store.
	if err := migration.MigrateSchema(ctx, stores.GetDB()); err != nil {
		_ = stores.Close()
		return nil, errors.Wrap(err, "failed to migrate database schema")
	}
	s.store = stores
	s.runnerCtx, s.runnerCancel = context.WithCancel(ctx)

	stateCfg, err := state.NewWithStore(stores)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize state")
	}
	s.stateCfg = stateCfg

	s.s3clientManager = s3client.New(stores)

	if err := s.initializeSetting(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to init config")
	}
	// Configure echo server.
	s.echoServer = echo.New()

	secret, err := s.store.GetSecret(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get secret")
	}

	s.dispatcher = dispatcher.New(stores)
	s.scheduler = scheduler.New(stores, s.dispatcher)

	if err := configureV1Routers(ctx, s.echoServer, s.store, secret, s.profile, s.stateCfg, s.s3clientManager, s.dispatcher); err != nil {
		return nil, errors.Wrapf(err, "failed to configure v1 routers")
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

	s.httpServer = &http.Server{
		Addr:    address,
		Handler: s.echoServer,
	}

	tlsCfg, err := auth.InitTLS(&auth.TLSConfig{
		Domain:  s.profile.TLSDomain,
		CertDir: s.profile.TLSCertDir,
		Hosts:   s.profile.TLSHosts,
		DataDir: s.profile.TLSDataDir,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to initialize TLS")
	}
	if tlsCfg != nil {
		s.httpServer.TLSConfig = tlsCfg
		listener = tls.NewListener(listener, tlsCfg)
	} else {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		s.httpServer.Protocols = protocols
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("http server listen error", log.WithError(err))
			}
		}
	}()

	if s.stateCfg.HeartbeatBuffer != nil {
		// Start spawns its own goroutine and is idempotent.
		s.stateCfg.HeartbeatBuffer.Start(s.runnerCtx)
	}

	// Start the reminder scheduler once the dispatcher is ready (it wakes agents
	// via the dispatcher). Start spawns its own goroutines.
	if s.scheduler != nil {
		s.scheduler.Start()
	}

	// pprof is served on a separate, dedicated listener — never the public
	// port — and only when runtime debug is enabled and an address is set.
	// Heap/goroutine/profile dumps are sensitive; binding to localhost keeps
	// them off the network. The lifecycle mirrors the http server.
	if s.profile.PprofAddr != "" {
		s.pprofServer = newPprofServer(s.profile.PprofAddr)
		go func() {
			slog.Info("starting pprof server", "addr", s.profile.PprofAddr)
			if err := s.pprofServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("pprof server error", log.WithError(err))
			}
		}()
	}

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

	// Stop heartbeat buffer
	if s.stateCfg.HeartbeatBuffer != nil {
		s.stateCfg.HeartbeatBuffer.Stop()
	}

	// Shutdown echo
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(ctx)
	}

	// Shutdown the standalone pprof server (best-effort; it may be nil).
	if s.pprofServer != nil {
		_ = s.pprofServer.Shutdown(ctx)
	}

	// Stop the reminder scheduler before the dispatcher so it stops firing
	// wakes while the dispatcher tears down sessions, and before the store is
	// closed so its scan loops do not use a closed *sql.DB.
	if s.scheduler != nil {
		s.scheduler.Stop()
	}

	// Join the dispatcher's ping monitor and any in-flight grace goroutines
	// before closing the store, so they do not use a closed *sql.DB.
	if s.dispatcher != nil {
		s.dispatcher.Stop()
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
