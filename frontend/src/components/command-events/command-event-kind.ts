import {
  AlertTriangle,
  Braces,
  CheckCircle2,
  Coins,
  FileDiff,
  Gauge,
  type LucideIcon,
  Minimize2,
  Play,
  RotateCcw,
  Send,
  Shield,
  Terminal,
  Wrench,
} from "lucide-react";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandOutput_StreamType,
} from "@/types/proto-es/v1/command_pb";

export interface CommandEventKind {
  /** i18n key for the tag label. */
  labelKey: string;
  /** Tailwind classes for the tag pill. */
  tagClass: string;
  /** Tailwind classes for the row text accent. */
  textClass: string;
  icon: LucideIcon;
  /** Coarse phase used for group headers. */
  phase: string;
}

const NEUTRAL_TAG = "bg-control-bg text-control";
const INFO_TAG = "bg-info/10 text-info";
const SUCCESS_TAG = "bg-success/10 text-success";
const WARNING_TAG = "bg-warning/10 text-warning";
const ERROR_TAG = "bg-error/10 text-error";
const ACCENT_TAG = "bg-accent/10 text-accent";

export const commandEventKind: Record<number, CommandEventKind> = {
  [CommandEventType.LIFECYCLE]: {
    labelKey: "command.event-lifecycle",
    tagClass: NEUTRAL_TAG,
    textClass: "text-control",
    icon: Play,
    phase: "lifecycle",
  },
  [CommandEventType.TOOL_CALL_STARTED]: {
    labelKey: "command.event-tool-started",
    tagClass: WARNING_TAG,
    textClass: "text-warning",
    icon: Wrench,
    phase: "tool",
  },
  [CommandEventType.TOOL_CALL_FINISHED]: {
    labelKey: "command.event-tool-finished",
    tagClass: SUCCESS_TAG,
    textClass: "text-success",
    icon: Wrench,
    phase: "tool",
  },
  [CommandEventType.DIFF_EMITTED]: {
    labelKey: "command.event-diff",
    tagClass: ACCENT_TAG,
    textClass: "text-accent",
    icon: FileDiff,
    phase: "diff",
  },
  [CommandEventType.WARNING]: {
    labelKey: "command.event-warning",
    tagClass: WARNING_TAG,
    textClass: "text-warning",
    icon: AlertTriangle,
    phase: "warning",
  },
  [CommandEventType.RAW_ACP]: {
    labelKey: "command.event-raw-acp",
    tagClass: NEUTRAL_TAG,
    textClass: "text-control-light",
    icon: Braces,
    phase: "raw",
  },
  [CommandEventType.FINAL_SUMMARY]: {
    labelKey: "command.event-final-summary",
    tagClass: SUCCESS_TAG,
    textClass: "text-success",
    icon: CheckCircle2,
    phase: "summary",
  },
  [CommandEventType.PERMISSION_REQUESTED]: {
    labelKey: "command.event-permission-requested",
    tagClass: INFO_TAG,
    textClass: "text-info",
    icon: Shield,
    phase: "permission",
  },
  [CommandEventType.PERMISSION_TIMED_OUT]: {
    labelKey: "command.event-permission-timed-out",
    tagClass: ERROR_TAG,
    textClass: "text-error",
    icon: Shield,
    phase: "permission",
  },
  [CommandEventType.PERMISSION_DECIDED]: {
    labelKey: "command.event-permission-decided",
    tagClass: INFO_TAG,
    textClass: "text-info",
    icon: Shield,
    phase: "permission",
  },
  [CommandEventType.CONTEXT_COMPACTION_STARTED]: {
    labelKey: "command.event-context-compaction-started",
    tagClass: WARNING_TAG,
    textClass: "text-warning",
    icon: Minimize2,
    phase: "compaction",
  },
  [CommandEventType.CONTEXT_COMPACTION_FINISHED]: {
    labelKey: "command.event-context-compaction-finished",
    tagClass: SUCCESS_TAG,
    textClass: "text-success",
    icon: Minimize2,
    phase: "compaction",
  },
  [CommandEventType.CONTEXT_USAGE_UPDATE]: {
    labelKey: "command.event-context-usage",
    tagClass: INFO_TAG,
    textClass: "text-info",
    icon: Gauge,
    phase: "usage",
  },
  [CommandEventType.TOKEN_USAGE]: {
    labelKey: "command.event-token-usage",
    tagClass: INFO_TAG,
    textClass: "text-info",
    icon: Coins,
    phase: "usage",
  },
};

// Placeholder kinds for event types that may be added later (STEER/RETRY/AGENT).
export const commandEventKindExtra: Record<number, CommandEventKind> = {
  16: {
    labelKey: "command.event-steer",
    tagClass: ACCENT_TAG,
    textClass: "text-accent",
    icon: Send,
    phase: "steer",
  },
  17: {
    labelKey: "command.event-retry-started",
    tagClass: WARNING_TAG,
    textClass: "text-warning",
    icon: RotateCcw,
    phase: "retry",
  },
  18: {
    labelKey: "command.event-retry-finished",
    tagClass: SUCCESS_TAG,
    textClass: "text-success",
    icon: RotateCcw,
    phase: "retry",
  },
};

export function getCommandEventKind(type: number): CommandEventKind {
  return (
    commandEventKind[type] ??
    commandEventKindExtra[type] ?? {
      labelKey: "command.event-unknown",
      tagClass: NEUTRAL_TAG,
      textClass: "text-control",
      icon: Braces,
      phase: "other",
    }
  );
}

export function isToolEvent(event: CommandEvent): boolean {
  return (
    event.type === CommandEventType.TOOL_CALL_STARTED ||
    event.type === CommandEventType.TOOL_CALL_FINISHED
  );
}

export function isVisibleEvent(event: CommandEvent): boolean {
  return (
    event.type !== CommandEventType.TEXT_DELTA &&
    event.type !== CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
  );
}

// --- Output stream kinds (terminal stdout/stderr/system merged into ledger) ---

export interface OutputStreamKind {
  labelKey: string;
  tagClass: string;
  textClass: string;
  icon: LucideIcon;
  phase: string;
}

export const outputStreamKind: Record<number, OutputStreamKind> = {
  [CommandOutput_StreamType.STDOUT]: {
    labelKey: "command.stream-stdout",
    tagClass: SUCCESS_TAG,
    textClass: "text-success",
    icon: Terminal,
    phase: "output",
  },
  [CommandOutput_StreamType.STDERR]: {
    labelKey: "command.stream-stderr",
    tagClass: ERROR_TAG,
    textClass: "text-error",
    icon: Terminal,
    phase: "output",
  },
  [CommandOutput_StreamType.SYSTEM]: {
    labelKey: "command.stream-system",
    tagClass: NEUTRAL_TAG,
    textClass: "text-control-light",
    icon: Terminal,
    phase: "output",
  },
  [CommandOutput_StreamType.ASSISTANT]: {
    labelKey: "command.stream-assistant",
    tagClass: ACCENT_TAG,
    textClass: "text-accent",
    icon: Terminal,
    phase: "output",
  },
};

export function getOutputStreamKind(type: number): OutputStreamKind {
  return (
    outputStreamKind[type] ?? {
      labelKey: "command.stream-unknown",
      tagClass: NEUTRAL_TAG,
      textClass: "text-control-light",
      icon: Terminal,
      phase: "output",
    }
  );
}
