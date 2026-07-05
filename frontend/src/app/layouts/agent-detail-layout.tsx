import { ArrowLeft, ListChecks, MessageSquare, UserCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { agentResourceName } from "@/lib/command-status";
import {
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_PROFILE,
  COMMAND_ROUTE_LIST,
} from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";

type TabKey = "profile" | "commands" | "chat";

export function AgentDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const agentCache = useAppStore((s) => s.agentCache);

  const [agent, setAgent] = useState<Agent | undefined>(undefined);

  const agentName = agentResourceName(agentId);

  useEffect(() => {
    if (!agentId) return;
    getAgent(agentName).then(setAgent);
  }, [agentId, agentName, getAgent]);

  const cached = agentCache[agentName];
  const displayAgent = agent ?? cached;

  const title = displayAgent?.title ?? agentId ?? "";

  // Derive the active tab from the URL so deep links, refresh, and back/forward
  // keep the highlight in sync with the rendered child route.
  const activeTab = useMemo<TabKey>(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    // ["agents", "<id>", ("commands"|"chat" | "commands", "<cmdId>")]
    const afterId = segments[2];
    if (afterId === "commands") return "commands";
    if (afterId === "chat") return "chat";
    return "profile";
  }, [location.pathname]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex items-center gap-3 border-b border-control-border px-4 py-2.5 shrink-0">
        <Button variant="ghost" size="sm" onClick={() => navigate("/agents")}>
          <ArrowLeft className="size-4" />
          {t("workspace.back-to-agents")}
        </Button>
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-sm font-semibold text-main truncate">{title}</h1>
          <ConnectionBadge state={displayAgent?.status?.state} />
        </div>
      </div>

      <Tabs value={activeTab} className="flex h-full flex-col overflow-hidden">
        <div className="px-4 border-b border-control-border shrink-0">
          <TabsList className="border-b-0">
            <TabsTrigger
              value="profile"
              onClick={() =>
                navigate(resolvePath(AGENT_ROUTE_PROFILE, { agentId }))
              }
            >
              <UserCircle className="size-3.5 mr-1.5" />
              {t("agent.tab-profile")}
            </TabsTrigger>
            <TabsTrigger
              value="commands"
              onClick={() =>
                navigate(resolvePath(COMMAND_ROUTE_LIST, { agentId }))
              }
            >
              <ListChecks className="size-3.5 mr-1.5" />
              {t("agent.tab-commands")}
            </TabsTrigger>
            <TabsTrigger
              value="chat"
              onClick={() =>
                navigate(resolvePath(AGENT_ROUTE_CHAT, { agentId }))
              }
            >
              <MessageSquare className="size-3.5 mr-1.5" />
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
