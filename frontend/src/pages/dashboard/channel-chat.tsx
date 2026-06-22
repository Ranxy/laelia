import {
  ArrowDown,
  ArrowLeft,
  Hash,
  Loader2,
  Plus,
  Send,
  Trash2,
  Users,
  X,
} from "lucide-react";
import MarkdownRender, {
  MarkdownCodeBlockNode,
  setCustomComponents,
} from "markstream-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/react/components/ui/select";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/react/components/ui/sheet";
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

function memberTypeLabel(
  t: (key: string) => string,
  memberType: number
): string {
  return memberType === 2
    ? t("channel.member-type-agent")
    : t("channel.member-type-user");
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
  const channelMembersByConv = useAppStore((s) => s.channelMembersByConv);
  const channelMembersLoading = useAppStore((s) => s.channelMembersLoading);
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const addChannelMember = useAppStore((s) => s.addChannelMember);
  const removeChannelMember = useAppStore((s) => s.removeChannelMember);
  const currentUser = useAppStore((s) => s.currentUser);
  const agents = useAppStore((s) => s.agents);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const fetchChannels = useAppStore((s) => s.fetchChannels);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastChannelRef = useRef<string | null>(null);
  const [showScrollDown, setShowScrollDown] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const [membersOpen, setMembersOpen] = useState(false);
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [addMemberType, setAddMemberType] = useState(2); // default AGENT
  const [addMemberId, setAddMemberId] = useState("");
  const [addingMember, setAddingMember] = useState(false);

  const conversationName = channelId ? `conversations/${channelId}` : "";
  const messages = chatMessages[conversationName] ?? [];
  const loading = chatLoading[conversationName] ?? false;

  const channel = channels.find((c) => c.name === conversationName);
  const members = channelMembersByConv[conversationName] ?? [];
  const membersLoading = channelMembersLoading[conversationName] ?? false;
  const isOwner =
    channel && currentUser
      ? channel.ownerId === currentUser.name.split("/").pop()
      : false;

  const init = useCallback(async () => {
    if (!channelId) return;
    if (lastChannelRef.current === channelId) return;
    lastChannelRef.current = channelId;
    try {
      await loadMessages(conversationName);
    } catch {
      // load failed
    }
    listChannelMembers(channelId);
    fetchAgents({ pageSize: 100 });
    fetchChannels();
  }, [
    channelId,
    conversationName,
    loadMessages,
    listChannelMembers,
    fetchAgents,
    fetchChannels,
  ]);

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

  const handleAddMember = useCallback(async () => {
    const memberId = addMemberId.trim();
    if (!memberId || addingMember || !channelId) return;
    setAddingMember(true);
    try {
      await addChannelMember(channelId, addMemberType, memberId);
      setAddMemberId("");
      setAddMemberOpen(false);
      listChannelMembers(channelId);
    } catch {
      // add failed
    } finally {
      setAddingMember(false);
    }
  }, [
    addMemberId,
    addMemberType,
    addingMember,
    channelId,
    addChannelMember,
    listChannelMembers,
  ]);

  const handleRemoveMember = useCallback(
    async (memberType: number, memberId: string) => {
      if (!channelId) return;
      try {
        await removeChannelMember(channelId, memberType, memberId);
        listChannelMembers(channelId);
      } catch {
        // remove failed
      }
    },
    [channelId, removeChannelMember, listChannelMembers]
  );

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
        <button
          type="button"
          onClick={() => {
            setMembersOpen(true);
            if (channelId) listChannelMembers(channelId);
          }}
          className="flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm text-control hover:text-main hover:bg-control-bg transition-colors"
        >
          <Users className="size-4" />
          <span className="hidden sm:inline">{members.length}</span>
        </button>
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

      {/* Members Sheet */}
      <Sheet
        open={membersOpen}
        onOpenChange={(open) => !open && setMembersOpen(false)}
      >
        <SheetContent width="narrow">
          <SheetHeader>
            <SheetTitle>
              {t("channel.members", { count: members.length })}
            </SheetTitle>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-0">
            {membersLoading && (
              <div className="flex items-center justify-center gap-2 py-12 text-control-light text-sm">
                <Loader2 className="size-4 animate-spin" />
                {t("common.loading")}
              </div>
            )}
            {!membersLoading && (
              <div className="divide-y divide-control-border">
                {members.map((m) => (
                  <div
                    key={`${m.memberType}-${m.memberId}`}
                    className="flex items-center gap-3 px-1 py-3"
                  >
                    <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-control-bg text-control text-xs font-medium">
                      {(m.displayName || m.memberId).charAt(0).toUpperCase()}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm text-main truncate">
                        {m.displayName || m.memberId}
                      </p>
                      <p className="text-xs text-control-placeholder flex items-center gap-1.5 mt-0.5">
                        <span>{memberTypeLabel(t, m.memberType)}</span>
                        {m.memberRole === 1 && (
                          <span className="rounded bg-accent/10 text-accent px-1.5 py-0 text-[10px] font-medium">
                            Owner
                          </span>
                        )}
                      </p>
                    </div>
                    {isOwner && m.memberRole !== 1 && (
                      <button
                        type="button"
                        onClick={() =>
                          handleRemoveMember(m.memberType, m.memberId)
                        }
                        className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-error hover:bg-error/10 transition-colors"
                        aria-label={t("common.delete")}
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
            )}

            {/* Add member section */}
            {isOwner && (
              <div className="mt-auto border-t border-control-border pt-4 px-1">
                {addMemberOpen ? (
                  <div className="space-y-3">
                    <div className="flex gap-2">
                      <button
                        type="button"
                        onClick={() => setAddMemberType(1)}
                        className={cn(
                          "flex-1 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors",
                          addMemberType === 1
                            ? "bg-accent text-accent-foreground"
                            : "bg-control-bg text-control hover:bg-control-bg/80"
                        )}
                      >
                        {t("channel.member-type-user")}
                      </button>
                      <button
                        type="button"
                        onClick={() => setAddMemberType(2)}
                        className={cn(
                          "flex-1 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors",
                          addMemberType === 2
                            ? "bg-accent text-accent-foreground"
                            : "bg-control-bg text-control hover:bg-control-bg/80"
                        )}
                      >
                        {t("channel.member-type-agent")}
                      </button>
                    </div>
                    <div className="flex gap-2">
                      {addMemberType === 2 ? (
                        <Select
                          value={addMemberId}
                          onValueChange={(v) => v && setAddMemberId(v)}
                        >
                          <SelectTrigger className="flex-1 h-auto rounded-md border border-control-border bg-background px-2.5 py-1.5 text-xs text-main">
                            <SelectValue
                              placeholder={t("channel.member-id-placeholder")}
                            />
                          </SelectTrigger>
                          <SelectContent>
                            {agents.map((agent) => {
                              const resourceId =
                                agent.name.split("/").pop() || agent.name;
                              return (
                                <SelectItem key={resourceId} value={resourceId}>
                                  {agent.title || resourceId}
                                </SelectItem>
                              );
                            })}
                          </SelectContent>
                        </Select>
                      ) : (
                        <input
                          type="text"
                          className={cn(
                            "flex-1 rounded-md border border-control-border bg-background px-2.5 py-1.5 text-xs text-main",
                            "placeholder:text-control-placeholder focus:outline-none focus:border-accent"
                          )}
                          placeholder={t("channel.member-id-placeholder")}
                          value={addMemberId}
                          onChange={(e) => setAddMemberId(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") handleAddMember();
                          }}
                          autoFocus
                        />
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setAddMemberOpen(false);
                          setAddMemberId("");
                        }}
                        className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
                      >
                        <X className="size-3.5" />
                      </button>
                    </div>
                    <button
                      type="button"
                      onClick={handleAddMember}
                      disabled={!addMemberId.trim() || addingMember}
                      className={cn(
                        "w-full rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors",
                        addMemberId.trim() && !addingMember
                          ? "bg-accent text-accent-foreground hover:bg-accent-hover"
                          : "bg-control-bg text-control-placeholder cursor-not-allowed"
                      )}
                    >
                      {addingMember
                        ? t("common.creating")
                        : t("channel.add-member")}
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setAddMemberOpen(true)}
                    className="flex w-full items-center gap-1.5 rounded-md px-2.5 py-2 text-sm text-control hover:text-main hover:bg-control-bg transition-colors"
                  >
                    <Plus className="size-4" />
                    {t("channel.add-member")}
                  </button>
                )}
              </div>
            )}
          </SheetBody>
        </SheetContent>
      </Sheet>
    </div>
  );
}

function Avatar({ label }: { label: string }) {
  return (
    <div
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
        label === "U"
          ? "bg-accent text-accent-foreground"
          : "bg-control-bg text-control"
      )}
    >
      {label.charAt(0).toUpperCase()}
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
          <Avatar label={isUser ? "U" : senderName || "A"} />
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
