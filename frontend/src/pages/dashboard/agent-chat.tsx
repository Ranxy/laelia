import { Bot, Hash } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { agentResourceName } from "@/lib/command-status";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

// Conversation.type values (see command.proto): 1 = direct DM, 2 = channel.
const CONVERSATION_TYPE_DM = 1;

export function AgentChatPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const fetchChannelsForAgent = useAppStore((s) => s.fetchChannelsForAgent);
  const agentChannelsByAgent = useAppStore((s) => s.agentChannelsByAgent);
  const loading = useAppStore((s) => s.agentChannelsLoading);

  const agentName = agentResourceName(agentId);
  const channels = agentChannelsByAgent[agentName] ?? [];

  useEffect(() => {
    if (!agentId) return;
    fetchChannelsForAgent(agentName);
  }, [agentId, agentName, fetchChannelsForAgent]);

  function openConversation(conv: Conversation) {
    const convId = conv.name.split("/").pop();
    if (convId) navigate(`/chat/${convId}`);
  }

  return (
    <div className="h-full overflow-y-auto p-4">
      {loading && channels.length === 0 ? (
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      ) : channels.length === 0 ? (
        <p className="text-sm text-control-light">{t("agent.chat-empty")}</p>
      ) : (
        <div className="flex flex-col gap-1">
          {channels.map((conv) => {
            const isDm = conv.type === CONVERSATION_TYPE_DM;
            return (
              <button
                key={conv.name}
                type="button"
                onClick={() => openConversation(conv)}
                className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left transition-colors hover:bg-accent/10 hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
              >
                <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-control-bg text-control">
                  {isDm ? (
                    <Bot className="size-4" />
                  ) : (
                    <Hash className="size-4" />
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <p className={cn("text-sm truncate font-medium text-main")}>
                    {conv.title || conv.name}
                  </p>
                  {!isDm && (
                    <p className="text-xs text-control-placeholder mt-0.5">
                      {t("channel.members", { count: conv.memberCount ?? 0 })}
                    </p>
                  )}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
