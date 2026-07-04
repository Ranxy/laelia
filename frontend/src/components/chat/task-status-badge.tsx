import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { taskStatusToI18nKey, taskStatusToVariant } from "@/lib/task-status";
import { cn } from "@/lib/utils";

interface TaskStatusBadgeProps {
  taskNumber: number;
  status: number;
  assigneeName?: string;
  className?: string;
}

// TaskStatusBadge renders the inline "[task #N status=... · assignee]" badge
// shown next to a task root message's header. Mirrors CommandStatusBadge.
export function TaskStatusBadge({
  taskNumber,
  status,
  assigneeName,
  className,
}: TaskStatusBadgeProps) {
  const { t } = useTranslation();
  const variant = taskStatusToVariant[status] ?? "default";
  const labelKey = taskStatusToI18nKey[status] ?? "channelTask.status-unknown";
  const assignee = assigneeName
    ? ` · ${assigneeName}`
    : ` · ${t("channelTask.unassigned")}`;
  return (
    <Badge
      variant={variant}
      className={cn("text-[10px] px-1.5 py-0", className)}
    >
      #{taskNumber} · {t(labelKey)}
      {assignee}
    </Badge>
  );
}
