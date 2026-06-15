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
	"github.com/Ranxy/laelia/backend/manager/component/ratelimit"
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

	cmdDispatcher := dispatcher.New(stores)
	cmdDispatcher.StartPingMonitor()

	userService := apiv1.NewUserService(stores, profile, stateCfg)
	authService := apiv1.NewAuthService(stores, secret, profile, stateCfg)
	agentService := apiv1.NewAgentService(stores, secret, profile, stateCfg, cmdDispatcher)
	commandService := apiv1.NewCommandService(stores, cmdDispatcher)
	commandService.SetACPEnabled(!profile.DisableACP)
	agentCommandService := apiv1.NewAgentCommandService(stores, cmdDispatcher)

	rateLimiter, err := ratelimit.New(ratelimit.DefaultConfig())
	if err != nil {
		return errors.Wrapf(err, "failed to create rate limiter")
	}

	onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
		stack := stacktrace.TakeStacktrace(20 /* n */, 5 /* skip */)
		slog.Error("v1 server panic error", "method", s.Procedure, log.WithError(errors.Errorf("error: %v\n%s", p, stack)))
		return connect.NewError(connect.CodeInternal, errors.Errorf("error: %v\n%s", p, stack))
	}

	ipValidator := auth.NewIPValidator(auth.IPValidationWarn, false)

	handlerOpts := connect.WithHandlerOptions(
		connect.WithInterceptors(
			apiv1.NewDebugInterceptor(),
			rateLimiter,
			ipValidator,
			auth.New(stores, secret, stateCfg, profile),
			apiv1.NewAuditInterceptor(stores),
		),
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
	agentCmdPath, agentCmdHandler := v1connect.NewAgentCommandServiceHandler(agentCommandService, handlerOpts)
	connectHandlers[agentCmdPath] = agentCmdHandler

	reflector := grpcreflect.NewStaticReflector(
		v1connect.UserServiceName,
		v1connect.AuthServiceName,
		v1connect.AgentServiceName,
		v1connect.CommandServiceName,
		v1connect.AgentCommandServiceName,
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

	e.Any("/v1/*", echo.WrapHandler(mux))

	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return nil
}
