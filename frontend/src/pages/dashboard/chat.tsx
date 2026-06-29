import { ArrowDown, Send, Square } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { agentResourceName } from "@/lib/command-status";
import "@/lib/markdown";
import {
  MessageRow,
  pairToolCallEvents,
  rowStreamingProps,
} from "@/components/chat/message-row";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

// Stable empty fallbacks so selectors returning `undefined` for an unloaded
// conversation don't create a new array literal each run (which would defeat
// zustand's Object.is equality and re-render on every store change).
const EMPTY_MESSAGES: ChatMessageUI[] = [];
const EMPTY_EVENTS: CommandEvent[] = [];

export function ChatPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = agentResourceName(agentId);

  // Fine-grained selectors: subscribe to per-key slices instead of whole
  // record maps. zustand's default Object.is equality means a component only
  // re-renders when its specific slice's reference changes, so streaming a
  // token in one conversation no longer re-renders components subscribed to
  // another. The action functions below are stable refs defined once in the
  // store slice, so selecting them never causes a re-render.
  const agentTitle =
    useAppStore((s) => s.agentCache[agent]?.title) ?? agentId ?? "";
  const conversation = useAppStore((s) => s.conversations[agent]);
  const messages =
    useAppStore((s) => s.chatMessages[conversation]) ?? EMPTY_MESSAGES;
  const loading = useAppStore((s) =>
    conversation ? s.chatLoading[conversation] : false
  );
  const getOrCreateConversation = useAppStore((s) => s.getOrCreateConversation);
  const loadMessages = useAppStore((s) => s.loadMessages);
  const sendChatMessage = useAppStore((s) => s.sendChatMessage);
  const streamChatCommand = useAppStore((s) => s.streamChatCommand);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const lastAgentRef = useRef<string | null>(null);
  const [showScrollDown, setShowScrollDown] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const streamingCommandName = useMemo(() => {
    const streamingMsg = messages.find((m) => m.streaming && m.commandName);
    return streamingMsg?.commandName;
  }, [messages]);

  // Subscribe only to the streaming command's slice, not the whole
  // streamingContent/streamingEvents maps. streamingCommandName is undefined
  // when nothing is streaming, so guard the lookup (undefined is not a valid
  // record key).
  const streamingContent =
    useAppStore((s) =>
      streamingCommandName
        ? s.streamingContent[streamingCommandName]
        : undefined
    ) ?? "";
  const streamingEvents =
    useAppStore((s) =>
      streamingCommandName ? s.streamingEvents[streamingCommandName] : undefined
    ) ?? EMPTY_EVENTS;

  const init = useCallback(async () => {
    if (lastAgentRef.current === agent) return;
    lastAgentRef.current = agent;
    try {
      const convName = await getOrCreateConversation(agent);
      await loadMessages(convName);
    } catch {
      // conversation init failed
    }
  }, [agent, getOrCreateConversation, loadMessages]);

  useEffect(() => {
    init();
  }, [init]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTo({
        top: scrollRef.current.scrollHeight,
        behavior: "smooth",
      });
    }
  }, []);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, streamingContent]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const nearBottom = scrollHeight - scrollTop - clientHeight < 100;
    setShowScrollDown(!nearBottom);
  }, []);

  const autoResize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, []);

  useEffect(() => {
    autoResize();
  }, [input, autoResize]);

  const handleSend = async () => {
    const text = input.trim();
    if (!text || sending) return;
    setInput("");
    setSending(true);

    try {
      const res = await sendChatMessage(agent, text);
      const commandName = res.commandId
        ? `${agent}/commands/${res.commandId}`
        : undefined;
      if (!commandName) return;

      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      const convName = useAppStore.getState().conversations[agent];
      if (!convName) return;

      streamChatCommand(commandName, convName, controller.signal).finally(
        () => {
          setSending(false);
        }
      );
    } catch {
      setSending(false);
    }
  };

  const handleStop = () => {
    abortRef.current?.abort();
    setSending(false);
  };

  const isStreaming = sending || !!streamingCommandName;

  // Stable per-page callback (depends only on agentId + navigate, both stable
  // across token re-renders) so memoized MessageRows don't re-render when the
  // parent re-renders. Each row passes its own commandId.
  const viewCommand = useCallback(
    (commandId: string) => {
      navigate(`/agents/${agentId}/commands/${commandId}`);
    },
    [agentId, navigate]
  );

  return (
    <div className="relative flex h-full flex-col overflow-hidden">
      {/* Messages scroll area */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-6 px-6 pt-8 pb-4">
          {loading && <LoadingState />}
          {!loading && messages.length === 0 && (
            <EmptyState icon={Send} message={t("chat.empty")} />
          )}
          {messages.map((msg, idx) => {
            const prevMsg = idx > 0 ? messages[idx - 1] : null;
            const showAvatar = !prevMsg || prevMsg.role !== msg.role;
            const isStreamingRow = !!(msg.streaming && msg.commandName);
            const rowProps = rowStreamingProps(
              msg,
              isStreamingRow,
              streamingContent,
              streamingEvents
            );
            return (
              <MessageRow
                key={msg.id}
                msg={msg}
                showAvatar={showAvatar}
                agentTitle={agentTitle}
                streamingContent={rowProps.streamingContent}
                streamingEvents={rowProps.streamingEvents}
                onViewDetails={viewCommand}
                markdownCustomId="chat"
              />
            );
          })}
        </div>
      </div>

      {/* Scroll to bottom button */}
      {showScrollDown && (
        <button
          type="button"
          onClick={scrollToBottom}
          className={cn(
            "absolute bottom-28 left-1/2 -translate-x-1/2 z-10",
            "flex size-9 items-center justify-center",
            "rounded-full border border-control-border bg-background shadow-lg",
            "text-control hover:text-main hover:bg-control-bg transition-all"
          )}
          aria-label={t("chat.scroll-to-bottom")}
        >
          <ArrowDown className="size-4" />
        </button>
      )}

      {/* Input area */}
      <div className="shrink-0 bg-background">
        <div className="mx-auto max-w-3xl px-6 pb-5 pt-2">
          <div className="rounded-2xl border border-control-border bg-control-bg/40 focus-within:border-accent focus-within:bg-background transition-colors">
            <Textarea
              ref={textareaRef}
              className={cn(
                "block w-full resize-none border-0 bg-transparent px-4 py-3 text-sm text-main",
                "placeholder:text-control-placeholder focus:ring-0 focus:border-transparent",
                "max-h-[200px] min-h-[24px]"
              )}
              rows={1}
              placeholder={
                isStreaming
                  ? t("chat.placeholder-processing")
                  : t("chat.placeholder")
              }
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  if (!isStreaming) handleSend();
                }
              }}
              disabled={isStreaming}
            />
            <div className="flex items-center justify-between px-3 pb-2">
              <span className="text-xs text-control-placeholder">
                {t("chat.send-hint")}
              </span>
              {isStreaming ? (
                <Button
                  type="button"
                  variant="destructive"
                  size="xs"
                  onClick={handleStop}
                >
                  <Square className="size-3" />
                  {t("chat.stop")}
                </Button>
              ) : (
                <Button
                  type="button"
                  size="xs"
                  onClick={handleSend}
                  disabled={!input.trim()}
                >
                  <Send className="size-3" />
                  {t("common.send")}
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export { formatTime } from "@/components/chat/avatar";
// Re-export the shared helpers/MessageRow so existing tests importing from
// this page keep working. New code should import directly from
// @/components/chat/message-row and @/components/chat/avatar.
export { MessageRow, pairToolCallEvents, rowStreamingProps };
