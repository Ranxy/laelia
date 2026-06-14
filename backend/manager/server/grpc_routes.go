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
	// Note: the gateway response modifier takes the token duration on server startup. If the value is changed,
	// the user has to restart the server to take the latest value.
	gatewayMarshaler := &grpcruntime.HTTPBodyMarshaler{
		Marshaler: newSuggestingMarshaler(&grpcruntime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{},
			//nolint:forbidigo
			UnmarshalOptions: protojson.UnmarshalOptions{},
		}),
	}
	mux := grpcruntime.NewServeMux(
		grpcruntime.WithMarshalerOption(grpcruntime.MIMEWildcard, gatewayMarshaler),
		// pass through request headers that need to be used by connect rpc handlers.
		grpcruntime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			switch strings.ToLower(key) {
			// grpc-gateway hard codes authorization pass-through already, we do it again anyways.
			// https://github.com/grpc-ecosystem/grpc-gateway/blob/2cca0efe61de30f05068b9e3b4eb4801b1b2c1aa/runtime/context.go#L160
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

	userService := apiv1.NewUserService(stores, profile, stateCfg)
	authService := apiv1.NewAuthService(stores, secret, profile, stateCfg)
	agentService := apiv1.NewAgentService(stores, secret, profile, stateCfg)

	rateLimiter, err := ratelimit.New(ratelimit.DefaultConfig())
	if err != nil {
		return errors.Wrapf(err, "failed to create rate limiter")
	}

	onPanic := func(_ context.Context, s connect.Spec, _ http.Header, p any) error {
		stack := stacktrace.TakeStacktrace(20 /* n */, 5 /* skip */)
		// keep a multiline stack
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
			apiv1.NewAuditInterceptor(),
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

	// grpc reflection handlers.
	reflector := grpcreflect.NewStaticReflector(
		v1connect.UserServiceName,
		v1connect.AuthServiceName,
		v1connect.AgentServiceName,
	)
	reflectPath, reflectHandler := grpcreflect.NewHandlerV1(reflector)
	connectHandlers[reflectPath] = reflectHandler

	reflectAlphaPath, reflectAlphaHandler := grpcreflect.NewHandlerV1Alpha(reflector)
	connectHandlers[reflectAlphaPath] = reflectAlphaHandler

	// REST gateway proxy.
	grpcEndpoint := fmt.Sprintf(":%d", profile.Port)
	grpcConn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(100*1024*1024), // Set MaxCallRecvMsgSize to 100M so that users can receive up to 100M via REST calls.
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

	e.Any("/v1/*", echo.WrapHandler(mux))

	// Register Connect RPC handlers
	for path, handler := range connectHandlers {
		e.Any(path+"*", echo.WrapHandler(handler))
	}

	return nil
}
