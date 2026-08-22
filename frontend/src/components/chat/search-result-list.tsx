import { timestampDate } from "@bufbuild/protobuf/wkt";
import { CornerDownRight, MessageSquare, Paperclip } from "lucide-react";
import { memo, useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { formatTime } from "@/components/chat/avatar";
import { cn } from "@/lib/utils";
import type {
  ChatMessage,
  SearchChatHistoryEntry,
} from "@/types/proto-es/v1/command_pb";
import { SenderType } from "@/types/proto-es/v1/command_pb";

// SearchResultList renders search hits as a single-column flow of cards. A
// normal hit is one card; a thread-reply hit is grouped under its root message
// so the reader sees the context the reply is answering. The root is shown
// above the indented, clickable replies and is itself not clickable.
export interface SearchResultListProps {
  entries: SearchChatHistoryEntry[];
  query: string;
  onOpen: (entry: SearchChatHistoryEntry) => void;
  className?: string;
  // compact reduces padding for narrow panels (e.g. the channel search panel).
  compact?: boolean;
  // threadLabel is the localized label for the THREAD tag.
  threadLabel?: string;
}

type SearchGroup =
  | { kind: "single"; entry: SearchChatHistoryEntry }
  | { kind: "thread"; root: ChatMessage; replies: SearchChatHistoryEntry[] };

function groupEntries(entries: SearchChatHistoryEntry[]): SearchGroup[] {
  const groups: SearchGroup[] = [];
  const threadIndex = new Map<string, number>();
  for (const entry of entries) {
    const root = entry.threadContext?.root;
    if (root?.name) {
      const idx = threadIndex.get(root.name);
      if (idx !== undefined) {
        const group = groups[idx];
        if (group.kind === "thread") group.replies.push(entry);
      } else {
        threadIndex.set(root.name, groups.length);
        groups.push({ kind: "thread", root, replies: [entry] });
      }
    } else {
      groups.push({ kind: "single", entry });
    }
  }
  return groups;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// SearchHighlight marks every query term in the displayed text with a
// background + black text, case-insensitively. Longer terms are matched first
// so overlapping terms (e.g. "foo" and "foobar") highlight the longer one.
const SearchHighlight = memo(function SearchHighlight({
  text,
  query,
}: {
  text: string;
  query: string;
}) {
  const parts = useMemo(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const unique = [...new Set(terms)].sort((a, b) => b.length - a.length);
    if (unique.length === 0 || !text) return [{ text, match: false }];
    const re = new RegExp(`(${unique.map(escapeRegExp).join("|")})`, "gi");
    return text.split(re).map((part, index) => ({
      text: part,
      match: index % 2 === 1,
    }));
  }, [query, text]);

  return (
    <>
      {parts.map((part, index) =>
        part.match ? (
          <mark
            key={index}
            className="rounded-sm bg-yellow-200 px-0.5 text-black"
          >
            {part.text}
          </mark>
        ) : (
          <span key={index}>{part.text}</span>
        )
      )}
    </>
  );
});

function channelLabel(entry: SearchChatHistoryEntry): string {
  const conv = entry.conversation;
  return conv?.address || conv?.title || entry.message?.conversation || "";
}

function senderLabel(msg: ChatMessage): string {
  const name = msg.senderName?.trim();
  if (msg.senderType === SenderType.USER) {
    return msg.principalId?.trim() || name || "";
  }
  return name || msg.principalId?.trim() || "";
}

function displayText(entry: SearchChatHistoryEntry): string {
  return (
    entry.matchedAttachmentName || entry.snippet || entry.message?.content || ""
  );
}

function TypeIcon({ entry }: { entry: SearchChatHistoryEntry }) {
  if (entry.matchField === 2) {
    return <Paperclip className="size-3.5 shrink-0" />;
  }
  return <MessageSquare className="size-3.5 shrink-0" />;
}

function MetaTag({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex max-w-48 items-center gap-1 truncate rounded bg-control-bg px-1.5 py-0.5 text-[11px] leading-4 text-control-light",
        className
      )}
    >
      {children}
    </span>
  );
}

function SingleCard({
  entry,
  query,
  onOpen,
  compact,
}: {
  entry: SearchChatHistoryEntry;
  query: string;
  onOpen: (entry: SearchChatHistoryEntry) => void;
  compact?: boolean;
}) {
  const { i18n } = useTranslation();
  const msg = entry.message;
  if (!msg) return null;
  const sender = senderLabel(msg);
  const channel = channelLabel(entry);
  const time = msg.createdAt
    ? formatTime(timestampDate(msg.createdAt), i18n.language)
    : "";

  return (
    <button
      type="button"
      onClick={() => onOpen(entry)}
      className={cn(
        "block w-full rounded-lg border border-control-border bg-background text-left transition-colors hover:bg-control-bg",
        compact ? "px-2.5 py-2" : "px-3 py-2.5"
      )}
    >
      <span className="flex items-center gap-1.5 text-xs text-control-light">
        <TypeIcon entry={entry} />
        {channel && <MetaTag>{channel}</MetaTag>}
        {sender && <MetaTag>@{sender}</MetaTag>}
        {time && <span className="ml-auto shrink-0">{time}</span>}
      </span>
      <span className="mt-1.5 line-clamp-2 block break-words text-sm text-main">
        <SearchHighlight text={displayText(entry)} query={query} />
      </span>
    </button>
  );
}

function ThreadCard({
  root,
  replies,
  query,
  onOpen,
  compact,
  threadLabel,
}: {
  root: ChatMessage;
  replies: SearchChatHistoryEntry[];
  query: string;
  onOpen: (entry: SearchChatHistoryEntry) => void;
  compact?: boolean;
  threadLabel?: string;
}) {
  const { i18n } = useTranslation();
  const first = replies[0];
  const rootSender = senderLabel(root);
  const rootTime = root.createdAt
    ? formatTime(timestampDate(root.createdAt), i18n.language)
    : "";

  return (
    <div className="rounded-lg border border-control-border bg-background">
      {/* Root message: context only, not clickable. */}
      <div className={cn(compact ? "px-2.5 py-2" : "px-3 py-2.5")}>
        <div className="flex items-center gap-1.5 text-xs text-control-light">
          <MessageSquare className="size-3.5 shrink-0" />
          {first && channelLabel(first) && (
            <MetaTag>{channelLabel(first)}</MetaTag>
          )}
          <MetaTag className="uppercase">{threadLabel || "Thread"}</MetaTag>
          {rootSender && <MetaTag>@{rootSender}</MetaTag>}
          {rootTime && <span className="ml-auto shrink-0">{rootTime}</span>}
        </div>
        <p className="mt-1.5 line-clamp-2 break-words text-sm text-control">
          {root.content}
        </p>
      </div>

      {/* Matched replies, indented under the root. */}
      <div
        className={cn(
          "ml-3 border-l border-control-border",
          compact ? "pb-1.5 pl-2.5 pr-1.5" : "pb-2 pl-3 pr-2"
        )}
      >
        {replies.map((entry) => {
          const msg = entry.message;
          if (!msg) return null;
          const sender = senderLabel(msg);
          const time = msg.createdAt
            ? formatTime(timestampDate(msg.createdAt), i18n.language)
            : "";
          return (
            <button
              key={msg.name}
              type="button"
              onClick={() => onOpen(entry)}
              className={cn(
                "mt-1.5 block w-full rounded-md border border-control-border bg-background text-left transition-colors hover:bg-control-bg",
                compact ? "px-2 py-1.5" : "px-2.5 py-2"
              )}
            >
              <span className="flex items-center gap-1.5 text-xs text-control-light">
                <CornerDownRight className="size-3.5 shrink-0" />
                {sender && <MetaTag>@{sender}</MetaTag>}
                {time && <span className="ml-auto shrink-0">{time}</span>}
              </span>
              <span className="mt-1 line-clamp-2 block break-words text-sm text-main">
                <SearchHighlight text={displayText(entry)} query={query} />
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

export function SearchResultList({
  entries,
  query,
  onOpen,
  className,
  compact,
  threadLabel,
}: SearchResultListProps) {
  const groups = useMemo(() => groupEntries(entries), [entries]);

  return (
    <div className={cn("flex w-full flex-col gap-2", className)}>
      {groups.map((group) => {
        if (group.kind === "single") {
          const key =
            group.entry.message?.name ||
            group.entry.matchedAttachmentName ||
            "single";
          return (
            <SingleCard
              key={key}
              entry={group.entry}
              query={query}
              onOpen={onOpen}
              compact={compact}
            />
          );
        }
        return (
          <ThreadCard
            key={group.root.name}
            root={group.root}
            replies={group.replies}
            query={query}
            onOpen={onOpen}
            compact={compact}
            threadLabel={threadLabel}
          />
        );
      })}
    </div>
  );
}
