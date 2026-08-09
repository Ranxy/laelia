import {
  Bell,
  Blocks,
  Bot,
  ChevronRight,
  ClipboardList,
  Database,
  Lock,
  Monitor,
  Server,
  Shield,
  UserCircle,
  UserCog,
  Users,
} from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate } from "react-router-dom";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useHasPermission } from "@/stores/permissions";

interface MenuItem {
  to: string;
  icon: typeof UserCircle;
  label: string;
}

function useSettingsMenuItems(): MenuItem[] {
  const { t } = useTranslation();
  const canViewStorage =
    useHasPermission("laelia.settings.get") ||
    useHasPermission("laelia.settings.update");
  const canViewUsers = useHasPermission("laelia.users.list");
  const canViewMachines = useHasPermission("laelia.machines.get");
  const canViewRoles = useHasPermission("laelia.roles.list");
  const canViewIam = useHasPermission("laelia.iam.getPolicy");
  const canViewGroups = useHasPermission("laelia.groups.list");
  const canViewApiProviders = useHasPermission("laelia.apiProviders.list");
  const canViewAudit = useHasPermission("laelia.auditLogs.search");
  const canViewPushConfig = useHasPermission("laelia.pushConfig.update");

  return useMemo(
    () =>
      [
        {
          to: "/settings/profile",
          icon: UserCircle,
          label: t("sidebar.settings-profile"),
        },
        canViewStorage && {
          to: "/settings/storage",
          icon: Database,
          label: t("sidebar.settings-storage"),
        },
        canViewStorage && {
          to: "/settings/agents",
          icon: Bot,
          label: t("sidebar.settings-agents"),
        },
        canViewPushConfig && {
          to: "/settings/notifications",
          icon: Bell,
          label: t("sidebar.settings-notifications"),
        },
        canViewUsers && {
          to: "/settings/users",
          icon: Users,
          label: t("sidebar.settings-users"),
        },
        canViewRoles && {
          to: "/settings/roles",
          icon: Shield,
          label: t("sidebar.settings-roles"),
        },
        canViewIam && {
          to: "/settings/iam",
          icon: Lock,
          label: t("sidebar.settings-iam"),
        },
        canViewGroups && {
          to: "/settings/groups",
          icon: UserCog,
          label: t("sidebar.settings-groups"),
        },
        canViewApiProviders && {
          to: "/settings/api-providers",
          icon: Blocks,
          label: t("sidebar.settings-api-providers"),
        },
        {
          to: "/settings/mcp-servers",
          icon: Server,
          label: t("sidebar.settings-mcp-servers"),
        },
        canViewAudit && {
          to: "/settings/audit",
          icon: ClipboardList,
          label: t("sidebar.settings-audit"),
        },
        canViewMachines && {
          to: "/machines",
          icon: Monitor,
          label: t("sidebar.machines"),
        },
      ].filter(Boolean) as MenuItem[],
    [
      t,
      canViewStorage,
      canViewUsers,
      canViewRoles,
      canViewIam,
      canViewGroups,
      canViewApiProviders,
      canViewAudit,
      canViewPushConfig,
      canViewMachines,
    ]
  );
}

export function SettingsMenuPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const items = useSettingsMenuItems();

  return (
    <div className="h-full overflow-y-auto px-4 pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+1rem)] pt-4 lg:p-6">
      <h1 className="mb-4 hidden text-xl font-semibold text-main lg:block">
        {t("settings.title")}
      </h1>
      <nav aria-label={t("settings.title")} className="flex flex-col gap-1">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.to}
              type="button"
              onClick={() => navigate(item.to)}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-3 text-left",
                "text-sm font-medium text-main transition-colors hover:bg-control-bg"
              )}
            >
              <Icon className="size-5 shrink-0 text-control" />
              <span className="flex-1 truncate">{item.label}</span>
              <ChevronRight className="size-4 shrink-0 text-control-light" />
            </button>
          );
        })}
      </nav>
    </div>
  );
}

export function SettingsIndex() {
  const isDesktop = useIsDesktop();
  if (isDesktop) {
    return <Navigate to="profile" replace />;
  }
  return <SettingsMenuPage />;
}
