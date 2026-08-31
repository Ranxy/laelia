import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { mergeStatusMapping, StatusBadge } from "@/components/ui/status-badge";
import {
  reminderStatusToI18nKey,
  reminderStatusToVariant,
} from "@/lib/reminder-status";

const reminderStatusEntry = mergeStatusMapping(
  reminderStatusToVariant,
  reminderStatusToI18nKey
);
const reminderStatusFallback: StatusBadgeEntry = {
  variant: "default",
  labelKey: "reminders.status-unknown",
};

// ReminderStatusBadge renders a colored status pill for a reminder, mirroring
// CommandStatusBadge.
export function ReminderStatusBadge({
  status,
  className,
}: {
  status: number;
  className?: string;
}) {
  return (
    <StatusBadge
      mapping={reminderStatusEntry}
      status={status}
      fallback={reminderStatusFallback}
      className={className}
    />
  );
}
