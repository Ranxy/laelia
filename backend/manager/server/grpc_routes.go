package server

import (
	"context"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	"github.com/labstack/echo/v5"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/stacktrace"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	apiv1 "github.com/Ranxy/laelia/backend/manager/api/v1"
	"github.com/Ranxy/laelia/backend/manager/component/device"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/mailer"
	"github.com/Ranxy/laelia/backend/manager/component/roomhub"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/component/webpush"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func configureV1Routers(
	ctx context.Context,
	e *echo.Echo,
	stores *store.Store,
	secret string,
	profile *config.Profile,
	stateCfg *state.State,
	s3clientmanager *s3client.Client,
	cmdDispatcher *dispatcher.Dispatcher,
) (*apiv1.AuditInterceptor, error) {
	cmdDispatcher.StartPingMonitor()

	// Room hub: in-process notifier that wakes long-polling message readers
	// (ListConversationMessages / ListThreadMessages with wait_ms) as soon as a
	// new message lands, instead of sleeping the full timeout. Injected into
	// the store (which fires it after version-bumping inserts) and into the
	// command service (which subscribes waiters). Single-process only; a
	// multi-instance deployment needs a shared notifier behind the same
	// interface.
	hub := roomhub.New()
	stores.SetRoomNotifier(hub)

	iamManager := iam.NewManager(stores)
	// Device login sessions: in-memory store for the device-code flow. The
	// sweeper runs for the server's lifetime and purges expired/denied
	// sessions.
	deviceStore := device.New()
	deviceStore.StartSweeper(ctx)
	// Mailer sender: reads the SMTP setting from the setting table on every
	// send, so admin changes take effect immediately without restart.
	mailerSender := mailer.NewSender(stores)
	userService := apiv1.NewUserService(stores, profile, stateCfg, iamManager, s3clientmanager, mailerSender)
	authService := apiv1.NewAuthService(stores, secret, profile, stateCfg, mailerSender)
	agentService := apiv1.NewAgentService(stores, secret, profile, stateCfg, cmdDispatcher, iamManager, s3clientmanager)
	commandService := apiv1.NewCommandService(stores, cmdDispatcher, s3clientmanager, iamManager, hub)
	agentCommandService := apiv1.NewAgentCommandService(stores, cmdDispatcher)
	machineService := apiv1.NewMachineService(stores, secret, profile, stateCfg, cmdDispatcher, iamManager)
	deviceService := apiv1.NewDeviceService(deviceStore, stores, secret, profile, iamManager)
	machineStreamService := apiv1.NewMachineStreamService(stores, cmdDispatcher)
	settingService := apiv1.NewSettingService(stores, s3clientmanager, profile, iamManager)
	roleService := apiv1.NewRoleService(stores)
	iamService := apiv1.NewIamService(stores, iamManager)
	groupService := apiv1.NewGroupService(stores, iamManager)
	apiProviderService := apiv1.NewAPIProviderService(stores, iamManager)
	mcpServerService := apiv1.NewMcpServerService(stores, iamManager)
	mcpGatewayService := apiv1.NewMcpGatewayService(stores, iamManager)
	auditLogService := apiv1.NewAuditLogService(stores)
	identityProviderService := apiv1.NewIdentityProviderService(stores)

	// Web Push: load the auto-generated VAPID keypair from the setting table
	// (initializeSetting guarantees a row exists by this point) and build the
	// sender. Inject it into the store so activity generation can fire push
	// notifications fire-and-forget.
	webpushCfg, err := stores.GetWebPushSetting(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load web push setting")
	}
	webpushSender := webpush.NewSender(webpushCfg.GetPublicKey(), webpushCfg.GetPrivateKey(), webpushCfg.GetSubject(), stores)
	stores.SetWebPushSender(webpushSender)
	notificationService := apiv1.NewNotificationService(stores, webpushSender, iamManager)

	onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
		stack := stacktrace.TakeStacktrace(20 /* n */, 5 /* skip */)
		slog.Error("v1 server panic error", "method", s.Procedure, log.WithError(errors.Errorf("error: %v\n%s", p, stack)))
		// Panic details (internal file paths, function names, line numbers)
		// are only safe to return to the client in debug mode; otherwise they
		// help an attacker locate unpatched dependencies. The full stack is
		// always logged above.
		if profile.RuntimeDebug.Load() {
			return connect.NewError(connect.CodeInternal, errors.Errorf("error: %v\n%s", p, stack))
		}
		return connect.NewError(connect.CodeInternal, errors.New("internal server error"))
	}

	ipValidator := auth.NewIPValidator(auth.IPValidationWarn, profile.TrustProxy)

	auditInterceptor := apiv1.NewAuditInterceptor(stores)

	// CSRF note: the Connect protocol can serve unary RPCs over GET, which
	// browsers send without CORS preflight. connect-go only enables GET for
	// procedures marked idempotency_level=NO_SIDE_EFFECTS, and none of the v1
	// services are annotated, so GET unary is disabled here. Do not add
	// NO_SIDE_EFFECTS annotations or connect.WithHTTPGet without re-auditing
	// the CSRF posture (cookie auth + SameSite + Origin validation).
	handlerOpts := connect.WithHandlerOptions(
		// Interceptors execute in the listed order. The rate limiter MUST run
		// after auth: it keys per-user/per-agent buckets on the principal that auth
		// injects into the context, so running it before auth leaves every
		// authenticated call misclassified as anonymous and throttled by the
		// tiny per-IP "connect" bucket (burst 5) — i.e. a few clicks -> 429.
		// Connection/login brute-force guards are unaffected: they are matched
		// by procedure name, not by context, so they still apply pre-handler.
		connect.WithInterceptors(
			apiv1.NewDebugInterceptor(),
			ipValidator,
			auth.New(stores, secret, stateCfg, profile),
			apiv1.NewIAMInterceptor(iam.NewManager(stores)),
			auditInterceptor,
		),
		// Cap unary request bodies so the bytes-based file upload RPC can't be
		// used to exhaust memory; matches apiv1.MaxUploadBytes.
		connect.WithReadMaxBytes(apiv1.MaxUploadBytes),
		connect.WithRecover(onPanic),
	)

	connectHandlers := make(map[string]http.Handler)

	userPath, userHandler := v1connect.NewUserServiceHandler(userService, handlerOpts)
	connectHandlers[userPath] = userHandler
	authPath, authHandler := v1connect.NewAuthServiceHandler(authService, handlerOpts)
	connectHandlers[authPath] = authHandler
	agentPath, agentHandler := v1connect.NewAgentServiceHandler(agentService, handlerOpts)
	connectHandlers[agentPath] = agentHandler
	commandPath, commandHandler := v1connect.NewCommandServiceHandler(commandService, handlerOpts)
	connectHandlers[commandPath] = commandHandler
	agentCmdPath, agentCmdHandler := v1connect.NewAgentStreamServiceHandler(agentCommandService, handlerOpts)
	connectHandlers[agentCmdPath] = agentCmdHandler
	machinePath, machineHandler := v1connect.NewMachineServiceHandler(machineService, handlerOpts)
	connectHandlers[machinePath] = machineHandler
	devicePath, deviceHandler := v1connect.NewDeviceServiceHandler(deviceService, handlerOpts)
	connectHandlers[devicePath] = deviceHandler
	machineStreamPath, machineStreamHandler := v1connect.NewMachineStreamServiceHandler(machineStreamService, handlerOpts)
	connectHandlers[machineStreamPath] = machineStreamHandler
	settingPath, settingHandler := v1connect.NewSettingServiceHandler(settingService, handlerOpts)
	connectHandlers[settingPath] = settingHandler
	rolePath, roleHandler := v1connect.NewRoleServiceHandler(roleService, handlerOpts)
	connectHandlers[rolePath] = roleHandler
	iamPath, iamHandler := v1connect.NewIamServiceHandler(iamService, handlerOpts)
	connectHandlers[iamPath] = iamHandler
	groupPath, groupHandler := v1connect.NewGroupServiceHandler(groupService, handlerOpts)
	connectHandlers[groupPath] = groupHandler
	apiProviderPath, apiProviderHandler := v1connect.NewApiProviderServiceHandler(apiProviderService, handlerOpts)
	connectHandlers[apiProviderPath] = apiProviderHandler
	mcpServerPath, mcpServerHandler := v1connect.NewMcpServerServiceHandler(mcpServerService, handlerOpts)
	connectHandlers[mcpServerPath] = mcpServerHandler
	mcpGatewayPath, mcpGatewayHandler := v1connect.NewMcpGatewayServiceHandler(mcpGatewayService, handlerOpts)
	connectHandlers[mcpGatewayPath] = mcpGatewayHandler
	auditLogPath, auditLogHandler := v1connect.NewAuditLogServiceHandler(auditLogService, handlerOpts)
	connectHandlers[auditLogPath] = auditLogHandler
	notificationPath, notificationHandler := v1connect.NewNotificationServiceHandler(notificationService, handlerOpts)
	connectHandlers[notificationPath] = notificationHandler
	identityProviderPath, identityProviderHandler := v1connect.NewIdentityProviderServiceHandler(identityProviderService, handlerOpts)
	connectHandlers[identityProviderPath] = identityProviderHandler

	// gRPC reflection is a dev-only convenience: it lets unauthenticated
	// callers enumerate every RPC, message shape, and permission annotation,
	// which lowers the bar for attackers in production. Register it only in
	// dev mode; the auth exemption in api/auth is gated the same way as
	// defense in depth.
	if profile.Mode == common.ReleaseModeDev {
		reflector := grpcreflect.NewStaticReflector(
			v1connect.UserServiceName,
			v1connect.AuthServiceName,
			v1connect.AgentServiceName,
			v1connect.CommandServiceName,
			v1connect.AgentStreamServiceName,
			v1connect.MachineServiceName,
			v1connect.DeviceServiceName,
			v1connect.MachineStreamServiceName,
			v1connect.SettingServiceName,
			v1connect.RoleServiceName,
			v1connect.IamServiceName,
			v1connect.GroupServiceName,
			v1connect.ApiProviderServiceName,
			v1connect.McpServerServiceName,
			v1connect.McpGatewayServiceName,
			v1connect.AuditLogServiceName,
			v1connect.NotificationServiceName,
			v1connect.IdentityProviderServiceName,
		)
		reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
		connectHandlers[reflectPath] = reflectHandler

		reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
		connectHandlers[reflectAlphaPath] = reflectAlphaHandler
	}

	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return auditInterceptor, nil
}
