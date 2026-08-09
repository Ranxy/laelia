import { ListChecks, Loader2, MessageCircleReply, X } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { TaskStatusBadge } from "@/components/chat/task-status-badge";
import { Button } from "@/components/ui/button";
import { taskStatusShort } from "@/lib/task-status";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";

export interface TasksPanelProps {
  channelId: string;
  channelTitle: string;
  onClose: () => void;
  // onOpenTask drills into a task's workspace: opens the task's thread in the
  // (reused) ThreadPanel, replacing the board. The page wires this to close
  // the tasks panel and open the thread rooted at the task message id.
  onOpenTask: (taskMessageId: string) => void;
}

const EMPTY_TASKS: ChatMessageUI[] = [];
const EMPTY_COUNTS = { todo: 0, inProgress: 0, inReview: 0, done: 0 };

// TasksPanel is the right-side task board for a channel: tasks (root messages
// with task metadata) listed newest-first with their number, status badge,
// assignee, and content. Mirrors ThreadPanel's open/close shape. Each card
// drills into the task's thread (its workspace) via onOpenTask. The list is
// paginated — scrolling to the bottom loads the next (older) page via
// loadMoreTasks — and the status summary uses ListTaskCounts totals so it stays
// accurate regardless of how many tasks are loaded. A "Convert to Task"
// affordance is intentionally NOT exposed per row here — conversion is driven
// from the message row's own context; this panel is browsing the board plus a
// refresh action.
export function TasksPanel({
  channelId,
  channelTitle,
  onClose,
  onOpenTask,
}: TasksPanelProps) {
  const { t } = useTranslation();
  const convName = `conversations/${channelId}`;

  const tasks = useAppStore((s) => s.tasksByConv[convName] ?? EMPTY_TASKS);
  const loading = useAppStore((s) => s.tasksLoading[convName] ?? false);
  const nextPageToken = useAppStore(
    (s) => s.tasksNextPageToken[convName] ?? ""
  );
  const counts = useAppStore(
    (s) => s.taskCountsByConv[convName] ?? EMPTY_COUNTS
  );
  const loadTasks = useAppStore((s) => s.loadTasks);
  const loadMoreTasks = useAppStore((s) => s.loadMoreTasks);
  const loadTaskCounts = useAppStore((s) => s.loadTaskCounts);

  const hasMore = nextPageToken !== "";

  // Infinite scroll: when the bottom sentinel enters the viewport and there is
  // another (older) page available, load it. The store's loadMoreTasks is a
  // no-op while a load is in flight, so repeated intersections are safe.
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasMore && !loading) {
        void loadMoreTasks(channelId);
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [channelId, hasMore, loading, loadMoreTasks]);

  const handleRefresh = () => {
    void loadTasks(channelId);
    void loadTaskCounts(channelId);
  };

  return (
    <aside className="flex w-[420px] shrink-0 flex-col border-l border-control-border">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2.5">
        <ListChecks className="size-4 text-control-placeholder" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-main truncate">
            {t("channelTask.panel-title")} — #{channelTitle}
          </p>
          <p className="text-[11px] text-control-placeholder">
            {t("channelTask.panel-summary", {
              todo: counts.todo,
              inProgress: counts.inProgress,
              inReview: counts.inReview,
              done: counts.done,
            })}
          </p>
        </div>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={handleRefresh}
          disabled={loading}
          aria-label={t("channelTask.refresh")}
        >
          {loading ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            t("channelTask.refresh")
          )}
        </Button>
        <button
          type="button"
          onClick={onClose}
          className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
          aria-label={t("channelTask.panel-close")}
        >
          <X className="size-4" />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="flex flex-col gap-2 px-3 pt-3 pb-4">
          {loading && tasks.length === 0 && <LoadingState />}
          {!loading && tasks.length === 0 && (
            <EmptyState
              icon={ListChecks}
              message={t("channelTask.panel-empty")}
            />
          )}
          {tasks.map((task) => (
            <TaskCard key={task.id} task={task} onOpenTask={onOpenTask} />
          ))}
          {/* Scroll-to-bottom sentinel: triggers loadMoreTasks when visible.
              Shown only when there is another page; a spinner replaces it while
              that page is loading. */}
          {hasMore && (
            <div
              ref={sentinelRef}
              className="flex items-center justify-center py-2 text-control-placeholder"
            >
              {loading && tasks.length > 0 && (
                <Loader2 className="size-4 animate-spin" />
              )}
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function TaskCard({
  task,
  onOpenTask,
}: {
  task: ChatMessageUI;
  onOpenTask: (taskMessageId: string) => void;
}) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const status = task.task?.status ?? 0;
  const title =
    task.content.split("\n")[0]?.trim() || t("channelTask.untitled");
  const replies = task.threadReplyCount ?? 0;
  return (
    <div
      onClick={!isDesktop ? () => onOpenTask(task.id) : undefined}
      className={cn(
        "group/card rounded-lg border border-control-border bg-control-bg/30 px-3 py-2 text-sm",
        !isDesktop && "cursor-pointer active:bg-control-bg/60"
      )}
      role={!isDesktop ? "button" : undefined}
      aria-label={!isDesktop ? t("channelTask.open") : undefined}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        {task.task && (
          <TaskStatusBadge
            taskNumber={task.task.taskNumber}
            status={task.task.status}
            assigneeName={task.task.assigneeName}
          />
        )}
        {replies > 0 && (
          <span className="text-[11px] text-control-placeholder">
            {t("channelTask.replies", { count: replies })}
          </span>
        )}
      </div>
      <p className="mt-1.5 text-main whitespace-pre-wrap break-words">
        {title}
      </p>
      <div className="mt-1.5 flex items-center justify-between gap-2">
        <p className="text-[11px] text-control-placeholder">
          {t("channelTask.status-label", { status: taskStatusShort(status) })}
        </p>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onOpenTask(task.id);
          }}
          className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-control-placeholder transition-colors hover:bg-control-bg hover:text-main cursor-pointer"
          aria-label={t("channelTask.open")}
        >
          <MessageCircleReply className="size-3" />
          <span className="hidden sm:inline">{t("channelTask.open")}</span>
        </button>
      </div>
    </div>
  );
}
