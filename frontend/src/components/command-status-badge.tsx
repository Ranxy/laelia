import { useTranslation } from "react-i18next";
import { Badge } from "@/react/components/ui/badge";
import {
  commandStatusToI18nKey,
  commandStatusToVariant,
} from "@/react/lib/command-status";
import { cn } from "@/react/lib/utils";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

interface CommandStatusBadgeProps {
  status: number;
  className?: string;
}

function CommandStatusBadge({ status, className }: CommandStatusBadgeProps) {
  const { t } = useTranslation();
  const variant = commandStatusToVariant[status as CommandStatus] ?? "default";
  const labelKey =
    commandStatusToI18nKey[status as CommandStatus] ?? "command.status-unknown";

  return (
    <Badge variant={variant} className={cn(className)}>
      {t(labelKey)}
    </Badge>
  );
}

export { CommandStatusBadge };
