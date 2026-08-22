import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { CornerDownRight, FileText, SearchX } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatTime } from "@/components/chat/avatar";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { SearchInput } from "@/components/ui/search-input";
import { commandServiceClient } from "@/connect";
import { cn } from "@/lib/utils";
import type {
  ChatMessage,
  SearchChatHistoryEntry,
} from "@/types/proto-es/v1/command_pb";
import { SearchChatHistoryRequestSchema } from "@/types/proto-es/v1/command_pb";

export interface ChannelSearchPanelProps {
  channelId: string;
  channelTitle: string;
  onClose: () => void;
  onJumpToMessage: (message: ChatMessage) => void;
}

const EMPTY_RESULTS: SearchChatHistoryEntry[] = [];

// ChannelSearchPanel searches one conversation's messages (main channel and
// thread replies) plus attachment file names. Clicking a result jumps to the
// message in the channel (or opens its thread for replies).
export function ChannelSearchPanel({
  channelId,
  channelTitle,
  onClose,
  onJumpToMessage,
}: ChannelSearchPanelProps) {
  const { t, i18n } = useTranslation();
  const [query, setQuery] = useState("");
  const [results, setResults] =
    useState<SearchChatHistoryEntry[]>(EMPTY_RESULTS);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults(EMPTY_RESULTS);
      setSearched(false);
      setLoading(false);
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      setLoading(true);
      commandServiceClient
        .searchChatHistory(
          create(SearchChatHistoryRequestSchema, {
            conversation: `conversations/${channelId}`,
            query: q,
            limit: 50,
          })
        )
        .then((res) => {
          if (cancelled) return;
          setResults(res.entries ?? EMPTY_RESULTS);
          setSearched(true);
        })
        .catch(() => {
          if (cancelled) return;
          setResults(EMPTY_RESULTS);
          setSearched(true);
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [channelId, query]);

  const body = useMemo(() => {
    if (loading) return <LoadingState />;
    if (!query.trim()) {
      return (
        <EmptyState
          icon={SearchX}
          message={t("channelSearch.empty", { channel: channelTitle })}
        />
      );
    }
    if (searched && results.length === 0) {
      return (
        <EmptyState
          icon={SearchX}
          message={t("channelSearch.no-results", { query: query.trim() })}
        />
      );
    }
    return (
      <div className="flex flex-col gap-1 overflow-y-auto p-2">
        {results.map((entry) => {
          const msg = entry.message;
          if (!msg) return null;
          const isReply = !!msg.threadRoot;
          const title =
            entry.matchedAttachmentName ||
            entry.snippet ||
            msg.content ||
            t("channelSearch.untitled");
          return (
            <button
              key={msg.name}
              type="button"
              onClick={() => onJumpToMessage(msg)}
              className="flex flex-col gap-1 rounded-md px-3 py-2 text-left transition-colors hover:bg-control-bg"
            >
              <span className="flex items-center gap-1.5 text-xs text-control-light">
                {isReply ? (
                  <CornerDownRight className="size-3.5" />
                ) : (
                  <FileText className="size-3.5" />
                )}
                <span className="truncate">{msg.senderName}</span>
                <span aria-hidden>·</span>
                <span className="shrink-0">
                  {msg.createdAt
                    ? formatTime(timestampDate(msg.createdAt), i18n.language)
                    : ""}
                </span>
              </span>
              <span className="line-clamp-2 text-sm text-main">{title}</span>
              {isReply && (
                <span className="text-xs text-control-light">
                  {t("channelSearch.in-thread")}
                </span>
              )}
            </button>
          );
        })}
      </div>
    );
  }, [
    channelTitle,
    i18n.language,
    loading,
    onJumpToMessage,
    query,
    results,
    searched,
    t,
  ]);

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2">
        <SearchInput
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("channelSearch.placeholder")}
          wrapperClassName="flex-1"
        />
        <button
          type="button"
          onClick={onClose}
          className="rounded-md px-2 py-1 text-xs text-control-light transition-colors hover:bg-control-bg hover:text-main"
        >
          {t("common.close")}
        </button>
      </div>
      <div
        className={cn(
          "min-h-0 flex-1",
          results.length > 0 && "overflow-hidden"
        )}
      >
        {body}
      </div>
    </div>
  );
}
