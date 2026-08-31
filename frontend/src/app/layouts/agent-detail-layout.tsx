import {
  Bell,
  FolderTree,
  ListChecks,
  Loader2,
  MessageSquare,
  Plug,
  UserCircle,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import {
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_MCP,
  AGENT_ROUTE_PROFILE,
  AGENT_ROUTE_WORKSPACE,
  COMMAND_ROUTE_LIST,
  REMINDER_ROUTE_LIST,
} from "@/router/handles";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import { type DetailTab, DetailTabsLayout } from "./detail-tabs-layout";

// AgentDetailLayout is the right-pane agent detail embedded in the Members
// page. It renders the agent tabs (profile / commands / reminders / chat /
// workspace / mcp) via the shared DetailTabsLayout and an Outlet for the
// active child route. The Members left rail already conveys the agent's
// identity and connection state, so — unlike the old standalone /agents page —
// this layout omits the back + title + status header bar. The global
// MobileHeader handles back navigation on small screens.
export function AgentDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
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

  const tabs: DetailTab[] = [
    {
      key: "profile",
      icon: UserCircle,
      labelKey: "agent.tab-profile",
      route: AGENT_ROUTE_PROFILE,
    },
    {
      key: "commands",
      icon: ListChecks,
      labelKey: "agent.tab-commands",
      route: COMMAND_ROUTE_LIST,
    },
    {
      key: "reminders",
      icon: Bell,
      labelKey: "agent.tab-reminders",
      route: REMINDER_ROUTE_LIST,
    },
    {
      key: "chat",
      icon: MessageSquare,
      labelKey: "agent.tab-chat",
      route: AGENT_ROUTE_CHAT,
    },
    {
      key: "mcp",
      icon: Plug,
      labelKey: "agent.tab-mcp",
      route: AGENT_ROUTE_MCP,
    },
    {
      key: "workspace",
      icon: FolderTree,
      labelKey: "agent.tab-workspace",
      route: AGENT_ROUTE_WORKSPACE,
      gate: canEdit,
    },
  ];

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
    <DetailTabsLayout
      idParam="agentId"
      tabs={tabs}
      tabsTrailing={
        <Button
          variant="outline"
          size="sm"
          onClick={startChat}
          disabled={startingChat || !agentId}
          className="mb-1 ml-auto hidden shrink-0 lg:inline-flex"
        >
          {startingChat ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <MessageSquare className="size-4" />
          )}
          {t("members.message-agent")}
        </Button>
      }
      footer={
        // Mobile send-message FAB: replaces the header Message button on touch
        // layouts, styled like the chat list's create-channel FAB.
        <button
          type="button"
          onClick={() => void startChat()}
          disabled={startingChat || !agentId}
          className="fixed right-4 bottom-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+0.75rem)] z-chrome flex h-14 items-center justify-center rounded-full bg-accent px-6 text-sm font-semibold whitespace-nowrap text-accent-text shadow-lg transition-all duration-200 focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 disabled:opacity-50 lg:hidden"
        >
          {startingChat ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            t("members.send-message")
          )}
        </button>
      }
    />
  );
}
