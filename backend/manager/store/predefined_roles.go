package store

import "github.com/Ranxy/laelia/backend/common/permission"

// Predefined role IDs. Predefined roles are defined in Go (not seeded into the
// role table) and cannot be overwritten by a DB row of the same resource_id.
const (
	WorkspaceAdminRole     = "workspaceAdmin"
	WorkspaceMemberRole    = "workspaceMember"
	ConversationMemberRole = "conversationMember"
	ConversationOwnerRole  = "conversationOwner"
	AgentEditorRole        = "agentEditor"
)

func permissionSet(perms ...permission.Permission) map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// allPermissionSet is the union of every catalog permission. The workspaceAdmin
// role holds it so that admin access falls out of the normal role->permission
// resolution rather than a special-case branch in CheckPermission.
var allPermissionSet = func() map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(permission.AllPermissions()))
	for _, p := range permission.AllPermissions() {
		m[p] = true
	}
	return m
}()

// memberBaselinePermissions is the permission set granted to any authenticated
// principal (roles/workspaceMember). It carries the workspace-scope perms: the
// discovery/list perms (conversations.list, commands.list — any member can
// enumerate), creation perms, and the command perms still gated per-command by
// requireCommandAccess until Phase 3. The per-resource perms
// (conversations.read/send/manage, agents.edit) are also still present: the
// interceptor does consult per-resource bindings for them, but in Phase 2 this
// is inert because CheckPermission short-circuits at the baseline before the
// resource consult runs (and resolveResource only runs for these resource-
// scoped perms). Phase 3 removes them from the baseline so the
// conversationMember/conversationOwner/agentEditor bindings become
// authoritative.
var memberBaselinePermissions = permissionSet(
	permission.AgentsGet,
	permission.AgentsEdit,
	permission.ConversationsCreate,
	permission.ConversationsList,
	permission.ConversationsRead,
	permission.ConversationsSend,
	permission.ConversationsManage,
	permission.CommandsGet,
	permission.CommandsList,
	permission.CommandsWatch,
	permission.CommandsCancel,
)

// PredefinedRoles contains all predefined roles. The list is the single source
// of truth for the role->permission matrix of built-in roles.
var PredefinedRoles = []*RoleMessage{
	{
		ResourceID:  WorkspaceAdminRole,
		Name:        "Workspace admin",
		Predefined:  true,
		Permissions: allPermissionSet,
	},
	{
		ResourceID:  WorkspaceMemberRole,
		Name:        "Workspace member",
		Predefined:  true,
		Permissions: memberBaselinePermissions,
	},
	{
		ResourceID: ConversationMemberRole,
		Name:       "Conversation member",
		Predefined: true,
		Permissions: permissionSet(
			permission.ConversationsRead,
			permission.ConversationsSend,
			permission.CommandsGet,
			permission.CommandsWatch,
		),
	},
	{
		ResourceID: ConversationOwnerRole,
		Name:       "Conversation owner",
		Predefined: true,
		Permissions: permissionSet(
			permission.ConversationsRead,
			permission.ConversationsSend,
			permission.ConversationsManage,
			permission.CommandsGet,
			permission.CommandsWatch,
		),
	},
	{
		ResourceID: AgentEditorRole,
		Name:       "Agent editor",
		Predefined: true,
		Permissions: permissionSet(
			permission.AgentsEdit,
		),
	},
}

var predefinedRolesMap = func() map[string]*RoleMessage {
	m := make(map[string]*RoleMessage, len(PredefinedRoles))
	for _, r := range PredefinedRoles {
		m[r.ResourceID] = r
	}
	return m
}()

// GetPredefinedRole returns the predefined role with the given resource ID, or
// nil if no such predefined role exists.
func GetPredefinedRole(resourceID string) *RoleMessage {
	return predefinedRolesMap[resourceID]
}

// IsPredefinedRole reports whether the given resource ID names a predefined role.
func IsPredefinedRole(resourceID string) bool {
	_, ok := predefinedRolesMap[resourceID]
	return ok
}
