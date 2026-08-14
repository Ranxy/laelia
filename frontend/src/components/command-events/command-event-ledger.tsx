import { useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { cn } from "@/lib/utils";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import {
  getCommandEventKind,
  getOutputStreamKind,
} from "./command-event-kind";

export type CommandEventFilter =
  | "all"
  | "tools"
  | "diffs"
  | "warnings"
  | "compaction"
  | "system"
  | "output";

export interface CommandEventLedgerProps {
  outputs: CommandOutput[];
  events: CommandEvent[];
  /** Unique row key (e.g. "out-7", "ev-3", "tool-5"); outputs and events use
   *  independent seq_no spaces, so a bare seq_no is ambiguous. */
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  /** When set, scrolls the matching row into view (e.g. from the timeline). */
  scrollToKey?: string | null;
  /** Keys inside the timeline range selection; rows outside are dimmed. */
  rangeKeys?: string[] | null;
  searchQuery?: string;
  filter?: CommandEventFilter;
  className?: string;
}

type LedgerRow =
  | {
      kind: "output";
      /** First chunk (metadata/kind) of the merged run. */
      output: CommandOutput;
      /** Concatenated content of consecutive same-type output chunks. */
      content: string;
      /** Epoch ms of the first chunk (start of the run). */
      startTs: number;
      /** Epoch ms of the last chunk (end of the run). */
      endTs: number;
      key: string;
    }
  | { kind: "event"; event: CommandEvent; key: string }
  | { kind: "tool"; pair: ToolCallPair; key: string };

function phaseOfEvent(event: CommandEvent): string {
  return getCommandEventKind(event.type).phase;
}

function phaseOfOutput(output: CommandOutput): string {
  return getOutputStreamKind(output.type).phase;
}

function matchesFilter(
  row: { phase: string },
  filter: CommandEventFilter
): boolean {
  if (filter === "all") return true;
  switch (filter) {
    case "tools":
      return row.phase === "tool";
    case "diffs":
      return row.phase === "diff";
    case "warnings":
      return row.phase === "warning";
    case "compaction":
      return row.phase === "compaction";
    case "output":
      return row.phase === "output";
    case "system":
      return (
        row.phase === "lifecycle" ||
        row.phase === "raw" ||
        row.phase === "summary" ||
        row.phase === "usage" ||
        row.phase === "permission" ||
        row.phase === "steer" ||
        row.phase === "retry"
      );
    default:
      return true;
  }
}

function matchesSearch(
  row: { searchText: string },
  query: string
): boolean {
  if (!query) return true;
  return row.searchText.toLowerCase().includes(query.toLowerCase());
}

function tsToMs(ts: { seconds?: bigint; nanos?: number } | undefined): number {
  if (!ts?.seconds) return 0;
  return Number(ts.seconds) * 1000 + (ts.nanos ?? 0) / 1_000_000;
}

function formatTime(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "";
  return new Date(Number(ts.seconds) * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatTimeMs(ms: number): string {
  return new Date(ms).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

// Renders a merged output run as "HH:MM:SS" when it is a single chunk, or
// "HH:MM:SS → HH:MM:SS" when it spans multiple chunks (start → end).
function formatTimeRange(startTs: number, endTs: number): string {
  if (!startTs) return "";
  if (!endTs || endTs <= startTs) return formatTimeMs(startTs);
  return `${formatTimeMs(startTs)} → ${formatTimeMs(endTs)}`;
}

function diffStats(event: CommandEvent): { added: number; removed: number } | null {
  if (event.payload.case !== "diffEmitted") return null;
  const added = event.payload.value.newText
    ? event.payload.value.newText.split("\n").length
    : 0;
  const removed = event.payload.value.oldText
    ? event.payload.value.oldText.split("\n").length
    : 0;
  return { added, removed };
}

function EventContent({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  const kind = getCommandEventKind(event.type);

  if (event.type === CommandEventType.DIFF_EMITTED && event.payload.case === "diffEmitted") {
    const stats = diffStats(event);
    return (
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate font-mono text-control">
          {event.payload.value.path}
        </span>
        {stats && (
          <span className="shrink-0 text-[10px] tabular-nums">
            <span className="text-success">+{stats.added}</span>
            <span className="text-control-light"> / </span>
            <span className="text-error">-{stats.removed}</span>
          </span>
        )}
      </span>
    );
  }

  if (event.type === CommandEventType.WARNING && event.payload.case === "warning") {
    return (
      <span className="truncate text-warning">{event.payload.value.message}</span>
    );
  }

  if (event.type === CommandEventType.TOKEN_USAGE && event.payload.case === "tokenUsage") {
    return (
      <span className="truncate text-info">
        {Number(event.payload.value.totalTokens).toLocaleString()} tokens
      </span>
    );
  }

  if (event.summary) {
    return <span className="truncate text-control">{event.summary}</span>;
  }

  return (
    <span className={cn("truncate italic", kind.textClass)}>
      {t("command.event-no-summary")}
    </span>
  );
}

function ToolContent({ pair }: { pair: ToolCallPair }) {
  const { t } = useTranslation();
  const title =
    pair.started.payload.case === "toolCallStarted"
      ? pair.started.payload.value.title
      : t("chat.tool-call");
  const status =
    pair.finished?.payload.case === "toolCallFinished"
      ? pair.finished.payload.value.status
      : undefined;
  const isError = status === "error" || status === "failed";
  const isFinished = !!pair.finished;

  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="truncate font-mono text-control">{title}</span>
      {isFinished ? (
        <span
          className={cn(
            "shrink-0 text-[10px]",
            isError ? "text-error" : "text-success"
          )}
        >
          {isError ? t("chat.tool-error") : t("chat.tool-finished")}
        </span>
      ) : (
        <span className="shrink-0 text-[10px] text-warning">
          {t("chat.tool-started")}
        </span>
      )}
    </span>
  );
}

function OutputContent({ content }: { content: string }) {
  return (
    <span className="block whitespace-pre-wrap break-all font-mono text-[11px] leading-4 text-control">
      {content}
    </span>
  );
}

export function CommandEventLedger({
  outputs,
  events,
  selectedKey,
  onSelect,
  scrollToKey,
  rangeKeys,
  searchQuery = "",
  filter = "all",
  className,
}: CommandEventLedgerProps) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!scrollToKey || !scrollRef.current) return;
    const container = scrollRef.current;
    const el = container.querySelector<HTMLElement>(
      `[data-row-key="${scrollToKey}"]`
    );
    if (!el) return;
    // Account for the sticky table header so the target row is not hidden
    // behind it, and add a little breathing room below the header.
    const header = container.querySelector("thead");
    const headerHeight = header ? header.getBoundingClientRect().height : 0;
    const containerRect = container.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    const targetTop =
      elRect.top - containerRect.top + container.scrollTop - headerHeight - 8;
    const top = Math.max(0, targetTop);
    if (typeof container.scrollTo === "function") {
      container.scrollTo({ top, behavior: "smooth" });
    } else {
      container.scrollTop = top;
    }
  }, [scrollToKey]);

  const rows = useMemo<LedgerRow[]>(() => {
    const pairs = pairToolCallEvents(events);
    const pairedFinished = new Set<number>();
    for (const p of pairs) if (p.finished) pairedFinished.add(p.finished.seqNo);

    // Build a unified timeline of output chunks + events, ordered by timestamp.
    const items: Array<{
      ts: number;
      row: LedgerRow;
      phase: string;
      searchText: string;
    }> = [];

    for (const output of outputs) {
      const phase = phaseOfOutput(output);
      const ots = tsToMs(output.timestamp);
      items.push({
        ts: ots,
        phase,
        searchText: output.content,
        row: {
          kind: "output",
          output,
          content: output.content,
          startTs: ots,
          endTs: ots,
          key: `out-${output.seqNo}`,
        },
      });
    }

    for (const event of events) {
      // Context usage and raw ACP frames are internal detail, not ledger rows.
      if (event.type === CommandEventType.CONTEXT_USAGE_UPDATE) continue;
      if (event.type === CommandEventType.RAW_ACP) continue;

      if (event.type === CommandEventType.TOOL_CALL_STARTED) {
        const pair = pairs.find((p) => p.started.seqNo === event.seqNo);
        if (pair) {
          const title =
            pair.started.payload.case === "toolCallStarted"
              ? pair.started.payload.value.title
              : "";
          items.push({
            ts: tsToMs(pair.started.timestamp),
            phase: "tool",
            searchText: title,
            row: { kind: "tool", pair, key: `tool-${event.seqNo}` },
          });
          continue;
        }
      }
      if (event.type === CommandEventType.TOOL_CALL_FINISHED) {
        if (pairedFinished.has(event.seqNo)) continue;
      }
      const phase = phaseOfEvent(event);
      items.push({
        ts: tsToMs(event.timestamp),
        phase,
        searchText: [
          event.summary,
          event.payload.case === "toolCallStarted"
            ? event.payload.value.title
            : "",
          event.payload.case === "diffEmitted" ? event.payload.value.path : "",
          event.payload.case === "warning" ? event.payload.value.message : "",
        ]
          .filter(Boolean)
          .join(" "),
        row: { kind: "event", event, key: `ev-${event.seqNo}` },
      });
    }

    items.sort((a, b) => a.ts - b.ts || 0);

    // Merge consecutive same-type output chunks FIRST, on the natural timeline
    // (tool/event rows break a merge). Filtering later must NOT re-merge
    // separate assistant messages into one giant row just because the events
    // between them were filtered out.
    const merged: Array<{
      ts: number;
      row: LedgerRow;
      phase: string;
      searchText: string;
    }> = [];
    for (const item of items) {
      const last = merged[merged.length - 1];
      if (
        last &&
        last.row.kind === "output" &&
        item.row.kind === "output" &&
        last.row.output.type === item.row.output.type
      ) {
        last.row.content += item.row.output.content;
        last.row.endTs = item.ts;
        last.searchText += item.searchText;
        continue;
      }
      merged.push(item);
    }

    const out: LedgerRow[] = [];
    for (const item of merged) {
      if (!matchesFilter(item, filter)) continue;
      if (!matchesSearch(item, searchQuery)) continue;
      out.push(item.row);
    }
    return out;
  }, [outputs, events, filter, searchQuery, t]);

  if (rows.length === 0) {
    return (
      <div
        className={cn(
          "flex h-full min-h-0 items-center justify-center rounded border border-control-border p-4 text-xs text-control-light",
          className
        )}
      >
        {t("command.waiting-events")}
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      className={cn(
        "flex h-full min-h-0 flex-col overflow-auto rounded border border-control-border bg-background",
        className
      )}
    >
      <table className="w-full border-collapse text-xs">
        <colgroup>
          <col className="w-[150px]" />
          <col />
        </colgroup>
        <thead className="sticky top-0 z-10 bg-control-bg">
          <tr className="text-left text-[10px] font-medium uppercase tracking-wide text-control-light">
            <th className="px-3 py-1.5 font-medium">
              {t("command.event-column")}
            </th>
            <th className="px-3 py-1.5 font-medium">
              {t("command.content-column")}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const isTool = row.kind === "tool";
            const isOutput = row.kind === "output";
            const event = isTool ? row.pair.started : isOutput ? undefined : row.event;
            const kind = isOutput
              ? getOutputStreamKind(row.output.type)
              : getCommandEventKind(event!.type);
            const seqNo = isTool
              ? row.pair.started.seqNo
              : isOutput
                ? row.output.seqNo
                : event!.seqNo;
            const selected = selectedKey === row.key;
            const Icon = kind.icon;

            return (
              <tr
                key={row.key}
                data-row-key={row.key}
                data-tool-seq={isTool ? seqNo : undefined}
                role={onSelect ? "button" : undefined}
                tabIndex={onSelect ? 0 : undefined}
                aria-pressed={selected}
                onClick={() => onSelect?.(row.key)}
                onKeyDown={(e) => {
                  if (!onSelect || (e.key !== "Enter" && e.key !== " ")) return;
                  e.preventDefault();
                  onSelect(row.key);
                }}
                className={cn(
                  "cursor-pointer border-b border-control-border/60 transition-colors",
                  "hover:bg-control-bg/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset",
                  selected &&
                    "bg-accent/5 shadow-[inset_3px_0_0_0_rgb(var(--color-accent))]",
                  rangeKeys &&
                    !rangeKeys.includes(row.key) &&
                    "opacity-30"
                )}
              >
                <td className="px-3 py-1.5 align-top">
                  <span
                    className={cn(
                      "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium",
                      kind.tagClass
                    )}
                  >
                    <Icon className="size-3 shrink-0" />
                    {t(kind.labelKey)}
                  </span>
                </td>
                <td className="min-w-0 px-3 py-1.5">
                  <div className="flex min-w-0 items-start gap-2">
                    <div className="min-w-0 flex-1">
                      {isOutput ? (
                        <OutputContent content={row.content} />
                      ) : isTool ? (
                        <ToolContent pair={row.pair} />
                      ) : (
                        <EventContent event={row.event} />
                      )}
                    </div>
                    <span className="shrink-0 pt-0.5 font-mono text-[9px] tabular-nums text-control-light/70">
                      {isOutput
                        ? formatTimeRange(row.startTs, row.endTs)
                        : formatTime(event?.timestamp)}
                    </span>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
