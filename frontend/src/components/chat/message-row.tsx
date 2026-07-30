import { ArrowUp, ChevronRight, MessageCircleReply } from "lucide-react";
import MarkdownRender from "markstream-react";
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
import { Avatar, formatTime } from "@/components/chat/avatar";
import { FileCard } from "@/components/chat/file-card";
import { LazyMarkdown } from "@/components/chat/lazy-markdown";
import {
  contentWithMentionTags,
  splitByMentions,
} from "@/components/chat/mentions";
import { RemoteImage } from "@/components/chat/remote-image";
import { TaskStatusBadge } from "@/components/chat/task-status-badge";
import { ChatDiff } from "@/components/chat-events/diff-view";
import { ChatPermissionRequest } from "@/components/chat-events/permission-request";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { ChatWarning } from "@/components/chat-events/warning";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { AttachmentCommentCard } from "@/components/preview/attachment-comment-card";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { isImageAttachment } from "@/lib/image-file";
import {
  isMarkdownAttachment,
  MAX_MARKDOWN_PREVIEW_BYTES,
} from "@/lib/markdown-file";
import { cn } from "@/lib/utils";
import { isOwnUserMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment, CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType, SenderType } from "@/types/proto-es/v1/command_pb";

// Stable empty fallback so selectors returning `undefined` for an unloaded
// conversation don't create a new array literal each run (which would defeat
// zustand's Object.is equality and re-render on every store change). Exported
// so non-streaming consumers (channel chat) can pass a stable empty slice.
const EMPTY_EVENTS: CommandEvent[] = [];

export { EMPTY_EVENTS };

// Computes the streaming props for a single row. Only the row that is
// actively streaming (`msg.streaming` with a bound commandName) receives the
// live streamingContent/streamingEvents slices; every other row gets stable
// empty / own-event values so React.memo skips it when the parent re-renders
// on each streamed token. Exported for unit testing.
export function rowStreamingProps(
  msg: ChatMessageUI,
  isStreamingRow: boolean,
  streamingContent: string,
  streamingEvents: CommandEvent[]
): { streamingContent: string; streamingEvents: CommandEvent[] } {
  return {
    streamingContent: isStreamingRow ? streamingContent : "",
    streamingEvents: isStreamingRow
      ? streamingEvents
      : (msg.events ?? EMPTY_EVENTS),
  };
}

// Re-exported from the shared util so existing imports (incl. tests) keep
// working while command-detail and chat share a single implementation.
import { pairToolCallEvents } from "@/lib/tool-call-events";

export { pairToolCallEvents };

export interface MessageRowProps {
  msg: ChatMessageUI;
  showAvatar: boolean;
  agentTitle: string;
  streamingContent: string;
  streamingEvents: CommandEvent[];
  onViewDetails: (commandId: string, agentId: string) => void;
  // Optional mention-aware rendering (channel chat). When provided, the row
  // renders @mentions as badges and lets the caller react to clicks.
  onMentionClick?: (type: string, id: string, name: string) => void;
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
}

export const MessageRow = memo(function MessageRow(props: MessageRowProps) {
  const {
    msg,
    showAvatar,
    agentTitle,
    streamingContent,
    streamingEvents,
    onViewDetails,
    onMentionClick,
    MentionBadge,
    markdownCustomId,
    onOpenThread,
    onPreviewAttachment,
    onJumpToSection,
    onPreviewImage,
    debugMode,
    currentPrincipalId,
    scrollRoot,
    eager = false,
  } = props;
  const { t } = useTranslation();
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

  const isStreaming = msg.streaming;
  const displayContent = isStreaming ? streamingContent : msg.content;
  const events = isStreaming ? streamingEvents : (msg.events ?? EMPTY_EVENTS);

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

  const hasEvents =
    !isUser &&
    (toolCallPairs.length > 0 ||
      diffEvents.length > 0 ||
      warningEvents.length > 0);

  const [eventsCollapsed, setEventsCollapsed] = useState(false);
  const prevStreamingRef = useRef(isStreaming);
  useEffect(() => {
    if (prevStreamingRef.current && !isStreaming && displayContent) {
      setEventsCollapsed(true);
    }
    prevStreamingRef.current = isStreaming;
  }, [isStreaming, displayContent]);

  // fade gates markstream-react's node fade-in on the streaming→final transition
  // that happens *within this mount*, not on "this is a final message." A row
  // that mounts already-final (history from the server) would otherwise replay
  // the 280ms opacity fade on every channel entry — and because rows are keyed
  // by msg.id they remount on every switch, so the flash recurred each visit.
  // The sticky ref records whether this row was ever streaming in its lifetime:
  // historical rows start false and stay false (no fade); rows that mounted
  // streaming stay true after finalizing, so the finalize fade still plays once.
  const wasStreamingRef = useRef(isStreaming);
  if (isStreaming) wasStreamingRef.current = true;
  const fade = wasStreamingRef.current && !isStreaming;

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

  // Agent markdown with @mentions rewritten to inline <mention> nodes, so a
  // mention flows inline with the surrounding prose instead of landing on its
  // own line (which happened when each text segment was rendered through its
  // own block-emitting MarkdownRender). Only computed for the mention-aware path.
  const agentMentionContent = useMemo(
    () =>
      MentionBadge && !isUser
        ? contentWithMentionTags(displayContent ?? "", msg.mentions ?? [])
        : null,
    [MentionBadge, isUser, displayContent, msg.mentions]
  );

  const MentionBadgeCmp = MentionBadge;

  // Delegated click handler for mention chips rendered inside agent markdown.
  // The custom <mention> node renders a span carrying {type, id, name} as
  // data-* attributes (see lib/markdown); recover them and dispatch
  // onMentionClick. Kept as a stable callback so the bubble div isn't
  // re-attached on every render.
  const handleBubbleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (!onMentionClick) return;
      const target = e.target as HTMLElement | null;
      const chip = target?.closest?.("[data-mtype]");
      if (!chip) return;
      const type = chip.getAttribute("data-mtype");
      const id = chip.getAttribute("data-mid");
      const name = chip.getAttribute("data-mname");
      if (!type || !id || !name) return;
      onMentionClick(type, id, name);
    },
    [onMentionClick]
  );

  // "Reply in thread" entry. Rendered in the header when the header is shown
  // (showAvatar), and as a standalone hover row otherwise — so every root
  // message in a consecutive group exposes the action, not just the first.
  // (Consecutive messages from the same sender skip the header to group the
  // bubble, which previously swallowed this button along with it.)
  const renderReplyInThread = () =>
    onOpenThread && !msg.threadRoot && !isStreaming ? (
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

  return (
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
          <Avatar
            seed={avatarSeed}
            src={avatarSrc}
            accent={isUser ? isOwnUser : false}
          />
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
            <span className="text-xs font-medium text-control">
              {isUser
                ? isOwnUser
                  ? t("chat.you")
                  : msg.senderName || t("chat.you")
                : agentTitle || msg.senderName || t("chat.agent")}
            </span>
            <span className="text-xs text-control-placeholder">
              {formatTime(msg.timestamp)}
            </span>
            {!isUser && msg.status !== undefined && !isStreaming && (
              <CommandStatusBadge
                status={msg.status}
                className="text-[10px] px-1.5 py-0"
              />
            )}
            {msg.task && !isStreaming && (
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
            {permissionEvent && !isPermissionDecided && msg.commandName && (
              <ChatPermissionRequest
                event={permissionEvent}
                commandName={msg.commandName}
              />
            )}
            {!isStreaming && (
              <button
                type="button"
                onClick={() => setEventsCollapsed(true)}
                className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent cursor-pointer transition-colors self-start"
              >
                <ArrowUp className="size-3" />
                {t("command.collapse")}
              </button>
            )}
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
              : displayContent || isStreaming
                ? "bg-control-bg/60 text-main rounded-tl-sm px-4 py-3 max-w-[80%]"
                : "hidden"
          )}
          onClick={handleBubbleClick}
        >
          {segments && MentionBadgeCmp ? (
            // Mention-aware rendering (channel chat / threads).
            isUser ? (
              // User text is plain (no markdown): interleave pre-wrapped text
              // spans with real MentionBadge chips. Both are inline-level, so
              // they already flow inline correctly.
              segments.length > 0 &&
              segments.map((seg, i) => {
                const mention = seg.mention;
                if (mention) {
                  return (
                    <MentionBadgeCmp
                      key={`${i}-${mention.name}`}
                      name={mention.name}
                      onClick={() =>
                        onMentionClick?.(mention.type, mention.id, mention.name)
                      }
                    />
                  );
                }
                if (!seg.text) return null;
                return (
                  <span key={i} className="whitespace-pre-wrap break-words">
                    {seg.text}
                  </span>
                );
              })
            ) : (
              // Agent: render the whole body in a single markdown pass with
              // @mentions rewritten to inline links (agentMentionContent). A
              // single MarkdownRender keeps the mention inside the same <p> as
              // the surrounding prose, so it flows inline instead of being
              // forced onto its own line by per-segment block <p> wrappers.
              <div className="markstream-chat break-words">
                <LazyMarkdown
                  eager={isStreaming || eager}
                  scrollRoot={scrollRoot}
                  fallback={
                    <span className="whitespace-pre-wrap break-words">
                      {displayContent}
                    </span>
                  }
                  render={() => (
                    <MarkdownRender
                      customId={markdownCustomId}
                      content={agentMentionContent ?? ""}
                      customHtmlTags={["mention"]}
                      final={!isStreaming}
                      smoothStreaming={isStreaming ? "auto" : false}
                      fade={fade}
                      typewriter={isStreaming}
                      maxLiveNodes={isStreaming ? 0 : undefined}
                    />
                  )}
                />
              </div>
            )
          ) : isUser ? (
            <div className="whitespace-pre-wrap break-words">
              {displayContent || ""}
            </div>
          ) : displayContent ? (
            <div className="markstream-chat break-words">
              <LazyMarkdown
                eager={isStreaming || eager}
                scrollRoot={scrollRoot}
                fallback={
                  <span className="whitespace-pre-wrap break-words">
                    {displayContent}
                  </span>
                }
                render={() => (
                  <MarkdownRender
                    customId={markdownCustomId}
                    content={displayContent}
                    final={!isStreaming}
                    smoothStreaming={isStreaming ? "auto" : false}
                    fade={fade}
                    typewriter={isStreaming}
                    maxLiveNodes={isStreaming ? 0 : undefined}
                  />
                )}
              />
            </div>
          ) : isStreaming ? (
            <div className="flex items-center gap-2 text-control-light text-xs py-1">
              <span className="flex gap-1">
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:0ms]" />
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:150ms]" />
                <span className="size-1.5 rounded-full bg-control-light/60 animate-bounce [animation-delay:300ms]" />
              </span>
            </div>
          ) : null}
          {msg.attachments && msg.attachments.length > 0 && (
            <div className="flex flex-col gap-1">
              {msg.attachments.map((att) => {
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
                const previewable = isMarkdownAttachment(att);
                const tooLarge =
                  previewable &&
                  (att.sizeBytes ?? 0n) > MAX_MARKDOWN_PREVIEW_BYTES;
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

        {/* View details link */}
        {!isUser && msg.commandId && !isStreaming && debugMode && (
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

        {/* Thread reply count + open-thread entry (root messages only). */}
        {onOpenThread &&
          !msg.threadRoot &&
          (msg.threadReplyCount ?? 0) > 0 &&
          !isStreaming && (
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
          )}
      </div>
    </div>
  );
});
