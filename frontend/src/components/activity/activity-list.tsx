import { Inbox, Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ActivityRow } from "@/components/activity/activity-row";
import { EmptyState, LoadingState } from "@/components/chat/states";
import {
  useActivityPages,
  useMarkActivityDone,
} from "@/hooks/use-activity-feed";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { cn } from "@/lib/utils";
import type { Activity } from "@/types/proto-es/v1/command_pb";
import {
  ActivityCategory,
  ActivityState,
} from "@/types/proto-es/v1/command_pb";

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

  const [filter, setFilter] = useState<Filter>("unread");
  // pageTokens[i] is the page_token to ENTER page i; page 0 is "" (offset 0).
  // One list, one paging semantic — infinite scroll on every viewport: the
  // stack grows as rows scroll in, and the data itself lives in the Query
  // cache, one entry per (filter, token) — use-activity-feed.ts owns it.
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [markingDone, setMarkingDone] = useState<string>("");

  const params = filterToParams(filter);
  // The 5s silent poll rides page 0 on every viewport. The feed is
  // newest-first (created_at DESC, offset pagination), so the head is the one
  // stable window to poll — a later offset window drifts as new rows are
  // inserted above it.
  const pages = useActivityPages({ params, pageTokens });
  const markDone = useMarkActivityDone();

  const handleFilterChange = (next: Filter) => {
    if (next === filter) return;
    setFilter(next);
    setPageTokens([""]);
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
    try {
      // The mutation removes the row from the cache optimistically and
      // invalidates the pages, so no extra refill is needed here.
      await markDone(a.name);
    } finally {
      setMarkingDone("");
    }
  };

  // Infinite scroll: when the bottom sentinel enters the viewport and another
  // page is available, load and append it. Already-loaded pages stay cached
  // rows on screen while an appended page is still pending. The tail token
  // comes from the last page WITH data — a just-appended pending page has no
  // rows yet, so the previous page still carries hasMore.
  const loadedPages = pages.filter((p) => !p.pending);
  const tailLoaded = loadedPages[loadedPages.length - 1];
  const tailToken = tailLoaded?.nextPageToken ?? "";
  const tailBusy = pages.length > loadedPages.length;
  const hasMore = tailToken !== "";
  const rows = pages.flatMap((p) => p.activities);
  const initialPending = pages[0]?.pending ?? true;

  const unreadCount = rows.filter(
    (a) => a.state === ActivityState.UNREAD
  ).length;

  const filters: Filter[] = ["all", "unread", "mention", "task", "reminder"];

  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasMore && !tailBusy) {
        setPageTokens((tok) =>
          tok.includes(tailToken) ? tok : [...tok, tailToken]
        );
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasMore, tailBusy, tailToken]);

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
        {rows.length === 0 && initialPending ? (
          <LoadingState />
        ) : rows.length === 0 ? (
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
            {rows.map((a) => (
              <ActivityRow
                key={a.name}
                activity={a}
                active={messageIdOf(a) === selectedId}
                onSelect={() => handleSelect(a)}
                onMarkDone={() => handleMarkDone(a)}
                markingDone={markingDone === a.name}
              />
            ))}
            {/* Infinite-scroll sentinel: appending the next page or nothing
                when the feed is exhausted. */}
            <div
              ref={sentinelRef}
              className="flex items-center justify-center py-3"
            >
              {tailBusy && hasMore && (
                <Loader2 className="size-4 animate-spin text-control-light" />
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
