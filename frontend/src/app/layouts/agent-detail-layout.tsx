import {
  ArrowLeft,
  Bell,
  FolderTree,
  ListChecks,
  Loader2,
  MessageSquare,
  UserCircle,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_PROFILE,
  AGENT_ROUTE_WORKSPACE,
  COMMAND_ROUTE_LIST,
  REMINDER_ROUTE_LIST,
} from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";

type TabKey = "profile" | "commands" | "reminders" | "chat" | "workspace";

// AgentDetailLayout is the right-pane agent detail embedded in the Members
// page. It renders the agent tabs (profile / commands / reminders / chat /
// workspace) and an Outlet for the active child route. The Members left rail
// already conveys the agent's identity and connection state, so — unlike the old
// standalone /agents page — this layout omits the back + title + status
// header bar. A slim mobile-only back button returns to the rail on small
// screens (the rail is hidden there when a detail is open, mirroring
// machines.tsx).
export function AgentDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { agentId } = useParams<{ agentId: string }>();
  const getOrCreateConversation = useAppStore((s) => s.getOrCreateConversation);
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const getAgent = useAppStore((s) => s.getAgent);
  const [agent, setAgent] = useState<Agent | undefined>(undefined);
  const [startingChat, setStartingChat] = useState(false);

  // The workspace tab is owner/admin-only and the file tree is sensitive, so
  // the tab is rendered only when the full GetAgent result says canEdit (a
  // per-caller field that must be fetched fresh, never read from the roster
  // cache).
  useEffect(() => {
    let cancelled = false;
    if (!agentId) return;
    getAgent(`agents/${agentId}`).then((a) => {
      if (!cancelled) setAgent(a);
    });
    return () => {
      cancelled = true;
    };
  }, [agentId, getAgent]);

  const canEdit = agent?.canEdit === true;

  // Derive the active tab from the URL so deep links, refresh, and back/forward
  // keep the highlight in sync with the rendered child route.
  const activeTab = useMemo<TabKey>(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    // /members/agents/:agentId/<tab?> — the segment after the agent id.
    const afterId = segments[segments.indexOf(agentId ?? "") + 1];
    if (afterId === "commands") return "commands";
    if (afterId === "reminders") return "reminders";
    if (afterId === "chat") return "chat";
    if (afterId === "workspace") return "workspace";
    return "profile";
  }, [location.pathname, agentId]);

  // startChat opens (or reuses) the user↔agent DM and jumps to the chat surface.
  // The DM also appears in the chat left rail once channels are refreshed.
  async function startChat() {
    if (!agentId || startingChat) return;
    setStartingChat(true);
    try {
      const name = await getOrCreateConversation(`agents/${agentId}`);
      await fetchChannels();
      navigate(`/${name.split("/").pop()}`);
    } finally {
      setStartingChat(false);
    }
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <Tabs value={activeTab} className="flex h-full flex-col overflow-hidden">
        <div className="border-b border-control-border shrink-0">
          <div className="flex items-end gap-2 px-4 pt-2 lg:px-6">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate("/members")}
              aria-label={t("agent.back")}
              className="size-8 p-0 lg:hidden"
            >
              <ArrowLeft className="size-4" />
            </Button>
            <TabsList className="gap-x-6 border-b-0!">
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
              {canEdit && (
                <TabsTrigger
                  value="workspace"
                  className="px-1"
                  onClick={() =>
                    navigate(resolvePath(AGENT_ROUTE_WORKSPACE, { agentId }))
                  }
                >
                  <FolderTree className="size-4" />
                  {t("agent.tab-workspace")}
                </TabsTrigger>
              )}
            </TabsList>
            <Button
              variant="outline"
              size="sm"
              onClick={startChat}
              disabled={startingChat || !agentId}
              className="mb-1 ml-auto shrink-0"
            >
              {startingChat ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <MessageSquare className="size-4" />
              )}
              {t("members.message-agent")}
            </Button>
          </div>
        </div>
        <div className="flex-1 overflow-hidden">
          <Outlet />
        </div>
      </Tabs>
    </div>
  );
}
