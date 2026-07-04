import { ListChecks, Loader2, X } from "lucide-react";
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
}

// TasksPanel is the right-side task board for a channel: every task (root
// message with task metadata) listed with its number, status badge, assignee,
// and content. Mirrors ThreadPanel's open/close shape. A "Convert to Task"
// affordordance is intentionally NOT exposed per row here — conversion is
// driven from the message row's own context; this panel is read-only browsing
// of the board plus a refresh action.
export function TasksPanel({
  channelId,
  channelTitle,
  onClose,
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
            <TaskCard key={task.id} task={task} />
          ))}
        </div>
      </div>
    </aside>
  );
}

function TaskCard({ task }: { task: ChatMessageUI }) {
  const { t } = useTranslation();
  const status = task.task?.status ?? 0;
  const title =
    task.content.split("\n")[0]?.trim() || t("channelTask.untitled");
  return (
    <div
      className={cn(
        "rounded-lg border border-control-border bg-control-bg/30 px-3 py-2 text-sm"
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
      </div>
      <p className="mt-1.5 text-main whitespace-pre-wrap break-words">
        {title}
      </p>
      <p className="mt-1 text-[11px] text-control-placeholder">
        {t("channelTask.status-label", { status: taskStatusShort(status) })}
      </p>
    </div>
  );
}

const EMPTY_TASKS: ChatMessageUI[] = [];
