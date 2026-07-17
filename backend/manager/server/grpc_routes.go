package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"connectrpc.com/connect"
	"connectrpc.com/grpcreflect"
	grpcruntime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/labstack/echo/v5"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/stacktrace"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	apiv1 "github.com/Ranxy/laelia/backend/manager/api/v1"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/ratelimit"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func configureGrpcRouters(
	ctx context.Context,
	e *echo.Echo,
	stores *store.Store,
	secret string,
	profile *config.Profile,
	stateCfg *state.State,
	s3clientmanager *s3client.Client,
	cmdDispatcher *dispatcher.Dispatcher,
) error {
	gatewayMarshaler := &grpcruntime.HTTPBodyMarshaler{
		Marshaler: newSuggestingMarshaler(&grpcruntime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{},
			//nolint:forbidigo
			UnmarshalOptions: protojson.UnmarshalOptions{},
		}),
	}
	mux := grpcruntime.NewServeMux(
		grpcruntime.WithMarshalerOption(grpcruntime.MIMEWildcard, gatewayMarshaler),
		grpcruntime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			switch strings.ToLower(key) {
			case "authorization", "cookie", "origin":
				return key, true
			default:
				return "", false
			}
		}),
		grpcruntime.WithOutgoingHeaderMatcher(func(key string) (string, bool) {
			switch strings.ToLower(key) {
			case "set-cookie":
				return key, true
			default:
				return "", false
			}
		}),
		grpcruntime.WithRoutingErrorHandler(func(ctx context.Context, sm *grpcruntime.ServeMux, m grpcruntime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
			err := &grpcruntime.HTTPStatusError{
				HTTPStatus: httpStatus,
				Err:        connect.NewError(connect.CodeNotFound, errors.Errorf("gateway routing error %d: request method %v, URI %v", httpStatus, r.Method, r.RequestURI)),
			}
			grpcruntime.DefaultHTTPErrorHandler(ctx, sm, m, w, r, err)
		}),
	)

	cmdDispatcher.StartPingMonitor()

	iamManager := iam.NewManager(stores)
	userService := apiv1.NewUserService(stores, profile, stateCfg, iamManager)
	authService := apiv1.NewAuthService(stores, secret, profile, stateCfg)
	agentService := apiv1.NewAgentService(stores, secret, profile, stateCfg, cmdDispatcher, iamManager)
	commandService := apiv1.NewCommandService(stores, cmdDispatcher, s3clientmanager, iamManager)
	agentCommandService := apiv1.NewAgentCommandService(stores, cmdDispatcher)
	settingService := apiv1.NewSettingService(stores, s3clientmanager, profile)
	roleService := apiv1.NewRoleService(stores)
	iamService := apiv1.NewIamService(stores)

	rateLimiterCfg := ratelimit.DefaultConfig()
	rateLimiterCfg.TrustProxy = profile.TrustProxy
	rateLimiter, err := ratelimit.New(rateLimiterCfg)
	if err != nil {
		return errors.Wrapf(err, "failed to create rate limiter")
	}

	onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
		stack := stacktrace.TakeStacktrace(20 /* n */, 5 /* skip */)
		slog.Error("v1 server panic error", "method", s.Procedure, log.WithError(errors.Errorf("error: %v\n%s", p, stack)))
		return connect.NewError(connect.CodeInternal, errors.Errorf("error: %v\n%s", p, stack))
	}

	ipValidator := auth.NewIPValidator(auth.IPValidationWarn, profile.TrustProxy)

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
			rateLimiter,
			apiv1.NewIAMInterceptor(iam.NewManager(stores)),
			apiv1.NewAuditInterceptor(stores),
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
	settingPath, settingHandler := v1connect.NewSettingServiceHandler(settingService, handlerOpts)
	connectHandlers[settingPath] = settingHandler
	rolePath, roleHandler := v1connect.NewRoleServiceHandler(roleService, handlerOpts)
	connectHandlers[rolePath] = roleHandler
	iamPath, iamHandler := v1connect.NewIamServiceHandler(iamService, handlerOpts)
	connectHandlers[iamPath] = iamHandler

	reflector := grpcreflect.NewStaticReflector(
		v1connect.UserServiceName,
		v1connect.AuthServiceName,
		v1connect.AgentServiceName,
		v1connect.CommandServiceName,
		v1connect.AgentStreamServiceName,
		v1connect.SettingServiceName,
		v1connect.RoleServiceName,
		v1connect.IamServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	connectHandlers[reflectPath] = reflectHandler

	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
	connectHandlers[reflectAlphaPath] = reflectAlphaHandler

	grpcEndpoint := fmt.Sprintf(":%d", profile.Port)
	grpcConn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(100*1024*1024),
		),
	)
	if err != nil {
		return err
	}

	if err := v1pb.RegisterAuthServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterUserServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterAgentServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterCommandServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterSettingServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterRoleServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}
	if err := v1pb.RegisterIamServiceHandler(ctx, mux, grpcConn); err != nil {
		return err
	}

	e.Any("/v1/*", echo.WrapHandler(mux))

	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return nil
}
