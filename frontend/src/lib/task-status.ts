import { TaskStatus } from "@/types/proto-es/v1/command_pb";

type BadgeVariant =
  | "default"
  | "secondary"
  | "success"
  | "warning"
  | "destructive";

// taskStatusToVariant picks a Badge color per task status. TODO is neutral,
// IN_PROGRESS is an active amber, IN_REVIEW is the amber "pending human"
// warning, DONE is the success green. Mirrors command-status mapping style.
export const taskStatusToVariant: Record<number, BadgeVariant> = {
  [TaskStatus.UNSPECIFIED]: "default",
  [TaskStatus.TODO]: "secondary",
  [TaskStatus.IN_PROGRESS]: "warning",
  [TaskStatus.IN_REVIEW]: "warning",
  [TaskStatus.DONE]: "success",
};

// taskStatusToI18nKey maps a TaskStatus to its i18n key under channelTask.
export const taskStatusToI18nKey: Record<number, string> = {
  [TaskStatus.UNSPECIFIED]: "channelTask.status-unknown",
  [TaskStatus.TODO]: "channelTask.status-todo",
  [TaskStatus.IN_PROGRESS]: "channelTask.status-in-progress",
  [TaskStatus.IN_REVIEW]: "channelTask.status-in-review",
  [TaskStatus.DONE]: "channelTask.status-done",
};

// taskStatusShort returns the short inline-badge label for a status (e.g.
// "TODO", "IN_REVIEW"), used in the [task #N status=...] badge text.
export function taskStatusShort(status: number): string {
  switch (status) {
    case TaskStatus.TODO:
      return "TODO";
    case TaskStatus.IN_PROGRESS:
      return "IN_PROGRESS";
    case TaskStatus.IN_REVIEW:
      return "IN_REVIEW";
    case TaskStatus.DONE:
      return "DONE";
    default:
      return "";
  }
}

// taskStatusLabel returns the localized label for a task status, for the
// thread-header status dropdown. Falls back to the unknown label.
export function taskStatusLabel(
  status: number,
  t: (key: string) => string
): string {
  return t(taskStatusToI18nKey[status] ?? "channelTask.status-unknown");
}
