import {
  ACTIVITY_ROUTE,
  ACTIVITY_ROUTE_DETAIL,
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_MCP,
  AGENT_ROUTE_PROFILE,
  AGENT_ROUTE_WORKSPACE,
  CHAT_ROUTE,
  CHAT_ROUTE_DETAIL,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  HUMAN_ROUTE_DETAIL,
  MACHINE_ROUTE_LIST,
  MACHINE_ROUTE_PROFILE,
  MACHINE_ROUTE_WORKSPACE,
  MEMBERS_ROUTE,
  REMINDER_ROUTE_DETAIL,
  REMINDER_ROUTE_LIST,
  SETTINGS_ROUTE,
  SETTINGS_ROUTE_AGENTS,
  SETTINGS_ROUTE_API_PROVIDERS,
  SETTINGS_ROUTE_AUDIT,
  SETTINGS_ROUTE_GROUPS,
  SETTINGS_ROUTE_IAM,
  SETTINGS_ROUTE_MCP_SERVERS,
  SETTINGS_ROUTE_NOTIFICATIONS,
  SETTINGS_ROUTE_PROFILE,
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "./handles";

export interface RouteInfo {
  titleKey: string;
  backTo?: string;
}

// Mobile chrome (header back button, swipe-back gesture) resolves the current
// route's title and one-level-back target from this table. Routes without a
// backTo are top-level tabs — there is nothing to go back to.
export const ROUTE_INFO: Record<string, RouteInfo> = {
  [CHAT_ROUTE]: { titleKey: "sidebar.home" },
  [CHAT_ROUTE_DETAIL]: { titleKey: "sidebar.home", backTo: "/" },
  [ACTIVITY_ROUTE]: { titleKey: "sidebar.activity" },
  [ACTIVITY_ROUTE_DETAIL]: {
    titleKey: "activity.title",
    backTo: "/activity",
  },
  [MEMBERS_ROUTE]: { titleKey: "sidebar.members" },
  [HUMAN_ROUTE_DETAIL]: { titleKey: "sidebar.members", backTo: "/members" },
  [AGENT_ROUTE_PROFILE]: { titleKey: "agent.tab-profile", backTo: "/members" },
  [AGENT_ROUTE_CHAT]: { titleKey: "agent.tab-chat", backTo: "/members" },
  [AGENT_ROUTE_MCP]: { titleKey: "agent.tab-mcp", backTo: "/members" },
  [AGENT_ROUTE_WORKSPACE]: {
    titleKey: "agent.tab-workspace",
    backTo: "/members",
  },
  [COMMAND_ROUTE_LIST]: {
    titleKey: "agent.tab-commands",
    backTo: "/members",
  },
  [COMMAND_ROUTE_DETAIL]: {
    titleKey: "agent.tab-commands",
    backTo: "/members",
  },
  [REMINDER_ROUTE_LIST]: {
    titleKey: "agent.tab-reminders",
    backTo: "/members",
  },
  [REMINDER_ROUTE_DETAIL]: {
    titleKey: "agent.tab-reminders",
    backTo: "/members",
  },
  [MACHINE_ROUTE_LIST]: { titleKey: "sidebar.machines", backTo: "/settings" },
  [MACHINE_ROUTE_PROFILE]: {
    titleKey: "sidebar.machines",
    backTo: "/machines",
  },
  [MACHINE_ROUTE_WORKSPACE]: {
    titleKey: "sidebar.machines",
    backTo: "/machines",
  },
  [SETTINGS_ROUTE]: { titleKey: "sidebar.settings" },
  [SETTINGS_ROUTE_PROFILE]: {
    titleKey: "sidebar.settings-profile",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_STORAGE]: {
    titleKey: "sidebar.settings-storage",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_AGENTS]: {
    titleKey: "sidebar.settings-agents",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_NOTIFICATIONS]: {
    titleKey: "sidebar.settings-notifications",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_USERS]: {
    titleKey: "sidebar.settings-users",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_ROLES]: {
    titleKey: "sidebar.settings-roles",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_IAM]: {
    titleKey: "sidebar.settings-iam",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_GROUPS]: {
    titleKey: "sidebar.settings-groups",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_API_PROVIDERS]: {
    titleKey: "sidebar.settings-api-providers",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_MCP_SERVERS]: {
    titleKey: "sidebar.settings-mcp-servers",
    backTo: "/settings",
  },
  [SETTINGS_ROUTE_AUDIT]: {
    titleKey: "sidebar.settings-audit",
    backTo: "/settings",
  },
};
