import { ChevronLeft, ChevronRight, Inbox, Loader2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ActivityRow } from "@/components/activity/activity-row";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { Button } from "@/components/ui/button";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { Activity } from "@/types/proto-es/v1/command_pb";
import {
  ActivityCategory,
  ActivityState,
} from "@/types/proto-es/v1/command_pb";

// Left-rail poll cadence. The activity feed is less urgent than the open
// conversation's message stream, so it polls at the same light cadence as the
// channel list (5s), silently so background refreshes never flicker the list.
const POLL_INTERVAL_MS = 5000;
const PAGE_SIZE = 50;

// Filter tabs. "all" = every not-done activity (read or unread); "unread" =
// unread across all categories; the category tabs narrow to not-done items of
// that category. The default is "unread", matching the product default.
type Filter = "all" | "unread" | "mention" | "task" | "reminder";

function filterToParams(filter: Filter): {
  readStateFilter: ActivityState;
  categoryFilter: ActivityCategory[];
} {
  switch (filter) {
    case "all":
      return { readStateFilter: ActivityState.UNSPECIFIED, categoryFilter: [] };
    case "unread":
      return { readStateFilter: ActivityState.UNREAD, categoryFilter: [] };
    case "mention":
      return {
        readStateFilter: ActivityState.UNSPECIFIED,
        categoryFilter: [ActivityCategory.MENTION],
      };
    case "task":
      return {
        readStateFilter: ActivityState.UNSPECIFIED,
        categoryFilter: [ActivityCategory.TASK],
      };
    case "reminder":
      return {
        readStateFilter: ActivityState.UNSPECIFIED,
        categoryFilter: [ActivityCategory.REMINDER],
      };
  }
}

export function ActivityList() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { messageId: selectedId } = useParams<{ messageId: string }>();
  const isDesktop = useIsDesktop();

  const activities = useAppStore((s) => s.activities);
  const loading = useAppStore((s) => s.activitiesLoading);
  const activitiesNextPageToken = useAppStore((s) => s.activitiesNextPageToken);
  const listActivities = useAppStore((s) => s.listActivities);
  const loadMoreActivities = useAppStore((s) => s.loadMoreActivities);
  const markActivityDone = useAppStore((s) => s.markActivityDone);

  const [filter, setFilter] = useState<Filter>("unread");
  // pageTokens[i] is the page_token to ENTER page i; page 0 is "" (offset 0).
  // Kept for desktop Prev/Next pagination. Mobile uses infinite scroll and
  // appends via the store's loadMoreActivities.
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [pageIndex, setPageIndex] = useState(0);
  const [markingDone, setMarkingDone] = useState<string>("");
  const initialLoadDone = useRef(false);
  // requestSeq is a monotonic epoch for in-flight listActivities calls. Every
  // load increments it; a response whose captured seq no longer matches the
  // current value is from a stale request (an old filter or old page) and is
  // dropped, so a slow poll can never overwrite the list after a filter/page
  // change.
  const requestSeq = useRef(0);

  const pageToken = pageTokens[pageIndex] ?? "";
  const canPrev = pageIndex > 0;
  const canNext =
    pageIndex < pageTokens.length - 1 || activitiesNextPageToken !== "";

  const load = useCallback(
    async (silent?: boolean) => {
      const seq = ++requestSeq.current;
      const { readStateFilter, categoryFilter } = filterToParams(filter);
      const res = await listActivities({
        filter: categoryFilter,
        readStateFilter,
        pageSize: PAGE_SIZE,
        pageToken,
        silent,
      });
      // A newer load (filter/page change, or a later poll) has started since
      // this one was issued — drop the stale result.
      if (seq !== requestSeq.current) return;
      // The store now owns activitiesNextPageToken; desktop pagination just
      // tracks the token stack locally.
      const next = res?.nextPageToken ?? "";
      if (!silent) {
        setPageTokens((tok) => {
          // If the first page returned a next token we haven't captured yet,
          // append it so the Next button works immediately.
          if (next && !tok.includes(next)) return [...tok, next];
          return tok;
        });
      }
    },
    [filter, pageToken, listActivities]
  );

  // Initial load + background polling. A single effect (mirroring ReminderList):
  // filter/page changes recreate `load`, which re-runs this effect, reloading
  // and restarting the interval. The pagination reset lives in
  // handleFilterChange, not here, so a filter switch issues exactly one fetch.
  useEffect(() => {
    initialLoadDone.current = false;
    load(false).then(() => {
      initialLoadDone.current = true;
    });
    const handle = setInterval(() => load(true), POLL_INTERVAL_MS);
    return () => clearInterval(handle);
  }, [load]);

  const handleFilterChange = (next: Filter) => {
    if (next === filter) return;
    setFilter(next);
    setPageTokens([""]);
    setPageIndex(0);
  };

  const gotoPage = (delta: number) => {
    if (delta > 0) {
      if (pageIndex < pageTokens.length - 1) {
        setPageIndex((i) => i + 1);
        return;
      }
      if (!activitiesNextPageToken) return;
      setPageTokens((tok) => [...tok, activitiesNextPageToken]);
      setPageIndex((i) => i + 1);
    } else {
      if (pageIndex <= 0) return;
      setPageIndex((i) => Math.max(0, i - 1));
    }
  };

  // A row's message id is the last path segment of its name
  // ("users/{uid}/activities/{message_id}").
  const messageIdOf = (a: { name: string }) => a.name.split("/").pop() ?? "";

  const handleSelect = (a: Activity) => {
    // Pass the activity via router state so the detail pane can render it
    // immediately even if the row drops out of the current filtered list before
    // the pane mounts (e.g. after a mark-read or a filter switch).
    navigate(`/activity/${messageIdOf(a)}`, { state: { activity: a } });
  };

  const handleMarkDone = async (a: { name: string }) => {
    setMarkingDone(a.name);
    await markActivityDone(a.name);
    setMarkingDone("");
    // Re-fetch the current filter so a now-DONE row drops out of the All/Unread
    // views (they exclude done). Best-effort: silent.
    load(true);
  };

  const unreadCount = activities.filter(
    (a) => a.state === ActivityState.UNREAD
  ).length;

  const filters: Filter[] = ["all", "unread", "mention", "task", "reminder"];

  // Mobile infinite scroll: when the bottom sentinel enters the viewport and
  // there is another page available, load and append it. Hidden on desktop,
  // which keeps the Prev/Next pagination footer.
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const hasMore = activitiesNextPageToken !== "";
  useEffect(() => {
    if (isDesktop) return;
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasMore && !loading) {
        const { readStateFilter, categoryFilter } = filterToParams(filter);
        void loadMoreActivities({
          filter: categoryFilter,
          readStateFilter,
          pageSize: PAGE_SIZE,
        });
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [filter, hasMore, loading, loadMoreActivities, isDesktop]);

  return (
    <div className="flex h-full flex-col">
      {/* Header: title + active count (desktop only). On mobile we keep just
          the filter tabs; the active count is redundant with the empty-state
          text and the title already lives in the top app bar.
          The bottom border spans the full viewport width on mobile so empty
          tabs look identical to tabs whose rows extend edge-to-edge. */}
      <div className="shrink-0 border-b border-control-border py-2 lg:px-4 lg:py-3">
        <div className="hidden items-center gap-2 px-4 lg:flex">
          <Inbox className="hidden lg:block size-4 text-control-light" />
          <h1 className="hidden lg:block text-sm font-semibold text-control">
            {t("activity.title")}
          </h1>
          <span className="ml-auto text-xs text-control-light">
            {t("activity.active-count", { n: unreadCount })}
          </span>
        </div>
        {/* Filter tabs.
            Mobile: horizontal scrollable bar. Each tab has a generous min-width
            so short labels like "All" remain tappable, and a bottom indicator
            marks the active tab.
            Desktop: wrapped pill buttons. */}
        <div
          className={cn(
            "flex",
            isDesktop
              ? "flex-wrap gap-1 px-4 lg:mt-3"
              : "overflow-x-auto px-3 pb-1 [&::-webkit-scrollbar]:hidden"
          )}
          style={isDesktop ? undefined : { scrollbarWidth: "none" }}
          role="tablist"
          aria-label={t("activity.title")}
        >
          {filters.map((f) => (
            <button
              key={f}
              type="button"
              role="tab"
              aria-selected={filter === f}
              onClick={() => handleFilterChange(f)}
              className={cn(
                "text-xs font-medium transition-colors",
                isDesktop
                  ? "shrink-0 rounded-xs px-2.5 py-1"
                  : "flex-none min-w-[60px] px-4 py-2",
                filter === f
                  ? isDesktop
                    ? "bg-accent text-accent-foreground"
                    : "border-b-2 border-accent text-main font-semibold"
                  : isDesktop
                    ? "text-control-light hover:bg-control-bg"
                    : "text-control-light"
              )}
            >
              {t(`activity.filter-${f}`)}
            </button>
          ))}
        </div>
      </div>

      {/* List. */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading && !initialLoadDone.current ? (
          <LoadingState />
        ) : activities.length === 0 ? (
          <EmptyState
            icon={Inbox}
            message={
              filter === "unread"
                ? t("activity.empty-unread")
                : t("activity.empty")
            }
          />
        ) : (
          <div className="divide-y divide-control-border/50">
            {activities.map((a) => (
              <ActivityRow
                key={a.name}
                activity={a}
                active={messageIdOf(a) === selectedId}
                onSelect={() => handleSelect(a)}
                onMarkDone={() => handleMarkDone(a)}
                markingDone={markingDone === a.name}
              />
            ))}
            {/* Mobile infinite-scroll sentinel. */}
            {!isDesktop && (
              <div
                ref={sentinelRef}
                className="flex items-center justify-center py-3"
              >
                {loading && (
                  <Loader2 className="size-4 animate-spin text-control-light" />
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Pagination footer — desktop only. On mobile infinite scroll replaces it;
          also hide entirely when there are no activities so empty tabs don't show
          two disabled icon-only buttons floating at the bottom. */}
      {isDesktop && activities.length > 0 && (
        <div className="flex shrink-0 items-center justify-between gap-2 border-t border-control-border px-3 py-2 text-xs text-control-light">
          <span className="hidden lg:inline">
            {t("activity.page", { n: pageIndex + 1 })}
          </span>
          <div className="flex w-full justify-end gap-1 lg:w-auto">
            <Button
              variant="outline"
              size="sm"
              onClick={() => gotoPage(-1)}
              disabled={!canPrev || loading}
            >
              <ChevronLeft className="size-3.5" />
              <span className="hidden lg:inline">{t("activity.prev")}</span>
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => gotoPage(1)}
              disabled={!canNext || loading}
            >
              <span className="hidden lg:inline">{t("activity.next")}</span>
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
