import {
  ArrowLeft,
  Bell,
  ListChecks,
  MessageSquare,
  UserCircle,
} from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { agentResourceName } from "@/lib/command-status";
import { agentLifecycle, lifecycleLabel } from "@/pages/dashboard/agents";
import {
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_PROFILE,
  COMMAND_ROUTE_LIST,
  REMINDER_ROUTE_LIST,
} from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";

type TabKey = "profile" | "commands" | "reminders" | "chat";

export function AgentDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { agentId } = useParams<{ agentId: string }>();
  const agents = useAppStore((s) => s.agents);

  const agentName = agentResourceName(agentId);

  // The header reads the AgentSummary list (loaded by the parent AgentsPage),
  // not GetAgent — title/status/lifecycle are all present on the summary, and
  // this avoids a per-navigation GetAgent round-trip. title falls back to the
  // raw id while the list loads on a hard refresh.
  const displayAgent = agents.find((a) => a.name === agentName);

  const title = displayAgent?.title ?? agentId ?? "";

  // Derive the active tab from the URL so deep links, refresh, and back/forward
  // keep the highlight in sync with the rendered child route.
  const activeTab = useMemo<TabKey>(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    const afterId = segments[2];
    if (afterId === "commands") return "commands";
    if (afterId === "reminders") return "reminders";
    if (afterId === "chat") return "chat";
    return "profile";
  }, [location.pathname]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex items-center gap-3 border-b border-control-border px-4 py-3 shrink-0 lg:px-6">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/agents")}
          aria-label={t("agent.back")}
          className="size-8 p-0 lg:hidden"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="text-base font-semibold text-main truncate">{title}</h1>
        <ConnectionBadge state={displayAgent?.status?.state} />
        {displayAgent && (
          <span className="text-xs text-control-light">
            {lifecycleLabel(t, agentLifecycle(displayAgent))}
          </span>
        )}
      </div>

      <Tabs value={activeTab} className="flex h-full flex-col overflow-hidden">
        <div className="px-4 border-b border-control-border shrink-0 lg:px-6">
          <TabsList className="gap-x-6 overflow-x-auto">
            <TabsTrigger
              value="profile"
              className="px-1"
              onClick={() =>
                navigate(resolvePath(AGENT_ROUTE_PROFILE, { agentId }))
              }
            >
              <UserCircle className="size-4" />
              {t("agent.tab-profile")}
            </TabsTrigger>
            <TabsTrigger
              value="commands"
              className="px-1"
              onClick={() =>
                navigate(resolvePath(COMMAND_ROUTE_LIST, { agentId }))
              }
            >
              <ListChecks className="size-4" />
              {t("agent.tab-commands")}
            </TabsTrigger>
            <TabsTrigger
              value="reminders"
              className="px-1"
              onClick={() =>
                navigate(resolvePath(REMINDER_ROUTE_LIST, { agentId }))
              }
            >
              <Bell className="size-4" />
              {t("agent.tab-reminders")}
            </TabsTrigger>
            <TabsTrigger
              value="chat"
              className="px-1"
              onClick={() =>
                navigate(resolvePath(AGENT_ROUTE_CHAT, { agentId }))
              }
            >
              <MessageSquare className="size-4" />
              {t("agent.tab-chat")}
            </TabsTrigger>
          </TabsList>
        </div>
        <div className="flex-1 overflow-hidden">
          <Outlet />
        </div>
      </Tabs>
    </div>
  );
}
