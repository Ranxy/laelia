import {
  ArrowDown,
  ArrowUp,
  ChevronRight,
  Loader2,
  Send,
  Square,
} from "lucide-react";
import MarkdownRender, {
  MarkdownCodeBlockNode,
  setCustomComponents,
} from "markstream-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ChatDiff } from "@/react/components/chat-events/diff-view";
import { ChatPermissionRequest } from "@/react/components/chat-events/permission-request";
import { ChatToolCall } from "@/react/components/chat-events/tool-call";
import { ChatWarning } from "@/react/components/chat-events/warning";
import { CommandStatusBadge } from "@/react/components/command-status-badge";
import { agentResourceName } from "@/react/lib/command-status";
import { cn } from "@/react/lib/utils";
import { useAppStore } from "@/react/stores";
import type { ChatMessageUI } from "@/react/stores/types";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";

setCustomComponents({
  // biome-ignore lint/suspicious/noExplicitAny: markstream custom component API is loosely typed
  code_block: ({ node, isDark, ctx }: any) => (
    <MarkdownCodeBlockNode
      node={node}
      isDark={isDark}
      stream={ctx?.codeBlockStream}
      {...(ctx?.codeBlockProps ?? {})}
    />
  ),
});

function formatTime(date: Date): string {
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  return `${hours}:${minutes}`;
}

function pairToolCallEvents(events: CommandEvent[]) {
  const started = events.filter(
    (e) => e.type === CommandEventType.TOOL_CALL_STARTED
  );
  const finished = events.filter(
    (e) => e.type === CommandEventType.TOOL_CALL_FINISHED
  );
  return started.map((s, i) => ({
    started: s,
    finished: finished[i],
  }));
}

export function ChatPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = agentResourceName(agentId);

  const conversations = useAppStore((s) => s.conversations);
  const chatMessages = useAppStore((s) => s.chatMessages);
  const chatLoading = useAppStore((s) => s.chatLoading);
  const streamingContent = useAppStore((s) => s.streamingContent);
  const streamingEvents = useAppStore((s) => s.streamingEvents);
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

  const conversation = conversations[agent];
  const messages = conversation ? (chatMessages[conversation] ?? []) : [];
  const loading = conversation ? (chatLoading[conversation] ?? false) : false;

  const streamingCommandName = useMemo(() => {
    const streamingMsg = messages.find((m) => m.streaming && m.commandName);
    return streamingMsg?.commandName;
  }, [messages]);

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
      if (!res.name) return;

      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      const convName = useAppStore.getState().conversations[agent];
      if (!convName) return;

      streamChatCommand(res.name, convName, controller.signal).finally(() => {
        setSending(false);
      });
    } catch {
      setSending(false);
    }
  };

  const handleStop = () => {
    abortRef.current?.abort();
    setSending(false);
  };

  const isStreaming = sending || !!streamingCommandName;

  return (
    <div className="relative flex h-full flex-col overflow-hidden">
      {/* Messages scroll area */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-6 px-6 pt-8 pb-4">
          {loading && (
            <div className="flex items-center justify-center gap-2 py-12 text-control-light text-sm">
              <Loader2 className="size-4 animate-spin" />
              {t("common.loading")}
            </div>
          )}
          {!loading && messages.length === 0 && (
            <div className="flex flex-col items-center justify-center gap-3 py-24 text-center">
              <div className="flex size-14 items-center justify-center rounded-full bg-control-bg text-control-light">
                <Send className="size-6" />
              </div>
              <p className="text-control-light text-sm">{t("chat.empty")}</p>
            </div>
          )}
          {messages.map((msg, idx) => {
            const prevMsg = idx > 0 ? messages[idx - 1] : null;
            const showAvatar = !prevMsg || prevMsg.role !== msg.role;
            return (
              <MessageRow
                key={msg.id}
                msg={msg}
                showAvatar={showAvatar}
                streamingContent={
                  msg.streaming && msg.commandName
                    ? (streamingContent[msg.commandName] ?? "")
                    : ""
                }
                streamingEvents={
                  msg.streaming && msg.commandName
                    ? (streamingEvents[msg.commandName] ?? [])
                    : (msg.events ?? [])
                }
                onViewDetails={() => {
                  if (msg.commandId) {
                    navigate(`/agents/${agentId}/commands/${msg.commandId}`);
                  }
                }}
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
            <textarea
              ref={textareaRef}
              className={cn(
                "block w-full resize-none bg-transparent px-4 py-3 text-sm text-main",
                "placeholder:text-control-placeholder",
                "focus:outline-none",
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
                Enter {t("common.send")} · Shift+Enter
              </span>
              {isStreaming ? (
                <button
                  type="button"
                  onClick={handleStop}
                  className={cn(
                    "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium",
                    "bg-error/10 text-error hover:bg-error/20 transition-colors"
                  )}
                >
                  <Square className="size-3" />
                  {t("chat.stop")}
                </button>
              ) : (
                <button
                  type="button"
                  onClick={handleSend}
                  disabled={!input.trim()}
                  className={cn(
                    "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                    input.trim()
                      ? "bg-accent text-accent-foreground hover:bg-accent-hover"
                      : "bg-control-bg text-control-placeholder cursor-not-allowed"
                  )}
                >
                  <Send className="size-3" />
                  {t("common.send")}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function Avatar({ isUser }: { isUser: boolean }) {
  return (
    <div
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
        isUser
          ? "bg-accent text-accent-foreground"
          : "bg-control-bg text-control"
      )}
    >
      {isUser ? "U" : "A"}
    </div>
  );
}

function MessageRow({
  msg,
  showAvatar,
  streamingContent,
  streamingEvents,
  onViewDetails,
}: {
  msg: ChatMessageUI;
  showAvatar: boolean;
  streamingContent: string;
  streamingEvents: CommandEvent[];
  onViewDetails: () => void;
}) {
  const { t } = useTranslation();
  const isUser = msg.role === "user";
  const isStreaming = msg.streaming;
  const displayContent = isStreaming ? streamingContent : msg.content;
  const events = isStreaming ? streamingEvents : (msg.events ?? []);

  const toolCallPairs = useMemo(() => pairToolCallEvents(events), [events]);
  const diffEvents = useMemo(
    () => events.filter((e) => e.type === CommandEventType.DIFF_EMITTED),
    [events]
  );
  const warningEvents = useMemo(
    () => events.filter((e) => e.type === CommandEventType.WARNING),
    [events]
  );
  const permissionEvent = useMemo(
    () => events.find((e) => e.type === CommandEventType.PERMISSION_REQUESTED),
    [events]
  );
  const isPermissionDecided = useMemo(
    () =>
      events.some(
        (e) =>
          e.type === CommandEventType.PERMISSION_DECIDED ||
          e.type === CommandEventType.PERMISSION_TIMED_OUT
      ),
    [events]
  );

  const hasEvents =
    !isUser &&
    (toolCallPairs.length > 0 ||
      diffEvents.length > 0 ||
      warningEvents.length > 0);

  const [eventsCollapsed, setEventsCollapsed] = useState(false);
  const prevStreamingRef = useRef(isStreaming);
  useEffect(() => {
    if (prevStreamingRef.current && !isStreaming && displayContent) {
      setEventsCollapsed(true);
    }
    prevStreamingRef.current = isStreaming;
  }, [isStreaming, displayContent]);

  const eventSummary = useMemo(() => {
    const parts: string[] = [];
    if (toolCallPairs.length > 0) {
      parts.push(`${toolCallPairs.length} ${t("chat.tool-call")}`);
    }
    if (diffEvents.length > 0) {
      parts.push(`${diffEvents.length} ${t("chat.diff")}`);
    }
    if (warningEvents.length > 0) {
      parts.push(`${warningEvents.length} ${t("chat.warning")}`);
    }
    return parts.join(" · ");
  }, [toolCallPairs.length, diffEvents.length, warningEvents.length, t]);

  return (
    <div className={cn("flex gap-3", isUser ? "flex-row-reverse" : "flex-row")}>
      {/* Avatar */}
      <div className="flex shrink-0 flex-col items-center pt-0.5">
        {showAvatar ? (
          <Avatar isUser={isUser} />
        ) : (
          <div className="size-8 shrink-0" />
        )}
      </div>

      {/* Message body */}
      <div
        className={cn(
          "flex min-w-0 flex-1 flex-col gap-1.5",
          isUser ? "items-end" : "items-start"
        )}
      >
        {/* Header */}
        {showAvatar && (
          <div className="flex items-center gap-2 px-0.5">
            <span className="text-xs font-medium text-control">
              {isUser ? t("chat.you") : t("chat.agent")}
            </span>
            <span className="text-xs text-control-placeholder">
              {formatTime(msg.timestamp)}
            </span>
            {!isUser && msg.status !== undefined && !isStreaming && (
              <CommandStatusBadge
                status={msg.status}
                className="text-[10px] px-1.5 py-0"
              />
            )}
          </div>
        )}

        {/* Events (tool calls, diffs, warnings) */}
        {hasEvents && !eventsCollapsed && (
          <div className="flex w-full flex-col gap-1.5">
            {toolCallPairs.map((pair, i) => (
              <ChatToolCall
                key={`tool-${i}-${pair.started.seqNo}`}
                startedEvent={pair.started}
                finishedEvent={pair.finished}
              />
            ))}
            {diffEvents.map((e) => (
              <ChatDiff key={`diff-${e.seqNo}`} event={e} />
            ))}
            {warningEvents.map((e) => (
              <ChatWarning key={`warn-${e.seqNo}`} event={e} />
            ))}
            {permissionEvent && !isPermissionDecided && msg.commandName && (
              <ChatPermissionRequest
                event={permissionEvent}
                commandName={msg.commandName}
              />
            )}
            {!isStreaming && (
              <button
                type="button"
                onClick={() => setEventsCollapsed(true)}
                className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent cursor-pointer transition-colors self-start"
              >
                <ArrowUp className="size-3" />
                {t("command.collapse")}
              </button>
            )}
          </div>
        )}

        {/* Collapsed events summary */}
        {hasEvents && eventsCollapsed && (
          <button
            type="button"
            onClick={() => setEventsCollapsed(false)}
            className="flex items-center gap-1.5 text-xs text-control-placeholder hover:text-accent cursor-pointer transition-colors"
          >
            <ChevronRight className="size-3" />
            <span>{eventSummary}</span>
          </button>
        )}

        {/* Content bubble */}
        <div
          className={cn(
            "rounded-2xl text-sm leading-relaxed",
            isUser
              ? "bg-accent text-accent-foreground rounded-tr-sm px-4 py-2.5 max-w-[80%]"
              : displayContent || isStreaming
                ? "bg-control-bg/60 text-main rounded-tl-sm px-4 py-3 max-w-[80%]"
                : "hidden"
          )}
        >
          {isUser ? (
            <div className="whitespace-pre-wrap break-words">
              {displayContent || ""}
            </div>
          ) : displayContent ? (
            <div className="markstream-chat break-words">
              <MarkdownRender
                customId="chat"
                content={displayContent}
                final={!isStreaming}
                smoothStreaming={isStreaming ? "auto" : false}
                fade={!isStreaming}
                typewriter={isStreaming}
                maxLiveNodes={isStreaming ? 0 : undefined}
              />
            </div>
          ) : isStreaming ? (
            <div className="flex items-center gap-2 text-control-light text-xs py-1">
              <span className="flex gap-1">
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:0ms]" />
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:150ms]" />
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:300ms]" />
              </span>
            </div>
          ) : null}
        </div>

        {/* View details link */}
        {!isUser && msg.commandId && !isStreaming && (
          <button
            type="button"
            className="text-xs text-control-placeholder hover:text-accent px-0.5 cursor-pointer transition-colors"
            onClick={onViewDetails}
          >
            {t("chat.view-details")} &rarr;
          </button>
        )}
      </div>
    </div>
  );
}
