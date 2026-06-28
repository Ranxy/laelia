import { create } from "@bufbuild/protobuf";
import {
  ArrowDown,
  ArrowLeft,
  Hash,
  Loader2,
  Paperclip,
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
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { AgentStatusBar } from "@/components/agent-status-bar";
import { FileCard } from "@/components/chat/FileCard";
import { MentionDetailSheet } from "@/components/chat/MentionDetailSheet";
import { MentionPopup } from "@/components/chat/MentionPopup";
import { detectMention } from "@/composables/useMentionDetect";
import {
  MentionTarget,
  targetToMention,
  useMentionTargets,
} from "@/composables/useMentionTargets";
import { commandServiceClient } from "@/connect";
import { getCaretCoordinates } from "@/lib/caret-position";
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
import type {
  AgentActivity,
  Attachment,
  ChannelMember,
} from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

// Stable empty fallbacks so per-key selectors returning undefined for an
// unloaded channel don't mint a new array each run (which would defeat
// zustand's Object.is equality and re-render on every store change).
const EMPTY_MESSAGES: ChatMessageUI[] = [];
const EMPTY_MEMBERS: ChannelMember[] = [];
const EMPTY_ACTIVITIES: AgentActivity[] = [];

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
  const loadMessages = useAppStore((s) => s.loadMessages);
  const sendChannelMessage = useAppStore((s) => s.sendChannelMessage);
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const startWatchingChannel = useAppStore((s) => s.startWatchingChannel);
  const stopWatchingChannel = useAppStore((s) => s.stopWatchingChannel);
  const addChannelMember = useAppStore((s) => s.addChannelMember);
  const removeChannelMember = useAppStore((s) => s.removeChannelMember);
  const currentUser = useAppStore((s) => s.currentUser);
  const agents = useAppStore((s) => s.agents);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const fetchChannels = useAppStore((s) => s.fetchChannels);

  const conversationName = channelId ? `conversations/${channelId}` : "";
  // Per-key slices: subscribe only to this channel's records, not the whole
  // map, so activity in other channels no longer re-renders this page. The
  // action functions above are stable store refs and never cause re-renders.
  const messages =
    useAppStore((s) => s.chatMessages[conversationName]) ?? EMPTY_MESSAGES;
  const loading = useAppStore((s) =>
    conversationName ? s.chatLoading[conversationName] : false
  );
  const members =
    useAppStore((s) => s.channelMembersByConv[conversationName]) ??
    EMPTY_MEMBERS;
  const membersLoading = useAppStore((s) =>
    conversationName ? s.channelMembersLoading[conversationName] : false
  );
  const activities =
    useAppStore((s) => s.agentActivities[conversationName]) ?? EMPTY_ACTIVITIES;

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>(
    []
  );
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastChannelRef = useRef<string | null>(null);
  const stickToBottomRef = useRef(true);
  const [showScrollDown, setShowScrollDown] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const [membersOpen, setMembersOpen] = useState(false);
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [addMemberType, setAddMemberType] = useState(2); // default AGENT
  const [addMemberId, setAddMemberId] = useState("");
  const [addingMember, setAddingMember] = useState(false);

  const [mentionState, setMentionState] = useState<{
    active: boolean;
    query: string;
    startIndex: number;
    matched: MentionTarget[];
  } | null>(null);
  const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
  const [mentionMap, setMentionMap] = useState<MentionTarget[]>([]);
  const [cursorPos, setCursorPos] = useState(0);
  const [detailMention, setDetailMention] = useState<{
    type: "user" | "agent";
    id: string;
    name: string;
  } | null>(null);

  const channel = channels.find((c) => c.name === conversationName);
  const isOwner =
    channel && currentUser
      ? channel.ownerId === currentUser.name.split("/").pop()
      : false;

  const mentionTargets = useMentionTargets(channelId);

  const init = useCallback(async () => {
    if (!channelId) return;
    if (lastChannelRef.current === channelId) return;
    // Stop watching the previous channel.
    if (lastChannelRef.current) {
      const prevName = `conversations/${lastChannelRef.current}`;
      stopWatchingChannel(prevName);
    }
    lastChannelRef.current = channelId;
    stickToBottomRef.current = true;
    try {
      await loadMessages(conversationName);
    } catch {
      // load failed
    }
    listChannelMembers(channelId);
    fetchAgents({ pageSize: 100 });
    fetchChannels();

    // Start background polling for new messages and agent activity.
    startWatchingChannel(conversationName);
  }, [
    channelId,
    conversationName,
    loadMessages,
    listChannelMembers,
    fetchAgents,
    fetchChannels,
    startWatchingChannel,
    stopWatchingChannel,
  ]);

  useEffect(() => {
    init();
    return () => {
      if (lastChannelRef.current) {
        stopWatchingChannel(`conversations/${lastChannelRef.current}`);
        lastChannelRef.current = null;
      }
    };
  }, [init, stopWatchingChannel]);

  const scrollToBottom = useCallback(() => {
    stickToBottomRef.current = true;
    if (scrollRef.current) {
      scrollRef.current.scrollTo({
        top: scrollRef.current.scrollHeight,
        behavior: "smooth",
      });
    }
  }, []);

  // Auto-stick to the bottom only when the user is already viewing the latest
  // messages. When they have scrolled up to read history, new polling updates
  // must not yank them back down.
  useEffect(() => {
    if (scrollRef.current && stickToBottomRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const nearBottom = scrollHeight - scrollTop - clientHeight < 100;
    stickToBottomRef.current = nearBottom;
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

  // uploadFile uploads a file via the CommandService.UploadFile RPC (the same
  // one agents use) and returns an Attachment describing it. The backend sniffs
  // the mime type and stores the blob in S3.
  const uploadFile = useCallback(
    async (file: File): Promise<Attachment | null> => {
      if (!channelId) return null;
      try {
        const res = await commandServiceClient.uploadFile({
          conversation: `conversations/${channelId}`,
          originalName: file.name,
          mimeType: file.type || "",
          data: new Uint8Array(await file.arrayBuffer()),
        });
        return create(AttachmentSchema, {
          id: res.id,
          name: res.originalName,
          mimeType: res.mimeType,
          sizeBytes: res.sizeBytes,
        });
      } catch (err) {
        console.error("file upload failed", err);
        return null;
      }
    },
    [channelId]
  );

  const handleFiles = useCallback(
    async (files: FileList | File[]) => {
      const list = Array.from(files);
      if (list.length === 0) return;
      setUploading(true);
      try {
        for (const file of list) {
          const att = await uploadFile(file);
          if (att) {
            setPendingAttachments((prev) => [...prev, att]);
          }
        }
      } finally {
        setUploading(false);
      }
    },
    [uploadFile]
  );

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if ((!text && pendingAttachments.length === 0) || sending || !channelId)
      return;
    const attachments = pendingAttachments;
    setInput("");
    setMentionState(null);
    setPendingAttachments([]);
    setSending(true);
    const mentions = mentionMap.map(targetToMention);
    try {
      await sendChannelMessage(channelId, text, mentions, attachments);
    } catch {
      // send failed — restore the attachments so the user can retry.
      setPendingAttachments(attachments);
    } finally {
      setSending(false);
    }
  }, [
    input,
    sending,
    channelId,
    mentionMap,
    sendChannelMessage,
    pendingAttachments,
  ]);

  const handleMentionClick = useCallback(
    (type: string, id: string, name: string) => {
      setDetailMention({
        type: type as "user" | "agent",
        id,
        name,
      });
    },
    []
  );

  const handleMentionSelect = useCallback(
    (target: MentionTarget) => {
      if (!mentionState) return;
      const before = input.slice(0, mentionState.startIndex);
      const after = input.slice(cursorPos);
      const newInput = `${before}@${target.name} ${after}`;
      setInput(newInput);
      setMentionMap((prev) => {
        if (prev.some((m) => m.id === target.id && m.type === target.type)) {
          return prev;
        }
        return [...prev, target];
      });
      setMentionState(null);
      setMentionSelectedIndex(0);
      setTimeout(() => {
        const el = textareaRef.current;
        if (el) {
          const newPos = mentionState.startIndex + target.name.length + 2;
          el.focus();
          el.setSelectionRange(newPos, newPos);
        }
      }, 0);
    },
    [input, cursorPos, mentionState]
  );

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
        <div className="min-w-0 flex-1 flex items-center gap-3">
          <h2 className="text-sm font-semibold text-main truncate">
            {channel?.title ?? channelId ?? ""}
          </h2>
          <AgentStatusBar activities={activities} />
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
            const showAvatar =
              !prevMsg ||
              prevMsg.role !== msg.role ||
              prevMsg.senderName !== msg.senderName;
            return (
              <ChannelMessageRow
                key={msg.id}
                msg={msg}
                showAvatar={showAvatar}
                onMentionClick={handleMentionClick}
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
          <div
            className="rounded-2xl border border-control-border bg-control-bg/40 focus-within:border-accent focus-within:bg-background transition-colors"
            onDragOver={(e) => {
              e.preventDefault();
            }}
            onDrop={(e) => {
              e.preventDefault();
              if (e.dataTransfer.files.length > 0)
                handleFiles(e.dataTransfer.files);
            }}
          >
            {pendingAttachments.length > 0 && (
              <div className="flex flex-wrap gap-1.5 px-3 pt-2">
                {pendingAttachments.map((att) => (
                  <span
                    key={att.id}
                    className="group flex items-center gap-1.5 rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main"
                  >
                    <span className="max-w-[160px] truncate">{att.name}</span>
                    <button
                      type="button"
                      onClick={() =>
                        setPendingAttachments((prev) =>
                          prev.filter((p) => p.id !== att.id)
                        )
                      }
                      className="text-control-placeholder hover:text-error transition-colors"
                      aria-label={t("common.delete")}
                    >
                      <X className="size-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}
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
              onChange={(e) => {
                const value = e.target.value;
                setInput(value);
                const pos = e.target.selectionStart ?? 0;
                setCursorPos(pos);
                const state = detectMention(value, pos, mentionTargets);
                setMentionState(state);
                setMentionSelectedIndex(0);
                if (state?.active) {
                  const newMap: MentionTarget[] = [];
                  const re = /(?:^|\s)@(\S+)/g;
                  let m: RegExpExecArray | null;
                  while ((m = re.exec(value)) !== null) {
                    const name = m[1];
                    const found = mentionTargets.find((t) => t.name === name);
                    if (found) newMap.push(found);
                  }
                  setMentionMap(newMap);
                }
              }}
              onKeyDown={(e) => {
                if (mentionState?.active) {
                  const total = mentionState.matched.length;
                  if (e.key === "ArrowDown") {
                    e.preventDefault();
                    setMentionSelectedIndex((idx) =>
                      idx + 1 < total ? idx + 1 : 0
                    );
                    return;
                  }
                  if (e.key === "ArrowUp") {
                    e.preventDefault();
                    setMentionSelectedIndex((idx) =>
                      idx - 1 >= 0 ? idx - 1 : total - 1
                    );
                    return;
                  }
                  if (e.key === "Enter" || e.key === "Tab") {
                    if (total === 0) return;
                    e.preventDefault();
                    if (mentionState.matched[mentionSelectedIndex]) {
                      handleMentionSelect(
                        mentionState.matched[mentionSelectedIndex]
                      );
                    }
                    return;
                  }
                  if (e.key === "Escape") {
                    e.preventDefault();
                    setMentionState(null);
                    return;
                  }
                }
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  handleSend();
                }
              }}
              onSelect={(e) => {
                const target = e.target as HTMLTextAreaElement;
                const pos = target.selectionStart ?? 0;
                setCursorPos(pos);
                const state = detectMention(target.value, pos, mentionTargets);
                setMentionState(state);
                setMentionSelectedIndex(0);
              }}
              disabled={sending}
            />
            <div className="flex items-center justify-between px-3 pb-2">
              <div className="flex items-center gap-2">
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  className="hidden"
                  onChange={(e) => {
                    if (e.target.files) handleFiles(e.target.files);
                    e.target.value = "";
                  }}
                />
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={uploading || sending}
                  className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors disabled:opacity-50"
                  aria-label={t("channel.attach-file")}
                >
                  {uploading ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Paperclip className="size-4" />
                  )}
                </button>
                <span className="text-xs text-control-placeholder">
                  Enter {t("common.send")} · Shift+Enter
                </span>
              </div>
              <button
                type="button"
                onClick={handleSend}
                disabled={
                  (!input.trim() && pendingAttachments.length === 0) || sending
                }
                className={cn(
                  "flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors",
                  (input.trim() || pendingAttachments.length > 0) && !sending
                    ? "bg-accent text-accent-foreground hover:bg-accent-hover"
                    : "bg-control-bg text-control-placeholder cursor-not-allowed"
                )}
              >
                <Send className="size-3" />
                {t("common.send")}
              </button>
            </div>
          </div>
          {mentionState?.active && textareaRef.current && (
            <MentionPopup
              targets={mentionState.matched}
              query={mentionState.query}
              position={getCaretCoordinates(textareaRef.current, cursorPos)}
              selectedIndex={mentionSelectedIndex}
              onSelect={handleMentionSelect}
              onClose={() => setMentionState(null)}
            />
          )}
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

      <MentionDetailSheet
        open={detailMention !== null}
        type={detailMention?.type ?? "user"}
        id={detailMention?.id ?? ""}
        name={detailMention?.name ?? ""}
        onClose={() => setDetailMention(null)}
      />
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

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function splitByMentions(
  content: string,
  mentions: { type: string; id: string; name: string }[]
): {
  text: string;
  mention: { type: string; id: string; name: string } | null;
}[] {
  if (mentions.length === 0) return [{ text: content, mention: null }];

  const segments: {
    text: string;
    mention: { type: string; id: string; name: string } | null;
  }[] = [];
  const sorted = [...mentions].sort((a, b) => {
    const ai = content.indexOf(`@${a.name}`);
    const bi = content.indexOf(`@${b.name}`);
    return ai - bi;
  });

  let lastIndex = 0;
  const used = new Set<string>();

  for (const m of sorted) {
    const pattern = `@${escapeRegex(m.name)}`;
    const re = new RegExp(pattern, "g");
    re.lastIndex = lastIndex;
    const match = re.exec(content);
    if (!match) continue;

    const idx = match.index;
    const key = `${idx}-${m.name}`;
    if (used.has(key)) continue;
    used.add(key);

    if (idx > lastIndex) {
      segments.push({ text: content.slice(lastIndex, idx), mention: null });
    }
    segments.push({ text: "", mention: m });
    lastIndex = idx + match[0].length;
  }

  if (lastIndex < content.length) {
    segments.push({ text: content.slice(lastIndex), mention: null });
  }

  return segments;
}

function MentionBadge({
  name,
  onClick,
}: {
  name: string;
  onClick: () => void;
}) {
  return (
    <span
      className="inline-flex items-center px-1 py-0.5 rounded bg-accent/15 text-accent font-medium cursor-pointer hover:bg-accent/25"
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter") onClick();
      }}
    >
      @{name}
    </span>
  );
}

const ChannelMessageRow = memo(function ChannelMessageRow({
  msg,
  showAvatar,
  onMentionClick,
}: {
  msg: ChatMessageUI;
  showAvatar: boolean;
  onMentionClick?: (type: string, id: string, name: string) => void;
}) {
  const { t } = useTranslation();
  const isUser = msg.role === "user";
  const senderName =
    msg.senderName || (isUser ? t("channel.you") : t("chat.agent"));
  const content = msg.content;
  const mentions = msg.mentions ?? [];

  const handleClick = (type: string, id: string, name: string) => {
    onMentionClick?.(type, id, name);
  };

  const segments = splitByMentions(content, mentions);

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
          {segments.length > 0 &&
            segments.map((seg, i) => {
              if (seg.mention) {
                return (
                  <MentionBadge
                    key={`${i}-${seg.mention.name}`}
                    name={seg.mention.name}
                    onClick={() =>
                      handleClick(
                        seg.mention!.type,
                        seg.mention!.id,
                        seg.mention!.name
                      )
                    }
                  />
                );
              }
              if (!seg.text) return null;
              if (isUser) {
                return (
                  <span key={i} className="whitespace-pre-wrap break-words">
                    {seg.text}
                  </span>
                );
              }
              return (
                <span key={i} className="markstream-chat break-words inline">
                  <MarkdownRender
                    customId="channel-chat"
                    content={seg.text}
                    final
                    fade
                  />
                </span>
              );
            })}
          {msg.attachments && msg.attachments.length > 0 && (
            <div className="flex flex-col gap-1">
              {msg.attachments.map((att) => (
                <FileCard key={att.id} attachment={att} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
});
