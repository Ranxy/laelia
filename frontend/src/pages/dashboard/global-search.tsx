import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  CalendarClock,
  ChevronDown,
  Search,
  SearchX,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { useEffect, useState } from "react";
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
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type {
  Conversation,
  SearchChatHistoryEntry,
} from "@/types/proto-es/v1/command_pb";
import {
  ListChannelsRequestSchema,
  SearchChatHistoryRequestSchema,
  SearchScope,
} from "@/types/proto-es/v1/command_pb";
import { ConversationPicker } from "./conversation-picker";
import type { FromSender } from "./from-sender-picker";
import { FromSenderPicker } from "./from-sender-picker";

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

// GlobalSearchPage searches every conversation the current user participates
// in: message content (main channel and thread replies) plus attachment file
// names. Results link back into the channel chat at the exact message.
export function GlobalSearchPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isDesktop = useIsDesktop();

  // The search filter should list every conversation the current user
  // participates in (channels, user DMs and agent DMs), including closed ones
  // that are hidden from the left rail. ListChannels already returns those
  // with includeClosed=true, so we fetch and paginate the full set here.
  const [conversations, setConversations] = useState<Conversation[]>(
    () => useAppStore.getState().channels
  );
  const [conversationsLoading, setConversationsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function loadConversations() {
      setConversationsLoading(true);
      const all: Conversation[] = [];
      let pageToken = "";
      try {
        do {
          const res = await commandServiceClient.listChannels(
            create(ListChannelsRequestSchema, {
              pageSize: 100,
              pageToken,
              includeClosed: true,
            })
          );
          all.push(...(res.channels ?? []));
          pageToken = res.nextPageToken ?? "";
        } while (pageToken);
        if (!cancelled) setConversations(all);
      } catch {
        // Keep whatever was loaded before; the picker will show an empty list.
      } finally {
        if (!cancelled) setConversationsLoading(false);
      }
    }
    void loadConversations();
    return () => {
      cancelled = true;
    };
  }, []);

  const [query, setQuery] = useState("");
  const [fromSender, setFromSender] = useState<FromSender | null>(null);
  const [scope, setScope] = useState<SearchScope>(SearchScope.UNSPECIFIED);
  const [channel, setChannel] = useState("");
  const [timeRange, setTimeRange] = useState<TimeRange>("any");
  const [filtersOpen, setFiltersOpen] = useState(false);

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
          buildSearchRequest({
            query: q,
            from: fromSender
              ? fromSender.kind === "user"
                ? fromSender.user.handle
                : fromSender.agent.handle
              : "",
            scope,
            channel,
            timeRange,
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
  }, [channel, fromSender, query, scope, timeRange]);

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
          from: fromSender
            ? fromSender.kind === "user"
              ? fromSender.user.handle
              : fromSender.agent.handle
            : "",
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

  // Plain render body: it captures per-render handlers (handleOpen/loadMore),
  // so a useMemo would need both as deps and could never bail out.
  const body = loading ? (
    <LoadingState />
  ) : !query.trim() ? (
    <EmptyState
      icon={Search}
      message={t("globalSearch.empty")}
      className="py-32"
    />
  ) : searched && results.length === 0 ? (
    <EmptyState
      icon={SearchX}
      message={t("globalSearch.no-results", { query: query.trim() })}
      className="py-32"
    />
  ) : (
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
            className="h-11 w-full touch-manipulation sm:h-7 sm:w-auto"
          >
            {t("globalSearch.load-more")}
          </Button>
        </div>
      )}
    </div>
  );

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
            className="h-10 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0 lg:h-11"
          />
          {query && (
            <button
              type="button"
              onClick={() => setQuery("")}
              aria-label={t("globalSearch.clear")}
              className="shrink-0 rounded p-1.5 text-control-light transition-colors hover:bg-control-bg hover:text-main lg:hidden"
            >
              <X className="size-4" />
            </button>
          )}
          <button
            type="button"
            onClick={() => setQuery("")}
            className="hidden shrink-0 rounded border border-control-border px-1.5 py-0.5 text-[10px] text-control-light transition-colors hover:bg-control-bg hover:text-main lg:inline-flex"
          >
            ESC
          </button>
        </div>
      </div>

      {isDesktop ? (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-control-border px-4 py-2">
          <FromSenderPicker
            value={fromSender}
            onChange={setFromSender}
            placeholder={t("globalSearch.from")}
          />

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

          <ConversationPicker
            value={channel}
            onChange={setChannel}
            conversations={conversations}
            loading={conversationsLoading}
            placeholder={t("globalSearch.all-channels")}
          />

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
      ) : (
        <div className="shrink-0 border-b border-control-border">
          <button
            type="button"
            onClick={() => setFiltersOpen((v) => !v)}
            aria-expanded={filtersOpen}
            className="flex w-full items-center gap-2 px-4 py-3 text-sm text-control transition-colors hover:bg-control-bg"
          >
            <SlidersHorizontal className="size-4 shrink-0 text-control-light" />
            <span>{t("globalSearch.filters")}</span>
            <ChevronDown
              className={cn(
                "ml-auto size-4 shrink-0 text-control-light transition-transform",
                filtersOpen && "rotate-180"
              )}
            />
          </button>
          {filtersOpen && (
            <div className="flex flex-col gap-3 px-4 pb-3">
              <FromSenderPicker
                fullWidth
                value={fromSender}
                onChange={setFromSender}
                placeholder={t("globalSearch.from")}
              />

              <Select
                value={String(scope)}
                onValueChange={(v) => setScope(Number(v) as SearchScope)}
              >
                <SelectTrigger size="md" className="w-full">
                  <SelectValue>
                    {(value) =>
                      value
                        ? Number(value) === SearchScope.MESSAGES
                          ? t("globalSearch.scope-messages")
                          : Number(value) === SearchScope.FILES
                            ? t("globalSearch.scope-files")
                            : t("globalSearch.scope-all")
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

              <ConversationPicker
                fullWidth
                value={channel}
                onChange={setChannel}
                conversations={conversations}
                loading={conversationsLoading}
                placeholder={t("globalSearch.all-channels")}
              />

              <Select
                value={timeRange}
                onValueChange={(v) => setTimeRange((v ?? "any") as TimeRange)}
              >
                <SelectTrigger size="md" className="w-full">
                  <CalendarClock className="size-4 shrink-0 text-control-light" />
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
          )}
        </div>
      )}

      {/* Results / empty state */}
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </div>
  );
}
