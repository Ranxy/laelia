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
    resource: "settings",
    permissions: ["laelia.settings.get", "laelia.settings.update"],
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
