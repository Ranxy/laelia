import {
  ArrowDown,
  Bot,
  ExternalLink,
  FolderOpen,
  Hash,
  ListTodo,
  Loader2,
  Search,
  Send,
  User,
  Users,
} from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { AgentStatusBar } from "@/components/agent-status-bar";
import { AgentBadge } from "@/components/chat/agent-badge";
import { Avatar } from "@/components/chat/avatar";
import { ChannelFilesPanel } from "@/components/chat/channel-files-panel";
import { ChannelMembersPanel } from "@/components/chat/channel-members-panel";
import { ChannelSearchPanel } from "@/components/chat/channel-search-panel";
import { ChatComposer } from "@/components/chat/chat-composer";
import { ChatDrawerSheet } from "@/components/chat/chat-drawer-sheet";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MentionDetailSheet } from "@/components/chat/mention-detail-sheet";
import { MessageRow } from "@/components/chat/message-row";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { TasksPanel } from "@/components/chat/tasks-panel";
import { ThreadPanel } from "@/components/chat/thread-panel";
import { Button } from "@/components/ui/button";
import { SheetBody, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { useChannel } from "@/hooks/use-channel";
import {
  type ComposerDraft,
  type ComposerDraftsRef,
} from "@/hooks/use-chat-composer";
import {
  useMentionLabelResolver,
  useMentionTargets,
} from "@/hooks/use-mention-targets";
import { useAvatar } from "@/lib/avatar-cache";
import "@/lib/markdown";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { useMessageScroller } from "@/hooks/use-message-scroller";
import { useOnlineUsers } from "@/hooks/use-presence-heartbeat";
import { useWindowedMessageRange } from "@/hooks/use-windowed-message-range";
import { peerPresenceOnline } from "@/lib/presence";
import { toastManager } from "@/lib/toast";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { senderKeyForMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/ui-models";
import type {
  AgentActivity,
  Attachment,
  ChannelMember,
  ChatMessage,
  ConversationFile,
} from "@/types/proto-es/v1/command_pb";

// Stable empty fallbacks so per-key selectors returning undefined for an
// unloaded conversation don't mint a new array each run (which would defeat
// zustand's Object.is equality and re-render on every store change).
const EMPTY_MESSAGES: ChatMessageUI[] = [];
const EMPTY_MEMBERS: ChannelMember[] = [];
const EMPTY_ACTIVITIES: AgentActivity[] = [];

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
}

interface MessageListProps {
  messages: ChatMessageUI[];
  onViewDetails: (commandId: string, agentId: string) => void;
  onMentionClick: (type: string, id: string, name: string) => void;
  mentionLabel: (handle: string) => string | undefined;
  onOpenThread: (msg: ChatMessageUI) => void;
  // Opens the thread drawer scrolled to a specific previewed reply (the inline
  // thread preview's per-reply rows).
  onOpenThreadAt: (rootMsg: ChatMessageUI, reply: ChatMessageUI) => void;
  onCopyMarkdown: (content: string) => void;
  onConvertToTask: (msg: ChatMessageUI) => void;
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
  onToggleReaction: (msg: ChatMessageUI, emoji: string) => void;
  /** Keep every row mounted (jump loading / focused jump window / small chat). */
  forceFullRender?: boolean;
}

// MessageList is memoized so typing in the composer (which re-renders the
// header + input state of the page) does not rebuild the whole message list on
// every keystroke. Its props are either stable store refs/callbacks or
// primitives, so it bails out unless a message actually changed — MessageRow's
// own memo then skips rows whose msg object is untouched.
// Below this row count the light window is bypassed entirely: everything
// mounts, matching the synchronous-markdown eager tier.
const WINDOW_MIN_ROWS = 60;
const MessageList = memo(function MessageList({
  messages,
  onViewDetails,
  onMentionClick,
  onOpenThread,
  onOpenThreadAt,
  onCopyMarkdown,
  onConvertToTask,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  debugMode,
  currentPrincipalId,
  scrollRoot,
  onToggleReaction,
  forceFullRender,
}: MessageListProps) {
  // ADR-3 step ③ light windowing: far rows degrade to height-memory
  // placeholders once the list is large enough to matter; small chats and
  // jsdom (zero-height container) render everything. A jump (loading or a
  // focused jump window) keeps the full tree mounted so the scroller's
  // target queries resolve against real rows, not placeholders.
  const rowIds = messages.map((msg) => msg.id);
  const { start, end, rowRef, placeholderHeight } = useWindowedMessageRange({
    containerRef: scrollRoot,
    rowIds,
    forceFullRender,
  });
  return (
    <div className="flex flex-col gap-4 px-6 pt-6 pb-4">
      {messages.map((msg, idx) => {
        if (idx < start || idx >= end) {
          return (
            <div
              key={msg.id}
              data-msg-id={msg.id}
              style={{ height: placeholderHeight(msg.id) }}
              aria-hidden="true"
            />
          );
        }
        const prevMsg = idx > 0 ? messages[idx - 1] : null;
        const showAvatar =
          !prevMsg || senderKeyForMessage(prevMsg) !== senderKeyForMessage(msg);
        return (
          <div key={msg.id} data-msg-id={msg.id} ref={rowRef(msg.id)}>
            <MessageRow
              msg={msg}
              showAvatar={showAvatar}
              agentTitle={msg.senderName ?? ""}
              onViewDetails={onViewDetails}
              onMentionClick={onMentionClick}
              onSenderClick={onMentionClick}
              MentionBadge={MentionBadge}
              markdownCustomId="channel-chat"
              onOpenThread={onOpenThread}
              onOpenThreadAt={onOpenThreadAt}
              onCopyMarkdown={onCopyMarkdown}
              onConvertToTask={onConvertToTask}
              onPreviewAttachment={onPreviewAttachment}
              onJumpToSection={onJumpToSection}
              onPreviewImage={onPreviewImage}
              debugMode={debugMode}
              currentPrincipalId={currentPrincipalId}
              scrollRoot={scrollRoot}
              onToggleReaction={onToggleReaction}
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
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const startWatchingChannel = useAppStore((s) => s.startWatchingChannel);
  const stopWatchingChannel = useAppStore((s) => s.stopWatchingChannel);
  const markConversationRead = useAppStore((s) => s.markConversationRead);
  const toggleReaction = useAppStore((s) => s.toggleReaction);
  const currentUser = useAppStore((s) => s.currentUser);
  // Per-user chat keybinding: Enter sends (default) or, when the user has
  // inverted it in Settings, Shift+Enter sends. Reactive so a settings change
  // takes effect on the next render without a reload.
  const enterToSend = currentUser?.chatPreferences?.enterToSend ?? true;
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  // DM-peer presence inputs for the header badge, same sources as the left
  // rail: agents from the roster's connection state, humans from the presence
  // heartbeat slice. Both are refreshed by ChatLayout's 30s tick.
  const agents = useAppStore((s) => s.agents);
  const onlineUsers = useOnlineUsers();
  const openThread = useAppStore((s) => s.openThread);
  const closeThread = useAppStore((s) => s.closeThread);
  const activeThreadRoot = useAppStore((s) => s.activeThreadRoot);
  const activeThreadConversation = useAppStore(
    (s) => s.activeThreadConversation
  );
  const toggleTasksPanel = useAppStore((s) => s.toggleTasksPanel);
  const closeTasksPanel = useAppStore((s) => s.closeTasksPanel);
  const convertMessageToTask = useAppStore((s) => s.convertMessageToTask);
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
  // Channel member breakdown shown in the header after the title: humans are
  // memberType 1, agents are memberType 2. Zero-count groups are omitted so a
  // channel without agents doesn't render "0 agents".
  const humanMemberCount = members.filter((m) => m.memberType === 1).length;
  const agentMemberCount = members.filter((m) => m.memberType === 2).length;
  const memberSummary =
    humanMemberCount > 0 && agentMemberCount > 0
      ? t("channel.members-summary", {
          humans: humanMemberCount,
          agents: agentMemberCount,
        })
      : humanMemberCount > 0
        ? t("channel.members-summary-humans", {
            humans: humanMemberCount,
          })
        : agentMemberCount > 0
          ? t("channel.members-summary-agents", {
              agents: agentMemberCount,
            })
          : null;
  const activities =
    useAppStore((s) => s.agentActivities[conversationName]) ?? EMPTY_ACTIVITIES;
  const jumpTarget = useAppStore(
    (s) => s.chatJumpByConv[conversationName] ?? null
  );
  const jumpLoading = useAppStore(
    (s) => s.chatJumpLoading[conversationName] ?? false
  );
  const hasOlder = useAppStore(
    (s) => s.chatHasOlderByConv[conversationName] ?? false
  );
  const hasNewer = useAppStore(
    (s) => s.chatHasNewerByConv[conversationName] ?? false
  );
  const jumpToMessage = useAppStore((s) => s.jumpToMessage);
  const loadOlderMessages = useAppStore((s) => s.loadOlderMessages);
  const loadNewerMessages = useAppStore((s) => s.loadNewerMessages);
  const clearJump = useAppStore((s) => s.clearJump);

  const lastChannelRef = useRef<string | null>(null);

  // Embedded deep-scroll inputs (Activity detail pane), consumed by the
  // scroller hook below.
  const scrollToMessageId = props?.scrollToMessageId ?? "";
  const scrollToReadVersion = props?.scrollToReadVersion ?? 0n;

  // Chat scroll state machine (new-message follow, history-load anchoring,
  // overflow-anchor handling, jump windows): see lib/use-message-scroller.ts.
  // Fresh reads at scroll time — the handler must not page off a stale
  // render-captured view of the store.
  const hasOlderNow = useCallback(
    () => useAppStore.getState().chatHasOlderByConv[conversationName] ?? false,
    [conversationName]
  );
  const hasNewerNow = useCallback(
    () => useAppStore.getState().chatHasNewerByConv[conversationName] ?? false,
    [conversationName]
  );
  const isJumpLoadingNow = useCallback(
    () => useAppStore.getState().chatJumpLoading[conversationName] ?? false,
    [conversationName]
  );
  const scroller = useMessageScroller({
    conversationName,
    messages,
    loadOlderMessages,
    loadNewerMessages,
    hasOlderMessages: hasOlderNow,
    hasNewerMessages: hasNewerNow,
    isJumpLoading: isJumpLoadingNow,
    jumpTarget,
    jumpLoading,
    clearJump,
    scrollToMessageId,
    scrollToReadVersion,
  });
  const {
    scrollRef,
    handleScroll,
    showScrollDown,
    scrollToBottom,
    beginJumpWindow,
    releaseHistorySuppression,
    resetScrollState,
  } = scroller;
  const isDesktop = useIsDesktop();
  // Per-conversation input draft cache (half-typed text + completed
  // attachments) owned here and consumed by ChatComposer keyed per channelId,
  // so switching channels does not leak the half-typed message across
  // channels. In-flight uploads are not part of the draft — they belong to
  // the composer instance that started them (see useChatComposer).
  const draftsRef: ComposerDraftsRef = useRef(new Map<string, ComposerDraft>());

  const [membersOpen, setMembersOpen] = useState(false);
  const [filesOpen, setFilesOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  // When a file lives in a thread reply, we open the thread and scroll to the
  // exact reply via ThreadPanel's scrollToMessageId.
  const [threadScrollToMessageId, setThreadScrollToMessageId] = useState<
    string | null
  >(null);
  // When true the thread panel fills the whole chat area and the channel's own
  // message pane is hidden (see the ThreadPanel expand toggle).
  const [threadExpanded, setThreadExpanded] = useState(false);

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
  // Shared Query cache with the channel-detail/activity reads (use-channel);
  // the enabled gate reproduces the old roster-skip: while the conversation is
  // present in the left rail the fetched fallback is null.

  // True only while the open conversation is present in the user's left-rail
  // list. Derived as a boolean (not the array) so the metadata read below
  // re-runs on a membership change but not on every fetchChannels poll that
  // replaces the array with equivalent content — that would otherwise re-fire
  // GetChannel every 5s for conversations outside the list (e.g. agent-DMs).
  const channelInList = channels.some((c) => c.name === conversationName);

  const { channel: fetchedChannel } = useChannel(conversationName, {
    enabled: !!channelId && !channelInList,
  });

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
  // An archived channel (conversation.archived, set by the owner) is read-only:
  // the composer is replaced with a notice and thread replies are disabled.
  const isArchived = channel?.archived === true;
  const membershipFixed = isDm || isAgentDm || isUserDm;
  const isOwner =
    channel && currentUser ? channel.ownerId === currentUser.handle : false;

  // DM header avatar: replace the generic Bot/User glyph with the peer's real
  // avatar whenever the peer is resolvable (Conversation.peer), plus the same
  // green presence badge as the left-rail rows. Channels keep the Hash icon.
  const peer = channel?.peer;
  const peerAvatarName = peer ? `${peer}/avatar` : undefined;
  const peerAvatarSrc = useAvatar(peerAvatarName);
  const peerId = peer ? (peer.split("/").pop() ?? "") : "";
  const peerOnline = peerPresenceOnline(
    peer,
    isDm || isAgentDm,
    agents,
    onlineUsers
  );

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
    resetScrollState();
    lastChannelRef.current = channelId;
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
    resetScrollState,
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

  // Deep-links from outside the channel (search results, reminders, activity):
  //   ?thread=<rootId>              open the thread and scroll to its root
  //   ?message=<id>&version=<v>     jump to a main-channel message
  //   ?thread=<rootId>&message=<id> open the thread and scroll to the reply
  // Runs at most once per target so it doesn't fight the user's own navigation.
  const [searchParams, setSearchParams] = useSearchParams();
  const threadDeepLinkId = searchParams.get("thread") ?? "";
  const messageDeepLinkId = searchParams.get("message") ?? "";
  const messageDeepLinkVersion = searchParams.get("version") ?? "";
  const deepLinkRef = useRef<string>("");
  // biome-ignore lint/correctness/useExhaustiveDependencies: deep-link scroll targets are read through refs; keyed on the link ids and load state.
  useEffect(() => {
    if (!channelId) return;
    const target = messageDeepLinkId || threadDeepLinkId;
    if (!target) return;
    if (deepLinkRef.current === target) return;
    if (messages.length === 0) return; // wait for the channel to load
    deepLinkRef.current = target;

    if (messageDeepLinkId && threadDeepLinkId) {
      // A thread reply: open the thread and let ThreadPanel scroll to the
      // exact reply, while the main list centers the root message.
      setThreadScrollToMessageId(messageDeepLinkId);
      openThread(`conversations/${channelId}`, threadDeepLinkId);
      requestAnimationFrame(() => {
        scrollRef.current
          ?.querySelector(`[data-msg-id="${threadDeepLinkId}"]`)
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      });
    } else if (messageDeepLinkId) {
      const version = BigInt(messageDeepLinkVersion || "0");
      if (version > 0n) {
        beginJumpWindow(messageDeepLinkId);
        void jumpToMessage(
          `conversations/${channelId}`,
          messageDeepLinkId,
          version
        ).catch(() => {
          releaseHistorySuppression();
        });
      }
    } else {
      openThread(`conversations/${channelId}`, threadDeepLinkId);
      requestAnimationFrame(() => {
        scrollRef.current
          ?.querySelector(`[data-msg-id="${threadDeepLinkId}"]`)
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      });
    }

    // Clean the params so a later in-page open of a different target doesn't
    // re-trigger this.
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("thread");
      next.delete("message");
      next.delete("version");
      return next;
    });
  }, [
    channelId,
    threadDeepLinkId,
    messageDeepLinkId,
    messageDeepLinkVersion,
    messages.length,
    openThread,
    jumpToMessage,
    setSearchParams,
    beginJumpWindow,
    releaseHistorySuppression,
  ]);

  // Keep the scroll handler's view of messages current without making the
  // handler depend on the messages array identity.
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

  // Toggle the caller's reaction on a message in the current conversation.
  // Stable across renders so MessageList's memo is not defeated.
  const handleToggleReaction = useCallback(
    (msg: ChatMessageUI, emoji: string) => {
      if (!conversationName) return;
      void toggleReaction(conversationName, msg.id, emoji);
    },
    [conversationName, toggleReaction]
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

  // Inline thread preview: clicking one of the latest replies opens the thread
  // drawer scrolled to that reply, mirroring the search-jump path. The main
  // list stays put (the preview row is already visible).
  const handleOpenThreadAt = useCallback(
    (rootMsg: ChatMessageUI, reply: ChatMessageUI) => {
      if (!channelId || rootMsg.threadRoot || !reply.threadRoot) return;
      if (channelId) closeTasksPanel(channelId);
      // The thread panel scrolls via data-msg-id which carries the full
      // resource name, same as the search jump.
      setThreadScrollToMessageId(reply.id);
      void openThread(conversationName, rootMsg.id);
    },
    [channelId, conversationName, openThread, closeTasksPanel]
  );

  // "Copy markdown" in the context menu: write the message's final raw markdown
  // to the clipboard and toast the outcome.
  const handleCopyMarkdown = useCallback(
    async (content: string) => {
      try {
        await navigator.clipboard.writeText(content);
        toastManager.add({
          type: "success",
          title: t("chat.copy-markdown-success"),
        });
      } catch {
        toastManager.add({
          type: "error",
          title: t("chat.copy-markdown-error"),
        });
      }
    },
    [t]
  );

  // "Convert to task" in the context menu: turn a root, non-task message into a
  // channel task. msg.id is the full resource name ("conversations/c/messages/m"),
  // so strip it to the bare message id the store action expects.
  const handleConvertToTask = useCallback(
    async (msg: ChatMessageUI) => {
      if (!channelId) return;
      const messageId = msg.id.split("/").pop() ?? msg.id;
      try {
        await convertMessageToTask(channelId, messageId);
        toastManager.add({
          type: "success",
          title: t("channelTask.convert-success"),
        });
      } catch {
        toastManager.add({
          type: "error",
          title: t("channelTask.convert-error"),
        });
      }
    },
    [channelId, convertMessageToTask, t]
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

  // Jump from the files drawer to the message where the file was attached.
  // Only a focused window around the target is loaded; older/newer pages are
  // fetched incrementally as the user scrolls.
  const handleJumpToMessage = useCallback(
    async (cf: ConversationFile) => {
      if (!channelId || !cf.messageId) return;
      setFilesOpen(false);
      // Files attached inside a thread reply live in the thread panel, not the
      // main channel list (which excludes replies). Open the thread and let
      // ThreadPanel scroll to the exact reply.
      if (cf.threadRoot) {
        setThreadScrollToMessageId(cf.messageId);
        void openThread(conversationName, cf.threadRoot);
        return;
      }
      if (!cf.roomVersion) return;
      // Entering a focused history view: beginJumpWindow releases
      // stick-to-bottom before the window loads so the auto-stick effect never
      // yanks the list to the bottom of the jump window, and drops any stale
      // scroll anchor/latch.
      beginJumpWindow(cf.messageId);
      try {
        await jumpToMessage(conversationName, cf.messageId, cf.roomVersion);
      } catch {
        // The jump failed, so the scroll effect will never run to release the
        // suppression; release it here so normal history paging still works.
        releaseHistorySuppression();
      }
    },
    [
      channelId,
      conversationName,
      jumpToMessage,
      openThread,
      beginJumpWindow,
      releaseHistorySuppression,
    ]
  );

  // Jump from the channel search panel to a matched message. Thread replies
  // open the thread and scroll to the reply; main-channel messages load a
  // focused window around the target, mirroring the files-drawer jump.
  const handleSearchJump = useCallback(
    (msg: ChatMessage) => {
      if (!channelId) return;
      setSearchOpen(false);
      if (msg.threadRoot) {
        setThreadScrollToMessageId(msg.name);
        void openThread(conversationName, msg.threadRoot);
        return;
      }
      if (!msg.roomVersion) return;
      beginJumpWindow(msg.name);
      void jumpToMessage(conversationName, msg.name, msg.roomVersion).catch(
        () => {
          releaseHistorySuppression();
        }
      );
    },
    [
      channelId,
      conversationName,
      jumpToMessage,
      openThread,
      beginJumpWindow,
      releaseHistorySuppression,
    ]
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
        {peer && (isDm || isAgentDm || isUserDm) ? (
          <Avatar
            src={peerAvatarSrc}
            seed={peerId || (channel?.title ?? "")}
            online={peerOnline}
            title={peerOnline ? t("chat.presence-online") : undefined}
          />
        ) : (
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
        )}
        <div className="min-w-0 flex-1 flex items-center gap-3">
          <h2 className="text-sm font-semibold text-main truncate">
            {channel?.title ?? channelId ?? ""}
          </h2>
          {/* Agent DMs (user↔agent and the admin-viewed agent↔agent DM) mark
              the title with the same AgentBadge as the left-rail rows. */}
          {(isDm || isAgentDm) && <AgentBadge />}
          {!isDm && !isAgentDm && !isUserDm && memberSummary && (
            <span className="shrink-0 text-xs text-control-placeholder">
              {memberSummary}
            </span>
          )}
          <AgentStatusBar activities={activities} />
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setSearchOpen(true)}
          aria-pressed={searchOpen}
          className="flex items-center gap-1.5 px-2.5 py-1.5"
        >
          <Search className="size-4" />
          <span className="hidden sm:inline">{t("channelSearch.title")}</span>
        </Button>
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
          onClick={() => setFilesOpen(true)}
          aria-pressed={filesOpen}
          className="flex items-center gap-1.5 px-2.5 py-1.5"
        >
          <FolderOpen className="size-4" />
          <span className="hidden sm:inline">{t("channelFiles.title")}</span>
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
          {/* Messages scroll area. Native CSS scroll anchoring is enabled
              except for the brief history-page transaction, where the manual
              anchor restore below owns the scroll position. Outside that
              window it keeps the currently-visible rows stationary when a lazy
              markdown row swaps its raw-text placeholder for the real (taller)
              markdown render. The sentinels below opt out of anchoring so the
              browser always picks a real message row as its anchor. */}
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
            {hasOlder && (
              <div
                className="flex h-6 items-center justify-center overflow-hidden whitespace-nowrap text-control-placeholder"
                style={{ overflowAnchor: "none" }}
              >
                {jumpLoading ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  t("channelFiles.load-older")
                )}
              </div>
            )}
            <MessageList
              messages={messages}
              mentionLabel={mentionLabel}
              onViewDetails={handleViewDetails}
              onMentionClick={handleMentionClick}
              onOpenThread={handleOpenThread}
              onOpenThreadAt={handleOpenThreadAt}
              onCopyMarkdown={handleCopyMarkdown}
              onConvertToTask={handleConvertToTask}
              onPreviewAttachment={handlePreviewAttachment}
              onJumpToSection={handleJumpToSection}
              onPreviewImage={handlePreviewImage}
              debugMode={currentUser?.debugMode ?? false}
              currentPrincipalId={currentUser?.handle}
              scrollRoot={scrollRef}
              onToggleReaction={handleToggleReaction}
              forceFullRender={
                Boolean(jumpTarget) ||
                jumpLoading ||
                messages.length <= WINDOW_MIN_ROWS
              }
            />
            {hasNewer && (
              <div
                className="flex h-6 items-center justify-center overflow-hidden whitespace-nowrap text-control-placeholder"
                style={{ overflowAnchor: "none" }}
              >
                {jumpLoading ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  t("channelFiles.load-newer")
                )}
              </div>
            )}
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
              ) : isArchived ? (
                <div className="px-6 py-4">
                  <div className="rounded-2xl border border-warning/30 bg-warning/5 px-4 py-3 text-center text-xs text-warning">
                    {t("chat.channel-archived")}
                  </div>
                </div>
              ) : channelId ? (
                <div className="px-4 pb-2 pt-2 lg:px-6 lg:pb-5">
                  <ChatComposer
                    key={channelId}
                    channelId={channelId}
                    draftKey={channelId}
                    draftsRef={draftsRef}
                    enterToSend={enterToSend}
                    mentionTargets={mentionTargets}
                    popupId="mention-popup"
                    placeholder={t("channel.placeholder")}
                    size="main"
                    taskEnabled
                  />
                </div>
              ) : null}
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
            readOnly={isAgentDm || isArchived}
            archived={isArchived}
            expanded={threadExpanded}
            onToggleExpand={() => setThreadExpanded((v) => !v)}
            fluid={threadExpanded}
            scrollToMessageId={threadScrollToMessageId ?? undefined}
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

      {/* Channel Search Sheet */}
      <ChatDrawerSheet open={searchOpen} onClose={() => setSearchOpen(false)}>
        <SheetBody className="flex flex-col gap-0 overflow-hidden p-0">
          {channelId && (
            <ChannelSearchPanel
              channelId={channelId}
              channelTitle={channel?.title ?? channelId ?? ""}
              onClose={() => setSearchOpen(false)}
              onJumpToMessage={handleSearchJump}
            />
          )}
        </SheetBody>
      </ChatDrawerSheet>

      {/* Files Sheet */}
      <ChatDrawerSheet open={filesOpen} onClose={() => setFilesOpen(false)}>
        <SheetBody className="flex flex-col gap-0 overflow-hidden p-0">
          {channelId && (
            <ChannelFilesPanel
              channelId={channelId}
              channelTitle={channel?.title ?? channelId ?? ""}
              onClose={() => setFilesOpen(false)}
              onPreviewAttachment={handlePreviewAttachment}
              onPreviewImage={handlePreviewImage}
              onJumpToMessage={handleJumpToMessage}
            />
          )}
        </SheetBody>
      </ChatDrawerSheet>

      {/* Members Sheet */}
      <ChatDrawerSheet open={membersOpen} onClose={() => setMembersOpen(false)}>
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
      </ChatDrawerSheet>

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
