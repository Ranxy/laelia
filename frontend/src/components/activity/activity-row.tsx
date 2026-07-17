import type { LucideIcon } from "lucide-react";
import { AtSign, Bell, Check, ListChecks, MessageSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import { formatTimestamp } from "@/lib/command-status";
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

// ActivityRow is one entry in the left-rail activity list. It shows the category
// icon, the channel/thread/agent context, a truncated message preview prefixed
// with the sender's name, the timestamp, and a "Mark as Done" button. The
// active row is highlighted; a DONE row is dimmed.
export function ActivityRow({
  activity,
  active,
  onSelect,
  onMarkDone,
  markingDone,
}: ActivityRowProps) {
  const { t } = useTranslation();
  const Icon = primaryCategoryIcon(activity.categories);
  const isDone = activity.state === 3; // ActivityState.DONE

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "group flex w-full gap-2.5 px-3 py-2.5 text-left transition-colors",
        active
          ? "bg-accent/10 border-l-2 border-accent"
          : "border-l-2 border-transparent hover:bg-control-bg",
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
            {formatTimestamp(activity.createdAt)}
          </span>
        </div>
        <p className="mt-0.5 line-clamp-2 text-xs text-control-light">
          {activity.summary}
        </p>
        <div className="mt-1.5 flex items-center justify-between gap-2">
          <CategoryBadges categories={activity.categories} />
          {!isDone && (
            <span
              role="button"
              tabIndex={0}
              onClick={(e) => {
                e.stopPropagation();
                onMarkDone();
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation();
                  onMarkDone();
                }
              }}
              className="inline-flex size-6 shrink-0 items-center justify-center rounded-xs text-control-light opacity-0 transition-opacity hover:bg-control-bg hover:text-control focus-visible:opacity-100 group-hover:opacity-100"
              title={t("activity.mark-done")}
              aria-label={t("activity.mark-done")}
            >
              {markingDone ? (
                <span className="size-3 animate-spin rounded-full border border-control-light border-t-transparent" />
              ) : (
                <Check className="size-3.5" />
              )}
            </span>
          )}
        </div>
      </div>
    </button>
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
