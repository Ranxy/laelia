// The laelia permission catalog, grouped by resource. This mirrors
// backend/common/permission/permission.go `allPermissions` and is the source
// of truth for the role-editor permission grid. Keep the two in sync: a
// permission added to the backend must be added here or the role editor will
// not be able to grant it (and the backend rejects unknown strings on write).

export interface PermissionGroup {
  resource: string;
  permissions: string[];
}

export const PERMISSION_GROUPS: PermissionGroup[] = [
  {
    resource: "agents",
    permissions: [
      "laelia.agents.create",
      "laelia.agents.get",
      "laelia.agents.edit",
      "laelia.agents.listSessions",
    ],
  },
  {
    resource: "machines",
    permissions: [
      "laelia.machines.create",
      "laelia.machines.get",
      "laelia.machines.edit",
      "laelia.machines.delete",
      "laelia.machines.createAgent",
    ],
  },
  {
    resource: "commands",
    permissions: [
      "laelia.commands.get",
      "laelia.commands.list",
      "laelia.commands.watch",
      "laelia.commands.cancel",
    ],
  },
  {
    resource: "conversations",
    permissions: [
      "laelia.conversations.create",
      "laelia.conversations.list",
      "laelia.conversations.read",
      "laelia.conversations.send",
      "laelia.conversations.manage",
      "laelia.conversations.reviewAgentDM",
      "laelia.conversations.reviewAll",
    ],
  },
  {
    resource: "reminders",
    permissions: [
      "laelia.reminders.get",
      "laelia.reminders.list",
      "laelia.reminders.update",
      "laelia.reminders.cancel",
    ],
  },
  {
    resource: "tasks",
    permissions: [
      "laelia.tasks.list",
      "laelia.tasks.create",
      "laelia.tasks.manage",
    ],
  },
  {
    resource: "activities",
    permissions: ["laelia.activities.list", "laelia.activities.markDone"],
  },
  {
    resource: "files",
    permissions: [
      "laelia.files.upload",
      "laelia.files.download",
      "laelia.files.list",
    ],
  },
  {
    resource: "users",
    permissions: [
      "laelia.users.create",
      "laelia.users.get",
      "laelia.users.list",
      "laelia.users.update",
      "laelia.users.delete",
    ],
  },
  {
    resource: "groups",
    permissions: [
      "laelia.groups.create",
      "laelia.groups.get",
      "laelia.groups.list",
      "laelia.groups.update",
      "laelia.groups.delete",
    ],
  },
  {
    resource: "apiProviders",
    permissions: [
      "laelia.apiProviders.create",
      "laelia.apiProviders.get",
      "laelia.apiProviders.list",
      "laelia.apiProviders.update",
      "laelia.apiProviders.delete",
    ],
  },
  {
    resource: "identityProviders",
    permissions: [
      "laelia.identityProviders.create",
      "laelia.identityProviders.get",
      "laelia.identityProviders.list",
      "laelia.identityProviders.update",
      "laelia.identityProviders.delete",
    ],
  },
  {
    resource: "mcpServers",
    permissions: [
      "laelia.mcpServers.create",
      "laelia.mcpServers.get",
      "laelia.mcpServers.list",
      "laelia.mcpServers.update",
      "laelia.mcpServers.delete",
    ],
  },
  {
    resource: "settings",
    permissions: ["laelia.settings.get", "laelia.settings.update"],
  },
  {
    resource: "notifications",
    permissions: [
      "laelia.pushConfig.get",
      "laelia.pushConfig.update",
      "laelia.pushSubscriptions.create",
      "laelia.pushSubscriptions.delete",
    ],
  },
  {
    resource: "roles",
    permissions: [
      "laelia.roles.create",
      "laelia.roles.get",
      "laelia.roles.list",
      "laelia.roles.update",
      "laelia.roles.delete",
    ],
  },
  {
    resource: "iam",
    permissions: ["laelia.iam.getPolicy", "laelia.iam.setPolicy"],
  },
  {
    resource: "auditLogs",
    permissions: ["laelia.auditLogs.search", "laelia.auditLogs.export"],
  },
];

// ALL_PERMISSIONS is the flat catalog, in display order.
export const ALL_PERMISSIONS: string[] = PERMISSION_GROUPS.flatMap(
  (g) => g.permissions
);

// permissionLabel turns "laelia.conversations.reviewAgentDM" into a short
// verb-only label ("reviewAgentDM") for the checkbox grid.
export function permissionLabel(perm: string): string {
  const parts = perm.split(".");
  return parts.length >= 3 ? parts.slice(2).join(".") : perm;
}
