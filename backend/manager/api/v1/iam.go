package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// PermissionChecker resolves whether a caller holds a permission. *iam.Manager
// is the production implementation; the interface lets the interceptor be
// unit-tested with a fake checker.
type PermissionChecker interface {
	CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage, agent *store.AgentMessage, resource *iam.ResourceRef) (bool, error)
}

// IAMInterceptor enforces the laelia.v1.permission annotation on RPCs annotated
// with auth_method = IAM. It runs after the auth interceptor (which populates
// the caller in the context) and before the audit interceptor.
//
// The interceptor delegates to a PermissionChecker (iam.Manager in production),
// which resolves the caller's effective permission set from the IAM model
// (roles + workspace IAM policy). Phase 1 passes a nil resource
// (workspace-scoped checks only); the handler-level helpers in authz_helper.go
// continue to enforce per-resource access (conversation membership, agent
// ownership, channel ownership, command access) until Phase 2 routes those
// through per-resource IAM policies.
type IAMInterceptor struct {
	iam PermissionChecker
}

// NewIAMInterceptor builds an interceptor backed by the given IAM manager.
func NewIAMInterceptor(iamManager *iam.Manager) *IAMInterceptor {
	return &IAMInterceptor{iam: iamManager}
}

func newIAMInterceptorWithChecker(checker PermissionChecker) *IAMInterceptor {
	return &IAMInterceptor{iam: checker}
}

// authorize enforces the RPC's declared permission against the caller's
// effective permission set. RPCs without an IAM auth method or without a
// permission string are not gated here (the handler remains responsible). When
// the request carries a recognizable resource (resolveResource), it is passed
// to CheckPermission so per-resource IAM policies are consulted too.
func (in *IAMInterceptor) authorize(ctx context.Context, req connect.AnyRequest) error {
	authCtx, ok := common.GetAuthContextFromContext(ctx)
	if !ok {
		// No auth context: the request did not pass through the auth interceptor
		// (e.g. a route outside the v1 chain). Do not gate; the handler is
		// responsible for its own access control.
		return nil
	}
	if authCtx.AllowWithoutCredential {
		return nil
	}
	if authCtx.AuthMethod != common.AuthMethodIAM || authCtx.Permission == "" {
		// CUSTOM auth or unannotated RPCs: the handler performs its own check.
		return nil
	}

	user, hasUser := GetUserFromContext(ctx)
	agent, hasAgent := GetAgentFromContext(ctx)
	if !hasUser && !hasAgent {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	var resource *iam.ResourceRef
	// Resolve the request's resource only for permissions the IAM engine
	// authorizes via a per-resource policy. For everything else (list/create,
	// handler-gated command perms, workspace-scope perms) resolution is wasted
	// work and — for non-baseline perms — a per-resource policy lookup could
	// turn a transient DB error into a 500 where the baseline path returned
	// PermissionDenied.
	if req != nil && permission.IsResourceScoped(permission.Permission(authCtx.Permission)) {
		resource = resolveResource(req)
	}
	ok, err := in.iam.CheckPermission(ctx, permission.Permission(authCtx.Permission), user, agent, resource)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", authCtx.Permission))
	}
	return nil
}

func (in *IAMInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.authorize(ctx, req); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (*IAMInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (in *IAMInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		// The streaming request body is not visible here (it arrives on the first
		// Receive); per-resource authorization for streaming RPCs is deferred to
		// Phase 3's first-Receive wrapper. Workspace-scoped authorization still
		// applies.
		if err := in.authorize(ctx, nil); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}
