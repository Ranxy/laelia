import { useTranslation } from "react-i18next";
import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { mergeStatusMapping, StatusBadge } from "@/components/ui/status-badge";
import { taskStatusToI18nKey, taskStatusToVariant } from "@/lib/task-status";

const taskStatusEntry = mergeStatusMapping(
  taskStatusToVariant,
  taskStatusToI18nKey
);
const taskStatusFallback: StatusBadgeEntry = {
  variant: "default",
  labelKey: "channelTask.status-unknown",
};

// TaskStatusBadge renders the inline "[task #N status=... · assignee]" badge
// shown next to a task root message's header. Mirrors CommandStatusBadge.
export function TaskStatusBadge({
  taskNumber,
  status,
  assigneeName,
  className,
}: {
  taskNumber: number;
  status: number;
  assigneeName?: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const assignee = assigneeName
    ? ` · ${assigneeName}`
    : ` · ${t("channelTask.unassigned")}`;
  return (
    <StatusBadge
      mapping={taskStatusEntry}
      status={status}
      fallback={taskStatusFallback}
      className={className}
    >
      {({ labelKey }) => (
        <>
          #{taskNumber} · {t(labelKey)}
          {assignee}
        </>
      )}
    </StatusBadge>
  );
}
