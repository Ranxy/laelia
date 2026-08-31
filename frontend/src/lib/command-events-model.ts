// lib/command-events-model.ts
//
// Normalization layer for the command event pipeline (analysis 08 F-R1/R2/R3,
// §7 L2). THE single implementation of the mechanics that the ledger, the
// timeline overview, the inspector and the chat message rows previously
// copied (with drifting semantics):
//
//   - proto Timestamp → epoch ms
//   - clock-time formatting for rows/spans
//   - merging consecutive same-type output chunks into runs with one shared
//     row-key space ("out-N" / "ev-N" / "tool-N")
//   - tool-call finished-status classification
//
// The event-kind registries (event/output-stream → label/icon/phase) live
// here too so there is exactly one mapping truth.
//
// Pure functions only: no React, no stores, no i18n. Tool-call pairing (FIFO)
// stays in lib/tool-call-events.ts.

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
  Shield,
  Terminal,
  Wrench,
} from "lucide-react";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandOutput_StreamType,
} from "@/types/proto-es/v1/command_pb";

// --- Time ------------------------------------------------------------------

/** Proto Timestamp → epoch ms; unset timestamps collapse to 0. */
export function tsToMs(
  ts: { seconds?: bigint; nanos?: number } | undefined
): number {
  if (!ts?.seconds) return 0;
  return Number(ts.seconds) * 1000 + (ts.nanos ?? 0) / 1_000_000;
}

/** "HH:MM:SS" for an epoch-ms instant. */
export function formatClockTime(ms: number): string {
  return new Date(ms).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** "HH:MM:SS" for a proto Timestamp; "" when unset. */
export function formatEventTime(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "";
  return formatClockTime(Number(ts.seconds) * 1000);
}

// Renders a merged output run as "HH:MM:SS" when it is a single chunk, or
// "HH:MM:SS → HH:MM:SS" when it spans multiple chunks (start → end).
export function formatRunTimeRange(startTs: number, endTs: number): string {
  if (!startTs) return "";
  if (!endTs || endTs <= startTs) return formatClockTime(startTs);
  return `${formatClockTime(startTs)} → ${formatClockTime(endTs)}`;
}

// --- Output-run merging (08 F-R1: the row-key drift bug is fixed by having
// every surface call this one function) -------------------------------------

export interface OutputRun {
  /** "out-${first.seqNo}" — shared key space with "ev-N"/"tool-N". */
  key: string;
  /** First chunk of the run; carries stream type + seqNo metadata. */
  output: CommandOutput;
  type: number;
  /** Concatenated content of the merged chunks. */
  content: string;
  /** Epoch ms of the first chunk (run start). */
  startTs: number;
  /** Epoch ms of the last chunk (run end). */
  endTs: number;
}

/**
 * Merge consecutive same-type output chunks into runs.
 *
 * `events` supplies the timeline interleaving: every event that produces an
 * inspector/ledger row is a run boundary, with exactly one exception set —
 * CONTEXT_USAGE_UPDATE and RAW_ACP frames are internal detail, neither rows
 * nor boundaries. Because ALL surfaces merge through this function with the
 * same break rules, the row-key space ("out-N") is now identical across
 * ledger, overview and inspector even under timestamp jitter; the stable sort
 * keeps the input order as the tie-break at equal timestamps.
 */
export function mergeOutputRuns(
  outputs: CommandOutput[],
  events: CommandEvent[]
): OutputRun[] {
  type Item =
    | { kind: "output"; ts: number; output: CommandOutput }
    | { kind: "break"; ts: number };
  const items: Item[] = [];
  for (const output of outputs) {
    items.push({ kind: "output", ts: tsToMs(output.timestamp), output });
  }
  for (const event of events) {
    if (
      event.type === CommandEventType.CONTEXT_USAGE_UPDATE ||
      event.type === CommandEventType.RAW_ACP
    ) {
      continue;
    }
    items.push({ kind: "break", ts: tsToMs(event.timestamp) });
  }
  items.sort((a, b) => a.ts - b.ts);

  const runs: OutputRun[] = [];
  let current: OutputRun | null = null;
  for (const item of items) {
    if (item.kind === "break") {
      current = null;
      continue;
    }
    if (current && current.type === item.output.type) {
      current.content += item.output.content;
      current.endTs = item.ts;
      continue;
    }
    current = {
      key: `out-${item.output.seqNo}`,
      output: item.output,
      type: item.output.type,
      content: item.output.content,
      startTs: item.ts,
      endTs: item.ts,
    };
    runs.push(current);
  }
  return runs;
}

// --- Tool-call finished status (08 F-R3: one predicate, four render sites) -

// The backend only ever sends "success" | "error" (executor.go). "completed"
// and "failed" are accepted legacy spellings from historical streams.
export function isToolCallError(status: string | undefined | null): boolean {
  return status === "error" || status === "failed";
}

// --- Event-kind registry (moved from components/command-events/) -----------

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

export function getCommandEventKind(type: number): CommandEventKind {
  return (
    commandEventKind[type] ?? {
      labelKey: "command.event-unknown",
      tagClass: NEUTRAL_TAG,
      textClass: "text-control",
      icon: Braces,
      phase: "other",
    }
  );
}

// Events the inspector/payload views can show in tabs but that never render
// as ledger rows or break output-run merging.
export function isVisibleEvent(event: CommandEvent): boolean {
  return (
    event.type !== CommandEventType.TEXT_DELTA &&
    event.type !== CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
  );
}

// --- Output stream kinds (terminal stdout/stderr/system merged into ledger) -

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
