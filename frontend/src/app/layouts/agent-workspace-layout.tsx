import { ArrowLeft, ListChecks, MessageSquare } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { agentResourceName } from "@/lib/command-status";
import { COMMAND_ROUTE_LIST } from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";

export function AgentWorkspaceLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
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

  // The agent workspace now hosts only the tasks/commands views; chat lives in
  // the unified /chat page. The Chat tab opens (creating if needed) the 1:1
  // conversation with this agent and navigates there. The workspace only ever
  // shows the tasks tab now, so it is always active.
  const activeTab = "tasks";

  const title = displayAgent?.title ?? agentId ?? "";

  const openDirectChat = async () => {
    try {
      const convName = await useAppStore
        .getState()
        .getOrCreateConversation(agentName);
      const convId = convName.split("/").pop();
      if (convId) navigate(`/chat/${convId}`);
    } catch {
      // open failed — stay on the workspace
    }
  };

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
            <TabsTrigger value="chat" onClick={() => openDirectChat()}>
              <MessageSquare className="size-3.5 mr-1.5" />
              {t("workspace.tab-chat")}
            </TabsTrigger>
            <TabsTrigger
              value="tasks"
              onClick={() =>
                navigate(resolvePath(COMMAND_ROUTE_LIST, { agentId }))
              }
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
