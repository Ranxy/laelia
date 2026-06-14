package v1

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
)

type AuditInterceptor struct {
	heartbeatSamplingRate int
}

func NewAuditInterceptor() *AuditInterceptor {
	return &AuditInterceptor{
		heartbeatSamplingRate: 100,
	}
}

func (a *AuditInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)

		authCtx, ok := common.GetAuthContextFromContext(ctx)
		if !ok || !authCtx.Audit {
			return resp, err
		}

		procedure := req.Spec().Procedure
		if isHeartbeatProcedure(procedure) && err == nil {
			if !shouldSampleHeartbeat(a.heartbeatSamplingRate) {
				return resp, err
			}
		}

		slog.Info("audit",
			"method", procedure,
			"actor_type", getActorType(ctx),
			"actor_id", getActorID(ctx),
			"source_ip", getSourceIP(ctx),
			"status", statusFromError(err),
			"error", errorFromError(err),
			"timestamp", time.Now().Format(time.RFC3339),
		)

		return resp, err
	}
}

func (*AuditInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (a *AuditInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)

		authCtx, ok := common.GetAuthContextFromContext(ctx)
		if !ok || !authCtx.Audit {
			return err
		}

		procedure := conn.Spec().Procedure
		if isHeartbeatProcedure(procedure) && err == nil {
			if !shouldSampleHeartbeat(a.heartbeatSamplingRate) {
				return err
			}
		}

		slog.Info("audit",
			"method", procedure,
			"actor_type", getActorType(ctx),
			"actor_id", getActorID(ctx),
			"source_ip", getSourceIP(ctx),
			"status", statusFromError(err),
			"error", errorFromError(err),
			"timestamp", time.Now().Format(time.RFC3339),
		)

		return err
	}
}

func shouldSampleHeartbeat(rate int) bool {
	return rand.Intn(rate) == 0
}

func isHeartbeatProcedure(procedure string) bool {
	return isAgentHeartbeat(procedure)
}

func isAgentHeartbeat(procedure string) bool {
	return procedure == "/laelia.v1.AgentService/AgentHeartbeat"
}

func getActorType(ctx context.Context) string {
	if _, ok := GetUserFromContext(ctx); ok {
		return "user"
	}
	if _, ok := GetAgentFromContext(ctx); ok {
		return "agent"
	}
	return "unknown"
}

func getActorID(ctx context.Context) string {
	if user, ok := GetUserFromContext(ctx); ok {
		return user.Email
	}
	if agent, ok := GetAgentFromContext(ctx); ok {
		return agent.ResourceID
	}
	return ""
}

func getSourceIP(ctx context.Context) string {
	if ip, ok := common.GetSourceIPFromContext(ctx); ok {
		return ip
	}
	return ""
}

func statusFromError(err error) string {
	if err == nil {
		return "ok"
	}
	if connectErr, ok := err.(*connect.Error); ok {
		return connectErr.Code().String()
	}
	return "error"
}

func errorFromError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
