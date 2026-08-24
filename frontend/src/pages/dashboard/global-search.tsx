import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import {
  CalendarClock,
  ChevronDown,
  Loader2,
  Search,
  SearchX,
  SlidersHorizontal,
  User as UserIcon,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
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
import { commandServiceClient, userServiceClient } from "@/connect";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { buildUserFilter } from "@/lib/user-filter";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import type { SearchChatHistoryEntry } from "@/types/proto-es/v1/command_pb";
import {
  SearchChatHistoryRequestSchema,
  SearchScope,
} from "@/types/proto-es/v1/command_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

const EMPTY_RESULTS: SearchChatHistoryEntry[] = [];

const TIME_RANGES = [
  { value: "any", hours: 0 },
  { value: "24h", hours: 24 },
  { value: "7d", hours: 24 * 7 },
  { value: "30d", hours: 24 * 30 },
] as const;

type TimeRange = (typeof TIME_RANGES)[number]["value"];

// FromSender is a selected "From" filter value: either a human user or an
// agent. The backend accepts both handles in SearchChatHistoryRequest.from.
type FromSender =
  | { kind: "user"; user: User }
  | { kind: "agent"; agent: AgentSummary };

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

// SenderOption is a shared row layout for the From autocomplete: avatar +
// display name + a Human/Agent badge so users can tell senders apart at a
// glance.
function SenderOption({
  seed,
  avatarSrc,
  title,
  subtitle,
  badge,
  onSelect,
}: {
  seed: string;
  avatarSrc: string | null;
  title: string;
  subtitle?: string;
  badge: string;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onMouseDown={(e) => {
        e.preventDefault();
        onSelect();
      }}
      className={cn(
        "flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm",
        "hover:bg-control-bg"
      )}
    >
      <Avatar seed={seed} src={avatarSrc} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-main">{title}</span>
        {subtitle && (
          <span className="block truncate text-xs text-control-placeholder">
            {subtitle}
          </span>
        )}
      </span>
      <span className="shrink-0 rounded bg-control-bg px-1.5 py-0.5 text-[10px] font-medium text-control">
        {badge}
      </span>
    </button>
  );
}

// UserPickerOption is one human row in the From autocomplete.
function UserPickerOption({
  user,
  onSelect,
}: {
  user: User;
  onSelect: (user: User) => void;
}) {
  const { t } = useTranslation();
  const avatarSrc = useAvatar(avatarNameForUserId(user.handle || ""));
  const label = user.title || user.email || user.handle;
  const sublabel = user.handle || user.email;

  return (
    <SenderOption
      seed={user.handle || user.name}
      avatarSrc={avatarSrc}
      title={label}
      subtitle={sublabel !== label ? sublabel : undefined}
      badge={t("members.kind-user")}
      onSelect={() => onSelect(user)}
    />
  );
}

// AgentPickerOption is one agent row in the From autocomplete.
function AgentPickerOption({
  agent,
  onSelect,
}: {
  agent: AgentSummary;
  onSelect: (agent: AgentSummary) => void;
}) {
  const { t } = useTranslation();
  const avatarSrc = useAvatar(avatarNameForAgentId(agent.handle || ""));
  const label = agent.title || agent.handle;
  const sublabel = agent.description || agent.handle;

  return (
    <SenderOption
      seed={agent.handle || agent.name}
      avatarSrc={avatarSrc}
      title={label}
      subtitle={sublabel !== label ? sublabel : undefined}
      badge={t("chat.agent")}
      onSelect={() => onSelect(agent)}
    />
  );
}

// FromSenderPicker is a single-select sender autocomplete used by the global
// search "From" filter. It matches both human users (server-side search) and
// agents (client-side filter over the shared roster), and shows a Human/Agent
// badge so the two kinds are easy to tell apart.
function FromSenderPicker({
  value,
  onChange,
  placeholder,
  fullWidth = false,
}: {
  value: FromSender | null;
  onChange: (value: FromSender | null) => void;
  placeholder: string;
  fullWidth?: boolean;
}) {
  const agents = useAppStore((s) => s.agents);
  const agentsLoading = useAppStore((s) => s.agentsLoading);
  const fetchAgents = useAppStore((s) => s.fetchAgents);

  const [query, setQuery] = useState(() =>
    value
      ? value.kind === "user"
        ? value.user.title || value.user.handle || ""
        : value.agent.title || value.agent.handle || ""
      : ""
  );
  const [open, setOpen] = useState(false);
  const [userResults, setUserResults] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Preload the agent roster as soon as the picker mounts so agents are ready
  // before the user starts typing (avoids agents popping in later and making
  // the dropdown look like it "refreshes" with different content).
  useEffect(() => {
    if (useAppStore.getState().agents.length === 0) {
      void fetchAgents({ pageSize: 100 });
    }
  }, [fetchAgents]);

  // Make sure the agent roster is loaded when the picker opens so agents can
  // appear in the dropdown alongside humans.
  useEffect(() => {
    if (!open) return;
    if (useAppStore.getState().agents.length === 0) {
      void fetchAgents({ pageSize: 100 });
    }
  }, [open, fetchAgents]);

  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [open]);

  // Debounced server-side user search, matching the existing MemberPicker
  // pattern. `loading` is turned on synchronously in the input onChange so the
  // dropdown does not briefly show stale results from the previous query.
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    const q = query.trim();
    if (!q) {
      setUserResults([]);
      setLoading(false);
      return;
    }
    debounceRef.current = setTimeout(async () => {
      try {
        const res = await userServiceClient.listUsers({
          pageSize: 50,
          filter: buildUserFilter(q),
        });
        setUserResults(res.users ?? []);
      } catch {
        setUserResults([]);
      } finally {
        setLoading(false);
      }
    }, 250);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [query]);

  const q = query.trim().toLowerCase();
  const filteredAgents = useMemo(
    () =>
      agents.filter(
        (a) =>
          q === "" ||
          a.title.toLowerCase().includes(q) ||
          a.handle.toLowerCase().includes(q) ||
          a.description.toLowerCase().includes(q)
      ),
    [agents, q]
  );

  const hasResults = userResults.length > 0 || filteredAgents.length > 0;
  // Wait for the first agent-roster fetch too, so the combined list doesn't
  // first appear with only users and then "refresh" when agents arrive.
  const waitingForAgents = agentsLoading && agents.length === 0;

  const currentLabel = value
    ? value.kind === "user"
      ? value.user.title || value.user.handle || ""
      : value.agent.title || value.agent.handle || ""
    : "";

  function selectUser(user: User) {
    onChange({ kind: "user", user });
    setQuery(user.title || user.handle || "");
    setOpen(false);
  }

  function selectAgent(agent: AgentSummary) {
    onChange({ kind: "agent", agent });
    setQuery(agent.title || agent.handle || "");
    setOpen(false);
  }

  function clear() {
    onChange(null);
    setQuery("");
    setOpen(false);
  }

  const showDropdown = open && query.trim().length > 0;

  return (
    <div ref={containerRef} className={cn("relative", fullWidth && "w-full")}>
      <div className="flex items-center gap-1.5 rounded-md border border-control-border px-2 py-1">
        <UserIcon className="size-3.5 shrink-0 text-control-light" />
        <Input
          value={query}
          onChange={(e) => {
            const next = e.target.value;
            setQuery(next);
            setOpen(true);
            // Drop stale user results immediately and show the loading state so
            // the dropdown doesn't flash old content while the new search runs.
            setUserResults([]);
            setLoading(next.trim().length > 0);
            // Typing away from the selected sender starts a fresh lookup.
            if (value && currentLabel !== next) {
              onChange(null);
            }
          }}
          onFocus={() => setOpen(true)}
          placeholder={placeholder}
          autoComplete="off"
          spellCheck={false}
          className={cn(
            "h-6 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0",
            fullWidth ? "w-full min-w-0" : "w-32"
          )}
        />
        {(value || query) && (
          <button
            type="button"
            onClick={clear}
            className="shrink-0 rounded p-0.5 text-control-light transition-colors hover:bg-control-bg hover:text-main"
            aria-label={placeholder}
          >
            <X className="size-3" />
          </button>
        )}
      </div>
      {showDropdown && (
        <div className="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-auto rounded border border-control-border bg-background py-1 shadow-md">
          {loading || waitingForAgents ? (
            <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-control-placeholder">
              <Loader2 className="size-3.5 animate-spin" />
            </div>
          ) : !hasResults ? (
            <div className="px-2 py-1.5 text-xs text-control-placeholder">
              {placeholder}
            </div>
          ) : (
            <>
              {userResults.map((user) => (
                <UserPickerOption
                  key={user.name}
                  user={user}
                  onSelect={selectUser}
                />
              ))}
              {filteredAgents.map((agent) => (
                <AgentPickerOption
                  key={agent.name}
                  agent={agent}
                  onSelect={selectAgent}
                />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// GlobalSearchPage searches every conversation the current user can read:
// message content (main channel and thread replies) plus attachment file
// names. Results link back into the channel chat at the exact message.
export function GlobalSearchPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isDesktop = useIsDesktop();
  const myChannels = useAppStore((s) => s.myChannels);
  const fetchChannels = useAppStore((s) => s.fetchChannels);

  useEffect(() => {
    void fetchChannels();
  }, [fetchChannels]);

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
              className="h-11 w-full touch-manipulation sm:h-7 sm:w-auto"
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

              <Select
                value={channel}
                onValueChange={(v) => setChannel(v ?? "")}
              >
                <SelectTrigger size="md" className="w-full">
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
                  <SelectItem value="">
                    {t("globalSearch.all-channels")}
                  </SelectItem>
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
