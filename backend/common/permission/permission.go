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

	// Commands
	CommandsGet    Permission = "laelia.commands.get"
	CommandsWatch  Permission = "laelia.commands.watch"
	CommandsCancel Permission = "laelia.commands.cancel"

	// Conversations (channels are modeled as conversations)
	ConversationsCreate Permission = "laelia.conversations.create"
	ConversationsRead   Permission = "laelia.conversations.read"
	ConversationsSend   Permission = "laelia.conversations.send"
	ConversationsManage Permission = "laelia.conversations.manage"

	// Reminders
	RemindersGet    Permission = "laelia.reminders.get"
	RemindersList   Permission = "laelia.reminders.list"
	RemindersUpdate Permission = "laelia.reminders.update"
	RemindersCancel Permission = "laelia.reminders.cancel"

	// Tasks
	TasksList   Permission = "laelia.tasks.list"
	TasksCreate Permission = "laelia.tasks.create"
	TasksManage Permission = "laelia.tasks.manage"

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

	CommandsGet,
	CommandsWatch,
	CommandsCancel,

	ConversationsCreate,
	ConversationsRead,
	ConversationsSend,
	ConversationsManage,

	RemindersGet,
	RemindersList,
	RemindersUpdate,
	RemindersCancel,

	TasksList,
	TasksCreate,
	TasksManage,

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
