import { ArrowLeft } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
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
} from "@/router/handles";
import { useCurrentRoute } from "@/router/use-current-route";

interface RouteInfo {
  titleKey: string;
  backTo?: string;
}

const ROUTE_INFO: Record<string, RouteInfo> = {
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

export function MobileHeader() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const currentRoute = useCurrentRoute();

  const { title, backPath } = useMemo(() => {
    const info = currentRoute.name ? ROUTE_INFO[currentRoute.name] : undefined;
    if (info) {
      return { title: t(info.titleKey), backPath: info.backTo };
    }
    return { title: t("sidebar.home"), backPath: undefined };
  }, [currentRoute.name, t]);

  return (
    <header className="flex h-[var(--mobile-header-height)] shrink-0 items-center gap-2 border-b border-control-border bg-background px-4 lg:hidden">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => backPath && navigate(backPath)}
        disabled={!backPath}
        aria-label={t("common.back")}
        className={cn(
          "size-8 shrink-0 p-0",
          !backPath && "invisible pointer-events-none"
        )}
      >
        <ArrowLeft className="size-5" />
      </Button>
      <h1 className="min-w-0 flex-1 truncate text-center text-base font-semibold text-main">
        {title}
      </h1>
    </header>
  );
}
