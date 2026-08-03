import {
  CommandEventType,
  CommandStatus,
} from "@/types/proto-es/v1/command_pb";

type BadgeVariant =
  | "default"
  | "secondary"
  | "success"
  | "warning"
  | "destructive";

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
  [CommandStatus.FAILED]: "destructive",
  [CommandStatus.CANCELLED]: "destructive",
  [CommandStatus.TIMEOUT]: "destructive",
};

const commandEventTypeToI18nKey: Record<CommandEventType, string> = {
  [CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED]: "command.event-unknown",
  [CommandEventType.LIFECYCLE]: "command.event-lifecycle",
  [CommandEventType.TEXT_DELTA]: "command.event-text",
  [CommandEventType.TOOL_CALL_STARTED]: "command.event-tool-started",
  [CommandEventType.TOOL_CALL_FINISHED]: "command.event-tool-finished",
  [CommandEventType.DIFF_EMITTED]: "command.event-diff",
  [CommandEventType.WARNING]: "command.event-warning",
  [CommandEventType.RAW_ACP]: "command.event-raw-acp",
  [CommandEventType.FINAL_SUMMARY]: "command.event-final-summary",
  [CommandEventType.PERMISSION_REQUESTED]: "command.event-permission-requested",
  [CommandEventType.PERMISSION_TIMED_OUT]: "command.event-permission-timed-out",
  [CommandEventType.PERMISSION_DECIDED]: "command.event-permission-decided",
  [CommandEventType.CONTEXT_COMPACTION_STARTED]:
    "command.event-context-compaction-started",
  [CommandEventType.CONTEXT_COMPACTION_FINISHED]:
    "command.event-context-compaction-finished",
  [CommandEventType.CONTEXT_USAGE_UPDATE]: "command.event-context-usage",
};

function formatDuration(ms: number | bigint | undefined): string {
  if (ms === undefined || ms === 0n) return "-";
  const num = Number(ms);
  if (num < 1000) return `${num}ms`;
  if (num < 60000) return `${(num / 1000).toFixed(1)}s`;
  return `${(num / 60000).toFixed(1)}m`;
}

function formatTimestamp(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "-";
  return new Date(Number(ts.seconds) * 1000).toLocaleString();
}

function formatTimeOfDay(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "";
  return new Date(Number(ts.seconds) * 1000).toLocaleTimeString();
}

function agentResourceName(agentId: string | undefined): string {
  return `agents/${agentId ?? ""}`;
}

// roleIDFromName extracts the bare id from `roles/{id}`.
function roleIDFromName(name: string | undefined): string {
  if (!name) return "";
  return name.startsWith("roles/") ? name.slice("roles/".length) : name;
}

function commandResourceName(
  agentId: string | undefined,
  commandId: string | undefined
): string {
  return `agents/${agentId ?? ""}/commands/${commandId ?? ""}`;
}

function commandIdFromName(name: string | undefined): string | undefined {
  if (!name) return undefined;
  return name.split("/").pop();
}

export type { BadgeVariant };
export {
  agentResourceName,
  commandEventTypeToI18nKey,
  commandIdFromName,
  commandResourceName,
  commandStatusToI18nKey,
  commandStatusToVariant,
  formatDuration,
  formatTimeOfDay,
  formatTimestamp,
  roleIDFromName,
};
