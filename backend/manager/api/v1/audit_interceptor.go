package v1

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

type AuditInterceptor struct {
	buffer                *AuditBuffer
	heartbeatSamplingRate int
}

func NewAuditInterceptor(stores *store.Store) *AuditInterceptor {
	return &AuditInterceptor{
		buffer:                NewAuditBuffer(stores, 2*time.Second),
		heartbeatSamplingRate: 100,
	}
}

// Start/Stop delegate to the underlying buffer so the server wires the audit
// flush loop into its own lifecycle (see server.Run/Shutdown).
func (a *AuditInterceptor) Start(ctx context.Context) {
	a.buffer.Start(ctx)
}

func (a *AuditInterceptor) Stop() {
	a.buffer.Stop()
}

func (a *AuditInterceptor) recordAudit(ctx context.Context, procedure string, err error, resource, payload string) {
	auditLog := &store.AuditLogMessage{
		Method:    procedure,
		ActorType: getActorType(ctx),
		ActorID:   getActorID(ctx),
		SourceIP:  getSourceIP(ctx),
		Status:    statusFromError(err),
		Error:     errorFromError(err),
		Resource:  resource,
		Payload:   payload,
		CreatedAt: time.Now(),
	}

	a.buffer.Record(auditLog)

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
		var serviceData *anypb.Any
		wrappedCtx := common.WithSetServiceData(ctx, func(a *anypb.Any) {
			serviceData = a
		})
		resp, err := next(wrappedCtx, req)

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

		resource, payload := serviceDataAuditFields(serviceData)
		a.recordAudit(ctx, procedure, err, resource, payload)

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

		a.recordAudit(ctx, procedure, err, "", "")

		return err
	}
}

// serviceDataAuditFields extracts the audit resource and JSON payload from the
// service data a handler attached to the request (e.g. an IamPolicyChange with
// the binding deltas of a SetIamPolicy call). A nil or unmarshalable Any
// yields empty fields.
func serviceDataAuditFields(a *anypb.Any) (resource, payload string) {
	if a == nil {
		return "", ""
	}
	m, err := a.UnmarshalNew()
	if err != nil {
		return "", ""
	}
	b, err := protojson.Marshal(m)
	if err != nil {
		return "", ""
	}
	if change, ok := m.(*v1pb.IamPolicyChange); ok {
		return change.GetResource(), string(b)
	}
	return "", string(b)
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
