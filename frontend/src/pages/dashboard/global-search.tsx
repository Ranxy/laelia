import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { CalendarClock, Search, SearchX, User } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { SearchResultList } from "@/components/chat/search-result-list";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { commandServiceClient } from "@/connect";
import { useAppStore } from "@/stores";
import type { SearchChatHistoryEntry } from "@/types/proto-es/v1/command_pb";
import {
  SearchChatHistoryRequestSchema,
  SearchScope,
} from "@/types/proto-es/v1/command_pb";

const EMPTY_RESULTS: SearchChatHistoryEntry[] = [];

const TIME_RANGES = [
  { value: "any", hours: 0 },
  { value: "24h", hours: 24 },
  { value: "7d", hours: 24 * 7 },
  { value: "30d", hours: 24 * 30 },
] as const;

type TimeRange = (typeof TIME_RANGES)[number]["value"];

function timeLabelKey(value: string): string {
  return `globalSearch.time-${value || "any"}`;
}

function buildSearchRequest({
  query,
  from,
  scope,
  channel,
  timeRange,
  pageToken,
}: {
  query: string;
  from: string;
  scope: SearchScope;
  channel: string;
  timeRange: TimeRange;
  pageToken?: string;
}) {
  const range = TIME_RANGES.find((r) => r.value === timeRange);
  const since =
    range && range.hours > 0
      ? create(TimestampSchema, {
          seconds: BigInt(Math.floor(Date.now() / 1000) - range.hours * 3600),
        })
      : undefined;
  return create(SearchChatHistoryRequestSchema, {
    query,
    from: from.trim() || "",
    scope,
    conversation: channel || "",
    since,
    limit: 50,
    pageToken: pageToken || "",
  });
}

// GlobalSearchPage searches every conversation the current user can read:
// message content (main channel and thread replies) plus attachment file
// names. Results link back into the channel chat at the exact message.
export function GlobalSearchPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const myChannels = useAppStore((s) => s.myChannels);
  const fetchChannels = useAppStore((s) => s.fetchChannels);

  useEffect(() => {
    void fetchChannels();
  }, [fetchChannels]);

  const [query, setQuery] = useState("");
  const [from, setFrom] = useState("");
  const [scope, setScope] = useState<SearchScope>(SearchScope.UNSPECIFIED);
  const [channel, setChannel] = useState("");
  const [timeRange, setTimeRange] = useState<TimeRange>("any");

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
          buildSearchRequest({ query: q, from, scope, channel, timeRange })
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
  }, [channel, from, query, scope, timeRange]);

  const handleOpen = (entry: SearchChatHistoryEntry) => {
    const msg = entry.message;
    if (!msg?.conversation) return;
    const params = new URLSearchParams();
    if (msg.threadRoot) {
      params.set("thread", msg.threadRoot);
      params.set("message", msg.name);
    } else {
      params.set("message", msg.name);
      params.set("version", String(msg.roomVersion));
    }
    navigate(`/${msg.conversation}?${params.toString()}`);
  };

  const loadMore = () => {
    const q = query.trim();
    if (!q || !nextPageToken || loadingMore) return;
    setLoadingMore(true);
    commandServiceClient
      .searchChatHistory(
        buildSearchRequest({
          query: q,
          from,
          scope,
          channel,
          timeRange,
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
          icon={Search}
          message={t("globalSearch.empty")}
          className="py-32"
        />
      );
    }
    if (searched && results.length === 0) {
      return (
        <EmptyState
          icon={SearchX}
          message={t("globalSearch.no-results", { query: query.trim() })}
          className="py-32"
        />
      );
    }
    return (
      <div className="flex w-full flex-col gap-3 px-4 py-3">
        <SearchResultList
          entries={results}
          query={query}
          onOpen={handleOpen}
          threadLabel={t("globalSearch.thread")}
        />
        {nextPageToken && (
          <div className="flex justify-center pb-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={loadMore}
              disabled={loadingMore}
            >
              {t("globalSearch.load-more")}
            </Button>
          </div>
        )}
      </div>
    );
  }, [loading, loadingMore, nextPageToken, query, results, searched, t]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Top search bar */}
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center gap-2 rounded-md border border-control-border bg-background px-3">
          <Search className="size-4 shrink-0 text-control-placeholder" />
          <Input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("globalSearch.placeholder")}
            className="h-10 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
          />
          <button
            type="button"
            onClick={() => setQuery("")}
            className="shrink-0 rounded border border-control-border px-1.5 py-0.5 text-[10px] text-control-light transition-colors hover:bg-control-bg hover:text-main"
          >
            ESC
          </button>
        </div>
      </div>

      {/* Filter toolbar */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-control-border px-4 py-2">
        <div className="flex items-center gap-1.5 rounded-md border border-control-border px-2 py-1">
          <User className="size-3.5 text-control-light" />
          <Input
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            placeholder={t("globalSearch.from")}
            className="h-6 w-32 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0"
          />
        </div>

        <Select
          value={String(scope)}
          onValueChange={(v) => setScope(Number(v) as SearchScope)}
        >
          <SelectTrigger size="sm" className="gap-1">
            <SelectValue>
              {(value) =>
                Number(value) === SearchScope.MESSAGES
                  ? t("globalSearch.scope-messages")
                  : Number(value) === SearchScope.FILES
                    ? t("globalSearch.scope-files")
                    : t("globalSearch.scope-all")
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={String(SearchScope.UNSPECIFIED)}>
              {t("globalSearch.scope-all")}
            </SelectItem>
            <SelectItem value={String(SearchScope.MESSAGES)}>
              {t("globalSearch.scope-messages")}
            </SelectItem>
            <SelectItem value={String(SearchScope.FILES)}>
              {t("globalSearch.scope-files")}
            </SelectItem>
          </SelectContent>
        </Select>

        <Select value={channel} onValueChange={(v) => setChannel(v ?? "")}>
          <SelectTrigger size="sm" className="max-w-48">
            <SelectValue>
              {(value) =>
                value
                  ? (myChannels.find((c) => c.name === value)?.title ??
                    t("globalSearch.channel"))
                  : t("globalSearch.all-channels")
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">{t("globalSearch.all-channels")}</SelectItem>
            {myChannels.map((c) => (
              <SelectItem key={c.name} value={c.name ?? ""}>
                {c.title || c.address || c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select
          value={timeRange}
          onValueChange={(v) => setTimeRange((v ?? "any") as TimeRange)}
        >
          <SelectTrigger size="sm" className="gap-1">
            <CalendarClock className="size-3.5 text-control-light" />
            <SelectValue>
              {(value) => t(timeLabelKey(String(value)))}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {TIME_RANGES.map((r) => (
              <SelectItem key={r.value} value={r.value}>
                {t(timeLabelKey(r.value))}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Results / empty state */}
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  );
}
