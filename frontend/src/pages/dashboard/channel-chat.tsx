import { ArrowDown, ArrowLeft, Hash, Loader2, Send } from "lucide-react";
import MarkdownRender, {
  MarkdownCodeBlockNode,
  setCustomComponents,
} from "markstream-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { cn } from "@/react/lib/utils";
import { useAppStore } from "@/react/stores";
import type { ChatMessageUI } from "@/react/stores/types";

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

export function ChannelChatPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { channelId } = useParams<{ channelId: string }>();

  const channels = useAppStore((s) => s.channels);
  const chatMessages = useAppStore((s) => s.chatMessages);
  const chatLoading = useAppStore((s) => s.chatLoading);
  const loadMessages = useAppStore((s) => s.loadMessages);
  const sendChannelMessage = useAppStore((s) => s.sendChannelMessage);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastChannelRef = useRef<string | null>(null);
  const [showScrollDown, setShowScrollDown] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const conversationName = channelId ? `conversations/${channelId}` : "";
  const messages = chatMessages[conversationName] ?? [];
  const loading = chatLoading[conversationName] ?? false;

  const channel = channels.find((c) => c.name === conversationName);

  const init = useCallback(async () => {
    if (!channelId) return;
    if (lastChannelRef.current === channelId) return;
    lastChannelRef.current = channelId;
    try {
      await loadMessages(conversationName);
    } catch {
      // load failed
    }
  }, [channelId, conversationName, loadMessages]);

  useEffect(() => {
    init();
  }, [init]);

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
  }, [messages]);

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

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || sending || !channelId) return;
    setInput("");
    setSending(true);
    try {
      await sendChannelMessage(channelId, text);
    } catch {
      // send failed
    } finally {
      setSending(false);
    }
  }, [input, sending, channelId, sendChannelMessage]);

  return (
    <div className="relative flex h-full flex-col overflow-hidden">
      {/* Header */}
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-3">
        <button
          type="button"
          onClick={() => navigate("/channels")}
          className="flex size-8 items-center justify-center rounded-md text-control hover:text-main hover:bg-control-bg transition-colors"
          aria-label={t("channel.back")}
        >
          <ArrowLeft className="size-4" />
        </button>
        <div className="flex size-8 items-center justify-center rounded-lg bg-control-bg text-control">
          <Hash className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold text-main truncate">
            {channel?.title ?? channelId ?? ""}
          </h2>
        </div>
      </div>

      {/* Messages scroll area */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-6 pt-6 pb-4">
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
              <ChannelMessageRow
                key={msg.id}
                msg={msg}
                showAvatar={showAvatar}
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
              placeholder={t("channel.placeholder")}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  handleSend();
                }
              }}
              disabled={sending}
            />
            <div className="flex items-center justify-between px-3 pb-2">
              <span className="text-xs text-control-placeholder">
                Enter {t("common.send")} · Shift+Enter
              </span>
              <button
                type="button"
                onClick={handleSend}
                disabled={!input.trim() || sending}
                className={cn(
                  "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                  input.trim() && !sending
                    ? "bg-accent text-accent-foreground hover:bg-accent-hover"
                    : "bg-control-bg text-control-placeholder cursor-not-allowed"
                )}
              >
                <Send className="size-3" />
                {t("common.send")}
              </button>
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

function ChannelMessageRow({
  msg,
  showAvatar,
}: {
  msg: ChatMessageUI;
  showAvatar: boolean;
}) {
  const { t } = useTranslation();
  const isUser = msg.role === "user";
  const senderName =
    msg.senderName || (isUser ? t("channel.you") : t("chat.agent"));
  const content = msg.content;
  return (
    <div className={cn("flex gap-3", isUser ? "flex-row-reverse" : "flex-row")}>
      <div className="flex shrink-0 flex-col items-center pt-0.5">
        {showAvatar ? (
          <Avatar isUser={isUser} />
        ) : (
          <div className="size-8 shrink-0" />
        )}
      </div>
      <div
        className={cn(
          "flex min-w-0 flex-1 flex-col gap-1.5",
          isUser ? "items-end" : "items-start"
        )}
      >
        {showAvatar && (
          <div className="flex items-center gap-2 px-0.5">
            <span className="text-xs font-medium text-control">
              {senderName}
            </span>
            <span className="text-xs text-control-placeholder">
              {formatTime(msg.timestamp)}
            </span>
          </div>
        )}

        <div
          className={cn(
            "rounded-2xl text-sm leading-relaxed",
            isUser
              ? "bg-accent text-accent-foreground rounded-tr-sm px-4 py-2.5 max-w-[80%]"
              : content
                ? "bg-control-bg/60 text-main rounded-tl-sm px-4 py-3 max-w-[80%]"
                : "hidden"
          )}
        >
          {isUser ? (
            <div className="whitespace-pre-wrap break-words">
              {content || ""}
            </div>
          ) : content ? (
            <div className="markstream-chat break-words">
              <MarkdownRender
                customId="channel-chat"
                content={content}
                final={true}
                fade={true}
              />
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
