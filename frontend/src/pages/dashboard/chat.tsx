import { ArrowDown, Loader2, Square } from "lucide-react";
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
import { Button } from "@/react/components/ui/button";
import { Textarea } from "@/react/components/ui/textarea";
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
  const resetStreaming = useAppStore((s) => s.resetStreaming);

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
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, streamingContent, scrollToBottom]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const nearBottom = scrollHeight - scrollTop - clientHeight < 80;
    setShowScrollDown(!nearBottom);
  }, []);

  const autoResize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
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
    if (streamingCommandName) {
      resetStreaming(streamingCommandName);
    }
    setSending(false);
  };

  const isStreaming = sending || !!streamingCommandName;

  return (
    <div className="flex h-full flex-col">
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-6">
          {loading && (
            <div className="flex items-center justify-center gap-2 py-8 text-control-light text-sm">
              <Loader2 className="size-4 animate-spin" />
              {t("common.loading")}
            </div>
          )}
          {!loading && messages.length === 0 && (
            <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
              <p className="text-control-light text-sm">{t("chat.empty")}</p>
            </div>
          )}
          {messages.map((msg) => (
            <MessageBubble
              key={msg.id}
              msg={msg}
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
          ))}
        </div>
      </div>

      {showScrollDown && (
        <div className="relative">
          <button
            type="button"
            onClick={scrollToBottom}
            className={cn(
              "absolute bottom-4 left-1/2 -translate-x-1/2",
              "rounded-full size-9 flex items-center justify-center",
              "bg-control-bg border border-control-border shadow-md",
              "text-control hover:text-main hover:bg-link-hover transition-colors"
            )}
            aria-label={t("chat.scroll-to-bottom")}
          >
            <ArrowDown className="size-4" />
          </button>
        </div>
      )}

      <div className="shrink-0 border-t border-control-border bg-background">
        <div className="mx-auto max-w-3xl px-4 py-3">
          <div className="flex items-end gap-2">
            <Textarea
              ref={textareaRef}
              className="flex-1 resize-none min-h-[44px] max-h-[160px]"
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
            {isStreaming ? (
              <Button
                variant="outline"
                onClick={handleStop}
                className="h-9 w-9 p-0"
              >
                <Square className="size-4" />
              </Button>
            ) : (
              <Button
                onClick={handleSend}
                disabled={!input.trim()}
                size="default"
              >
                {t("common.send")}
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function MessageBubble({
  msg,
  streamingContent,
  streamingEvents,
  onViewDetails,
}: {
  msg: ChatMessageUI;
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

  return (
    <div
      className={cn(
        "flex flex-col gap-1",
        isUser ? "items-end" : "items-start"
      )}
    >
      <div className="flex items-center gap-2 px-1">
        <span className="text-xs text-control-light">
          {isUser ? t("chat.you") : t("chat.agent")}
        </span>
        <span className="text-xs text-control-light/60">
          {formatTime(msg.timestamp)}
        </span>
        {!isUser && msg.status !== undefined && !isStreaming && (
          <CommandStatusBadge
            status={msg.status}
            className="text-[10px] px-1.5 py-0"
          />
        )}
      </div>

      <div
        className={cn(
          "max-w-[85%] rounded-lg px-4 py-3 text-sm",
          isUser
            ? "bg-accent text-accent-foreground"
            : "bg-control-bg text-main"
        )}
      >
        {isUser ? (
          <div className="whitespace-pre-wrap">{displayContent || ""}</div>
        ) : displayContent ? (
          <div className="markstream-chat">
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
          <div className="flex items-center gap-2 text-control-light text-xs">
            <Loader2 className="size-3 animate-spin" />
            {t("chat.thinking")}
          </div>
        ) : (
          <div className="text-control-light text-xs italic">
            {displayContent || ""}
          </div>
        )}
      </div>

      {!isUser && (
        <div className="flex flex-col gap-1.5 w-full max-w-[85%]">
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
        </div>
      )}

      {!isUser && msg.commandId && !isStreaming && (
        <button
          type="button"
          className="text-xs text-control-light hover:text-accent px-1 cursor-pointer transition-colors"
          onClick={onViewDetails}
        >
          {t("chat.view-details")} &rarr;
        </button>
      )}
    </div>
  );
}
