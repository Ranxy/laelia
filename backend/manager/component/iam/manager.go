// Package iam implements the laelia permission-check engine.
//
// It resolves a caller's effective permission set from the IAM model: a
// permission is a fine-grained string (backend/common/permission), a role is a
// named bundle of permissions (backend/manager/store/predefined_roles.go for
// built-in roles, the role table for custom roles), and an IAM policy binds
// principals to roles on a resource (backend/manager/store/policy.go). This
// mirrors bytebase's backend/component/iam, adapted to laelia's single-workspace
// model.
package iam

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/Ranxy/laelia/backend/manager/utils"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// ResourceRef identifies the target resource of a permission check. Phase 1
// passes nil (workspace-scoped checks only); Phase 2 populates it so
// CheckPermission can consult per-resource IAM policies.
type ResourceRef struct {
	ResourceType models.Policy_Resource
	Name         string
}

// Manager resolves permissions against the IAM model.
type Manager struct {
	store *store.Store
}

// NewManager builds an IAM manager backed by the given store.
func NewManager(stores *store.Store) *Manager {
	return &Manager{store: stores}
}

// CheckPermission reports whether the caller holds the permission.
//
// Phase 1 (workspace-scoped): every authenticated principal receives the
// roles/workspaceMember baseline (the implicit allUsers->workspaceMember
// binding of the single-workspace model); user callers additionally receive the
// permissions of every role they hold in the workspace IAM policy (e.g.
// roles/workspaceAdmin). This reproduces the former
// permissionsForCaller(isAdmin, isAgent) behavior exactly: any authenticated
// principal gets the member tier, admin users get the admin tier, agents never
// get the admin tier.
//
// The check short-circuits on the first granting set rather than materializing
// the full effective permission set, so the common case costs one map lookup
// and no allocation.
//
// When resource is non-nil (Phase 2), the caller's permissions from that
// resource's IAM policy are consulted as well.
//
//nolint:revive // agent is consulted by per-resource binding checks in Phase 2.
func (m *Manager) CheckPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage, agent *store.AgentMessage, resource *ResourceRef) (bool, error) {
	// Baseline: every authenticated principal gets roles/workspaceMember (the
	// implicit allUsers->workspaceMember binding of the single-workspace model).
	if store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions[perm] {
		return true, nil
	}

	if user != nil {
		workspacePolicy, err := m.store.GetWorkspaceIamPolicy(ctx)
		if err != nil {
			return false, err
		}
		for _, role := range utils.GetUserRolesInIamPolicy(ctx, m.store, user, workspacePolicy.Policy) {
			if rolePerms := m.rolePermissions(ctx, role); rolePerms != nil && rolePerms[perm] {
				return true, nil
			}
		}
	}

	if resource != nil {
		ok, err := m.checkResourcePermission(ctx, perm, user, agent, resource)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	return false, nil
}

// checkResourcePermission loads the resource's IAM policy and reports whether
// the caller holds perm via any binding they match on that resource. Bindings
// are resolved agent-aware (users/{uid} or agents/{rid} principals, group
// expansion, CEL condition). A resource type without a backing policy store
// (e.g. a command, handled in Phase 3) yields no permissions here.
func (m *Manager) checkResourcePermission(ctx context.Context, perm permission.Permission, user *store.UserMessage, agent *store.AgentMessage, resource *ResourceRef) (bool, error) {
	var policy *models.IamPolicy
	switch resource.ResourceType {
	case models.Policy_CONVERSATION:
		p, err := m.store.GetConversationIamPolicy(ctx, resource.Name)
		if err != nil {
			return false, err
		}
		policy = p.Policy
	case models.Policy_AGENT:
		p, err := m.store.GetAgentIamPolicy(ctx, resource.Name)
		if err != nil {
			return false, err
		}
		policy = p.Policy
	default:
		return false, nil
	}

	for _, binding := range utils.GetCallerIAMPolicyBindings(ctx, m.store, user, agent, policy) {
		if rolePerms := m.rolePermissions(ctx, binding.Role); rolePerms != nil && rolePerms[perm] {
			return true, nil
		}
	}
	return false, nil
}

// rolePermissions returns the permission set for the role named roles/{id},
// resolving predefined roles in-memory first and custom roles from the DB
// (cached) via GetRoleSnapshot. Returns nil if the role is unknown; a DB error
// is logged but treated as "role resolves to no permissions" (fail-closed),
// matching utils/member.go's convention.
func (m *Manager) rolePermissions(ctx context.Context, role string) map[permission.Permission]bool {
	resourceID := strings.TrimPrefix(role, common.RolePrefix)
	roleMessage, err := m.store.GetRoleSnapshot(ctx, resourceID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to resolve role permissions",
			slog.String("role", role),
			slog.Any("err", err))
		return nil
	}
	if roleMessage == nil {
		return nil
	}
	return roleMessage.Permissions
}
