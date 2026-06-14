package v1

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type AuditInterceptor struct {
	stores                *store.Store
	heartbeatSamplingRate int
}

func NewAuditInterceptor(stores *store.Store) *AuditInterceptor {
	return &AuditInterceptor{
		stores:                stores,
		heartbeatSamplingRate: 100,
	}
}

func (a *AuditInterceptor) recordAudit(ctx context.Context, procedure string, err error) {
	auditLog := &store.AuditLogMessage{
		Method:    procedure,
		ActorType: getActorType(ctx),
		ActorID:   getActorID(ctx),
		SourceIP:  getSourceIP(ctx),
		Status:    statusFromError(err),
		Error:     errorFromError(err),
		CreatedAt: time.Now(),
	}

	if a.stores != nil {
		go func() {
			if dbErr := a.stores.CreateAuditLog(context.Background(), auditLog); dbErr != nil {
				slog.Warn("failed to persist audit log", "error", dbErr)
			}
		}()
	}

	slog.Info("audit",
		"method", auditLog.Method,
		"actor_type", auditLog.ActorType,
		"actor_id", auditLog.ActorID,
		"source_ip", auditLog.SourceIP,
		"status", auditLog.Status,
		"error", auditLog.Error,
		"timestamp", auditLog.CreatedAt.Format(time.RFC3339),
	)
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

		a.recordAudit(ctx, procedure, err)

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

		a.recordAudit(ctx, procedure, err)

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
