import { ArrowUp, ChevronRight, MessageCircleReply } from "lucide-react";
import MarkdownRender from "markstream-react";
import { memo, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { FileCard } from "@/components/chat/file-card";
import { splitByMentions } from "@/components/chat/mentions";
import { TaskStatusBadge } from "@/components/chat/task-status-badge";
import { ChatDiff } from "@/components/chat-events/diff-view";
import { ChatPermissionRequest } from "@/components/chat-events/permission-request";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { ChatWarning } from "@/components/chat-events/warning";
import { CommandStatusBadge } from "@/components/command-status-badge";
import {
  isMarkdownAttachment,
  MAX_MARKDOWN_PREVIEW_BYTES,
} from "@/lib/markdown-file";
import { cn } from "@/lib/utils";
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

// Pairs each TOOL_CALL_STARTED event with its matching TOOL_CALL_FINISHED
// event. Tool-call payloads carry no correlation id, so we pair by event
// order: each finished event closes the oldest still-open tool call (FIFO).
// Unlike index-based pairing, this stays correct when a started event has no
// finished yet (the tool call is still in flight). Exported for unit testing.
export function pairToolCallEvents(events: CommandEvent[]): {
  started: CommandEvent;
  finished?: CommandEvent;
}[] {
  const pairs: { started: CommandEvent; finished?: CommandEvent }[] = [];
  const pendingIndices: number[] = [];
  for (const event of events) {
    if (event.type === CommandEventType.TOOL_CALL_STARTED) {
      pendingIndices.push(pairs.length);
      pairs.push({ started: event });
    } else if (event.type === CommandEventType.TOOL_CALL_FINISHED) {
      const idx = pendingIndices.shift();
      if (idx !== undefined) pairs[idx].finished = event;
    }
  }
  return pairs;
}

export interface MessageRowProps {
  msg: ChatMessageUI;
  showAvatar: boolean;
  agentTitle: string;
  streamingContent: string;
  streamingEvents: CommandEvent[];
  onViewDetails: (commandId: string) => void;
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
  } = props;
  const { t } = useTranslation();
  const isUser = msg.role === "user";
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

  const MentionBadgeCmp = MentionBadge;

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
        isUser ? "flex-row-reverse" : "flex-row"
      )}
    >
      {/* Avatar */}
      <div className="flex shrink-0 flex-col items-center pt-0.5">
        {showAvatar ? (
          <Avatar label={isUser ? "U" : agentTitle || "A"} />
        ) : (
          <div className="size-8 shrink-0" />
        )}
      </div>

      {/* Message body */}
      <div
        className={cn(
          "flex min-w-0 flex-1 flex-col gap-1.5",
          isUser ? "items-end" : "items-start"
        )}
      >
        {/* Header */}
        {showAvatar && (
          <div className="flex items-center gap-2 px-0.5">
            <span className="text-xs font-medium text-control">
              {isUser
                ? t("chat.you")
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
            {onOpenThread && !msg.threadRoot && !isStreaming && (
              <button
                type="button"
                onClick={() => onOpenThread(msg)}
                // A task's thread is its workspace, so keep the entry visible on
                // task messages instead of hover-only; non-task roots stay hover-gated.
                className={cn(
                  "ml-1 flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-control-placeholder transition-all hover:bg-control-bg hover:text-main focus:opacity-100 cursor-pointer",
                  msg.task ? "opacity-100" : "opacity-0 group-hover:opacity-100"
                )}
                aria-label={t("chat.reply-in-thread")}
              >
                <MessageCircleReply className="size-3" />
                <span className="hidden sm:inline">
                  {t("chat.reply-in-thread")}
                </span>
              </button>
            )}
          </div>
        )}

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
            isUser
              ? "bg-control-bg/60 text-main rounded-tr-sm px-4 py-2.5 max-w-[80%]"
              : displayContent || isStreaming
                ? "bg-control-bg/60 text-main rounded-tl-sm px-4 py-3 max-w-[80%]"
                : "hidden"
          )}
        >
          {segments && MentionBadgeCmp ? (
            // Mention-aware rendering (channel chat): interleave plain text and
            // mention badges. User text stays pre-wrapped; agent text goes
            // through the markdown renderer.
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
                    customId={markdownCustomId}
                    content={seg.text}
                    final
                    fade
                  />
                </span>
              );
            })
          ) : isUser ? (
            <div className="whitespace-pre-wrap break-words">
              {displayContent || ""}
            </div>
          ) : displayContent ? (
            <div className="markstream-chat break-words">
              <MarkdownRender
                customId={markdownCustomId}
                content={displayContent}
                final={!isStreaming}
                smoothStreaming={isStreaming ? "auto" : false}
                fade={!isStreaming}
                typewriter={isStreaming}
                maxLiveNodes={isStreaming ? 0 : undefined}
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
        {!isUser && msg.commandId && !isStreaming && (
          <button
            type="button"
            className="text-xs text-control-placeholder hover:text-accent px-0.5 cursor-pointer transition-colors"
            onClick={() => {
              if (msg.commandId) onViewDetails(msg.commandId);
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
