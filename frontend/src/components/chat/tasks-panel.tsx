import { ListChecks, Loader2, MessageCircleReply, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { TaskStatusBadge } from "@/components/chat/task-status-badge";
import { Button } from "@/components/ui/button";
import { taskStatusShort } from "@/lib/task-status";
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

// TasksPanel is the right-side task board for a channel: every task (root
// message with task metadata) listed with its number, status badge, assignee,
// and content. Mirrors ThreadPanel's open/close shape. Each card drills into
// the task's thread (its workspace) via onOpenTask. A "Convert to Task"
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
  const loadTasks = useAppStore((s) => s.loadTasks);

  const todoCount = tasks.filter((m) => m.task?.status === 1).length;
  const inProgressCount = tasks.filter((m) => m.task?.status === 2).length;
  const inReviewCount = tasks.filter((m) => m.task?.status === 3).length;
  const doneCount = tasks.filter((m) => m.task?.status === 4).length;

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
              todo: todoCount,
              inProgress: inProgressCount,
              inReview: inReviewCount,
              done: doneCount,
            })}
          </p>
        </div>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => void loadTasks(channelId)}
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
  const status = task.task?.status ?? 0;
  const title =
    task.content.split("\n")[0]?.trim() || t("channelTask.untitled");
  const replies = task.threadReplyCount ?? 0;
  return (
    <div
      className={cn(
        "group/card rounded-lg border border-control-border bg-control-bg/30 px-3 py-2 text-sm"
      )}
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
          onClick={() => onOpenTask(task.id)}
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

const EMPTY_TASKS: ChatMessageUI[] = [];
