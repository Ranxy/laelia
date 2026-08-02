package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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
// (roles + workspace IAM policy). Resource-scoped permissions (conversations,
// agents, commands, reminders, files) resolve the request's resources via
// resource_reference annotations and require the permission on every one.
// Handler-level checks remain only for owner-of-record operations that are not
// expressible as catalog permissions (channel ownership, agent ownership,
// reminder assignee).
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
// permission string are not gated here (the handler remains responsible).
// When the request carries recognizable resources (resolveResources), the
// caller must hold the permission on every one of them; otherwise the
// workspace-scope check applies. The resolved resources are also recorded on
// the auth context for audit and error detail.
func (in *IAMInterceptor) authorize(ctx context.Context, msg any, fullMethod string) error {
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

	var refs []*iam.ResourceRef
	// Resolve the request's resource only for permissions the IAM engine
	// authorizes via a per-resource policy. For everything else (list/create,
	// handler-gated command perms, workspace-scope perms) resolution is wasted
	// work and — for non-baseline perms — a per-resource policy lookup could
	// turn a transient DB error into a 500 where the baseline path returned
	// PermissionDenied.
	if permission.IsResourceScoped(permission.Permission(authCtx.Permission)) {
		refs = resolveResources(msg)
	}
	authCtx.Resources = authCtx.Resources[:0]
	for _, ref := range refs {
		authCtx.Resources = append(authCtx.Resources, &common.Resource{Name: ref.Name})
	}

	var deniedResources []string
	if len(refs) == 0 {
		refs = []*iam.ResourceRef{nil}
	}
	allOK := true
	for _, ref := range refs {
		ok, err := in.iam.CheckPermission(ctx, permission.Permission(authCtx.Permission), user, agent, ref)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
		}
		if !ok {
			allOK = false
			if ref != nil {
				deniedResources = append(deniedResources, ref.Name)
			} else {
				deniedResources = append(deniedResources, "workspaces/-")
			}
		}
	}
	if !allOK {
		err := connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", authCtx.Permission))
		if detail, detailErr := connect.NewErrorDetail(&v1pb.PermissionDeniedDetail{
			Method:              fullMethod,
			RequiredPermissions: []string{authCtx.Permission},
			Resources:           deniedResources,
		}); detailErr == nil {
			err.AddDetail(detail)
		}
		return err
	}
	return nil
}

func (in *IAMInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.authorize(ctx, req.Any(), req.Spec().Procedure); err != nil {
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
		// Workspace-scoped authorization applies up front. For resource-scoped
		// permissions (e.g. commands.watch on WatchCommand) the resource only
		// arrives in the first message, so the check is deferred to the wrapped
		// conn's Receive — an up-front nil-resource check would deny members
		// whose access is granted per resource.
		skipUpfront := false
		if authCtx, ok := common.GetAuthContextFromContext(ctx); ok {
			skipUpfront = authCtx.AuthMethod == common.AuthMethodIAM &&
				authCtx.Permission != "" &&
				permission.IsResourceScoped(permission.Permission(authCtx.Permission))
		}
		if !skipUpfront {
			if err := in.authorize(ctx, nil, conn.Spec().Procedure); err != nil {
				return err
			}
		}
		return next(ctx, &iamStreamingConn{
			StreamingHandlerConn: conn,
			interceptor:          in,
			fullMethod:           conn.Spec().Procedure,
			ctx:                  ctx,
		})
	}
}

// iamStreamingConn wraps a streaming handler connection so every received
// message is authorized before it reaches the handler.
type iamStreamingConn struct {
	connect.StreamingHandlerConn
	interceptor *IAMInterceptor
	fullMethod  string
	ctx         context.Context
}

func (c *iamStreamingConn) Receive(msg any) error {
	if err := c.interceptor.authorize(c.ctx, msg, c.fullMethod); err != nil {
		return err
	}
	return c.StreamingHandlerConn.Receive(msg)
}
