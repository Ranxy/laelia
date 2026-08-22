import { create } from "@bufbuild/protobuf";
import { SearchX } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { SearchResultList } from "@/components/chat/search-result-list";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { Button } from "@/components/ui/button";
import { SearchInput } from "@/components/ui/search-input";
import { commandServiceClient } from "@/connect";
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
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const [results, setResults] =
    useState<SearchChatHistoryEntry[]>(EMPTY_RESULTS);
  const [nextPageToken, setNextPageToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [searched, setSearched] = useState(false);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults(EMPTY_RESULTS);
      setNextPageToken("");
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
          setNextPageToken(res.nextPageToken ?? "");
          setSearched(true);
        })
        .catch(() => {
          if (cancelled) return;
          setResults(EMPTY_RESULTS);
          setNextPageToken("");
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

  const handleOpen = (entry: SearchChatHistoryEntry) => {
    if (entry.message) onJumpToMessage(entry.message);
  };

  const loadMore = () => {
    const q = query.trim();
    if (!q || !nextPageToken || loadingMore) return;
    setLoadingMore(true);
    commandServiceClient
      .searchChatHistory(
        create(SearchChatHistoryRequestSchema, {
          conversation: `conversations/${channelId}`,
          query: q,
          limit: 50,
          pageToken: nextPageToken,
        })
      )
      .then((res) => {
        setResults((prev) => [...prev, ...(res.entries ?? EMPTY_RESULTS)]);
        setNextPageToken(res.nextPageToken ?? "");
      })
      .catch(() => {
        // Keep the current page; the user can retry the load-more button.
      })
      .finally(() => setLoadingMore(false));
  };

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
      <div className="flex flex-col gap-2 p-2">
        <SearchResultList
          entries={results}
          query={query}
          onOpen={handleOpen}
          compact
          threadLabel={t("channelSearch.thread")}
        />
        {nextPageToken && (
          <div className="flex justify-center pb-1">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={loadMore}
              disabled={loadingMore}
            >
              {t("channelSearch.load-more")}
            </Button>
          </div>
        )}
      </div>
    );
  }, [
    channelTitle,
    loading,
    loadingMore,
    nextPageToken,
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
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  );
}
