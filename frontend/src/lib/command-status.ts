import type { BadgeVariant } from "@/components/ui/badge";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

// CommandStatus → i18n key / Badge variant lookups. The single consumer is the
// StatusBadge glue in components/command-status-badge.tsx. Time formatters and
// resource-name helpers that used to share this junk drawer live in
// lib/time-format.ts and lib/resource.ts.
const commandStatusToI18nKey: Record<CommandStatus, string> = {
  [CommandStatus.COMMAND_STATUS_UNSPECIFIED]: "command.status-unknown",
  [CommandStatus.PENDING]: "command.status-pending",
  [CommandStatus.RUNNING]: "command.status-running",
  [CommandStatus.COMPLETED]: "command.status-completed",
  [CommandStatus.FAILED]: "command.status-failed",
  [CommandStatus.CANCELLED]: "command.status-cancelled",
  [CommandStatus.TIMEOUT]: "command.status-timeout",
};

const commandStatusToVariant: Record<CommandStatus, BadgeVariant> = {
  [CommandStatus.COMMAND_STATUS_UNSPECIFIED]: "default",
  [CommandStatus.PENDING]: "secondary",
  [CommandStatus.RUNNING]: "warning",
  [CommandStatus.COMPLETED]: "success",
  [CommandStatus.FAILED]: "error",
  [CommandStatus.CANCELLED]: "error",
  [CommandStatus.TIMEOUT]: "error",
};

export { commandStatusToI18nKey, commandStatusToVariant };
