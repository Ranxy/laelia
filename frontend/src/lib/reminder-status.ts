import { ReminderStatus } from "@/types/proto-es/v1/command_pb";

type BadgeVariant =
  | "default"
  | "secondary"
  | "success"
  | "warning"
  | "destructive";

// reminderStatusToVariant picks a Badge color per reminder status. PENDING is
// neutral, DUE is an active amber, COMPLETED is success green, MISSED is the
// warning amber (a fire was skipped but the reminder may reschedule), and
// CANCELLED/FAILED are destructive.
export const reminderStatusToVariant: Record<number, BadgeVariant> = {
  [ReminderStatus.UNSPECIFIED]: "default",
  [ReminderStatus.PENDING]: "secondary",
  [ReminderStatus.DUE]: "warning",
  [ReminderStatus.COMPLETED]: "success",
  [ReminderStatus.CANCELLED]: "destructive",
  [ReminderStatus.MISSED]: "warning",
  [ReminderStatus.FAILED]: "destructive",
};

// reminderStatusToI18nKey maps a ReminderStatus to its i18n key under reminders.
export const reminderStatusToI18nKey: Record<number, string> = {
  [ReminderStatus.UNSPECIFIED]: "reminders.status-unknown",
  [ReminderStatus.PENDING]: "reminders.status-pending",
  [ReminderStatus.DUE]: "reminders.status-due",
  [ReminderStatus.COMPLETED]: "reminders.status-completed",
  [ReminderStatus.CANCELLED]: "reminders.status-cancelled",
  [ReminderStatus.MISSED]: "reminders.status-missed",
  [ReminderStatus.FAILED]: "reminders.status-failed",
};

// reminderStatusShort returns the short inline-badge label for a status (e.g.
// "DUE", "MISSED").
export function reminderStatusShort(status: number): string {
  switch (status) {
    case ReminderStatus.PENDING:
      return "PENDING";
    case ReminderStatus.DUE:
      return "DUE";
    case ReminderStatus.COMPLETED:
      return "COMPLETED";
    case ReminderStatus.CANCELLED:
      return "CANCELLED";
    case ReminderStatus.MISSED:
      return "MISSED";
    case ReminderStatus.FAILED:
      return "FAILED";
    default:
      return "";
  }
}
