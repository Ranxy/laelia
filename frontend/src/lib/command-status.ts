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

const commandEventTypeToI18nKey: Partial<Record<CommandEventType, string>> = {
  [CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED]: "command.event-unknown",
  [CommandEventType.LIFECYCLE]: "command.event-lifecycle",
  [CommandEventType.TEXT_DELTA]: "command.event-text",
  [CommandEventType.TOOL_CALL_STARTED]: "command.event-tool-started",
  [CommandEventType.TOOL_CALL_FINISHED]: "command.event-tool-finished",
  [CommandEventType.DIFF_EMITTED]: "command.event-diff",
  [CommandEventType.WARNING]: "command.event-warning",
  [CommandEventType.RAW_ACP]: "command.event-raw-acp",
  [CommandEventType.FINAL_SUMMARY]: "command.event-final-summary",
  [CommandEventType.CONTEXT_COMPACTION_STARTED]:
    "command.event-context-compaction-started",
  [CommandEventType.CONTEXT_COMPACTION_FINISHED]:
    "command.event-context-compaction-finished",
  [CommandEventType.CONTEXT_USAGE_UPDATE]: "command.event-context-usage",
  [CommandEventType.TOKEN_USAGE]: "command.event-token-usage",
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

// formatActivityListTime returns a compact representation for the activity feed
// on small screens: time-of-day for today, otherwise "M/D time".
function formatActivityListTime(ts: { seconds?: bigint } | undefined): {
  time: string;
  date: string;
} {
  if (!ts?.seconds) return { time: "", date: "" };
  const date = new Date(Number(ts.seconds) * 1000);
  const now = new Date();
  const isToday = date.toDateString() === now.toDateString();
  const time = date.toLocaleTimeString([], {
    hour: "numeric",
    minute: "2-digit",
  });
  if (isToday) return { time, date: "" };
  return {
    time,
    date: date.toLocaleDateString([], { month: "numeric", day: "numeric" }),
  };
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
  formatActivityListTime,
  formatDuration,
  formatTimeOfDay,
  formatTimestamp,
  roleIDFromName,
};
