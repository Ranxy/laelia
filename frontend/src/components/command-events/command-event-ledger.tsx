import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  formatEventTime,
  formatRunTimeRange,
  getCommandEventKind,
  getOutputStreamKind,
  isToolCallError,
  mergeOutputRuns,
  tsToMs,
} from "@/lib/command-events-model";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { cn } from "@/lib/utils";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";

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

function matchesSearch(row: { searchText: string }, query: string): boolean {
  if (!query) return true;
  return row.searchText.toLowerCase().includes(query.toLowerCase());
}

function diffStats(
  event: CommandEvent
): { added: number; removed: number } | null {
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

  if (
    event.type === CommandEventType.DIFF_EMITTED &&
    event.payload.case === "diffEmitted"
  ) {
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

  if (
    event.type === CommandEventType.WARNING &&
    event.payload.case === "warning"
  ) {
    return (
      <span className="truncate text-warning">
        {event.payload.value.message}
      </span>
    );
  }

  if (
    event.type === CommandEventType.TOKEN_USAGE &&
    event.payload.case === "tokenUsage"
  ) {
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
  const isError = isToolCallError(status);
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

// A merged run can carry megabytes of stream output (08 F-P2); rendering the
// whole payload as one text node is what made the ledger unusable at length.
// The row shows the head of the content — the inspector's raw tab has the
// full text — and the virtualizer bounding rows keeps the rest off-DOM.
const MAX_OUTPUT_RENDER_CHARS = 100_000;

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
  // Range dimming is O(1) per row: the prop arrives as an array from the
  // timeline selection, so build the set once per change instead of the
  // O(rows × keys) includes() inside the row render.
  const rangeKeySet = useMemo(
    () => (rangeKeys ? new Set(rangeKeys) : null),
    [rangeKeys]
  );

  const rows = useMemo<LedgerRow[]>(() => {
    const pairs = pairToolCallEvents(events);
    const pairedFinished = new Set<number>();
    for (const p of pairs) if (p.finished) pairedFinished.add(p.finished.seqNo);

    // One shared merge implementation (lib/command-events-model.ts) builds
    // the output runs, so the row-key space is identical to the overview and
    // the inspector's merged outputs under any timestamp ordering. Merging
    // happens BEFORE filtering: tool/event rows break a merge, so filtering
    // later must NOT re-merge separate assistant messages into one giant row
    // just because the events between them were filtered out.
    const runs = mergeOutputRuns(outputs, events);

    type Item = {
      ts: number;
      phase: string;
      searchText: string;
      row: LedgerRow;
    };
    const items: Item[] = [];

    for (const run of runs) {
      items.push({
        ts: run.startTs,
        phase: phaseOfOutput(run.output),
        searchText: run.content,
        row: {
          kind: "output",
          output: run.output,
          content: run.content,
          startTs: run.startTs,
          endTs: run.endTs,
          key: run.key,
        },
      });
    }

    for (const event of events) {
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

    items.sort((a, b) => a.ts - b.ts);

    const out: LedgerRow[] = [];
    for (const item of items) {
      if (!matchesFilter(item, filter)) continue;
      if (!matchesSearch(item, searchQuery)) continue;
      out.push(item.row);
    }
    return out;
  }, [outputs, events, filter, searchQuery]);

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

  // The virtualized table lives in a child so its lifecycle starts with the
  // scroll container already mounted (the virtualizer snapshot the element at
  // mount; an empty-ledger early return above keeps that guaranteed).
  return (
    <LedgerTable
      rows={rows}
      selectedKey={selectedKey}
      onSelect={onSelect}
      scrollToKey={scrollToKey}
      rangeKeySet={rangeKeySet}
      className={className}
    />
  );
}

interface LedgerTableProps {
  rows: LedgerRow[];
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  scrollToKey?: string | null;
  rangeKeySet: Set<string> | null;
  className?: string;
}

function LedgerTable({
  rows,
  selectedKey,
  onSelect,
  scrollToKey,
  rangeKeySet,
  className,
}: LedgerTableProps) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);

  // Virtualization engages only once a ResizeObserver has measured the scroll
  // viewport. jsdom reports zero-height containers and never fires observers,
  // so tests (and any embed before layout) render all rows in normal flow —
  // no test-side mocks needed.
  const [virtualizationReady, setVirtualizationReady] = useState(false);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      setVirtualizationReady(true);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 30,
    overscan: 12,
    getItemKey: (index) => rows[index]?.key ?? index,
    measureElement: (el) => el.getBoundingClientRect().height,
  });
  const virtualRows = virtualizationReady
    ? virtualizer.getVirtualItems()
    : null;
  const virtualTotalSize = virtualRows ? virtualizer.getTotalSize() : 0;

  // Scroll the requested row into view. With virtualization the target row
  // may not be in the DOM yet, so jump by the virtual offset; the fallback
  // path resolves the rendered element (accounting for nothing — the header
  // lives outside the scroll area in this layout).
  useEffect(() => {
    if (!scrollToKey || !scrollRef.current) return;
    const container = scrollRef.current;
    if (virtualRows) {
      const index = rows.findIndex((row) => row.key === scrollToKey);
      if (index < 0) return;
      virtualizer.scrollToIndex(index, { align: "start" });
      return;
    }
    const el = container.querySelector<HTMLElement>(
      `[data-row-key="${scrollToKey}"]`
    );
    if (!el) return;
    const containerRect = container.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    const targetTop = elRect.top - containerRect.top + container.scrollTop - 8;
    const top = Math.max(0, targetTop);
    if (typeof container.scrollTo === "function") {
      container.scrollTo({ top, behavior: "smooth" });
    } else {
      container.scrollTop = top;
    }
  }, [scrollToKey, virtualRows, rows, virtualizer]);

  return (
    <div
      className={cn(
        "flex h-full min-h-0 flex-col rounded border border-control-border bg-background",
        className
      )}
    >
      <div className="grid shrink-0 grid-cols-[150px_minmax(0,1fr)] border-b border-control-border bg-control-bg text-left text-[10px] font-medium uppercase tracking-wide text-control-light">
        <div className="px-3 py-1.5 font-medium">
          {t("command.event-column")}
        </div>
        <div className="px-3 py-1.5 font-medium">
          {t("command.content-column")}
        </div>
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
        <div style={{ position: "relative", height: virtualTotalSize }}>
          {(virtualRows ?? rows.map((_, index) => ({ index, start: 0 }))).map(
            (entry) => {
              const index = entry.index;
              const row = rows[index]!;
              const isTool = row.kind === "tool";
              const isOutput = row.kind === "output";
              const event = isTool
                ? row.pair.started
                : isOutput
                  ? undefined
                  : row.event;
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
              const outputTruncated =
                isOutput && row.content.length > MAX_OUTPUT_RENDER_CHARS;

              return (
                <div
                  key={row.key}
                  data-row-key={row.key}
                  data-tool-seq={isTool ? seqNo : undefined}
                  role={onSelect ? "button" : undefined}
                  tabIndex={onSelect ? 0 : undefined}
                  aria-pressed={selected}
                  onClick={() => onSelect?.(row.key)}
                  onKeyDown={(e) => {
                    if (!onSelect || (e.key !== "Enter" && e.key !== " "))
                      return;
                    e.preventDefault();
                    onSelect(row.key);
                  }}
                  ref={virtualRows ? virtualizer.measureElement : undefined}
                  data-index={virtualRows ? index : undefined}
                  style={
                    virtualRows
                      ? {
                          position: "absolute",
                          top: 0,
                          left: 0,
                          width: "100%",
                          transform: `translateY(${entry.start}px)`,
                        }
                      : undefined
                  }
                  className={cn(
                    "grid grid-cols-[150px_minmax(0,1fr)] border-b border-control-border/60 transition-colors",
                    "hover:bg-control-bg/60 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset",
                    selected &&
                      "bg-accent/5 shadow-[inset_3px_0_0_0_rgb(var(--color-accent))]",
                    rangeKeySet && !rangeKeySet.has(row.key) && "opacity-30"
                  )}
                >
                  <div className="px-3 py-1.5 align-top">
                    <span
                      className={cn(
                        "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium",
                        kind.tagClass
                      )}
                    >
                      <Icon className="size-3 shrink-0" />
                      {t(kind.labelKey)}
                    </span>
                  </div>
                  <div className="min-w-0 px-3 py-1.5">
                    <div className="flex min-w-0 items-start gap-2">
                      <div className="min-w-0 flex-1">
                        {isOutput ? (
                          <>
                            <OutputContent
                              content={
                                outputTruncated
                                  ? `${row.content.slice(0, MAX_OUTPUT_RENDER_CHARS)}\n…`
                                  : row.content
                              }
                            />
                            {outputTruncated && (
                              <span className="mt-1 block text-[10px] text-warning">
                                {t("command.output-truncated", {
                                  count:
                                    row.content.length -
                                    MAX_OUTPUT_RENDER_CHARS,
                                })}
                              </span>
                            )}
                          </>
                        ) : isTool ? (
                          <ToolContent pair={row.pair} />
                        ) : (
                          <EventContent event={row.event} />
                        )}
                      </div>
                      <span className="shrink-0 pt-0.5 font-mono text-[9px] tabular-nums text-control-light/70">
                        {isOutput
                          ? formatRunTimeRange(row.startTs, row.endTs)
                          : formatEventTime(event?.timestamp)}
                      </span>
                    </div>
                  </div>
                </div>
              );
            }
          )}
        </div>
      </div>
    </div>
  );
}
