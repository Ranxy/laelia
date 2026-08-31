import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { mergeStatusMapping, StatusBadge } from "@/components/ui/status-badge";
import {
  commandStatusToI18nKey,
  commandStatusToVariant,
} from "@/lib/command-status";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

const commandStatusEntry = mergeStatusMapping(
  commandStatusToVariant,
  commandStatusToI18nKey
);
const commandStatusFallback: StatusBadgeEntry = {
  variant: "default",
  labelKey: "command.status-unknown",
};

function CommandStatusBadge({
  status,
  className,
}: {
  status: number;
  className?: string;
}) {
  return (
    <StatusBadge
      mapping={commandStatusEntry}
      status={status as CommandStatus}
      fallback={commandStatusFallback}
      className={className}
    />
  );
}

export { CommandStatusBadge };
