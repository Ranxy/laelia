import {
  ACTIVITY_ROUTE,
  ACTIVITY_ROUTE_DETAIL,
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_MCP,
  AGENT_ROUTE_PROFILE,
  AGENT_ROUTE_WORKSPACE,
  CHANNEL_ROUTE_DETAIL,
  CHAT_ROUTE,
  CHAT_ROUTE_DETAIL,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  HUMAN_ROUTE_DETAIL,
  HUMAN_TEAM_ROUTE,
  MACHINE_ROUTE_LIST,
  MACHINE_ROUTE_NEW,
  MACHINE_ROUTE_PROFILE,
  MACHINE_ROUTE_WORKSPACE,
  MEMBERS_ROUTE,
  REMINDER_ROUTE_DETAIL,
  REMINDER_ROUTE_LIST,
  SEARCH_ROUTE,
  SETTINGS_ROUTE,
  SETTINGS_ROUTE_AGENTS,
  SETTINGS_ROUTE_API_PROVIDERS,
  SETTINGS_ROUTE_AUDIT,
  SETTINGS_ROUTE_GENERAL,
  SETTINGS_ROUTE_GROUPS,
  SETTINGS_ROUTE_IAM,
  SETTINGS_ROUTE_IDENTITY_PROVIDERS,
  SETTINGS_ROUTE_MCP_SERVERS,
  SETTINGS_ROUTE_NOTIFICATIONS,
  SETTINGS_ROUTE_PROFILE,
  SETTINGS_ROUTE_PROVISIONERS,
  SETTINGS_ROUTE_PROVISIONER_DETAIL,
  SETTINGS_ROUTE_PROVISIONER_CLEANUP,
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_SMTP,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "./handles";

// The compile-time union of every named route, derived from the handles.ts
// constants (single source of truth — 06 Rt/P1 "路由名三处真相"). A route
// registered in dashboard.tsx without an entry here fails type-check, and a
// name in this union without a registration is unreachable from ROUTE_INFO.
export type RouteName =
  | typeof CHAT_ROUTE
  | typeof CHAT_ROUTE_DETAIL
  | typeof SEARCH_ROUTE
  | typeof ACTIVITY_ROUTE
  | typeof ACTIVITY_ROUTE_DETAIL
  | typeof MEMBERS_ROUTE
  | typeof HUMAN_ROUTE_DETAIL
  | typeof HUMAN_TEAM_ROUTE
  | typeof AGENT_ROUTE_PROFILE
  | typeof AGENT_ROUTE_CHAT
  | typeof AGENT_ROUTE_MCP
  | typeof AGENT_ROUTE_WORKSPACE
  | typeof COMMAND_ROUTE_LIST
  | typeof COMMAND_ROUTE_DETAIL
  | typeof REMINDER_ROUTE_LIST
  | typeof REMINDER_ROUTE_DETAIL
  | typeof CHANNEL_ROUTE_DETAIL
  | typeof MACHINE_ROUTE_LIST
  | typeof MACHINE_ROUTE_NEW
  | typeof MACHINE_ROUTE_PROFILE
  | typeof MACHINE_ROUTE_WORKSPACE
  | typeof SETTINGS_ROUTE
  | typeof SETTINGS_ROUTE_PROFILE
  | typeof SETTINGS_ROUTE_STORAGE
  | typeof SETTINGS_ROUTE_AGENTS
  | typeof SETTINGS_ROUTE_GENERAL
  | typeof SETTINGS_ROUTE_SMTP
  | typeof SETTINGS_ROUTE_NOTIFICATIONS
  | typeof SETTINGS_ROUTE_USERS
  | typeof SETTINGS_ROUTE_ROLES
  | typeof SETTINGS_ROUTE_IAM
  | typeof SETTINGS_ROUTE_GROUPS
  | typeof SETTINGS_ROUTE_API_PROVIDERS
  | typeof SETTINGS_ROUTE_IDENTITY_PROVIDERS
  | typeof SETTINGS_ROUTE_PROVISIONERS
  | typeof SETTINGS_ROUTE_PROVISIONER_DETAIL
  | typeof SETTINGS_ROUTE_PROVISIONER_CLEANUP
  | typeof SETTINGS_ROUTE_MCP_SERVERS
  | typeof SETTINGS_ROUTE_AUDIT;

// The `handle` payload every dashboard route attaches; `satisfies RouteHandle`
// at each definition keeps route-tree names inside this union.
export interface RouteHandle {
  name: RouteName;
  // Route-level permission gate (06 Rt-02): the permission(s) the sidebar and
  // settings menu use to hide this entry. A string is a single requirement;
  // an array is ANY-of (holding one satisfies the handle), matching the
  // sidebar's `a || b` view gates. Enforced by RoutePermissionGate
  // (app/layouts/route-permission-gate.tsx) around the dashboard Outlet.
  permission?: string | string[];
}

export interface RouteInfo {
  titleKey: string;
  // One-level-back target as a ROUTE NAME (not a path) — consumers navigate
  // with resolvePath and resolve titles directly from this table, so a
  // back-target can never drift from the route it names.
  backTo?: RouteName;
}

// Mobile chrome (header back button, swipe-back gesture) resolves the current
// route's title and one-level-back target from this table. Routes without a
// backTo are top-level tabs — there is nothing to go back to.
export const ROUTE_INFO: Record<RouteName, RouteInfo> = {
  [CHAT_ROUTE]: { titleKey: "sidebar.home" },
  [CHAT_ROUTE_DETAIL]: { titleKey: "sidebar.home", backTo: CHAT_ROUTE },
  [SEARCH_ROUTE]: { titleKey: "globalSearch.title", backTo: CHAT_ROUTE },
  [ACTIVITY_ROUTE]: { titleKey: "sidebar.activity" },
  [ACTIVITY_ROUTE_DETAIL]: {
    titleKey: "activity.title",
    backTo: ACTIVITY_ROUTE,
  },
  [MEMBERS_ROUTE]: { titleKey: "sidebar.members" },
  [HUMAN_ROUTE_DETAIL]: { titleKey: "sidebar.members", backTo: MEMBERS_ROUTE },
  [HUMAN_TEAM_ROUTE]: { titleKey: "sidebar.members" },
  [AGENT_ROUTE_PROFILE]: {
    titleKey: "agent.tab-profile",
    backTo: MEMBERS_ROUTE,
  },
  [AGENT_ROUTE_CHAT]: { titleKey: "agent.tab-chat", backTo: MEMBERS_ROUTE },
  [AGENT_ROUTE_MCP]: { titleKey: "agent.tab-mcp", backTo: MEMBERS_ROUTE },
  [AGENT_ROUTE_WORKSPACE]: {
    titleKey: "agent.tab-workspace",
    backTo: MEMBERS_ROUTE,
  },
  [COMMAND_ROUTE_LIST]: {
    titleKey: "agent.tab-commands",
    backTo: MEMBERS_ROUTE,
  },
  [COMMAND_ROUTE_DETAIL]: {
    titleKey: "agent.tab-commands",
    backTo: MEMBERS_ROUTE,
  },
  [REMINDER_ROUTE_LIST]: {
    titleKey: "agent.tab-reminders",
    backTo: MEMBERS_ROUTE,
  },
  [REMINDER_ROUTE_DETAIL]: {
    titleKey: "agent.tab-reminders",
    backTo: MEMBERS_ROUTE,
  },
  [CHANNEL_ROUTE_DETAIL]: {
    titleKey: "sidebar.members",
    backTo: MEMBERS_ROUTE,
  },
  [MACHINE_ROUTE_LIST]: {
    titleKey: "sidebar.machines",
    backTo: SETTINGS_ROUTE,
  },
  [MACHINE_ROUTE_NEW]: {
    titleKey: "sidebar.machines",
    backTo: MACHINE_ROUTE_LIST,
  },
  [MACHINE_ROUTE_PROFILE]: {
    titleKey: "sidebar.machines",
    backTo: MACHINE_ROUTE_LIST,
  },
  [MACHINE_ROUTE_WORKSPACE]: {
    titleKey: "sidebar.machines",
    backTo: MACHINE_ROUTE_LIST,
  },
  [SETTINGS_ROUTE]: { titleKey: "sidebar.settings" },
  [SETTINGS_ROUTE_PROFILE]: {
    titleKey: "sidebar.settings-profile",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_STORAGE]: {
    titleKey: "sidebar.settings-storage",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_AGENTS]: {
    titleKey: "sidebar.settings-agents",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_GENERAL]: {
    titleKey: "sidebar.settings-general",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_SMTP]: {
    titleKey: "sidebar.settings-smtp",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_NOTIFICATIONS]: {
    titleKey: "sidebar.settings-notifications",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_USERS]: {
    titleKey: "sidebar.settings-users",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_ROLES]: {
    titleKey: "sidebar.settings-roles",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_IAM]: {
    titleKey: "sidebar.settings-iam",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_GROUPS]: {
    titleKey: "sidebar.settings-groups",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_API_PROVIDERS]: {
    titleKey: "sidebar.settings-api-providers",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_IDENTITY_PROVIDERS]: {
    titleKey: "sidebar.settings-identity-providers",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_PROVISIONERS]: {
    titleKey: "sidebar.settings-provisioners",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_PROVISIONER_DETAIL]: {
    titleKey: "sidebar.settings-provisioners",
    backTo: SETTINGS_ROUTE_PROVISIONERS,
  },
  [SETTINGS_ROUTE_PROVISIONER_CLEANUP]: {
    titleKey: "sidebar.settings-provisioners",
    backTo: SETTINGS_ROUTE_PROVISIONERS,
  },
  [SETTINGS_ROUTE_MCP_SERVERS]: {
    titleKey: "sidebar.settings-mcp-servers",
    backTo: SETTINGS_ROUTE,
  },
  [SETTINGS_ROUTE_AUDIT]: {
    titleKey: "sidebar.settings-audit",
    backTo: SETTINGS_ROUTE,
  },
};
