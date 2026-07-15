package store

import "github.com/Ranxy/laelia/backend/common/permission"

// Predefined role IDs. Predefined roles are defined in Go (not seeded into the
// role table) and cannot be overwritten by a DB row of the same resource_id.
const (
	WorkspaceAdminRole     = "workspaceAdmin"
	WorkspaceMemberRole    = "workspaceMember"
	ConversationMemberRole = "conversationMember"
	ConversationAdminRole  = "conversationAdmin"
	ConversationOwnerRole  = "conversationOwner"
	AgentEditorRole        = "agentEditor"
	AgentDMReviewerRole    = "agentDMReviewer"
	OversightReviewerRole  = "oversightReviewer"
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
// discovery/list perms (conversations.list, commands.list, reminders.list,
// files.list), creation perms, and the perms whose RPCs carry no single
// conversation resource and so cannot be authorized per-conversation in the
// interceptor — reminders.get/update/cancel, files.upload/download, and the
// command perms. Those are gated per-object by the handler instead
// (requireReminderAccess reads the chat-membership table; requireFileMember
// checks conversation membership; requireCommandAccess reads the chat table +
// owning agent + reviewAll). The per-conversation perms (conversations.
// read/send/manage) and agents.edit are deliberately absent: they are
// authorized per-resource — by the caller's chat role (conversation_member) for
// conversations and by the agentEditor IAM binding for agents. The review perms
// (reviewAgentDM, reviewAll) are also absent: they are granted only via the
// agentDMReviewer / oversightReviewer / workspaceAdmin roles.
var memberBaselinePermissions = permissionSet(
	permission.AgentsGet,
	permission.ConversationsCreate,
	permission.ConversationsList,
	permission.CommandsGet,
	permission.CommandsList,
	permission.CommandsWatch,
	permission.CommandsCancel,
	permission.RemindersGet,
	permission.RemindersList,
	permission.RemindersUpdate,
	permission.RemindersCancel,
	permission.FilesUpload,
	permission.FilesDownload,
	permission.FilesList,
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
	// Conversation roles are the chat role→permission maps used by the IAM
	// engine's conversation branch (chatRolePermissions in component/iam). They
	// are NOT IAM bindings: a caller's chat role is read from conversation_member
	// and mapped to one of these permission sets. Owner-only operations (delete
	// channel, transfer ownership, grant/revoke admin) are gated by an in-handler
	// role==Owner check, so they need no separate catalog permission.
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
		ResourceID: ConversationAdminRole,
		Name:       "Conversation admin",
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
	// Reviewer roles make the grantable review perms assignable to non-admins via
	// the workspace IAM policy. workspaceAdmin already holds them through the
	// all-permissions union.
	{
		ResourceID: AgentDMReviewerRole,
		Name:       "Agent-DM reviewer",
		Predefined: true,
		Permissions: permissionSet(
			permission.ConversationsReviewAgentDM,
		),
	},
	{
		ResourceID: OversightReviewerRole,
		Name:       "Oversight reviewer",
		Predefined: true,
		Permissions: permissionSet(
			permission.ConversationsReviewAgentDM,
			permission.ConversationsReviewAll,
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
