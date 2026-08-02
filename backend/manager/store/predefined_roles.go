package store

import "github.com/Ranxy/laelia/backend/common/permission"

// Well-known role identifiers. Only WorkspaceAdminRole and WorkspaceMemberRole
// are predefined roles (defined in Go, read-only over the API, resolvable
// in-memory). The conversation* identifiers are the conversation-scope IAM
// roles stored in conversation IAM policies (resource_type=CONVERSATION); they
// are mapped to permission sets by component/iam.conversationRolePermissions
// and rejected as bindings on workspace/agent policies by the iam_service
// handler.
const (
	WorkspaceAdminRole     = "workspaceAdmin"
	WorkspaceMemberRole    = "workspaceMember"
	ConversationMemberRole = "conversationMember"
	ConversationAdminRole  = "conversationAdmin"
	ConversationOwnerRole  = "conversationOwner"
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
// principal (roles/workspaceMember). It carries only workspace-scope perms: the
// discovery/list perms (conversations.list, commands.list, reminders.list),
// creation perms, and files.upload — the one per-object operation that may
// legitimately target no resource (the agent file tool uploads
// conversation-less blobs) and therefore cannot be authorized per-resource.
// The per-object perms (conversations.read/send/manage, agents.edit,
// commands.get/watch/cancel, reminders.get/update/cancel, files.download/list)
// are deliberately absent: the IAM engine authorizes them per resource.
// files.list is conversation-scoped (ListFilesRequest carries a conversation).
// The review perms (reviewAgentDM, reviewAll) are also absent: they are granted
// only via workspaceAdmin.
var memberBaselinePermissions = permissionSet(
	permission.AgentsGet,
	permission.MachinesGet,
	permission.ConversationsCreate,
	permission.ConversationsList,
	permission.CommandsList,
	permission.RemindersList,
	permission.ActivitiesList,
	permission.ActivitiesMarkDone,
	permission.PushConfigGet,
	permission.PushSubscriptionsCreate,
	permission.PushSubscriptionsDelete,
	permission.FilesUpload,
	permission.GroupsGet,
	permission.GroupsList,
	permission.GroupsCreate,
)

// PredefinedRoles are the read-only, Go-defined roles shown on the management
// Roles page and resolvable in-memory by the engine. Only the two workspace
// tiers are predefined: workspaceAdmin (the full catalog) and workspaceMember
// (the authenticated-principal baseline). Conversation roles
// (conversationMember/Admin/Owner) are not roles — they are chat-membership
// markers whose permission sets live in component/iam.chatRolePermissions — and
// agentEditor / the reviewer roles were removed, so their capabilities
// (per-agent editing, agent-DM/oversight review) are now obtainable only via
// workspaceAdmin.
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
