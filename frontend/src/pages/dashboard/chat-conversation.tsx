import { create } from "@bufbuild/protobuf";
import {
  ArrowDown,
  ArrowLeft,
  Bot,
  ExternalLink,
  Hash,
  ListTodo,
  Loader2,
  Paperclip,
  Plus,
  Send,
  Trash2,
  Users,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { AgentStatusBar } from "@/components/agent-status-bar";
import {
  MemberPicker,
  type MemberPickerType,
} from "@/components/chat/member-picker";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MentionDetailSheet } from "@/components/chat/mention-detail-sheet";
import { MentionPopup } from "@/components/chat/mention-popup";
import {
  EMPTY_EVENTS,
  MessageRow,
  rowStreamingProps,
} from "@/components/chat/message-row";
import { RemoteImage } from "@/components/chat/remote-image";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { TasksPanel } from "@/components/chat/tasks-panel";
import { ThreadPanel } from "@/components/chat/thread-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { detectMention } from "@/composables/useMentionDetect";
import {
  type MentionTarget,
  targetToMention,
  useMentionTargets,
} from "@/composables/useMentionTargets";
import { commandServiceClient } from "@/connect";
import { getCaretCoordinates } from "@/lib/caret-position";
import { isImageAttachment } from "@/lib/image-file";
import "@/lib/markdown";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";
import type {
  AgentActivity,
  Attachment,
  ChannelMember,
  Conversation,
} from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

// Stable empty fallbacks so per-key selectors returning undefined for an
// unloaded conversation don't mint a new array each run (which would defeat
// zustand's Object.is equality and re-render on every store change).
const EMPTY_MESSAGES: ChatMessageUI[] = [];
const EMPTY_MEMBERS: ChannelMember[] = [];
const EMPTY_ACTIVITIES: AgentActivity[] = [];

// DOM id of the mention popup listbox, used to wire the textarea's
// aria-controls / aria-activedescendant to the active option.
const MENTION_POPUP_ID = "mention-popup";

// Conversation type values mirror Conversation.type: 1 = direct/DM (user+agent),
// 2 = channel, 3 = AGENT_DM (agent+agent, owned by the system bot).
const CONVERSATION_TYPE_DM = 1;
const CONVERSATION_TYPE_AGENT_DM = 3;

function memberTypeLabel(
  t: (key: string) => string,
  memberType: number
): string {
  return memberType === 2
    ? t("channel.member-type-agent")
    : t("channel.member-type-user");
}

// ChannelConversationViewProps lets this page be reused embedded (read-only)
// in the Activity detail pane, in addition to its primary use as the chat
// route. When `conversationId` is omitted the route param is used; `readOnly`
// hides the composer (like an agent-to-agent DM); `scrollToMessageId` scrolls
// the list to a specific message once loaded; `onViewInChannel` renders a
// header "View in channel" affordance. All props are optional and default to
// the route-driven behavior, so the existing chat page is unchanged.
export interface ChannelConversationViewProps {
  conversationId?: string;
  readOnly?: boolean;
  scrollToMessageId?: string;
  onViewInChannel?: () => void;
}

export function ChatConversationPage(props?: ChannelConversationViewProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const params = useParams<{ conversationId: string }>();
  // An explicit prop (embedded Activity view) overrides the route param.
  const channelId = props?.conversationId ?? params.conversationId;

  const channels = useAppStore((s) => s.channels);
  const loadMessages = useAppStore((s) => s.loadMessages);
  const sendChannelMessage = useAppStore((s) => s.sendChannelMessage);
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const startWatchingChannel = useAppStore((s) => s.startWatchingChannel);
  const stopWatchingChannel = useAppStore((s) => s.stopWatchingChannel);
  const addChannelMember = useAppStore((s) => s.addChannelMember);
  const removeChannelMember = useAppStore((s) => s.removeChannelMember);
  const markConversationRead = useAppStore((s) => s.markConversationRead);
  const currentUser = useAppStore((s) => s.currentUser);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const openThread = useAppStore((s) => s.openThread);
  const closeThread = useAppStore((s) => s.closeThread);
  const activeThreadRoot = useAppStore((s) => s.activeThreadRoot);
  const activeThreadConversation = useAppStore(
    (s) => s.activeThreadConversation
  );
  const toggleTasksPanel = useAppStore((s) => s.toggleTasksPanel);
  const closeTasksPanel = useAppStore((s) => s.closeTasksPanel);
  const openFilePreview = useAppStore((s) => s.openFilePreview);
  const openImagePreview = useAppStore((s) => s.openImagePreview);
  const tasksPanelOpen = useAppStore((s) =>
    channelId
      ? (s.tasksPanelOpen[`conversations/${channelId}`] ?? false)
      : false
  );

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
  // Per-conversation input draft cache so switching channels does not leak the
  // half-typed message, pending attachments, or @mention map across channels.
  // Keyed by channelId; lives for the lifetime of this page instance.
  const draftRef = useRef<
    Record<
      string,
      { input: string; attachments: Attachment[]; mentions: MentionTarget[] }
    >
  >({});

  const [membersOpen, setMembersOpen] = useState(false);
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [addMemberType, setAddMemberType] = useState<MemberPickerType>(2); // default AGENT
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
  // asTask toggles whether the next send creates a channel task (a top-level
  // message with task metadata) instead of a plain message. It resets to false
  // after each send so task creation is deliberate per message.
  const [asTask, setAsTask] = useState(false);
  const [detailMention, setDetailMention] = useState<{
    type: "user" | "agent";
    id: string;
    name: string;
  } | null>(null);
  // Conversation metadata fetched on demand via GetChannel. The user's left-rail
  // ListChannels excludes agent-DMs (type 3 — the user is not a member), so when
  // an admin opens one directly `channels` has no entry and `channel` would be
  // undefined. We fetch the single conversation so its type is known and the
  // composer can be gated for agent-DMs. Null while unset or on fetch failure.
  const [fetchedChannel, setFetchedChannel] = useState<Conversation | null>(
    null
  );

  const channel =
    channels.find((c) => c.name === conversationName) ??
    fetchedChannel ??
    undefined;
  const isDm = channel?.type === CONVERSATION_TYPE_DM;
  // Agent-to-agent DMs (type 3) are admin view-only: a user cannot send or
  // alter membership there. membershipFixed covers both DM shapes (user+agent
  // and agent+agent), whose rosters are fixed at creation.
  const isAgentDm = channel?.type === CONVERSATION_TYPE_AGENT_DM;
  const membershipFixed = isDm || isAgentDm;
  const isOwner =
    channel && currentUser
      ? channel.ownerId === currentUser.name.split("/").pop()
      : false;

  // Fetch conversation metadata when the open conversation is absent from the
  // user's left-rail `channels` (notably agent-DMs, which ListChannels excludes
  // by membership). GetChannel's admin bypass lets an admin read it; a non-admin
  // is denied and the fetch fails silently (they cannot view the DM at all).
  useEffect(() => {
    if (!channelId || !conversationName) return;
    if (channels.some((c) => c.name === conversationName)) {
      setFetchedChannel(null);
      return;
    }
    let cancelled = false;
    commandServiceClient
      .getChannel({ name: conversationName })
      .then((res) => {
        if (!cancelled) setFetchedChannel(res);
      })
      .catch(() => {
        if (!cancelled) setFetchedChannel(null);
      });
    return () => {
      cancelled = true;
    };
  }, [channelId, conversationName, channels]);

  // The thread panel is open only when it belongs to the currently-viewed
  // channel; switching channels closes it (see init()).
  const threadRootOpen =
    activeThreadConversation === conversationName ? activeThreadRoot : null;

  // memberIds already in the channel for the currently-selected add-member
  // type, used to disable + badge them in the picker so they can't be re-added.
  const existingMemberIds = useMemo(
    () =>
      new Set(
        members
          .filter((m) => m.memberType === addMemberType)
          .map((m) => m.memberId)
      ),
    [members, addMemberType]
  );

  const mentionTargets = useMentionTargets(channelId);

  const init = useCallback(async () => {
    if (!channelId) return;
    if (lastChannelRef.current === channelId) return;
    // Stop watching the previous channel.
    if (lastChannelRef.current) {
      const prevName = `conversations/${lastChannelRef.current}`;
      stopWatchingChannel(prevName);
    }
    // Close any open thread panel — it belongs to the previous channel.
    closeThread();
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

    // Clear the unread badge for this conversation now that the user has it
    // open. Done after loadMessages so the cursor advance reflects the latest
    // fetched version.
    markConversationRead(channelId);

    // Start background polling for new messages and agent activity.
    startWatchingChannel(conversationName);
  }, [
    channelId,
    conversationName,
    loadMessages,
    listChannelMembers,
    fetchAgents,
    fetchChannels,
    markConversationRead,
    startWatchingChannel,
    stopWatchingChannel,
    closeThread,
  ]);

  useEffect(() => {
    init();
    return () => {
      if (lastChannelRef.current) {
        stopWatchingChannel(`conversations/${lastChannelRef.current}`);
        lastChannelRef.current = null;
      }
      closeThread();
    };
  }, [init, stopWatchingChannel, closeThread]);

  // Deep-link from outside the channel (e.g. a reminder's "View in channel"):
  // ?thread=<rootId> opens that thread and scrolls the main list to its root
  // message once the channel's messages are mounted. Runs at most once per
  // root id so it doesn't fight the user's own navigation.
  const [searchParams, setSearchParams] = useSearchParams();
  const threadDeepLinkId = searchParams.get("thread") ?? "";
  const threadDeepLinkRef = useRef<string>("");
  useEffect(() => {
    if (!channelId || !threadDeepLinkId) return;
    if (threadDeepLinkRef.current === threadDeepLinkId) return;
    if (messages.length === 0) return; // wait for the channel to load
    threadDeepLinkRef.current = threadDeepLinkId;
    openThread(`conversations/${channelId}`, threadDeepLinkId);
    // Defer so the root message is in the DOM.
    requestAnimationFrame(() => {
      scrollRef.current
        ?.querySelector(`[data-msg-id="${threadDeepLinkId}"]`)
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
    // Clean the param so a later in-page open of a different thread doesn't
    // re-trigger this.
    setSearchParams((prev) => {
      if (!prev.has("thread")) return prev;
      const next = new URLSearchParams(prev);
      next.delete("thread");
      return next;
    });
  }, [
    channelId,
    threadDeepLinkId,
    messages.length,
    openThread,
    setSearchParams,
  ]);

  // When embedded with a scrollToMessageId (the Activity detail pane pointing
  // at the message an activity references), scroll the main list to that
  // message once the channel's messages are mounted. Runs at most once per id
  // so it does not fight the user's own scrolling. Mirrors the thread deep-link
  // scroll above.
  const scrollToMessageId = props?.scrollToMessageId ?? "";
  const scrollToMessageRef = useRef<string>("");
  useEffect(() => {
    if (!scrollToMessageId || messages.length === 0) return;
    if (scrollToMessageRef.current === scrollToMessageId) return;
    scrollToMessageRef.current = scrollToMessageId;
    requestAnimationFrame(() => {
      scrollRef.current
        ?.querySelector(`[data-msg-id="${scrollToMessageId}"]`)
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
  }, [scrollToMessageId, messages.length]);

  // Restore the entering conversation's draft. Declared before the persist
  // effect so it reads the saved draft before any stale write lands.
  useEffect(() => {
    if (!channelId) return;
    const d = draftRef.current[channelId];
    setInput(d?.input ?? "");
    setPendingAttachments(d?.attachments ?? []);
    setMentionMap(d?.mentions ?? []);
    setMentionState(null);
    setMentionSelectedIndex(0);
    setCursorPos(0);
  }, [channelId]);

  // Persist the current input/attachments/mentions to the draft cache on every
  // change. On a switch the restore effect above seeds the new conversation's
  // state, which re-triggers this effect and writes the restored values back —
  // so each conversation keeps its own draft without cross-talk.
  useEffect(() => {
    if (channelId) {
      draftRef.current[channelId] = {
        input,
        attachments: pendingAttachments,
        mentions: mentionMap,
      };
    }
  }, [channelId, input, pendingAttachments, mentionMap]);

  // Auto-mark-read as new messages arrive via polling while the conversation
  // is open. On conversation switch we just reset the baseline; the initial
  // markRead is handled by init() above.
  const prevMsgCountRef = useRef(0);
  const lastMarkConvRef = useRef<string | null>(null);
  useEffect(() => {
    if (!channelId) return;
    if (lastMarkConvRef.current !== channelId) {
      lastMarkConvRef.current = channelId;
      prevMsgCountRef.current = messages.length;
      return;
    }
    if (messages.length > prevMsgCountRef.current) {
      markConversationRead(channelId);
    }
    prevMsgCountRef.current = messages.length;
  }, [messages, channelId, markConversationRead]);

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
    const sendAsTask = asTask;
    setInput("");
    setMentionState(null);
    setPendingAttachments([]);
    setAsTask(false);
    setSending(true);
    const mentions = mentionMap.map(targetToMention);
    try {
      await sendChannelMessage(
        channelId,
        text,
        mentions,
        attachments,
        sendAsTask
      );
    } catch {
      // send failed — restore the attachments (and the asTask toggle) so the
      // user can retry.
      setPendingAttachments(attachments);
      setAsTask(sendAsTask);
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
    asTask,
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

  // Channel rows are never in DM-style streaming mode (channel messages are
  // polled, not streamed token-by-token), so every row receives stable empty
  // streaming slices. The shared MessageRow still accepts them.
  const handleViewDetails = useCallback(
    (commandId: string, agentId: string) => {
      navigate(`/agents/${agentId}/commands/${commandId}`);
    },
    [navigate]
  );

  const handleOpenThread = useCallback(
    (msg: ChatMessageUI) => {
      if (!channelId || msg.threadRoot) return;
      if (channelId) closeTasksPanel(channelId);
      openThread(conversationName, msg.id);
    },
    [channelId, conversationName, openThread, closeTasksPanel]
  );

  // Open the full-page markdown preview for an attachment. The rootMessageId
  // is the attachment owner's effective thread root (its own threadRoot when
  // it is a reply, otherwise its own id) — used in Phase 2 to route comments.
  const handlePreviewAttachment = useCallback(
    (att: Attachment, rootMessageId: string) => {
      if (!channelId) return;
      openFilePreview(conversationName, rootMessageId, att);
    },
    [channelId, conversationName, openFilePreview]
  );

  // Cross-scenario anchor jump: a comment's anchor chip (rendered in a thread
  // reply or channel message) opens the file's preview already scrolled to the
  // section the comment is anchored to. The anchored attachment references the
  // file (same id/name/mime/size), so openFilePreview downloads and renders it,
  // then the overlay scrolls to scrollToSectionId once the DOM is ready.
  const handleJumpToSection = useCallback(
    (att: Attachment, sectionId: string, rootMessageId: string) => {
      if (!channelId) return;
      openFilePreview(conversationName, rootMessageId, att, sectionId);
    },
    [channelId, conversationName, openFilePreview]
  );

  // Open the image lightbox for an inline image attachment (published message
  // or thread root/reply). The attachment id is the download key.
  const handlePreviewImage = useCallback(
    (att: Attachment) => {
      openImagePreview(att);
    },
    [openImagePreview]
  );

  const handleToggleTasksPanel = useCallback(() => {
    if (!channelId) return;
    // Opening the tasks panel closes the thread panel — two 420px side panels
    // plus the main list is too wide on most screens.
    if (!tasksPanelOpen) closeThread();
    toggleTasksPanel(channelId);
  }, [channelId, tasksPanelOpen, toggleTasksPanel, closeThread]);

  // handleOpenTaskThread drills from the task board into a task's workspace:
  // close the tasks panel and open the task's thread (reused ThreadPanel).
  const handleOpenTaskThread = useCallback(
    (taskMessageId: string) => {
      if (!channelId) return;
      closeTasksPanel(channelId);
      openThread(conversationName, taskMessageId);
    },
    [channelId, conversationName, openThread, closeTasksPanel]
  );

  const handleViewInChannel = useCallback(() => {
    const rootId = threadRootOpen;
    closeThread();
    if (rootId && scrollRef.current) {
      // Defer until the panel unmounts so the main list reclaims width.
      requestAnimationFrame(() => {
        scrollRef.current
          ?.querySelector(`[data-msg-id="${rootId}"]`)
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      });
    }
  }, [threadRootOpen, closeThread]);

  return (
    <div className="relative flex h-full flex-col overflow-hidden">
      {/* Header */}
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/")}
          aria-label={t("channel.back")}
          className="size-8 p-0 lg:hidden"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <div
          className={cn(
            "flex size-8 items-center justify-center rounded-lg",
            isDm || isAgentDm
              ? "bg-accent/10 text-accent"
              : "bg-control-bg text-control"
          )}
        >
          {isDm || isAgentDm ? (
            <Bot className="size-4" />
          ) : (
            <Hash className="size-4" />
          )}
        </div>
        <div className="min-w-0 flex-1 flex items-center gap-3">
          <h2 className="text-sm font-semibold text-main truncate">
            {channel?.title ?? channelId ?? ""}
          </h2>
          <AgentStatusBar activities={activities} />
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={handleToggleTasksPanel}
          aria-pressed={tasksPanelOpen}
          className="flex items-center gap-1.5 px-2.5 py-1.5"
        >
          <ListTodo className="size-4" />
          <span className="hidden sm:inline">
            {t("channelTask.panel-toggle")}
          </span>
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setMembersOpen(true);
            if (channelId) listChannelMembers(channelId);
          }}
          className="flex items-center gap-1.5 px-2.5 py-1.5"
        >
          <Users className="size-4" />
          <span className="hidden sm:inline">{members.length}</span>
        </Button>
        {props?.onViewInChannel && (
          <Button
            variant="ghost"
            size="sm"
            onClick={props.onViewInChannel}
            className="flex items-center gap-1.5 px-2.5 py-1.5"
            title={t("activity.view-in-channel")}
          >
            <ExternalLink className="size-4" />
            <span className="hidden sm:inline">
              {t("activity.view-in-channel")}
            </span>
          </Button>
        )}
      </div>

      <div className="flex flex-1 min-h-0">
        <div className="relative flex flex-1 flex-col min-w-0">
          {/* Messages scroll area */}
          <div
            ref={scrollRef}
            onScroll={handleScroll}
            className="flex-1 overflow-y-auto"
          >
            <div className="mx-auto flex max-w-3xl flex-col gap-4 px-6 pt-6 pb-4">
              {loading && <LoadingState />}
              {!loading && messages.length === 0 && (
                <EmptyState icon={Send} message={t("chat.empty")} />
              )}
              {messages.map((msg, idx) => {
                const prevMsg = idx > 0 ? messages[idx - 1] : null;
                const showAvatar =
                  !prevMsg ||
                  prevMsg.role !== msg.role ||
                  prevMsg.senderName !== msg.senderName;
                const rowProps = rowStreamingProps(
                  msg,
                  false,
                  "",
                  EMPTY_EVENTS
                );
                return (
                  <div key={msg.id} data-msg-id={msg.id}>
                    <MessageRow
                      msg={msg}
                      showAvatar={showAvatar}
                      agentTitle={msg.senderName ?? ""}
                      streamingContent={rowProps.streamingContent}
                      streamingEvents={rowProps.streamingEvents}
                      onViewDetails={handleViewDetails}
                      onMentionClick={handleMentionClick}
                      MentionBadge={MentionBadge}
                      markdownCustomId="channel-chat"
                      onOpenThread={handleOpenThread}
                      onPreviewAttachment={handlePreviewAttachment}
                      onJumpToSection={handleJumpToSection}
                      onPreviewImage={handlePreviewImage}
                      debugMode={currentUser?.debugMode ?? false}
                    />
                  </div>
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

          {/* Input area — hidden for agent-to-agent DMs (type 3) and for any
              read-only embedded view (e.g. the Activity detail pane). A user
              can read but cannot send or intervene. */}
          <div className="shrink-0 bg-background">
            {isAgentDm || props?.readOnly ? (
              <div className="mx-auto max-w-3xl px-6 py-4">
                <div className="rounded-2xl border border-control-border bg-control-bg/40 px-4 py-3 text-center text-xs text-control-placeholder">
                  {t("chat.agent-dm-view-only")}
                </div>
              </div>
            ) : (
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
                      {pendingAttachments.map((att) =>
                        isImageAttachment(att) ? (
                          <div key={att.id} className="group relative shrink-0">
                            <RemoteImage
                              attachment={att}
                              variant="thumb"
                              onClick={() => openImagePreview(att)}
                            />
                            <button
                              type="button"
                              onClick={() =>
                                setPendingAttachments((prev) =>
                                  prev.filter((p) => p.id !== att.id)
                                )
                              }
                              className="absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-control-border bg-background text-control-placeholder opacity-0 transition-opacity hover:text-error group-hover:opacity-100"
                              aria-label={t("common.delete")}
                            >
                              <X className="size-3" />
                            </button>
                          </div>
                        ) : (
                          <span
                            key={att.id}
                            className="group flex items-center gap-1.5 rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main"
                          >
                            <span className="max-w-[160px] truncate">
                              {att.name}
                            </span>
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
                        )
                      )}
                    </div>
                  )}
                  <Textarea
                    ref={textareaRef}
                    className={cn(
                      "block w-full resize-none border-0 bg-transparent px-4 py-3 text-sm text-main",
                      "placeholder:text-control-placeholder focus:ring-0 focus:border-transparent",
                      "max-h-[200px] min-h-[24px]"
                    )}
                    rows={1}
                    placeholder={t("channel.placeholder")}
                    aria-controls={
                      mentionState?.active ? MENTION_POPUP_ID : undefined
                    }
                    aria-activedescendant={
                      mentionState?.active && mentionState.matched.length > 0
                        ? `${MENTION_POPUP_ID}-opt-${mentionSelectedIndex}`
                        : undefined
                    }
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
                          const found = mentionTargets.find(
                            (t) => t.name === name
                          );
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
                      const state = detectMention(
                        target.value,
                        pos,
                        mentionTargets
                      );
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
                      <button
                        type="button"
                        onClick={() => setAsTask((v) => !v)}
                        aria-pressed={asTask}
                        disabled={sending}
                        className={cn(
                          "flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors disabled:opacity-50",
                          asTask
                            ? "bg-accent/15 text-accent"
                            : "text-control-placeholder hover:text-main hover:bg-control-bg"
                        )}
                        aria-label={t("channelTask.as-task")}
                        title={t("channelTask.as-task-hint")}
                      >
                        <ListTodo className="size-3.5" />
                        <span className="hidden sm:inline">
                          {t("channelTask.as-task")}
                        </span>
                      </button>
                      <span className="text-xs text-control-placeholder">
                        {t("chat.send-hint")}
                      </span>
                    </div>
                    <Button
                      type="button"
                      size="xs"
                      onClick={handleSend}
                      disabled={
                        (!input.trim() && pendingAttachments.length === 0) ||
                        sending
                      }
                    >
                      <Send className="size-3" />
                      {t("common.send")}
                    </Button>
                  </div>
                </div>
                {mentionState?.active && textareaRef.current && (
                  <MentionPopup
                    id={MENTION_POPUP_ID}
                    targets={mentionState.matched}
                    query={mentionState.query}
                    position={getCaretCoordinates(
                      textareaRef.current,
                      cursorPos
                    )}
                    selectedIndex={mentionSelectedIndex}
                    onSelect={handleMentionSelect}
                    onClose={() => setMentionState(null)}
                  />
                )}
              </div>
            )}
          </div>
        </div>
        {threadRootOpen && (
          <ThreadPanel
            channelId={channelId ?? ""}
            channelTitle={channel?.title ?? channelId ?? ""}
            rootMessageId={threadRootOpen}
            onClose={closeThread}
            onViewInChannel={handleViewInChannel}
            onPreviewAttachment={handlePreviewAttachment}
            onJumpToSection={handleJumpToSection}
            onPreviewImage={handlePreviewImage}
            readOnly={isAgentDm}
          />
        )}
        {tasksPanelOpen && channelId && (
          <TasksPanel
            channelId={channelId}
            channelTitle={channel?.title ?? channelId ?? ""}
            onClose={() => closeTasksPanel(channelId)}
            onOpenTask={handleOpenTaskThread}
          />
        )}
      </div>

      {/* Members Sheet */}
      <Sheet
        open={membersOpen}
        onOpenChange={(open) => !open && setMembersOpen(false)}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("channel.members", { count: members.length })}
            </SheetTitle>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-0">
            {membersLoading && <LoadingState />}
            {!membersLoading && (
              <div className="flex flex-col gap-2">
                {members.map((m) => (
                  <div
                    key={`${m.memberType}-${m.memberId}`}
                    className="flex items-center gap-3 rounded-xs border border-control-border bg-background p-3 transition-colors hover:bg-control-bg/60"
                  >
                    <div className="flex size-9 shrink-0 items-center justify-center rounded-full bg-accent/10 text-accent text-sm font-medium">
                      {(m.displayName || m.memberId).charAt(0).toUpperCase()}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-main truncate">
                        {m.displayName || m.memberId}
                      </p>
                      <div className="flex items-center gap-1.5 mt-0.5">
                        <span className="text-xs text-control-light">
                          {memberTypeLabel(t, m.memberType)}
                        </span>
                        {m.memberRole === 1 && (
                          <Badge variant="success" className="text-xs">
                            Owner
                          </Badge>
                        )}
                      </div>
                    </div>
                    {/* DMs have fixed membership (user + agent); only channel
                        owners can remove members. */}
                    {!membershipFixed && isOwner && m.memberRole !== 1 && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          handleRemoveMember(m.memberType, m.memberId)
                        }
                        aria-label={t("common.delete")}
                        className="size-7 p-0 text-control-placeholder hover:text-error hover:bg-error/10"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </div>
                ))}
              </div>
            )}

            {/* Add member section — channels only (both DM shapes are fixed
                1:1 rosters: user+agent and agent+agent). */}
            {!membershipFixed && isOwner && (
              <div className="mt-4 border-t border-control-border pt-5">
                {addMemberOpen ? (
                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-1.5">
                      <span className="text-xs font-semibold uppercase tracking-wide text-control">
                        {t("channel.member-type-label")}
                      </span>
                      <div className="flex gap-2">
                        <Button
                          variant={addMemberType === 1 ? "default" : "outline"}
                          size="sm"
                          onClick={() => setAddMemberType(1)}
                          className="flex-1"
                        >
                          {t("channel.member-type-user")}
                        </Button>
                        <Button
                          variant={addMemberType === 2 ? "default" : "outline"}
                          size="sm"
                          onClick={() => setAddMemberType(2)}
                          className="flex-1"
                        >
                          {t("channel.member-type-agent")}
                        </Button>
                      </div>
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <span className="text-xs font-semibold uppercase tracking-wide text-control">
                        {t("channel.member-id-label")}
                      </span>
                      <div className="flex gap-2">
                        <MemberPicker
                          key={addMemberType}
                          memberType={addMemberType}
                          existingMemberIds={existingMemberIds}
                          value={addMemberId}
                          onPick={setAddMemberId}
                          placeholder={t("channel.member-id-placeholder")}
                        />
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setAddMemberOpen(false);
                            setAddMemberId("");
                          }}
                          className="size-7 p-0"
                        >
                          <X className="size-3.5" />
                        </Button>
                      </div>
                    </div>
                    <Button
                      onClick={handleAddMember}
                      disabled={!addMemberId.trim() || addingMember}
                      className="w-full"
                    >
                      {addingMember
                        ? t("common.creating")
                        : t("channel.add-member")}
                    </Button>
                  </div>
                ) : (
                  <Button
                    variant="ghost"
                    onClick={() => setAddMemberOpen(true)}
                    className="flex w-full items-center gap-1.5 rounded-md px-2.5 py-2 text-sm"
                  >
                    <Plus className="size-4" />
                    {t("channel.add-member")}
                  </Button>
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

export function ChatEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
      <div className="flex size-14 items-center justify-center rounded-full bg-control-bg text-control-light">
        <Hash className="size-6" />
      </div>
      <p className="text-control-light text-sm max-w-xs">
        {t("chat.select-conversation")}
      </p>
    </div>
  );
}

// ChannelConversationView is the reusable form of this page, embedded read-only
// in the Activity detail pane for top-level channel/DM activity items. It is the
// same component with optional props (conversationId/readOnly/
// scrollToMessageId/onViewInChannel) that default to the route-driven behavior,
// so the chat route itself is unchanged.
export const ChannelConversationView = ChatConversationPage;
