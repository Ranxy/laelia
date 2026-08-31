import {
  ArrowUp,
  ChevronRight,
  Loader2,
  MessageCircleReply,
} from "lucide-react";
import MarkdownRender from "markstream-react";
import { memo, type RefObject, useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { FileCard } from "@/components/chat/file-card";
import { LazyMarkdown } from "@/components/chat/lazy-markdown";
import {
  contentWithMentionTags,
  splitByMentions,
} from "@/components/chat/mentions";
import { MessageContextMenu } from "@/components/chat/message-context-menu";
import { RemoteImage } from "@/components/chat/remote-image";
import { TaskStatusBadge } from "@/components/chat/task-status-badge";
import { ChatDiff } from "@/components/chat-events/diff-view";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { ChatWarning } from "@/components/chat-events/warning";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { AttachmentCommentCard } from "@/components/preview/attachment-comment-card";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { isHtmlAttachment, MAX_HTML_PREVIEW_BYTES } from "@/lib/html-file";
import { isImageAttachment } from "@/lib/image-file";
import {
  isMarkdownAttachment,
  MAX_MARKDOWN_PREVIEW_BYTES,
} from "@/lib/markdown-file";
import { pairToolCallEvents } from "@/lib/tool-call-events";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { isOwnUserMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/ui-models";
import type { Attachment, CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType, SenderType } from "@/types/proto-es/v1/command_pb";

// Stable empty fallback so events-bearing rows (no events) keep a stable
// array reference across renders, and MemoMarkdown's memo keeps bailing out.
const EMPTY_EVENTS: CommandEvent[] = [];

// Module-level constant for the mention path's customHtmlTags prop. Passing an
// inline ["mention"] array literal would mint a fresh reference every render,
// defeating MemoMarkdown's React.memo (shallow props compare) in exactly the
// channel/thread path the memo was added for.
const MENTION_HTML_TAGS = ["mention"];

// MemoMarkdown isolates the markstream/LazyMarkdown subtree so it only
// re-renders (and re-parses markdown) when the content actually changed. A row
// otherwise re-renders on cheap field patches — e.g. the channel watcher
// replacing the msg object to update a reply-count or task badge — and without
// this the unchanged (possibly long) markdown was re-parsed on every such
// patch. (The streaming pipeline is retired: rows always render their
// committed content, so the memo input surface is content + eager only.)
const MemoMarkdown = memo(function MemoMarkdown({
  content,
  eager,
  scrollRoot,
  markdownCustomId,
  customHtmlTags,
}: {
  content: string;
  eager: boolean;
  scrollRoot?: RefObject<HTMLElement | null>;
  markdownCustomId: string;
  customHtmlTags?: string[];
}) {
  return (
    <LazyMarkdown
      eager={eager}
      scrollRoot={scrollRoot}
      fallback={
        <span className="whitespace-pre-wrap break-words">{content}</span>
      }
      render={() => (
        <MarkdownRender
          customId={markdownCustomId}
          content={content}
          customHtmlTags={customHtmlTags}
          final
        />
      )}
    />
  );
});

export interface MessageRowProps {
  msg: ChatMessageUI;
  showAvatar: boolean;
  agentTitle: string;
  onViewDetails: (commandId: string, agentId: string) => void;
  // Optional mention-aware rendering (channel chat). When provided, the row
  // renders @mentions as badges and lets the caller react to clicks.
  onMentionClick?: (type: string, id: string, name: string) => void;
  // When provided, clicking a message sender's avatar or display name opens
  // the same user/agent detail sheet as a mention click.
  onSenderClick?: (type: "user" | "agent", id: string, name: string) => void;
  // Maps a mention handle to its display label (display name, or
  // "name(handle)" when the channel has same-named members). Purely cosmetic:
  // matching and click dispatch keep using the handle. Falls back to the
  // handle when the resolver returns undefined or is absent.
  mentionLabel?: (handle: string) => string | undefined;
  // MentionBadge is injected (rather than imported) so the shared MessageRow
  // doesn't pull the channel-specific popup machinery into the DM chat bundle.
  MentionBadge?: typeof import("@/components/chat/mention-badge").MentionBadge;
  // markdownCustomId distinguishes the markstream renderer instance between
  // DM and channel chat so each can carry independent streaming state.
  markdownCustomId: string;
  // onOpenThread, when provided (channel chat only), enables the "Reply in
  // thread" hover action and the reply-count entry. The message's id is the
  // thread root id the panel opens against.
  onOpenThread?: (msg: ChatMessageUI) => void;
  // onOpenThreadAt, when provided, wires the inline thread preview's per-reply
  // rows (root messages only): clicking one opens the thread drawer and
  // scrolls to that reply. Receives the root message and the previewed reply.
  onOpenThreadAt?: (rootMsg: ChatMessageUI, reply: ChatMessageUI) => void;
  // onPreviewAttachment, when provided, wires markdown attachments to the
  // full-page preview overlay. Receives the attachment and the effective
  // thread root (the message's threadRoot, or its own id when it is a root)
  // so Phase 2 comments can route to the right thread.
  onPreviewAttachment?: (attachment: Attachment, rootMessageId: string) => void;
  // onJumpToSection, when provided, turns an anchored-comment card's anchor
  // chip into a cross-scenario jump: it opens the file's preview overlay
  // already scrolled to the section the comment is anchored to. Receives the
  // anchored attachment (which references the file), the section id, and the
  // effective thread root.
  onJumpToSection?: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  // onPreviewImage, when provided, wires image attachments to the lightbox
  // overlay. Unlike markdown, images render inline directly (scaled to fit);
  // this handler is the click-to-zoom affordance on that inline image.
  onPreviewImage?: (attachment: Attachment) => void;
  debugMode: boolean;
  // currentPrincipalId is the current user's principal id (the {user} segment
  // of their "users/{user}" name), used to distinguish the current user's own
  // messages from other users' messages in shared channels. Optional: when
  // absent the row falls back to treating every user message as the current
  // user's (the pre-existing behavior).
  currentPrincipalId?: string;
  // scrollRoot is the chat list's scroll container, forwarded to LazyMarkdown so
  // its IntersectionObserver roots against the real scroll viewport without
  // rediscovering it per row (which would thrash layout on a 100-row mount).
  // Optional: when omitted LazyMarkdown walks the DOM to find the container.
  scrollRoot?: RefObject<HTMLElement | null>;
  // eager renders the markdown synchronously on first paint (skipping
  // LazyMarkdown's fallback→swap) for every row. The channel chat sets this for
  // small/medium conversations so entering them doesn't flash as each visible
  // row swaps its inline raw-text placeholder for block markdown a frame later.
  // Large histories leave it off so off-screen rows stay cheap to mount.
  eager?: boolean;
  // onToggleReaction, when provided, enables the reaction bar under the
  // message: clicking an emoji pill adds the caller's reaction (or removes it
  // if they already reacted). Receives the message and the emoji.
  onToggleReaction?: (msg: ChatMessageUI, emoji: string) => void;
  // Context-menu wiring (main chat only). When onCopyMarkdown is provided the
  // row gets a right-click menu with Copy Markdown (final content only). The
  // caller wires onOpenThread (the same action as the hover entry, shown for
  // root messages) and onConvertToTask (shown for root, non-task messages with
  // the laelia.conversations.send permission) via the same callbacks.
  onCopyMarkdown?: (content: string) => void;
  onConvertToTask?: (msg: ChatMessageUI) => void;
  // contextMenuRootOnly hides the "Open thread" entry from the menu for this
  // row (used by the thread panel, where opening a thread from a reply is
  // meaningless). Defaults to false.
  contextMenuRootOnly?: boolean;
}

// ThreadPreviewRow renders one reply in a root message's inline thread
// preview: small avatar, sender label, a single-line truncated plain-text
// excerpt, and the time pushed to the far edge. Clicking the row opens the
// thread drawer scrolled to this reply. Memoized with stable props so the
// channel list's badge poll (5s) does not re-render unchanged rows.
const ThreadPreviewRow = memo(function ThreadPreviewRow({
  rootMsg,
  reply,
  onOpenThreadAt,
  currentPrincipalId,
}: {
  rootMsg: ChatMessageUI;
  reply: ChatMessageUI;
  onOpenThreadAt: (rootMsg: ChatMessageUI, reply: ChatMessageUI) => void;
  currentPrincipalId?: string;
}) {
  const { t, i18n } = useTranslation();
  const isUser = reply.role === "user";
  const isOwn = isOwnUserMessage(reply, currentPrincipalId);
  const avatarSeed = isUser
    ? reply.principalId || currentPrincipalId || ""
    : reply.agentId || reply.senderName || "agent";
  const avatarName = isUser
    ? reply.principalId || currentPrincipalId
      ? avatarNameForUserId(reply.principalId || currentPrincipalId || "")
      : undefined
    : reply.agentId
      ? avatarNameForAgentId(reply.agentId)
      : undefined;
  const avatarSrc = useAvatar(avatarName);
  // Sender label mirrors MessageRow's header: "You" for the current user's own
  // messages in shared channels, the display name otherwise.
  const senderLabel = isUser
    ? isOwn
      ? t("chat.you")
      : reply.senderName || t("chat.you")
    : reply.senderName || t("chat.agent");
  // Single-line plain-text excerpt. Markdown renders raw here on purpose (the
  // drawer carries the rich rendering); a file-only reply falls back to its
  // attachment names, same rule as the left-rail channel preview.
  const excerpt =
    reply.content ||
    (reply.attachments ?? [])
      .map((a) => a.name)
      .filter(Boolean)
      .join(", ");
  return (
    <button
      type="button"
      onClick={() => onOpenThreadAt(rootMsg, reply)}
      className="flex w-full items-center gap-2 rounded-md px-1 py-1 text-left text-xs transition-colors hover:bg-control-bg/60 cursor-pointer"
      aria-label={senderLabel}
    >
      <Avatar seed={avatarSeed} src={avatarSrc} size={6} />
      <span className="max-w-[10rem] shrink-0 truncate font-medium text-control">
        {senderLabel}
      </span>
      <span className="min-w-0 flex-1 truncate text-control-placeholder">
        {excerpt}
      </span>
      <span className="shrink-0 text-[11px] text-control-light tabular-nums">
        {formatTime(reply.timestamp, i18n.language)}
      </span>
    </button>
  );
});

// ThreadPreview renders the "N replies · M new" entry plus the latest replies
// of that thread beneath it (Slack-style inline preview). The header keeps the
// existing open-thread action (opens the drawer without a target reply); each
// preview row jumps to its exact reply. The whole block only spans the message
// body column, so it reads as part of the root message. The caller falls back
// to the plain count entry when no preview has been synced yet.
const ThreadPreviewBlock = memo(function ThreadPreviewBlock({
  rootMsg,
  replies,
  newCount,
  onOpenThread,
  onOpenThreadAt,
  currentPrincipalId,
}: {
  rootMsg: ChatMessageUI;
  replies: ChatMessageUI[];
  newCount: number;
  onOpenThread: (msg: ChatMessageUI) => void;
  onOpenThreadAt: (rootMsg: ChatMessageUI, reply: ChatMessageUI) => void;
  currentPrincipalId?: string;
}) {
  const { t } = useTranslation();
  return (
    // Grouped panel: header + preview rows inside one soft card so the
    // preview reads as a single sub-conversation attached to the root. Shares
    // the content bubble's max-w-[80%] cap so both scale together with the
    // window width instead of drifting apart on wide screens.
    <div className="flex w-full max-w-[80%] flex-col gap-0.5 rounded-lg border border-control-border bg-control-bg/40 p-1">
      <button
        type="button"
        onClick={() => onOpenThread(rootMsg)}
        className="flex w-fit items-center gap-1 px-1 text-xs font-medium text-control hover:text-accent cursor-pointer transition-colors"
      >
        <MessageCircleReply className="size-3" />
        <span>
          {t("chat.thread-replies", { count: rootMsg.threadReplyCount ?? 0 })}
        </span>
        {newCount > 0 && (
          <span className="text-accent">
            {t("chat.thread-new-replies", { count: newCount })}
          </span>
        )}
        <ChevronRight className="size-3 text-control-light" />
      </button>
      {replies.map((reply) => (
        <ThreadPreviewRow
          key={reply.id}
          rootMsg={rootMsg}
          reply={reply}
          onOpenThreadAt={onOpenThreadAt}
          currentPrincipalId={currentPrincipalId}
        />
      ))}
    </div>
  );
});

export const MessageRow = memo(function MessageRow(props: MessageRowProps) {
  const {
    msg,
    showAvatar,
    agentTitle,
    onViewDetails,
    onMentionClick,
    onSenderClick,
    MentionBadge,
    markdownCustomId,
    onOpenThread,
    onOpenThreadAt,
    onPreviewAttachment,
    onJumpToSection,
    onPreviewImage,
    debugMode,
    currentPrincipalId,
    scrollRoot,
    eager = false,
    onToggleReaction,
    onCopyMarkdown,
    onConvertToTask,
    contextMenuRootOnly = false,
  } = props;
  const { t, i18n } = useTranslation();
  const isDesktop = useIsDesktop();
  const isUser = msg.role === "user";
  // In a shared channel, user messages from other users must render with their
  // own name rather than the current user's "You" label. isOwnUser falls back
  // to true when either id is unknown (optimistic send / legacy rows) so the
  // label never flips mid-stream.
  const isOwnUser = isOwnUserMessage(msg, currentPrincipalId);

  // Avatar: the pixel identicon is seeded by a stable id (the user's principal
  // id, or the agent's resource id). When the sender has an uploaded avatar,
  // useAvatar fetches its blob URL (cached per session); otherwise fall back to
  // the pixel identicon.
  const avatarSeed = isUser
    ? msg.principalId || currentPrincipalId || ""
    : msg.agentId || agentTitle || "agent";
  const avatarName = isUser
    ? msg.principalId || currentPrincipalId
      ? avatarNameForUserId(msg.principalId || currentPrincipalId || "")
      : undefined
    : msg.agentId
      ? avatarNameForAgentId(msg.agentId)
      : undefined;
  const avatarSrc = useAvatar(avatarName);

  // The sender identity used when clicking the avatar or name to open the
  // Agent/User detail sheet. Only enabled when we can resolve a stable id.
  const senderClickTarget = (() => {
    if (isUser) {
      const id = msg.principalId || currentPrincipalId || "";
      if (!id) return null;
      return {
        type: "user" as const,
        id,
        name: msg.senderName || msg.principalId || currentPrincipalId || "",
      };
    }
    const id = msg.agentId || "";
    if (!id) return null;
    return {
      type: "agent" as const,
      id,
      name: agentTitle || msg.senderName || "",
    };
  })();
  const handleSenderClick =
    senderClickTarget && onSenderClick
      ? () =>
          onSenderClick(
            senderClickTarget.type,
            senderClickTarget.id,
            senderClickTarget.name
          )
      : undefined;
  const senderName = isUser
    ? isOwnUser
      ? t("chat.you")
      : msg.senderName || t("chat.you")
    : agentTitle || msg.senderName || t("chat.agent");

  // The streaming pipeline is retired: rows always render their committed
  // content and their own (already final) event list.
  const displayContent = msg.content;
  // A file-only message (attachment with no text) still needs its bubble: the
  // attachments render inside it, so hiding an empty-content bubble would hide
  // the files from every viewer except the sender (whose bubble is always
  // visible via the isOwnUser branch below).
  const hasAttachments = (msg.attachments?.length ?? 0) > 0;
  const events = msg.events ?? EMPTY_EVENTS;

  const toolCallPairs = useMemo(() => pairToolCallEvents(events), [events]);
  const diffEvents = useMemo(
    () => events.filter((e) => e.type === CommandEventType.DIFF_EMITTED),
    [events]
  );
  const warningEvents = useMemo(
    () => events.filter((e) => e.type === CommandEventType.WARNING),
    [events]
  );
  const hasEvents =
    !isUser &&
    (toolCallPairs.length > 0 ||
      diffEvents.length > 0 ||
      warningEvents.length > 0);

  const [eventsCollapsed, setEventsCollapsed] = useState(false);

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

  const segments = useMemo(
    () =>
      MentionBadge
        ? splitByMentions(displayContent ?? "", msg.mentions ?? [])
        : null,
    [MentionBadge, displayContent, msg.mentions]
  );

  // Markdown with @mentions rewritten to inline <mention> nodes, so a mention
  // flows inline with the surrounding prose instead of landing on its own line
  // (which happened when each text segment was rendered through its own
  // block-emitting MarkdownRender). Used for both user and agent messages in
  // the mention-aware path (channel chat / threads).
  const mentionContent = useMemo(
    () =>
      MentionBadge
        ? contentWithMentionTags(displayContent ?? "", msg.mentions ?? [])
        : null,
    [MentionBadge, displayContent, msg.mentions]
  );

  const MentionBadgeCmp = MentionBadge;

  // Delegated click handler for mention chips rendered inside agent markdown.
  // The custom <mention> node renders a span carrying {type, id, name} as
  // data-* attributes (see lib/markdown); recover them and dispatch
  // onMentionClick. Kept as a stable callback so the bubble div isn't
  // re-attached on every render.
  const handleBubbleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const target = e.target as HTMLElement | null;
      // Mention chips (agent markdown <mention> nodes) keep their own action.
      if (onMentionClick) {
        const chip = target?.closest?.("[data-mtype]");
        if (chip) {
          const type = chip.getAttribute("data-mtype");
          const id = chip.getAttribute("data-mid");
          const name = chip.getAttribute("data-mname");
          if (type && id && name) {
            onMentionClick(type, id, name);
            return;
          }
        }
      }
      // On mobile the "Reply in thread" entry is hidden (too small to tap), so
      // tapping the message bubble itself opens the thread. Interactive
      // elements (links, buttons, mention chips, image zoom) keep their own
      // behavior.
      if (!isDesktop && onOpenThread && !msg.threadRoot) {
        if (target?.closest?.("a, button, [role='button'], [data-mtype]"))
          return;
        onOpenThread(msg);
      }
    },
    [onMentionClick, isDesktop, onOpenThread, msg]
  );

  // "Reply in thread" entry. Rendered in the header when the header is shown
  // (showAvatar), and as a standalone hover row otherwise — so every root
  // message in a consecutive group exposes the action, not just the first.
  // (Consecutive messages from the same sender skip the header to group the
  // bubble, which previously swallowed this button along with it.)
  const renderReplyInThread = () =>
    isDesktop && onOpenThread && !msg.threadRoot ? (
      <button
        type="button"
        onClick={() => onOpenThread(msg)}
        // A task's thread is its workspace, so keep the entry visible on
        // task messages instead of hover-only; non-task roots stay hover-gated.
        className={cn(
          "flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-control-placeholder transition-all hover:bg-control-bg hover:text-main focus:opacity-100 cursor-pointer",
          msg.task ? "opacity-100" : "opacity-0 group-hover:opacity-100"
        )}
        aria-label={t("chat.reply-in-thread")}
      >
        <MessageCircleReply className="size-3" />
        <span className="hidden sm:inline">{t("chat.reply-in-thread")}</span>
      </button>
    ) : null;

  // System messages (sender_type=SYSTEM) are task lifecycle notifications —
  // "📋 Alice created task #3", "🙋 Bob claimed task #3", etc. They render as a
  // single centered, low-contrast line with no avatar or bubble, so they read as
  // channel events rather than conversational turns.
  if (msg.senderType === SenderType.SYSTEM) {
    return (
      <div className="flex justify-center py-1">
        <p className="text-xs text-control-placeholder px-3 py-1 rounded-md bg-control-bg/30 text-center max-w-[90%]">
          {msg.content}
        </p>
      </div>
    );
  }

  const row = (
    <div
      className={cn(
        "group flex gap-3",
        // Own messages align right; other users' messages align left next to
        // the agents' (a shared channel reads top-to-bottom by sender, not with
        // every other user mirrored to the right).
        isOwnUser ? "flex-row-reverse" : "flex-row"
      )}
    >
      {/* Avatar */}
      <div className="flex shrink-0 flex-col items-center pt-0.5">
        {showAvatar ? (
          handleSenderClick ? (
            <button
              type="button"
              onClick={handleSenderClick}
              className="cursor-pointer rounded-full transition-opacity hover:opacity-80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              aria-label={senderName}
            >
              <Avatar
                seed={avatarSeed}
                src={avatarSrc}
                accent={isUser ? isOwnUser : false}
              />
            </button>
          ) : (
            <Avatar
              seed={avatarSeed}
              src={avatarSrc}
              accent={isUser ? isOwnUser : false}
            />
          )
        ) : (
          <div className="size-8 shrink-0" />
        )}
      </div>

      {/* Message body */}
      <div
        className={cn(
          "flex min-w-0 flex-1 flex-col gap-1.5",
          isOwnUser ? "items-end" : "items-start"
        )}
      >
        {/* Header */}
        {showAvatar && (
          <div className="flex items-center gap-2 px-0.5">
            {handleSenderClick ? (
              <button
                type="button"
                onClick={handleSenderClick}
                className="text-xs font-medium text-control transition-colors hover:text-accent cursor-pointer"
              >
                {senderName}
              </button>
            ) : (
              <span className="text-xs font-medium text-control">
                {senderName}
              </span>
            )}
            <span className="text-xs text-control-placeholder">
              {formatTime(msg.timestamp, i18n.language)}
            </span>
            {msg.sending && (
              <span className="flex items-center gap-1 text-[10px] text-control-placeholder">
                <Loader2 className="size-3 animate-spin" />
                {t("chat.sending")}
              </span>
            )}
            {!isUser && msg.status !== undefined && (
              <CommandStatusBadge
                status={msg.status}
                className="text-[10px] px-1.5 py-0"
              />
            )}
            {msg.task && (
              <TaskStatusBadge
                taskNumber={msg.task.taskNumber}
                status={msg.task.status}
                assigneeName={msg.task.assigneeName}
                className="text-[10px] px-1.5 py-0"
              />
            )}
            {renderReplyInThread()}
          </div>
        )}

        {/* Standalone "Reply in thread" entry for grouped messages whose
            header is suppressed (showAvatar=false). Keeps the action available
            on every root message, not just the first in a consecutive group. */}
        {!showAvatar && renderReplyInThread()}

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
            <button
              type="button"
              onClick={() => setEventsCollapsed(true)}
              className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent cursor-pointer transition-colors self-start"
            >
              <ArrowUp className="size-3" />
              {t("command.collapse")}
            </button>
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
            // Bubble corner points toward the sender's avatar: own messages
            // sit on the right (top-right corner sharp), other users' and
            // agents' sit on the left (top-left corner sharp).
            isOwnUser
              ? "bg-control-bg/60 text-main rounded-tr-sm px-4 py-2.5 max-w-[80%]"
              : displayContent || hasAttachments
                ? "bg-control-bg/60 text-main rounded-tl-sm px-4 py-3 max-w-[80%]"
                : "hidden"
          )}
          onClick={handleBubbleClick}
        >
          {segments && MentionBadgeCmp ? (
            // Mention-aware rendering (channel chat / threads): render the
            // whole body in a single markdown pass with @mentions rewritten
            // to inline <mention> nodes (mentionContent) for both user and
            // agent messages. A single MarkdownRender keeps each mention
            // inside the same <p> as the surrounding prose, so it flows
            // inline instead of being forced onto its own line by per-segment
            // block <p> wrappers.
            <div className="markstream-chat break-words">
              <MemoMarkdown
                content={mentionContent ?? ""}
                eager={eager}
                scrollRoot={scrollRoot}
                markdownCustomId={markdownCustomId}
                customHtmlTags={MENTION_HTML_TAGS}
              />
            </div>
          ) : displayContent ? (
            <div className="markstream-chat break-words">
              <MemoMarkdown
                content={displayContent}
                eager={eager}
                scrollRoot={scrollRoot}
                markdownCustomId={markdownCustomId}
              />
            </div>
          ) : null}
          {msg.attachments && msg.attachments.length > 0 && (
            <div className="flex flex-col gap-1">
              {msg.attachments.map((att) => {
                // While a file is still uploading, show its progress inline in
                // the message instead of the final file/image card.
                const uploadProgress = msg.uploadProgress?.[att.id];
                if (uploadProgress !== undefined) {
                  return (
                    <div
                      key={att.id}
                      className="flex items-center gap-2 rounded-lg border border-control-border bg-control-bg/40 px-3 py-2 text-xs text-main"
                    >
                      <Loader2 className="size-3.5 animate-spin text-control-light" />
                      <span className="max-w-[160px] truncate">{att.name}</span>
                      <span className="text-control-placeholder">
                        {uploadProgress}%
                      </span>
                    </div>
                  );
                }
                // An attachment carrying a section anchor is a comment on a
                // span of a file, not a whole-file upload — render the anchor
                // + quote inline instead of a FileCard.
                if (att.sectionAnchor) {
                  return (
                    <AttachmentCommentCard
                      key={att.id}
                      attachment={att}
                      variant="inline"
                      onJumpToSection={
                        onJumpToSection
                          ? (sectionId) =>
                              onJumpToSection(
                                att,
                                sectionId,
                                msg.threadRoot ?? msg.id
                              )
                          : undefined
                      }
                    />
                  );
                }
                // Image attachments render inline directly (scaled to fit the
                // chat width), with click-to-zoom opening the lightbox.
                if (isImageAttachment(att)) {
                  return (
                    <RemoteImage
                      key={att.id}
                      attachment={att}
                      variant="inline"
                      onClick={
                        onPreviewImage ? () => onPreviewImage(att) : undefined
                      }
                    />
                  );
                }
                const previewable =
                  isMarkdownAttachment(att) || isHtmlAttachment(att);
                const tooLarge =
                  previewable &&
                  (att.sizeBytes ?? 0n) >
                    (isHtmlAttachment(att)
                      ? MAX_HTML_PREVIEW_BYTES
                      : MAX_MARKDOWN_PREVIEW_BYTES);
                const rootMessageId = msg.threadRoot ?? msg.id;
                return (
                  <FileCard
                    key={att.id}
                    attachment={att}
                    onPreview={
                      previewable && onPreviewAttachment
                        ? () => onPreviewAttachment(att, rootMessageId)
                        : undefined
                    }
                    previewDisabledReason={
                      tooLarge ? t("preview.too-large-tooltip") : undefined
                    }
                  />
                );
              })}
            </div>
          )}
        </div>

        {/* Reaction bar: existing emoji pills, click to toggle the caller's
            reaction. Only rendered when the message has reactions and the
            caller wired the toggle handler. */}
        {onToggleReaction && (msg.reactions?.length ?? 0) > 0 && (
          <div className="flex flex-wrap items-center gap-1 px-0.5">
            {msg.reactions!.map((r) => (
              <button
                key={r.emoji}
                type="button"
                onClick={() => onToggleReaction(msg, r.emoji)}
                title={r.reactors.join(", ")}
                className={cn(
                  "flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs transition-colors cursor-pointer",
                  r.reacted
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-control-border text-control hover:border-accent hover:text-accent"
                )}
              >
                <span>{r.emoji}</span>
                <span>{r.count}</span>
              </button>
            ))}
          </div>
        )}

        {/* View details link */}
        {!isUser && msg.commandId && debugMode && (
          <button
            type="button"
            className="text-xs text-control-placeholder hover:text-accent px-0.5 cursor-pointer transition-colors"
            onClick={() => {
              if (msg.commandId)
                onViewDetails(msg.commandId, msg.agentId ?? "");
            }}
          >
            {t("chat.view-details")} &rarr;
          </button>
        )}

        {/* Thread entry (root messages only): "N replies · M new" above the
            latest replies once the channel's thread summary has synced; until
            then fall back to the bare count button. Clicking the entry opens
            the thread; preview rows jump to their exact reply. */}
        {onOpenThread &&
          !msg.threadRoot &&
          (msg.threadReplyCount ?? 0) > 0 &&
          (msg.threadPreview?.length ? (
            <ThreadPreviewBlock
              rootMsg={msg}
              replies={msg.threadPreview}
              newCount={msg.threadNewReplyCount ?? 0}
              onOpenThread={onOpenThread}
              onOpenThreadAt={onOpenThreadAt ?? onOpenThread}
              currentPrincipalId={currentPrincipalId}
            />
          ) : (
            <button
              type="button"
              onClick={() => onOpenThread(msg)}
              className="flex items-center gap-1.5 text-xs text-control-placeholder hover:text-accent px-0.5 cursor-pointer transition-colors"
            >
              <MessageCircleReply className="size-3" />
              <span>
                {t("chat.thread-replies", { count: msg.threadReplyCount ?? 0 })}
              </span>
            </button>
          ))}
      </div>
    </div>
  );

  if (!onCopyMarkdown) return row;

  return (
    <MessageContextMenu
      msg={msg}
      content={msg.content}
      onCopy={onCopyMarkdown}
      onOpenThread={onOpenThread}
      onConvertToTask={onConvertToTask}
      canOpenThread={!contextMenuRootOnly}
    >
      {row}
    </MessageContextMenu>
  );
});
