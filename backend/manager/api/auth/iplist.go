package auth

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	errs "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
)

type IPValidationPolicy int

const (
	IPValidationOff    IPValidationPolicy = 0
	IPValidationWarn   IPValidationPolicy = 1
	IPValidationStrict IPValidationPolicy = 2
)

type IPValidator struct {
	policy     IPValidationPolicy
	trustProxy bool
}

func NewIPValidator(policy IPValidationPolicy, trustProxy bool) *IPValidator {
	return &IPValidator{
		policy:     policy,
		trustProxy: trustProxy,
	}
}

func (v *IPValidator) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if v.policy != IPValidationOff {
			sourceIP := extractSourceIP(req.Header(), v.trustProxy)
			if sourceIP != "" {
				ctx = context.WithValue(ctx, common.SourceIPContextKey, sourceIP)
			}
		}
		return next(ctx, req)
	}
}

func (*IPValidator) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (v *IPValidator) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if v.policy != IPValidationOff {
			sourceIP := extractSourceIP(conn.RequestHeader(), v.trustProxy)
			if sourceIP != "" {
				ctx = context.WithValue(ctx, common.SourceIPContextKey, sourceIP)
			}
		}
		return next(ctx, conn)
	}
}

func ValidateAgentIP(reportedIP string, sourceIP string, policy IPValidationPolicy) error {
	if policy == IPValidationOff || reportedIP == "" || sourceIP == "" {
		return nil
	}
	if reportedIP != sourceIP {
		switch policy {
		case IPValidationWarn:
			slog.Warn("agent IP mismatch", "reported", reportedIP, "source", sourceIP)
			return nil
		case IPValidationStrict:
			return connect.NewError(connect.CodePermissionDenied,
				errs.Errorf("agent-reported IP %s doesn't match source IP %s", reportedIP, sourceIP))
		}
	}
	return nil
}
