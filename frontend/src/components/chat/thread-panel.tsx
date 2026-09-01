import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { ChatComposer } from "@/components/chat/chat-composer";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MentionDetailSheet } from "@/components/chat/mention-detail-sheet";
import { MessageRow } from "@/components/chat/message-row";
import { LoadingState } from "@/components/chat/states";
import { ThreadHeader } from "@/components/chat/thread-header";
import { ThreadReplies } from "@/components/chat/thread-replies";
import { type ComposerDraft } from "@/hooks/use-chat-composer";
import { useHistorySentinel } from "@/hooks/use-history-sentinel";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import {
  useMentionLabelResolver,
  useMentionTargets,
} from "@/hooks/use-mention-targets";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

const EMPTY_THREAD: ChatMessageUI[] = [];

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
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed on the reply-arrival count; the body intentionally reads nothing else.
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
                onViewDetails={handleViewDetails}
                onSenderClick={handleSenderClick}
                mentionLabel={mentionLabel}
                MentionBadge={MentionBadge}
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
