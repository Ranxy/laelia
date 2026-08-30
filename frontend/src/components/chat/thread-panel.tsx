import {
  ArrowLeft,
  ExternalLink,
  Maximize2,
  Minimize2,
  Send,
  X,
} from "lucide-react";
import {
  memo,
  type RefObject,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { ChatComposer } from "@/components/chat/chat-composer";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MentionDetailSheet } from "@/components/chat/mention-detail-sheet";
import {
  EMPTY_EVENTS,
  MessageRow,
  rowStreamingProps,
} from "@/components/chat/message-row";
import { EmptyState, LoadingState } from "@/components/chat/states";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { type ComposerDraft } from "@/composables/use-chat-composer";
import {
  useMentionLabelResolver,
  useMentionTargets,
} from "@/composables/useMentionTargets";
import { agentTeamServiceClient } from "@/connect";
import { taskStatusLabel } from "@/lib/task-status";
import { toastManager } from "@/lib/toast";
import { useHistorySentinel } from "@/lib/use-history-sentinel";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { useAppStore } from "@/stores";
import { senderKeyForMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/types";
import type { AgentTeam } from "@/types/proto-es/v1/agent_team_service_pb";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { TaskStatus } from "@/types/proto-es/v1/command_pb";

// ThreadReplies renders the beginning-of-replies divider + the reply list. It
// is memoized so typing a reply (which re-renders the panel's header/composer
// state) does not rebuild every reply row; its props are stable store refs,
// callbacks, and the memoized replies array, so it bails out unless a reply
// actually changed.
const ThreadReplies = memo(function ThreadReplies({
  replies,
  loading,
  agentTitleFor,
  onViewDetails,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  debugMode,
  currentPrincipalId,
  mentionLabel,
  onSenderClick,
  onCopyMarkdown,
  onConvertToTask,
  scrollRoot,
}: {
  replies: ChatMessageUI[];
  loading: boolean;
  agentTitleFor: (msg: ChatMessageUI) => string;
  onViewDetails: (commandId: string, agentId: string) => void;
  onPreviewAttachment?: (attachment: Attachment, rootMessageId: string) => void;
  onJumpToSection?: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  onPreviewImage?: (attachment: Attachment) => void;
  debugMode: boolean;
  currentPrincipalId?: string;
  mentionLabel?: (handle: string) => string | undefined;
  onSenderClick?: (type: "user" | "agent", id: string, name: string) => void;
  onCopyMarkdown?: (content: string) => void;
  onConvertToTask?: (msg: ChatMessageUI) => void;
  scrollRoot?: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation();
  return (
    <>
      {/* Beginning-of-replies divider. */}
      <div className="flex items-center gap-2 py-1">
        <div className="h-px flex-1 bg-control-border" />
        <span className="text-[11px] text-control-light">
          {t("chat.thread-beginning")}
        </span>
        <div className="h-px flex-1 bg-control-border" />
      </div>

      {/* Replies. */}
      {replies.length === 0 && !loading && (
        <EmptyState icon={Send} message={t("chat.thread-empty")} />
      )}
      {replies.map((msg, idx) => {
        const prev = idx > 0 ? replies[idx - 1] : null;
        const showAvatar =
          !prev || senderKeyForMessage(prev) !== senderKeyForMessage(msg);
        const rowProps = rowStreamingProps(msg, false, "", EMPTY_EVENTS);
        return (
          <div key={msg.id} data-msg-id={msg.id}>
            <MessageRow
              msg={msg}
              showAvatar={showAvatar}
              agentTitle={agentTitleFor(msg)}
              streamingContent={rowProps.streamingContent}
              streamingEvents={rowProps.streamingEvents}
              onViewDetails={onViewDetails}
              onSenderClick={onSenderClick}
              mentionLabel={mentionLabel}
              MentionBadge={MentionBadge}
              markdownCustomId="thread-chat"
              onPreviewAttachment={onPreviewAttachment}
              onJumpToSection={onJumpToSection}
              onPreviewImage={onPreviewImage}
              debugMode={debugMode}
              currentPrincipalId={currentPrincipalId}
              scrollRoot={scrollRoot}
              onCopyMarkdown={onCopyMarkdown}
              onConvertToTask={onConvertToTask}
              // In a thread, opening a thread from a reply is meaningless, so
              // hide the context-menu "Open thread" entry on every row.
              contextMenuRootOnly
              // Small threads render markdown synchronously to avoid the
              // per-row fallback→swap flash on open; large threads keep the
              // lazy gate so off-screen replies stay cheap.
              eager={replies.length <= 40}
            />
          </div>
        );
      })}
    </>
  );
});

export interface ThreadPanelProps {
  channelId: string;
  channelTitle: string;
  rootMessageId: string;
  onClose: () => void;
  // onViewInChannel renders the header "View in channel" jump. Omitted (and the
  // button hidden) when the thread is already shown inside its channel — the
  // jump is only meaningful from a standalone/embedded context (activity detail,
  // reminder detail) that is not the channel itself.
  onViewInChannel?: () => void;
  onPreviewAttachment?: (attachment: Attachment, rootMessageId: string) => void;
  onJumpToSection?: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  onPreviewImage?: (attachment: Attachment) => void;
  // fluid makes the panel fill its container's width/height instead of the
  // fixed 420px right-dock aside used in the channel page. Used when the
  // panel is embedded standalone (e.g. the reminder detail page).
  fluid?: boolean;
  // readOnly hides the reply composer + attachment upload. Set for
  // agent-to-agent DMs (type 3), which are admin view-only: a user can read
  // the thread but must not reply in or upload into it.
  readOnly?: boolean;
  // archived marks a thread whose channel has been archived by its owner. It
  // also forces readOnly, but the composer notice must say the channel is
  // archived (not the agent-DM view-only message).
  archived?: boolean;
  // scrollToMessageId scrolls the thread to a specific message once loaded —
  // used by the Activity detail pane to locate the exact message an activity
  // references (a @mention reply, or the latest reply of a folded task/reminder
  // thread). Runs at most once per id so it does not fight the user's scrolling.
  scrollToMessageId?: string;
  // expanded + onToggleExpand render an expand/collapse toggle in the header:
  // expanded makes the panel fill the full chat area (the channel page hides
  // its main pane behind it). Omitted in standalone/embedded contexts
  // (activity/reminder detail) that already render full-width via fluid.
  expanded?: boolean;
  onToggleExpand?: () => void;
}

export function ThreadPanel({
  channelId,
  channelTitle,
  rootMessageId,
  onClose,
  onViewInChannel,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  fluid,
  readOnly,
  archived,
  scrollToMessageId,
  expanded,
  onToggleExpand,
}: ThreadPanelProps) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  // On real iOS/iPadOS browsers the system edge-swipe owns edge touches (see
  // platform-edge-swipe.ts): the history sentinel turns it (and the back
  // button) into a thread dismissal instead of leaving the page.
  useHistorySentinel(true, onClose);
  const asideClass = fluid
    ? "flex h-full w-full flex-col"
    : "fixed inset-0 z-panel flex w-full flex-col bg-background pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:static lg:inset-auto lg:w-[420px] lg:shrink-0 lg:border-l lg:border-control-border lg:pt-0 lg:pb-0";

  const thread = useAppStore((s) => s.threadByRoot[rootMessageId]);
  const agents = useAppStore((s) => s.agents);
  const currentUser = useAppStore((s) => s.currentUser);
  const convertMessageToTask = useAppStore((s) => s.convertMessageToTask);
  // Per-user chat keybinding (see chat-conversation.tsx for rationale).
  const enterToSend = currentUser?.chatPreferences?.enterToSend ?? true;
  const navigate = useNavigate();

  const messages = thread?.messages ?? EMPTY_THREAD;
  const loading = thread?.loading ?? false;
  // The first message is always the root (context); the rest are replies.
  // Memoized so the slice reference is stable across composer re-renders —
  // otherwise ThreadReplies' memo below would never bail out.
  const rootMsg = messages.length > 0 ? messages[0] : null;
  const replies = useMemo(
    () => (rootMsg ? messages.slice(1) : messages),
    [messages, rootMsg]
  );

  const mentionTargets = useMentionTargets(channelId);
  const mentionLabel = useMentionLabelResolver(channelId);

  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);
  const [detailMention, setDetailMention] = useState<{
    type: "user" | "agent";
    id: string;
    name: string;
  } | null>(null);
  // Per-thread composer draft cache, owned here and consumed by ChatComposer
  // keyed per rootMessageId, so switching threads does not leak a half-typed
  // reply into the next thread. In-flight uploads are not part of the draft —
  // they belong to the composer instance that started them (see
  // useChatComposer).
  const draftsRef = useRef(new Map<string, ComposerDraft>());

  // Keyed by both the agent's resource name and its title so sender-title
  // lookup is O(1) instead of a linear scan per message (a thread with many
  // replies × a large roster previously scanned agents on every render).
  const agentsByKey = useMemo(() => {
    const map = new Map<string, (typeof agents)[number]>();
    for (const a of agents) {
      map.set(a.name, a);
      if (a.title) map.set(a.title, a);
    }
    return map;
  }, [agents]);

  const agentTitleFor = useCallback(
    (msg: ChatMessageUI) => {
      if (msg.role === "user") return "";
      const agent =
        agentsByKey.get(`agents/${msg.senderName}`) ??
        agentsByKey.get(msg.senderName ?? "");
      return agent?.title ?? msg.senderName ?? "";
    },
    [agentsByKey]
  );

  // Auto-stick to bottom as replies arrive.
  useEffect(() => {
    if (scrollRef.current && stickToBottomRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [replies.length]);

  // Scroll to a specific message once the thread has loaded (Activity detail
  // pane locating the exact message). Runs at most once per id and yields
  // stick-to-bottom so a later arriving reply doesn't yank the view away.
  const scrollToMessageRef = useRef<string>("");
  useEffect(() => {
    if (!scrollToMessageId || !rootMsg) return;
    if (scrollToMessageRef.current === scrollToMessageId) return;
    scrollToMessageRef.current = scrollToMessageId;
    stickToBottomRef.current = false;
    requestAnimationFrame(() => {
      scrollRef.current
        ?.querySelector(`[data-msg-id="${scrollToMessageId}"]`)
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
  }, [scrollToMessageId, rootMsg]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    stickToBottomRef.current = scrollHeight - scrollTop - clientHeight < 100;
  }, []);

  const handleViewDetails = useCallback(
    (commandId: string, agentId: string) => {
      navigate(`/members/agents/${agentId}/commands/${commandId}`);
    },
    [navigate]
  );

  const handleSenderClick = useCallback(
    (type: "user" | "agent", id: string, name: string) => {
      setDetailMention({ type, id, name });
    },
    []
  );

  // Right-click "Copy markdown" (shared with main chat): copy the raw
  // markdown body and surface a success/error toast.
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

  // Right-click "Convert to task" (shared with main chat): turn a root,
  // non-task message into a channel task. msg.id is the full resource name
  // ("conversations/c/messages/m"), so strip it to the bare id the store
  // action expects. Replies already carry a threadRoot, so the context menu
  // only offers this on the thread's root message.
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

  if (loading && !rootMsg) {
    return (
      <aside
        className={asideClass}
        style={
          isDesktop
            ? undefined
            : {
                // The swipe-back gesture drives the mobile full-screen panel
                // via CSS variables set on the layout root (see use-swipe-back).
                transform: "translateX(var(--swipe-offset, 0px))",
                transition: "var(--swipe-transition, none)",
              }
        }
      >
        <ThreadHeader
          title={t("chat.thread-title")}
          channelName={channelTitle}
          channelId={channelId}
          rootMsg={null}
          onClose={onClose}
          onViewInChannel={onViewInChannel}
          readOnly={readOnly}
          expanded={expanded}
          onToggleExpand={onToggleExpand}
        />
        <LoadingState />
      </aside>
    );
  }

  return (
    <aside
      className={asideClass}
      style={
        isDesktop
          ? undefined
          : {
              // The swipe-back gesture drives the mobile full-screen panel
              // via CSS variables set on the layout root (see use-swipe-back).
              transform: "translateX(var(--swipe-offset, 0px))",
              transition: "var(--swipe-transition, none)",
            }
      }
    >
      <ThreadHeader
        title={t("chat.thread-title")}
        channelName={channelTitle}
        channelId={channelId}
        rootMsg={rootMsg}
        onClose={onClose}
        onViewInChannel={onViewInChannel}
        readOnly={readOnly}
        expanded={expanded}
        onToggleExpand={onToggleExpand}
      />

      {/* Scroll area: root context + replies + composer. */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="flex flex-col gap-3 px-4 pt-4 pb-4">
          {/* Root message context. Rendered through the shared MessageRow so it
              gets the same markdown/mention/attachment treatment as channel
              chat and thread replies (it previously rendered raw text). */}
          {rootMsg && (
            <div data-msg-id={rootMsg.id}>
              <MessageRow
                msg={rootMsg}
                showAvatar
                agentTitle={agentTitleFor(rootMsg)}
                streamingContent=""
                streamingEvents={rootMsg.events ?? EMPTY_EVENTS}
                onViewDetails={handleViewDetails}
                onSenderClick={handleSenderClick}
                mentionLabel={mentionLabel}
                MentionBadge={MentionBadge}
                markdownCustomId="thread-chat"
                onPreviewAttachment={onPreviewAttachment}
                onJumpToSection={onJumpToSection}
                onPreviewImage={onPreviewImage}
                debugMode={currentUser?.debugMode ?? false}
                currentPrincipalId={currentUser?.handle}
                scrollRoot={scrollRef}
                onCopyMarkdown={handleCopyMarkdown}
                onConvertToTask={handleConvertToTask}
                // A thread's root message is already in a thread; hide the
                // context-menu "Open thread" entry for it too.
                contextMenuRootOnly
                // The root is a single message — render its markdown synchronously
                // so opening the thread doesn't flash as it swaps the raw-text
                // placeholder for the real markdown a frame later.
                eager
              />
            </div>
          )}

          <ThreadReplies
            replies={replies}
            loading={loading}
            agentTitleFor={agentTitleFor}
            onViewDetails={handleViewDetails}
            onPreviewAttachment={onPreviewAttachment}
            onJumpToSection={onJumpToSection}
            onPreviewImage={onPreviewImage}
            debugMode={currentUser?.debugMode ?? false}
            currentPrincipalId={currentUser?.handle}
            mentionLabel={mentionLabel}
            onSenderClick={handleSenderClick}
            onCopyMarkdown={handleCopyMarkdown}
            onConvertToTask={handleConvertToTask}
            scrollRoot={scrollRef}
          />
        </div>
      </div>

      {/* Composer — hidden when readOnly (agent-to-agent DMs are admin
          view-only: no replying or uploading into them). */}
      <div className="shrink-0 border-t border-control-border bg-background px-3 pb-3 pt-2">
        {readOnly ? (
          <div className="rounded-2xl border border-control-border bg-control-bg/40 px-4 py-3 text-center text-xs text-control-placeholder">
            {archived
              ? t("chat.channel-archived")
              : t("chat.agent-dm-view-only")}
          </div>
        ) : (
          <ChatComposer
            key={rootMessageId}
            channelId={channelId}
            rootMessageId={rootMessageId}
            draftKey={rootMessageId}
            draftsRef={draftsRef}
            enterToSend={enterToSend}
            mentionTargets={mentionTargets}
            popupId="thread-mention-popup"
            placeholder={t("chat.thread-placeholder")}
            size="compact"
            onSendStart={() => {
              stickToBottomRef.current = true;
            }}
          />
        )}
      </div>

      <MentionDetailSheet
        open={detailMention !== null}
        type={detailMention?.type ?? "user"}
        id={detailMention?.id ?? ""}
        name={detailMention?.name ?? ""}
        onClose={() => setDetailMention(null)}
      />
    </aside>
  );
}

function ThreadHeader({
  title,
  channelName,
  channelId,
  rootMsg,
  onClose,
  onViewInChannel,
  readOnly,
  expanded,
  onToggleExpand,
}: {
  title: string;
  channelName: string;
  channelId: string;
  rootMsg: ChatMessageUI | null;
  onClose: () => void;
  // onViewInChannel renders the header "View in channel" jump. Omitted (and the
  // button hidden) when the thread is already shown inside its channel — the
  // jump is only meaningful from a standalone/embedded context (activity detail,
  // reminder detail) that is not the channel itself.
  onViewInChannel?: () => void;
  // readOnly hides the close-task action (agent-to-agent DMs are admin
  // view-only, same as the composer).
  readOnly?: boolean;
  // onToggleExpand renders the expand/collapse toggle; expanded selects the
  // icon shown. Both omitted outside the channel page.
  expanded?: boolean;
  onToggleExpand?: () => void;
}) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const closeThread = useAppStore((s) => s.closeThread);
  const toggleTasksPanel = useAppStore((s) => s.toggleTasksPanel);
  const updateTaskStatus = useAppStore((s) => s.updateTaskStatus);
  const assignTask = useAppStore((s) => s.assignTask);
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const conversationName = `conversations/${channelId}`;
  const members =
    useAppStore((s) => s.channelMembersByConv[conversationName]) ?? [];
  const [statusUpdating, setStatusUpdating] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [teams, setTeams] = useState<AgentTeam[]>([]);
  const isTask = !!rootMsg?.task;
  // Task controls (status + assignee dropdowns) are hidden in readOnly
  // (admin agent-to-agent DMs) and when the thread has no task root.
  const canManageTask = isTask && !readOnly;

  // Load the channel roster for the assignee dropdown on first render of a
  // task thread (the members panel may not have been opened yet).
  useEffect(() => {
    if (canManageTask && members.length === 0) {
      void listChannelMembers(channelId);
    }
    if (canManageTask) {
      void agentTeamServiceClient
        .listAgentTeams({ pageSize: 1000 })
        .then((res) => setTeams(res.agentTeams ?? []))
        .catch(() => setTeams([]));
    }
  }, [canManageTask, channelId]);

  const handleStatusChange = async (value: string | null) => {
    if (!rootMsg || value == null) return;
    const status = Number(value);
    if (status === rootMsg.task?.status) return;
    setStatusUpdating(true);
    try {
      await updateTaskStatus(channelId, rootMsg.id, status);
      toastManager.add({
        type: "success",
        title: t("channelTask.status-change-success"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("channelTask.status-change-error"),
        description:
          err instanceof Error
            ? err.message
            : t("channelTask.status-change-error-description"),
      });
    } finally {
      setStatusUpdating(false);
    }
  };

  const handleAssigneeChange = async (value: string | null) => {
    if (!rootMsg || value == null) return;
    // value is "<memberType>:<memberId>".
    const [memberType, memberId] = value.split(":");
    if (!memberType || !memberId) return;
    setAssigning(true);
    try {
      await assignTask(channelId, rootMsg.id, Number(memberType), memberId);
      toastManager.add({
        type: "success",
        title: t("channelTask.assignee-success"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("channelTask.assignee-error"),
        description:
          err instanceof Error
            ? err.message
            : t("channelTask.assignee-error-description"),
      });
    } finally {
      setAssigning(false);
    }
  };

  const handleBackToTasks = () => {
    // Drill back from a task's thread to the channel's task board.
    closeThread();
    toggleTasksPanel(channelId);
  };
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2.5">
      {isTask && (
        <button
          type="button"
          onClick={handleBackToTasks}
          className="flex items-center gap-1 text-xs text-control-placeholder hover:text-main transition-colors"
          aria-label={t("channelTask.back-to-tasks")}
        >
          <ArrowLeft className="size-3.5" />
          <span className="hidden sm:inline">
            {t("channelTask.back-to-tasks")}
          </span>
        </button>
      )}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold text-main truncate">
          {title} — #{channelName}
        </p>
      </div>
      {onViewInChannel && (
        <button
          type="button"
          onClick={onViewInChannel}
          className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent transition-colors"
        >
          <ExternalLink className="size-3.5" />
          <span className="hidden sm:inline">
            {t("chat.thread-view-in-channel")}
          </span>
        </button>
      )}
      {canManageTask && (
        <>
          {/* Status dropdown: move the task between any of the four statuses.
              DONE closes the task (sets completed_at). */}
          <Select
            value={String(rootMsg?.task?.status ?? TaskStatus.TODO)}
            onValueChange={(v) => void handleStatusChange(v)}
            disabled={statusUpdating}
          >
            <SelectTrigger
              size="xs"
              className="shrink-0"
              aria-label={t("channelTask.status-change-aria") ?? ""}
            >
              <SelectValue>
                {(value) => taskStatusLabel(Number(value), t)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={String(TaskStatus.TODO)}>
                {t("channelTask.status-todo")}
              </SelectItem>
              <SelectItem value={String(TaskStatus.IN_PROGRESS)}>
                {t("channelTask.status-in-progress")}
              </SelectItem>
              <SelectItem value={String(TaskStatus.IN_REVIEW)}>
                {t("channelTask.status-in-review")}
              </SelectItem>
              <SelectItem value={String(TaskStatus.DONE)}>
                {t("channelTask.status-done")}
              </SelectItem>
            </SelectContent>
          </Select>

          {/* Assignee dropdown: assign any channel member (user or agent) as
              the task's owner. */}
          <Select
            value={
              rootMsg?.task?.assigneeType && rootMsg.task.assigneeResourceId
                ? `${rootMsg.task.assigneeType}:${rootMsg.task.assigneeResourceId}`
                : ""
            }
            onValueChange={(v) => void handleAssigneeChange(v)}
            disabled={assigning}
          >
            <SelectTrigger
              size="xs"
              className="shrink-0"
              aria-label={t("channelTask.assignee-aria") ?? ""}
            >
              <SelectValue>
                {() =>
                  rootMsg?.task?.assigneeName ||
                  t("channelTask.assignee-placeholder")
                }
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {members.map((m) => (
                <SelectItem
                  key={`${m.memberType}:${m.memberId}`}
                  value={`${m.memberType}:${m.memberId}`}
                >
                  {m.displayName}
                </SelectItem>
              ))}
              {teams.length > 0 && (
                <SelectItem
                  key="team-separator"
                  value="team-separator"
                  disabled
                >
                  {t("channelTask.assignee-teams")}
                </SelectItem>
              )}
              {teams.map((team) => {
                const teamId = team.name.split("/").pop() ?? "";
                return (
                  <SelectItem key={team.name} value={`3:${teamId}`}>
                    {team.title}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </>
      )}
      {/* Creator display: the task's author (display name, no avatar). */}
      {isTask && rootMsg?.senderName && (
        <span className="hidden shrink-0 text-xs text-control-placeholder sm:inline">
          {t("channelTask.creator", { name: rootMsg.senderName })}
        </span>
      )}
      {/* The expand/collapse toggle is a desktop affordance: on mobile the
          thread panel already fills the screen, so the toggle is meaningless. */}
      {isDesktop && onToggleExpand && (
        <button
          type="button"
          onClick={onToggleExpand}
          className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
          aria-label={t(
            expanded ? "chat.thread-collapse" : "chat.thread-expand"
          )}
          title={t(expanded ? "chat.thread-collapse" : "chat.thread-expand")}
        >
          {expanded ? (
            <Minimize2 className="size-4" />
          ) : (
            <Maximize2 className="size-4" />
          )}
        </button>
      )}
      <button
        type="button"
        onClick={onClose}
        className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
        aria-label={t("chat.thread-close")}
      >
        {isDesktop ? (
          <X className="size-4" />
        ) : (
          <ArrowLeft className="size-4" />
        )}
      </button>
    </div>
  );
}

const EMPTY_THREAD: ChatMessageUI[] = [];
