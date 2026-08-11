import type { LucideIcon } from "lucide-react";
import { AtSign, Bell, Check, ListChecks, MessageSquare } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { formatActivityListTime, formatTimestamp } from "@/lib/command-status";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import type { Activity } from "@/types/proto-es/v1/command_pb";
import { ActivityCategory } from "@/types/proto-es/v1/command_pb";

// ActivityCategory flags (mirror the proto enum). A single activity row may
// carry several OR-ed together; we pick the most specific icon for the row.
const CAT_MENTION = Number(ActivityCategory.MENTION);
const CAT_TASK = Number(ActivityCategory.TASK);
const CAT_REMINDER = Number(ActivityCategory.REMINDER);
const CAT_THREAD = Number(ActivityCategory.THREAD);

// primaryCategoryIcon picks the icon that best represents an activity's
// category mix. Reminder > Task > Mention > Thread, so a task reply that also
// mentions the user shows the Task icon (the stronger signal).
function primaryCategoryIcon(categories: number): LucideIcon {
  if (categories & CAT_REMINDER) return Bell;
  if (categories & CAT_TASK) return ListChecks;
  if (categories & CAT_MENTION) return AtSign;
  return MessageSquare;
}

export interface ActivityRowProps {
  activity: Activity;
  active: boolean;
  onSelect: () => void;
  onMarkDone: () => void;
  markingDone?: boolean;
}

// Single 72px swipe action (mark as done), same width as one chat swipe action.
const SWIPE_ACTION_WIDTH = 72;

// ActivityRow is one entry in the left-rail activity list. It shows the category
// icon, the channel/thread/agent context, a truncated message preview prefixed
// with the sender's name, the timestamp, and a "Mark as Done" button. The
// active row is highlighted; a DONE row is dimmed.
// Mobile marks done by left-swiping the row (mirroring the chat list's swipe
// actions); desktop marks done via the right-click context menu.
export function ActivityRow({
  activity,
  active,
  onSelect,
  onMarkDone,
  markingDone,
}: ActivityRowProps) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const Icon = primaryCategoryIcon(activity.categories);
  const isDone = activity.state === 3; // ActivityState.DONE
  const { date: mobileDate, time: mobileTime } = formatActivityListTime(
    activity.createdAt
  );

  const [offset, setOffset] = useState(0);
  const startXRef = useRef(0);
  const startOffsetRef = useRef(0);

  // Close the swipe action when the row becomes active (user navigated into it)
  // or when the activity is marked done so the UI doesn't feel stuck.
  useEffect(() => {
    setOffset(0);
  }, [active, isDone]);

  const clampOffset = useCallback((value: number) => {
    return Math.max(0, Math.min(SWIPE_ACTION_WIDTH, value));
  }, []);

  const handleTouchStart = useCallback(
    (e: React.TouchEvent) => {
      startXRef.current = e.touches[0].clientX;
      startOffsetRef.current = offset;
    },
    [offset]
  );

  const handleTouchMove = useCallback(
    (e: React.TouchEvent) => {
      const clientX = e.touches[0]?.clientX ?? startXRef.current;
      const delta = startXRef.current - clientX;
      // Only allow left-swipe (positive delta) from an already-open or closed
      // state; right-swipe closes the action.
      const next = clampOffset(startOffsetRef.current + delta);
      setOffset(next);
    },
    [clampOffset]
  );

  const handleTouchEnd = useCallback(() => {
    setOffset((current) => {
      // Snap open if dragged past half the action width, otherwise close.
      return current > SWIPE_ACTION_WIDTH / 2 ? SWIPE_ACTION_WIDTH : 0;
    });
  }, []);

  const handleSelect = useCallback(() => {
    if (offset > 8) {
      setOffset(0);
      return;
    }
    onSelect();
  }, [offset, onSelect]);

  const handleMarkDone = useCallback(() => {
    onMarkDone();
    setOffset(0);
  }, [onMarkDone]);

  const row = (
    <>
      {/* Mobile swipe action: revealed by left-swiping the row. */}
      {!isDone && (
        <button
          type="button"
          onClick={handleMarkDone}
          aria-label={t("activity.mark-done")}
          data-testid="swipe-mark-done"
          className={cn(
            "absolute right-0 top-1 bottom-1 z-0 flex w-[72px] shrink-0 items-center justify-center rounded-lg",
            "bg-accent text-accent-text transition-colors lg:hidden"
          )}
        >
          {markingDone ? (
            <span className="size-5 animate-spin rounded-full border-2 border-accent-text border-t-transparent" />
          ) : (
            <Check className="size-5" />
          )}
        </button>
      )}

      <button
        type="button"
        onClick={handleSelect}
        onTouchStart={handleTouchStart}
        onTouchMove={handleTouchMove}
        onTouchEnd={handleTouchEnd}
        style={{
          transform: `translateX(${-offset}px)`,
          transition: offset === 0 ? "transform 200ms ease-out" : "none",
        }}
        className={cn(
          "relative z-10 flex w-full gap-2.5 bg-background text-left transition-colors",
          "px-2.5 py-2 lg:px-3 lg:py-2.5",
          active
            ? "bg-accent/10 border-l-2 border-l-accent"
            : "border-l-2 border-l-transparent hover:bg-control-bg",
          isDone && "opacity-60"
        )}
      >
        <div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full bg-control-bg text-control-light">
          <Icon className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-sm font-medium text-control">
              {activity.senderName || t("activity.thread")}
            </span>
            <span className="shrink-0 font-mono text-[11px] text-control-light">
              <span className="hidden lg:inline">
                {formatTimestamp(activity.createdAt)}
              </span>
              <span className="lg:hidden">
                {mobileDate ? `${mobileDate} ${mobileTime}` : mobileTime}
              </span>
            </span>
          </div>
          <p className="mt-0.5 line-clamp-1 lg:line-clamp-2 text-xs text-control-light">
            {activity.summary}
          </p>
          <div className="mt-1.5">
            <CategoryBadges categories={activity.categories} />
          </div>
        </div>
      </button>
    </>
  );

  // Desktop right-click menu (Mark as done). Mobile gets the swipe action only:
  // the trigger div would otherwise also answer long-presses, which fights the
  // swipe gesture. Done rows have no action, so they skip the menu entirely.
  if (isDesktop && !isDone) {
    return (
      <ContextMenu>
        <ContextMenuTrigger className="group relative flex w-full items-center overflow-hidden">
          {row}
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={handleMarkDone}>
            <Check className="size-4" />
            {t("activity.mark-done")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    );
  }

  return (
    <div className="group relative flex w-full items-center overflow-hidden">
      {row}
    </div>
  );
}

// CategoryBadges renders the small pills for each category flag set on the
// activity (mention/task/reminder/thread), so a multi-category row shows all
// signals at a glance.
function CategoryBadges({ categories }: { categories: number }) {
  const { t } = useTranslation();
  const badges: { key: number; label: string }[] = [];
  if (categories & CAT_MENTION)
    badges.push({ key: CAT_MENTION, label: t("activity.category-mention") });
  if (categories & CAT_TASK)
    badges.push({ key: CAT_TASK, label: t("activity.category-task") });
  if (categories & CAT_REMINDER)
    badges.push({ key: CAT_REMINDER, label: t("activity.category-reminder") });
  if (categories & CAT_THREAD)
    badges.push({ key: CAT_THREAD, label: t("activity.category-thread") });
  if (badges.length === 0) return <span />;
  return (
    <div className="flex flex-wrap gap-1">
      {badges.map((b) => (
        <span
          key={b.key}
          className="rounded-xs bg-control-bg px-1.5 py-0.5 text-[10px] font-medium text-control-light"
        >
          {b.label}
        </span>
      ))}
    </div>
  );
}
