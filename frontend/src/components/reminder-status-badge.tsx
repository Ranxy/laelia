import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  reminderStatusToI18nKey,
  reminderStatusToVariant,
} from "@/lib/reminder-status";
import { cn } from "@/lib/utils";

interface ReminderStatusBadgeProps {
  status: number;
  className?: string;
}

// ReminderStatusBadge renders a colored status pill for a reminder, mirroring
// CommandStatusBadge.
export function ReminderStatusBadge({
  status,
  className,
}: ReminderStatusBadgeProps) {
  const { t } = useTranslation();
  const variant = reminderStatusToVariant[status] ?? "default";
  const labelKey =
    reminderStatusToI18nKey[status] ?? "reminders.status-unknown";
  return (
    <Badge variant={variant} className={cn(className)}>
      {t(labelKey)}
    </Badge>
  );
}
