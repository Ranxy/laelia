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
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

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

// checkResourcePermission authorizes a resource-scoped permission. For an
// agent it loads the agentEditor IAM policy (creator binding). For a
// conversation it does NOT use the IAM policy table — chat authorization is
// membership-based: it reads the caller's chat role from conversation_member
// and maps it to conversation permissions (member/admin/owner). A non-member
// user may still read an agent-DM (type 3) if they hold the workspace-scope
// conversations.reviewAgentDM permission. A resource type without a handler
// here (e.g. commands, which stay handler-gated) yields no permissions.
func (m *Manager) checkResourcePermission(ctx context.Context, perm permission.Permission, user *store.UserMessage, agent *store.AgentMessage, resource *ResourceRef) (bool, error) {
	switch resource.ResourceType {
	case models.Policy_CONVERSATION:
		return m.checkConversationPermission(ctx, perm, user, agent, resource)
	case models.Policy_AGENT:
		p, err := m.store.GetAgentIamPolicy(ctx, resource.Name)
		if err != nil {
			return false, err
		}
		for _, binding := range utils.GetCallerIAMPolicyBindings(ctx, m.store, user, agent, p.Policy) {
			if rolePerms := m.rolePermissions(ctx, binding.Role); rolePerms != nil && rolePerms[perm] {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

// checkConversationPermission maps the caller's chat role on the conversation
// to its permission set and reports whether it grants perm. Agents resolve to
// member_type=Agent/member_id=ResourceID; users to member_type=User/member_id
// =principal id. A non-member user with conversations.reviewAgentDM may read
// an agent-DM (type 3); agents are always members of their own agent-DMs so
// they never need the review override.
func (m *Manager) checkConversationPermission(ctx context.Context, perm permission.Permission, user *store.UserMessage, agent *store.AgentMessage, resource *ResourceRef) (bool, error) {
	convIDStr, err := common.GetConversationResourceID(resource.Name)
	if err != nil {
		// A malformed conversation resource name denies rather than 500s: the
		// resource came from a request field, and fail-closed is the safe choice.
		return false, nil //nolint:nilerr
	}
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		return false, nil //nolint:nilerr
	}

	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return false, nil
	}

	role, convType, err := m.store.GetConversationMembership(ctx, convID, memberType, memberID)
	if err != nil {
		return false, err
	}
	if rolePerms := chatRolePermissions(role); rolePerms != nil && rolePerms[perm] {
		return true, nil
	}

	// Non-member override: a user holding the grantable reviewAgentDM workspace
	// permission may read (not send/manage) an agent-to-agent DM.
	if role == 0 && perm == permission.ConversationsRead && convType == store.ConversationTypeAgentDM && user != nil {
		if ok, rErr := m.CheckPermission(ctx, permission.ConversationsReviewAgentDM, user, nil, nil); rErr == nil && ok {
			return true, nil
		}
	}
	return false, nil
}

// chatRolePermissions maps a conversation_member chat role to its predefined
// role's permission set. MemberRoleMember→conversationMember,
// MemberRoleAdmin→conversationAdmin, MemberRoleOwner→conversationOwner. Any
// other value (including 0 = not a member) yields nil.
func chatRolePermissions(role int32) map[permission.Permission]bool {
	var roleID string
	switch role {
	case store.MemberRoleOwner:
		roleID = store.ConversationOwnerRole
	case store.MemberRoleAdmin:
		roleID = store.ConversationAdminRole
	case store.MemberRoleMember:
		roleID = store.ConversationMemberRole
	default:
		return nil
	}
	if r := store.GetPredefinedRole(roleID); r != nil {
		return r.Permissions
	}
	return nil
}

// callerMemberInfo maps a caller to its conversation_member (memberType,
// memberID) key, mirroring the former authz_helper callerMemberInfo. Returns
// ok=false when the caller is neither a user nor an agent.
func callerMemberInfo(user *store.UserMessage, agent *store.AgentMessage) (memberType int32, memberID string, ok bool) {
	switch {
	case user != nil:
		return store.MemberTypeUser, fmt.Sprintf("%d", user.ID), true
	case agent != nil:
		return store.MemberTypeAgent, agent.ResourceID, true
	default:
		return 0, "", false
	}
}

// EffectiveWorkspacePermissions returns the caller's workspace-scope permission
// set: the roles/workspaceMember baseline (granted to every authenticated
// principal) unioned with the permissions of every role the user holds in the
// workspace IAM policy. Per-resource permissions (conversations.read/send/manage
// on a specific conversation, agents.edit via an agentEditor binding) are NOT
// represented here — they are resolved per resource by CheckPermission and
// surfaced on the relevant resource (e.g. Agent.can_edit). For a workspaceAdmin
// the union is the full catalog, so admin-tier workspace perms are included.
//
// Used by GetCurrentUser to populate User.permissions for frontend gating.
func (m *Manager) EffectiveWorkspacePermissions(ctx context.Context, user *store.UserMessage) ([]permission.Permission, error) {
	perms := make(map[permission.Permission]bool)
	for p := range store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions {
		perms[p] = true
	}
	if user != nil {
		workspacePolicy, err := m.store.GetWorkspaceIamPolicy(ctx)
		if err != nil {
			return nil, err
		}
		for _, role := range utils.GetUserRolesInIamPolicy(ctx, m.store, user, workspacePolicy.Policy) {
			for p := range m.rolePermissions(ctx, role) {
				perms[p] = true
			}
		}
	}
	out := make([]permission.Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
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
