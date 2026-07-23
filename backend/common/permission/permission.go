// Package permission defines the laelia permission catalog.
//
// A permission is a fine-grained string of the form "laelia.<resource>.<verb>"
// (e.g. "laelia.agents.create"). Permissions are flat — they are not grouped by
// resource in code, but the naming convention groups them logically. A role is
// a named bundle of permissions (see backend/manager/store/predefined_roles.go),
// and an IAM policy binds principals to roles on a resource
// (see backend/manager/store/policy.go).
//
// This mirrors bytebase's backend/common/permission, adapted to laelia's domain.
package permission

// Permission is a fine-grained capability string. It is a type alias of string
// so that it can be used as a map key and compared with raw strings carried by
// the laelia.v1.permission proto annotation.
type Permission = string

// Permissions are grouped by resource. The constants are the typed mirror used
// by the role matrix and proto annotations; allPermissions below is the
// validation set consumed by Exist.
const (
	// Agents
	AgentsCreate       Permission = "laelia.agents.create"
	AgentsGet          Permission = "laelia.agents.get"
	AgentsEdit         Permission = "laelia.agents.edit"
	AgentsListSessions Permission = "laelia.agents.listSessions"

	// Machines — a machine is the long-lived agent-application process a user
	// runs once on a host; it hosts one or more agents. create/get are workspace
	// baseline; edit/delete are admin-tier (rotate/revoke token, force-disconnect,
	// delete). Per-resource scoping is not used (machines are workspace-scoped).
	MachinesCreate Permission = "laelia.machines.create"
	MachinesGet    Permission = "laelia.machines.get"
	MachinesEdit   Permission = "laelia.machines.edit"
	MachinesDelete Permission = "laelia.machines.delete"

	// Commands
	CommandsGet    Permission = "laelia.commands.get"
	CommandsList   Permission = "laelia.commands.list"
	CommandsWatch  Permission = "laelia.commands.watch"
	CommandsCancel Permission = "laelia.commands.cancel"

	// Conversations (channels are modeled as conversations). list is the
	// workspace-scope discovery permission (any member can enumerate channels/
	// commands); read/send/manage are per-conversation, granted by the caller's
	// chat role (member/admin/owner) in conversation_member. reviewAgentDM and
	// reviewAll are workspace-scope "review" permissions grantable to any user
	// (admin or not): reviewAgentDM reads/lists agent-to-agent DMs; reviewAll is
	// cross-conversation oversight (ListChannelsForAgent all, ListReminders
	// across conversations, raw WatchCommandEvents). workspaceAdmin holds both
	// via the all-permissions union.
	ConversationsCreate        Permission = "laelia.conversations.create"
	ConversationsList          Permission = "laelia.conversations.list"
	ConversationsRead          Permission = "laelia.conversations.read"
	ConversationsSend          Permission = "laelia.conversations.send"
	ConversationsManage        Permission = "laelia.conversations.manage"
	ConversationsReviewAgentDM Permission = "laelia.conversations.reviewAgentDM"
	ConversationsReviewAll     Permission = "laelia.conversations.reviewAll"

	// Reminders
	RemindersGet    Permission = "laelia.reminders.get"
	RemindersList   Permission = "laelia.reminders.list"
	RemindersUpdate Permission = "laelia.reminders.update"
	RemindersCancel Permission = "laelia.reminders.cancel"

	// Tasks
	TasksList   Permission = "laelia.tasks.list"
	TasksCreate Permission = "laelia.tasks.create"
	TasksManage Permission = "laelia.tasks.manage"

	// Activities (per-user activity feed). Workspace-scope: the caller's own
	// principal id is the implicit filter, so these are NOT resource-scoped.
	ActivitiesList     Permission = "laelia.activities.list"
	ActivitiesMarkDone Permission = "laelia.activities.markDone"

	// Files
	FilesUpload   Permission = "laelia.files.upload"
	FilesDownload Permission = "laelia.files.download"
	FilesList     Permission = "laelia.files.list"

	// Users
	UsersCreate Permission = "laelia.users.create"
	UsersGet    Permission = "laelia.users.get"
	UsersList   Permission = "laelia.users.list"
	UsersUpdate Permission = "laelia.users.update"
	UsersDelete Permission = "laelia.users.delete"

	// Settings
	SettingsGet    Permission = "laelia.settings.get"
	SettingsUpdate Permission = "laelia.settings.update"

	// Custom role management
	RolesCreate Permission = "laelia.roles.create"
	RolesGet    Permission = "laelia.roles.get"
	RolesList   Permission = "laelia.roles.list"
	RolesUpdate Permission = "laelia.roles.update"
	RolesDelete Permission = "laelia.roles.delete"

	// IAM policy management (workspace + per-resource bindings)
	IAMGetPolicy Permission = "laelia.iam.getPolicy"
	IAMSetPolicy Permission = "laelia.iam.setPolicy"
)

// allPermissions is the complete, authoritative list of valid permission
// strings. It is the source of truth for Exist and for the workspaceAdmin
// superuser role (which unions every permission). Add new permissions here and
// as constants above; the two lists must stay in sync.
var allPermissions = []Permission{
	AgentsCreate,
	AgentsGet,
	AgentsEdit,
	AgentsListSessions,

	MachinesCreate,
	MachinesGet,
	MachinesEdit,
	MachinesDelete,

	CommandsGet,
	CommandsList,
	CommandsWatch,
	CommandsCancel,

	ConversationsCreate,
	ConversationsList,
	ConversationsRead,
	ConversationsSend,
	ConversationsManage,
	ConversationsReviewAgentDM,
	ConversationsReviewAll,

	RemindersGet,
	RemindersList,
	RemindersUpdate,
	RemindersCancel,

	TasksList,
	TasksCreate,
	TasksManage,

	ActivitiesList,
	ActivitiesMarkDone,

	FilesUpload,
	FilesDownload,
	FilesList,

	UsersCreate,
	UsersGet,
	UsersList,
	UsersUpdate,
	UsersDelete,

	SettingsGet,
	SettingsUpdate,

	RolesCreate,
	RolesGet,
	RolesList,
	RolesUpdate,
	RolesDelete,

	IAMGetPolicy,
	IAMSetPolicy,
}

var allPermissionsMap = func() map[Permission]bool {
	m := make(map[Permission]bool, len(allPermissions))
	for _, p := range allPermissions {
		m[p] = true
	}
	return m
}()

// AllPermissions returns a copy of the full permission catalog.
func AllPermissions() []Permission {
	out := make([]Permission, len(allPermissions))
	copy(out, allPermissions)
	return out
}

// Exist reports whether the permission string is a known catalog entry.
func Exist(permission string) bool {
	return allPermissionsMap[permission]
}

// Exists reports whether every permission string is a known catalog entry.
func Exists(permissions ...string) bool {
	for _, p := range permissions {
		if !Exist(p) {
			return false
		}
	}
	return true
}

// resourceScopedPermissions are the permissions the IAM engine authorizes via a
// per-resource lookup rather than the workspace baseline or handler-level
// helpers. For conversations the lookup reads the caller's chat role
// (member/admin/owner) from conversation_member; for agents it reads the
// agentEditor IAM binding. The interceptor resolves the request's resource only
// for these, so list/create, the review perms, and handler-gated RPCs pay no
// resource-resolution cost and never hit a per-resource lookup (which would
// otherwise turn a DB error into a 500 where the baseline path returned
// PermissionDenied). The review perms (reviewAgentDM, reviewAll) are
// workspace-scope and intentionally NOT in this set.
var resourceScopedPermissions = map[Permission]bool{
	ConversationsRead:   true,
	ConversationsSend:   true,
	ConversationsManage: true,
	AgentsEdit:          true,
}

// IsResourceScoped reports whether perm is authorized by a per-resource IAM
// policy (and so the interceptor should resolve the request's resource).
func IsResourceScoped(perm Permission) bool {
	return resourceScopedPermissions[perm]
}
