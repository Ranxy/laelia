import {
  Activity as ActivityIcon,
  Home,
  type LucideIcon,
  Monitor,
  Search,
  Settings,
  Users,
} from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  ACTIVITY_ROUTE,
  CHAT_ROUTE,
  MACHINE_ROUTE_LIST,
  MEMBERS_ROUTE,
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
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_SMTP,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "@/router/handles";
import { useHasPermission } from "@/stores/permissions";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface SidebarItem {
  title?: string;
  name?: string;
  icon?: LucideIcon;
  hide?: boolean;
  type: "route" | "group";
  children?: SidebarItem[];
}

// ---------------------------------------------------------------------------
// Sidebar item list builder
// ---------------------------------------------------------------------------

export function useSidebarItems(): SidebarItem[] {
  const { t } = useTranslation();
  // Gate each Settings sub-item on the permission its page actually needs.
  // An ordinary workspace member (roles/workspaceMember baseline) holds none of
  // these, so every child is hidden for them and filterSidebarList then drops
  // the now-empty Settings group entirely — leaving no settings affordance in
  // the sidebar for non-admins. Per-resource perms are not considered here.
  // Evaluate both permission hooks before OR-ing their results. A direct
  // `useHasPermission(a) || useHasPermission(b)` short-circuits the second hook
  // when the first permission is granted, which changes the hook count between
  // renders (it depends on the logged-in user's permission set) and desyncs
  // React's hook list.
  const canSettingsGet = useHasPermission("laelia.settings.get");
  const canSettingsUpdate = useHasPermission("laelia.settings.update");
  const canViewStorage = canSettingsGet || canSettingsUpdate;
  const canViewUsers = useHasPermission("laelia.users.list");
  const canViewMachines = useHasPermission("laelia.machines.get");
  const canViewRoles = useHasPermission("laelia.roles.list");
  const canViewIam = useHasPermission("laelia.iam.getPolicy");
  const canViewGroups = useHasPermission("laelia.groups.list");
  const canViewApiProviders = useHasPermission("laelia.apiProviders.list");
  const canViewIdentityProviders = useHasPermission(
    "laelia.identityProviders.list"
  );
  const canViewProvisioners = useHasPermission("laelia.provisioners.create");
  const canViewAudit = useHasPermission("laelia.auditLogs.search");
  const canViewPushConfig = useHasPermission("laelia.pushConfig.update");

  return useMemo(
    (): SidebarItem[] => [
      {
        title: t("sidebar.home"),
        icon: Home,
        name: CHAT_ROUTE,
        type: "route",
      },
      {
        title: t("globalSearch.title"),
        icon: Search,
        name: SEARCH_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.activity"),
        icon: ActivityIcon,
        name: ACTIVITY_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.members"),
        icon: Users,
        name: MEMBERS_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.machines"),
        icon: Monitor,
        name: MACHINE_ROUTE_LIST,
        type: "route",
        hide: !canViewMachines,
      },
      {
        title: t("sidebar.settings"),
        icon: Settings,
        name: SETTINGS_ROUTE,
        type: "group",
        children: [
          {
            title: t("sidebar.settings-profile"),
            name: SETTINGS_ROUTE_PROFILE,
            type: "route",
          },
          {
            title: t("sidebar.settings-storage"),
            name: SETTINGS_ROUTE_STORAGE,
            type: "route",
            hide: !canViewStorage,
          },
          {
            title: t("sidebar.settings-general"),
            name: SETTINGS_ROUTE_GENERAL,
            type: "route",
            hide: !canViewStorage,
          },
          {
            title: t("sidebar.settings-smtp"),
            name: SETTINGS_ROUTE_SMTP,
            type: "route",
            hide: !canViewStorage,
          },
          {
            title: t("sidebar.settings-agents"),
            name: SETTINGS_ROUTE_AGENTS,
            type: "route",
            hide: !canViewStorage,
          },
          {
            title: t("sidebar.settings-notifications"),
            name: SETTINGS_ROUTE_NOTIFICATIONS,
            type: "route",
            hide: !canViewPushConfig,
          },
          {
            title: t("sidebar.settings-users"),
            name: SETTINGS_ROUTE_USERS,
            type: "route",
            hide: !canViewUsers,
          },
          {
            title: t("sidebar.settings-roles"),
            name: SETTINGS_ROUTE_ROLES,
            type: "route",
            hide: !canViewRoles,
          },
          {
            title: t("sidebar.settings-iam"),
            name: SETTINGS_ROUTE_IAM,
            type: "route",
            hide: !canViewIam,
          },
          {
            title: t("sidebar.settings-groups"),
            name: SETTINGS_ROUTE_GROUPS,
            type: "route",
            hide: !canViewGroups,
          },
          {
            title: t("sidebar.settings-api-providers"),
            name: SETTINGS_ROUTE_API_PROVIDERS,
            type: "route",
            hide: !canViewApiProviders,
          },
          {
            title: t("sidebar.settings-identity-providers"),
            name: SETTINGS_ROUTE_IDENTITY_PROVIDERS,
            type: "route",
            hide: !canViewIdentityProviders,
          },
          {
            title: t("sidebar.settings-provisioners"),
            name: SETTINGS_ROUTE_PROVISIONERS,
            type: "route",
            hide: !canViewProvisioners,
          },
          {
            title: t("sidebar.settings-mcp-servers"),
            name: SETTINGS_ROUTE_MCP_SERVERS,
            type: "route",
          },
          {
            title: t("sidebar.settings-audit"),
            name: SETTINGS_ROUTE_AUDIT,
            type: "route",
            hide: !canViewAudit,
          },
        ],
      },
    ],
    [
      t,
      canViewStorage,
      canViewMachines,
      canViewUsers,
      canViewRoles,
      canViewIam,
      canViewGroups,
      canViewApiProviders,
      canViewIdentityProviders,
      canViewProvisioners,
      canViewAudit,
      canViewPushConfig,
    ]
  );
}

// ---------------------------------------------------------------------------
// Filter logic
// ---------------------------------------------------------------------------

export function filterSidebarList(items: SidebarItem[]): SidebarItem[] {
  return items
    .map((item) => ({
      ...item,
      children: (item.children ?? []).filter((child) => !child.hide),
    }))
    .filter((item) => {
      if (item.hide) return false;
      if (item.children && item.children.length > 0) return true;
      if (item.type === "group") return false;
      return !!item.name;
    });
}
