package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// Permission strings. Each is carried by a proto RPC via the
// laelia.v1.permission option and resolved against the caller's effective
// permission set by the IAM interceptor.
//
// Permissions are grouped into tiers:
//   - admin-tier: workspace admin only (never granted to agent tokens)
//   - member-tier: any authenticated principal (user or agent)
//
// Membership-scoped RPCs (read a conversation, watch a command) are granted at
// member-tier by the interceptor; the handler still enforces that the caller is
// a member of the specific conversation/command. Admin-tier RPCs are fully
// gated here: an agent token or a non-admin user is rejected before the handler
// runs.
const (
	// admin-tier
	PermAgentCreate       = "laelia.agents.create"
	PermAgentListSessions = "laelia.agents.listSessions"
	PermUserUpdate        = "laelia.users.update"
	PermUserDelete        = "laelia.users.delete"
	PermSettingRead       = "laelia.settings.get"
	PermSettingUpdate     = "laelia.settings.update"

	// member-tier (any authenticated user or agent). The agent-edit RPCs (config,
	// providers, token rotate/revoke, delete, force-disconnect) are member-tier
	// here so the interceptor lets a non-admin creator reach the handler; the
	// handler then enforces creator-or-admin via requireAgentEditor (agents are
	// denied there, so an agent token never mutates an agent profile).
	PermAgentRead          = "laelia.agents.get"
	PermAgentEdit          = "laelia.agents.edit"
	PermConversationRead   = "laelia.conversations.read"
	PermConversationSend   = "laelia.conversations.send"
	PermConversationManage = "laelia.conversations.manage"
	PermConversationCreate = "laelia.conversations.create"
	PermCommandRead        = "laelia.commands.get"
	PermCommandWatch       = "laelia.commands.watch"
	PermCommandCancel      = "laelia.commands.cancel"
)

var adminPermissions = map[string]bool{
	PermAgentCreate:       true,
	PermAgentListSessions: true,
	PermUserUpdate:        true,
	PermUserDelete:        true,
	PermSettingRead:       true,
	PermSettingUpdate:     true,
}

var memberPermissions = map[string]bool{
	PermAgentRead:          true,
	PermAgentEdit:          true,
	PermConversationRead:   true,
	PermConversationSend:   true,
	PermConversationManage: true,
	PermConversationCreate: true,
	PermCommandRead:        true,
	PermCommandWatch:       true,
	PermCommandCancel:      true,
}

// permissionsForCaller returns the effective permission set for a caller.
//
// Admins receive every permission. Every other authenticated principal (a user
// without the workspace-admin role, or an agent) receives the member-tier set.
// Agents never receive admin-tier permissions, so admin-only RPCs (create,
// listSessions, user/setting management) are denied to agent tokens regardless
// of any future role grant. The agent-edit RPCs are member-tier, so a non-admin
// creator passes the interceptor and requireAgentEditor enforces creator-or-admin
// in the handler (agents are denied there).
func permissionsForCaller(isAdmin, isAgent bool) map[string]bool {
	if isAgent {
		return copyPermSet(memberPermissions)
	}
	if isAdmin {
		out := make(map[string]bool, len(adminPermissions)+len(memberPermissions))
		for k := range adminPermissions {
			out[k] = true
		}
		for k := range memberPermissions {
			out[k] = true
		}
		return out
	}
	return copyPermSet(memberPermissions)
}

func copyPermSet(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	for k := range src {
		out[k] = true
	}
	return out
}

// AdminCheckFunc resolves whether a user holds the workspace-admin role. It is
// abstracted so the interceptor can be unit-tested without a database.
type AdminCheckFunc func(ctx context.Context, user *store.UserMessage) (bool, error)

// IAMInterceptor enforces the laelia.v1.permission annotation on RPCs annotated
// with auth_method = IAM. It runs after the auth interceptor (which populates
// the caller in the context) and before the audit interceptor.
type IAMInterceptor struct {
	isAdmin AdminCheckFunc
}

// NewIAMInterceptor builds an interceptor that resolves workspace-admin
// membership against the given store.
func NewIAMInterceptor(stores *store.Store) *IAMInterceptor {
	return &IAMInterceptor{
		isAdmin: func(ctx context.Context, u *store.UserMessage) (bool, error) {
			return isUserWorkspaceAdmin(ctx, stores, u)
		},
	}
}

func newIAMInterceptorWithAdminCheck(isAdmin AdminCheckFunc) *IAMInterceptor {
	return &IAMInterceptor{isAdmin: isAdmin}
}

// authorize enforces the RPC's declared permission against the caller's
// effective permission set. RPCs without an IAM auth method or without a
// permission string are not gated here (the handler remains responsible).
func (in *IAMInterceptor) authorize(ctx context.Context) error {
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

	var isAdmin bool
	if hasUser && user != nil {
		var err error
		isAdmin, err = in.isAdmin(ctx, user)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve workspace admin"))
		}
	}

	perms := permissionsForCaller(isAdmin, hasAgent && agent != nil)
	if !perms[authCtx.Permission] {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", authCtx.Permission))
	}
	return nil
}

func (in *IAMInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := in.authorize(ctx); err != nil {
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
		if err := in.authorize(ctx); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}
