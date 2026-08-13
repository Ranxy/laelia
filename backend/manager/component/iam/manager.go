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
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"

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
// agent it consults the agent's IAM policy (custom roles bound on the agent).
// For a machine it consults the machine's IAM policy (who may create agents on
// it). For a conversation it reads the conversation IAM policy and maps the
// caller's binding roles to conversation permissions (member/admin/owner). A
// non-member user may still read an agent-DM (type 3) if they hold the
// workspace-scope conversations.reviewAgentDM permission. A resource type
// without a handler here yields no permissions.
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
	case models.Policy_MACHINE:
		p, err := m.store.GetMachineIamPolicy(ctx, resource.Name)
		if err != nil {
			return false, err
		}
		for _, binding := range utils.GetCallerIAMPolicyBindings(ctx, m.store, user, agent, p.Policy) {
			if rolePerms := m.machineRolePermissions(ctx, binding.Role); rolePerms != nil && rolePerms[perm] {
				return true, nil
			}
		}
		return false, nil
	case models.Policy_COMMAND:
		return m.checkCommandPermission(ctx, perm, user, agent, resource.Name)
	case models.Policy_REMINDER:
		return m.checkReminderPermission(ctx, perm, user, agent, resource.Name)
	case models.Policy_FILE:
		return m.checkFilePermission(ctx, perm, user, agent, resource.Name)
	default:
		return false, nil
	}
}

// checkConversationPermission authorizes a conversation-scoped permission via
// the conversation's IAM policy. The caller's binding roles (the built-in
// conversationMember/Admin/Owner roles, or custom roles) resolve to permission
// sets; group members, allUsers, and binding conditions are honored by
// GetCallerIAMPolicyBindings. A caller with no binding (a non-member user)
// holding conversations.reviewAgentDM may still read an agent-DM (type 3);
// agents are always members of their own agent-DMs so they never need the
// review override.
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

	policy, err := m.store.GetConversationIamPolicy(ctx, convID)
	if err != nil {
		return false, err
	}
	for _, binding := range utils.GetCallerIAMPolicyBindings(ctx, m.store, user, agent, policy.Policy) {
		rolePerms := m.conversationRolePermissions(ctx, binding.GetRole())
		if rolePerms != nil && rolePerms[perm] {
			return true, nil
		}
	}

	// Owner-follow read: when the calling agent has follow_owner_permissions
	// enabled, it can read any conversation its owner can read (channels and
	// DMs) — the owner's direct, group-expanded, and allUsers bindings. The
	// ConversationsRead guard keeps the follow grant read-only (sending still
	// requires explicit membership); the reviewAgentDM override below stays
	// user-only so agents never inherit it. A missing or deleted owner denies
	// (there are no bindings to follow).
	if perm == permission.ConversationsRead && agent != nil && agent.FollowOwnerPermissions {
		owner, err := m.store.GetUserByID(ctx, agent.OwnerID)
		if err != nil {
			return false, err
		}
		if owner != nil && !owner.MemberDeleted {
			for _, binding := range utils.GetUserIAMPolicyBindings(ctx, m.store, owner, policy.Policy) {
				rolePerms := m.conversationRolePermissions(ctx, binding.GetRole())
				if rolePerms != nil && rolePerms[perm] {
					return true, nil
				}
			}
		}
	}

	// Owner-follow member management: when the calling agent has
	// can_manage_channel_members enabled, it may add/remove members in any
	// channel its owner manages (Admin/Owner). Deliberately scoped to
	// manageMembers — the agent never inherits the owner's other manage powers
	// (rename, delete, transfer, roles). Gated by can_manage_channel_members,
	// not follow_owner_permissions: the two are independent (visibility vs.
	// member management).
	if perm == permission.ConversationsManageMembers && agent != nil && agent.CanManageChannelMembers {
		owner, err := m.store.GetUserByID(ctx, agent.OwnerID)
		if err != nil {
			return false, err
		}
		if owner != nil && !owner.MemberDeleted {
			for _, binding := range utils.GetUserIAMPolicyBindings(ctx, m.store, owner, policy.Policy) {
				rolePerms := m.conversationRolePermissions(ctx, binding.GetRole())
				if rolePerms != nil && rolePerms[perm] {
					return true, nil
				}
			}
		}
	}

	// Non-member override: a user holding the grantable reviewAgentDM workspace
	// permission may read (not send/manage) an agent-to-agent DM.
	if perm == permission.ConversationsRead && user != nil {
		conv, convErr := m.store.GetConversation(ctx, convID)
		if convErr != nil {
			// A missing conversation denies rather than 500s (fail-closed, no
			// existence probing), matching the pre-migration behavior.
			if errors.Is(convErr, store.ErrConversationNotFound) {
				return false, nil
			}
			return false, convErr
		}
		if conv == nil || conv.Type != store.ConversationTypeAgentDM {
			return false, nil
		}
		if ok, rErr := m.CheckPermission(ctx, permission.ConversationsReviewAgentDM, user, nil, nil); rErr == nil && ok {
			return true, nil
		}
	}
	return false, nil
}

// conversationRolePermissions resolves an IAM binding role on a conversation to
// its permission set: the built-in conversation roles map to the chat role
// sets, any other role resolves through the normal role catalog (custom roles).
func (m *Manager) conversationRolePermissions(ctx context.Context, role string) map[permission.Permission]bool {
	switch role {
	case common.FormatRole(store.ConversationOwnerRole):
		return chatOwnerPermissions
	case common.FormatRole(store.ConversationAdminRole):
		return chatAdminPermissions
	case common.FormatRole(store.ConversationMemberRole):
		return chatMemberPermissions
	default:
		return m.rolePermissions(ctx, role)
	}
}

// machineRolePermissions resolves an IAM binding role on a machine to its
// permission set: the built-in machineAgentCreator role maps to the machine
// agent-creator set; any other role resolves through the normal role catalog
// (custom roles).
func (m *Manager) machineRolePermissions(ctx context.Context, role string) map[permission.Permission]bool {
	if role == common.FormatRole(store.MachineAgentCreatorRole) {
		return machineAgentCreatorPermissions
	}
	return m.rolePermissions(ctx, role)
}

// machineAgentCreatorPermissions is the permission set of the machine-scope
// roles/machineAgentCreator marker role: it grants creating agents on the
// machine whose IAM policy binds it. Like the conversation roles it is
// deliberately not in store.PredefinedRoles (so it never appears on the
// management Roles page as a workspace-bindable role).
var machineAgentCreatorPermissions = permSet(
	permission.MachinesCreateAgent,
)

// chatRolePermissions maps a conversation chat role value to its permission
// set. The values come from the conversation IAM policy's built-in roles
// (roles/conversationMember/Admin/Owner); custom roles resolve through the
// normal role catalog. The built-in conversation roles are deliberately not in
// store.PredefinedRoles (so they never appear on the management Roles page as
// workspace-bindable roles). Owner-only operations (delete channel, transfer
// ownership, grant/revoke admin) are gated by an in-handler role==Owner check,
// so they need no separate catalog permission. Any value other than
// member/admin/owner (including 0 = not a member) yields nil.
func chatRolePermissions(role int32) map[permission.Permission]bool {
	switch role {
	case store.MemberRoleOwner:
		return chatOwnerPermissions
	case store.MemberRoleAdmin:
		return chatAdminPermissions
	case store.MemberRoleMember:
		return chatMemberPermissions
	default:
		return nil
	}
}

// chatMemberPermissions / chatAdminPermissions / chatOwnerPermissions are the
// chat role→permission maps. Admin and owner share the same catalog perms;
// owner's extra authority (delete/transfer/grant-admin) is enforced by direct
// role==Owner checks in the handlers, not by catalog permissions.
var (
	chatMemberPermissions = permSet(
		permission.ConversationsRead,
		permission.ConversationsSend,
		permission.CommandsGet,
		permission.CommandsWatch,
		permission.FilesList,
	)
	chatAdminPermissions = permSet(
		permission.ConversationsRead,
		permission.ConversationsSend,
		permission.ConversationsManage,
		permission.ConversationsManageMembers,
		permission.CommandsGet,
		permission.CommandsWatch,
		permission.FilesList,
	)
	chatOwnerPermissions = permSet(
		permission.ConversationsRead,
		permission.ConversationsSend,
		permission.ConversationsManage,
		permission.ConversationsManageMembers,
		permission.CommandsGet,
		permission.CommandsWatch,
		permission.FilesList,
	)
)

// permSet builds an immutable permission set from the given permissions.
func permSet(perms ...permission.Permission) map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// callerMemberInfo maps a caller to its conversation_member (memberType,
// memberID) key, mirroring the former authz_helper callerMemberInfo. Returns
// ok=false when the caller is neither a user nor an agent.
func callerMemberInfo(user *store.UserMessage, agent *store.AgentMessage) (memberType int32, memberID string, ok bool) {
	switch {
	case user != nil:
		return store.MemberTypeUser, user.Handle, true
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
// on a specific conversation) are NOT represented here — they are resolved per
// resource by CheckPermission and surfaced on the relevant resource. For a
// workspaceAdmin the union is the full catalog, so admin-tier workspace perms
// (including agents.edit, reviewAgentDM, reviewAll) are included.
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
