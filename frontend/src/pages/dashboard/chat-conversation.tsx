import { create } from "@bufbuild/protobuf";
import {
  ArrowDown,
  Bot,
  ExternalLink,
  Hash,
  ListTodo,
  Loader2,
  Paperclip,
  Send,
  User,
  Users,
  X,
} from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { AgentStatusBar } from "@/components/agent-status-bar";
import { ChannelMembersPanel } from "@/components/chat/channel-members-panel";
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
  useMentionLabelResolver,
  useMentionTargets,
} from "@/composables/useMentionTargets";
import { commandServiceClient } from "@/connect";
import { getCaretCoordinates } from "@/lib/caret-position";
import { isImageAttachment } from "@/lib/image-file";
import "@/lib/markdown";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { senderKeyForMessage } from "@/stores/chat-helpers";
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
// 2 = channel, 3 = AGENT_DM (agent+agent, owned by the system bot),
// 4 = USER_DM (user+user, owned by the initiator).
const CONVERSATION_TYPE_DM = 1;
const CONVERSATION_TYPE_AGENT_DM = 3;
const CONVERSATION_TYPE_USER_DM = 4;

// ChannelConversationViewProps lets this page be reused embedded in the
// Activity detail pane, in addition to its primary use as the chat route. It
// is always writable there (the user replies inline, mirroring task/reminder);
// only an agent-to-agent DM (type 3) shows the admin view-only banner, gated
// inside on the conversation type. When `conversationId` is omitted the route
// param is used; `scrollToMessageId` scrolls the list to a specific message
// once loaded (a top-level channel mention); `scrollToReadVersion` instead
// scrolls to the first message whose room_version exceeds the given read
// cursor (the user's last-read position), used for a DM; `onViewInChannel`
// renders a header "View in channel" affordance. All props are optional and
// default to the route-driven behavior, so the existing chat page is unchanged.
export interface ChannelConversationViewProps {
  conversationId?: string;
  scrollToMessageId?: string;
  scrollToReadVersion?: bigint;
  onViewInChannel?: () => void;
  onClose?: () => void;
}

interface MessageListProps {
  messages: ChatMessageUI[];
  onViewDetails: (commandId: string, agentId: string) => void;
  onMentionClick: (type: string, id: string, name: string) => void;
  mentionLabel: (handle: string) => string | undefined;
  onOpenThread: (msg: ChatMessageUI) => void;
  onPreviewAttachment: (attachment: Attachment, rootMessageId: string) => void;
  onJumpToSection: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  onPreviewImage: (attachment: Attachment) => void;
  debugMode: boolean;
  currentPrincipalId?: string;
  scrollRoot: React.RefObject<HTMLDivElement | null>;
}

// MessageList is memoized so typing in the composer (which re-renders the
// header + input state of the page) does not rebuild the whole message list on
// every keystroke. Its props are either stable store refs/callbacks or
// primitives, so it bails out unless a message actually changed — MessageRow's
// own memo then skips rows whose msg object is untouched.
const MessageList = memo(function MessageList({
  messages,
  onViewDetails,
  onMentionClick,
  mentionLabel,
  onOpenThread,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  debugMode,
  currentPrincipalId,
  scrollRoot,
}: MessageListProps) {
  return (
    <div className="flex flex-col gap-4 px-6 pt-6 pb-4">
      {messages.map((msg, idx) => {
        const prevMsg = idx > 0 ? messages[idx - 1] : null;
        const showAvatar =
          !prevMsg || senderKeyForMessage(prevMsg) !== senderKeyForMessage(msg);
        const rowProps = rowStreamingProps(msg, false, "", EMPTY_EVENTS);
        return (
          <div key={msg.id} data-msg-id={msg.id}>
            <MessageRow
              msg={msg}
              showAvatar={showAvatar}
              agentTitle={msg.senderName ?? ""}
              streamingContent={rowProps.streamingContent}
              streamingEvents={rowProps.streamingEvents}
              onViewDetails={onViewDetails}
              onMentionClick={onMentionClick}
              mentionLabel={mentionLabel}
              MentionBadge={MentionBadge}
              markdownCustomId="channel-chat"
              onOpenThread={onOpenThread}
              onPreviewAttachment={onPreviewAttachment}
              onJumpToSection={onJumpToSection}
              onPreviewImage={onPreviewImage}
              debugMode={debugMode}
              currentPrincipalId={currentPrincipalId}
              scrollRoot={scrollRoot}
              // For small/medium chats, render markdown synchronously on first
              // paint so entering the conversation doesn't flash as each visible
              // row swaps its inline raw-text placeholder for block markdown a
              // frame later. Large histories keep the lazy gate so off-screen
              // rows stay cheap to mount.
              eager={messages.length <= 40}
            />
          </div>
        );
      })}
    </div>
  );
});

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
  const markConversationRead = useAppStore((s) => s.markConversationRead);
  const currentUser = useAppStore((s) => s.currentUser);
  // Per-user chat keybinding: Enter sends (default) or, when the user has
  // inverted it in Settings, Shift+Enter sends. Reactive so a settings change
  // takes effect on the next render without a reload.
  const enterToSend = currentUser?.chatPreferences?.enterToSend ?? true;
  const fetchAgents = useAppStore((s) => s.fetchAgents);
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
  const isDesktop = useIsDesktop();
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
  // When true the thread panel fills the whole chat area and the channel's own
  // message pane is hidden (see the ThreadPanel expand toggle).
  const [threadExpanded, setThreadExpanded] = useState(false);

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

  // True only while the open conversation is present in the user's left-rail
  // list. Derived as a boolean (not the array) so the metadata effect below
  // re-runs on a membership change but not on every fetchChannels poll that
  // replaces the array with equivalent content — that would otherwise re-fire
  // GetChannel every 5s for conversations outside the list (e.g. agent-DMs).
  const channelInList = channels.some((c) => c.name === conversationName);

  const channel =
    channels.find((c) => c.name === conversationName) ??
    fetchedChannel ??
    undefined;
  const isDm = channel?.type === CONVERSATION_TYPE_DM;
  // Agent-to-agent DMs (type 3) are admin view-only: a user cannot send or
  // alter membership there. User-to-user DMs (type 4) are writable by both
  // users but their roster is fixed at creation. membershipFixed covers all
  // three DM shapes (user+agent, agent+agent, user+user).
  const isAgentDm = channel?.type === CONVERSATION_TYPE_AGENT_DM;
  const isUserDm = channel?.type === CONVERSATION_TYPE_USER_DM;
  const membershipFixed = isDm || isAgentDm || isUserDm;
  const isOwner =
    channel && currentUser ? channel.ownerId === currentUser.handle : false;

  // Fetch conversation metadata when the open conversation is absent from the
  // user's left-rail `channels` (notably agent-DMs, which ListChannels excludes
  // by membership). GetChannel's admin bypass lets an admin read it; a non-admin
  // is denied and the fetch fails silently (they cannot view the DM at all).
  useEffect(() => {
    if (!channelId || !conversationName) return;
    if (channelInList) {
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
  }, [channelId, conversationName, channelInList]);

  // The thread panel is open only when it belongs to the currently-viewed
  // channel; switching channels closes it (see init()).
  const threadRootOpen =
    activeThreadConversation === conversationName ? activeThreadRoot : null;

  const mentionTargets = useMentionTargets(channelId);
  const mentionLabel = useMentionLabelResolver(channelId);

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
    setThreadExpanded(false);
    lastChannelRef.current = channelId;
    stickToBottomRef.current = true;
    try {
      await loadMessages(conversationName);
    } catch {
      // load failed
    }
    listChannelMembers(channelId);
    // Load the agent roster once per session (thread titles / the add-member
    // picker rely on it) without clobbering the drained roster on every channel
    // switch — fetchAgents replaces the slice with its one page.
    if (useAppStore.getState().agents.length === 0) {
      fetchAgents({ pageSize: 100 });
    }
    // fetchChannels is intentionally omitted here: ChatLayout owns the 5s
    // left-rail poll, and markConversationRead below clears this
    // conversation's badge locally, so an extra fetch would triple up.
    markConversationRead(channelId);

    // Start background polling for new messages and agent activity.
    startWatchingChannel(conversationName);
  }, [
    channelId,
    conversationName,
    loadMessages,
    listChannelMembers,
    fetchAgents,
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

  // When embedded with a scrollToReadVersion (the Activity detail pane for a
  // DM, pointing at the user's last-read position), scroll the list to the
  // first message whose room_version exceeds the read cursor — the first
  // unread message, where the user resumes reading. If every loaded message
  // is already read (cursor caught up), stick to the bottom instead. Runs at
  // most once per version so it does not fight the user's own scrolling.
  const scrollToReadVersion = props?.scrollToReadVersion ?? 0n;
  const scrollToReadVersionRef = useRef<bigint>(0n);
  useEffect(() => {
    if (scrollToReadVersion <= 0n || messages.length === 0) return;
    if (scrollToReadVersionRef.current === scrollToReadVersion) return;
    scrollToReadVersionRef.current = scrollToReadVersion;
    // Find the first message strictly past the read cursor. Messages are
    // chronological (oldest first), so the first match is the resume point.
    const target = messages.find(
      (m) => (m.roomVersion ?? 0n) > scrollToReadVersion
    );
    // Defer so the layout has settled after the messages update.
    requestAnimationFrame(() => {
      if (target) {
        // Park at the resume point and release stick-to-bottom so the
        // background poller does not yank the user back down while they read
        // forward from their last-read position.
        stickToBottomRef.current = false;
        scrollRef.current
          ?.querySelector(`[data-msg-id="${target.id}"]`)
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      } else if (scrollRef.current) {
        // Caught up — land at the bottom.
        stickToBottomRef.current = true;
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      }
    });
  }, [scrollToReadVersion, messages.length]);

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
      // The textarea is disabled while sending, which drops focus; restore it
      // after the send settles so the user can keep typing.
      setTimeout(() => textareaRef.current?.focus(), 0);
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
      const newInput = `${before}@${target.handle} ${after}`;
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
          const newPos = mentionState.startIndex + target.handle.length + 2;
          el.focus();
          el.setSelectionRange(newPos, newPos);
        }
      }, 0);
    },
    [input, cursorPos, mentionState]
  );

  // Channel rows are never in DM-style streaming mode (channel messages are
  // polled, not streamed token-by-token), so every row receives stable empty
  // streaming slices. The shared MessageRow still accepts them.
  const handleViewDetails = useCallback(
    (commandId: string, agentId: string) => {
      navigate(`/members/agents/${agentId}/commands/${commandId}`);
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
  // then the overlay scrolls to the anchor (heading id for markdown, quote +
  // locate spec for html) once the DOM is ready.
  const handleJumpToSection = useCallback(
    (att: Attachment, sectionId: string, rootMessageId: string) => {
      if (!channelId) return;
      openFilePreview(
        conversationName,
        rootMessageId,
        att,
        sectionId,
        att.quotedText
      );
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

  return (
    <div className="relative flex h-full flex-col overflow-hidden">
      {/* Header */}
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-3">
        <div
          className={cn(
            "flex size-8 items-center justify-center rounded-lg",
            isDm || isAgentDm || isUserDm
              ? "bg-accent/10 text-accent"
              : "bg-control-bg text-control"
          )}
        >
          {isUserDm ? (
            <User className="size-4" />
          ) : isDm || isAgentDm ? (
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
          onClick={() => setMembersOpen(true)}
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
        <div
          className={cn(
            "relative flex flex-1 flex-col min-w-0",
            // Expanded thread replaces the channel pane entirely.
            threadRootOpen && threadExpanded && "hidden"
          )}
        >
          {/* Messages scroll area */}
          <div
            ref={scrollRef}
            onScroll={handleScroll}
            className="flex-1 overflow-y-auto"
          >
            {/* LoadingState only when there's genuinely nothing to show yet.
                On a revisit cached messages are already in the store, so a
                background refetch (which flips chatLoading true) must NOT hide
                them behind a spinner — that was the per-revisit flash. */}
            {loading && messages.length === 0 && <LoadingState />}
            {!loading && messages.length === 0 && (
              <EmptyState icon={Send} message={t("chat.empty")} />
            )}
            <MessageList
              messages={messages}
              mentionLabel={mentionLabel}
              onViewDetails={handleViewDetails}
              onMentionClick={handleMentionClick}
              onOpenThread={handleOpenThread}
              onPreviewAttachment={handlePreviewAttachment}
              onJumpToSection={handleJumpToSection}
              onPreviewImage={handlePreviewImage}
              debugMode={currentUser?.debugMode ?? false}
              currentPrincipalId={currentUser?.handle}
              scrollRoot={scrollRef}
            />
          </div>

          {/* Scroll to bottom button — hidden while the tasks panel is open so
              it doesn't float over the task board or its close affordance. */}
          {showScrollDown && !tasksPanelOpen && (
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

          {/* Input area — hidden for agent-to-agent DMs (type 3: an admin can
              view but cannot intervene) and when the tasks panel is open on
              mobile (the panel itself is the focus, not composing). Desktop
              keeps the composer visible. Every other embedded view is writable. */}
          {(!tasksPanelOpen || isDesktop) && (
            <div className="shrink-0 bg-background">
              {isAgentDm ? (
                <div className="px-6 py-4">
                  <div className="rounded-2xl border border-control-border bg-control-bg/40 px-4 py-3 text-center text-xs text-control-placeholder">
                    {t("chat.agent-dm-view-only")}
                  </div>
                </div>
              ) : (
                <div className="px-4 pb-2 pt-2 lg:px-6 lg:pb-5">
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
                            <div
                              key={att.id}
                              className="group relative shrink-0"
                            >
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
                              (t) => t.handle === name
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
                        if (e.nativeEvent.isComposing) return;
                        if (e.key !== "Enter") return;
                        const wantSend = enterToSend ? !e.shiftKey : e.shiftKey;
                        if (wantSend) {
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
                            "flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors disabled:opacity-50 lg:ml-0",
                            "ml-2",
                            asTask
                              ? "bg-accent/15 text-accent"
                              : "text-control-placeholder hover:text-main hover:bg-control-bg"
                          )}
                          aria-label={t("channelTask.as-task")}
                          title={t("channelTask.as-task-hint")}
                        >
                          <ListTodo className="size-3.5" />
                          <span className="sm:hidden lg:inline">
                            {t("channelTask.as-task")}
                          </span>
                        </button>
                        {isDesktop && (
                          <span className="text-xs text-control-placeholder">
                            {t(
                              enterToSend
                                ? "chat.send-hint"
                                : "chat.send-hint-inverted"
                            )}
                          </span>
                        )}
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
          )}
        </div>
        {threadRootOpen && (
          <ThreadPanel
            channelId={channelId ?? ""}
            channelTitle={channel?.title ?? channelId ?? ""}
            rootMessageId={threadRootOpen}
            onClose={closeThread}
            onPreviewAttachment={handlePreviewAttachment}
            onJumpToSection={handleJumpToSection}
            onPreviewImage={handlePreviewImage}
            readOnly={isAgentDm}
            expanded={threadExpanded}
            onToggleExpand={() => setThreadExpanded((v) => !v)}
            fluid={threadExpanded}
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
            {channelId && (
              <ChannelMembersPanel
                conversationId={channelId}
                canManage={isOwner}
                membershipFixed={membershipFixed}
              />
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

// ChannelConversationView is the reusable form of this page, embedded in the
// Activity detail pane for top-level channel/DM activity items (writable, like
// task/reminder). It is the same component with optional props
// (conversationId/scrollToMessageId/scrollToReadVersion/onViewInChannel) that
// default to the route-driven behavior, so the chat route itself is unchanged.
export const ChannelConversationView = ChatConversationPage;
