import { ArrowLeft, ListChecks, MessageSquare } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { agentResourceName } from "@/lib/command-status";
import { useCurrentRoute } from "@/router/use-current-route";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";

export function AgentWorkspaceLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const currentRoute = useCurrentRoute();
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

  const activeTab =
    currentRoute.name === "command.list" ||
    currentRoute.name === "command.detail"
      ? "tasks"
      : "chat";

  const title = displayAgent?.title ?? agentId ?? "";

  return (
    <div className="flex h-full flex-col">
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
              value="chat"
              onClick={() => navigate(`/agents/${agentId}/chat`)}
            >
              <MessageSquare className="size-3.5 mr-1.5" />
              {t("workspace.tab-chat")}
            </TabsTrigger>
            <TabsTrigger
              value="tasks"
              onClick={() => navigate(`/agents/${agentId}/commands`)}
            >
              <ListChecks className="size-3.5 mr-1.5" />
              {t("workspace.tab-tasks")}
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
