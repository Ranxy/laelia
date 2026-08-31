import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  getCommandEventKind,
  isToolCallError,
  mergeOutputRuns,
  tsToMs,
} from "@/lib/command-events-model";
import { pairToolCallEvents } from "@/lib/tool-call-events";
import { cn } from "@/lib/utils";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";

export interface CommandEventTimelineOverviewProps {
  outputs: CommandOutput[];
  events: CommandEvent[];
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
  /** Called when a drag range is selected (keys inside the range) or cleared
   *  (null) so the ledger can dim rows outside the range. */
  onRangeSelect?: (keys: string[] | null) => void;
  className?: string;
}

interface Span {
  lane: 0 | 1 | 2; // 0=output, 1=tools, 2=system
  start: number;
  end: number;
  seqNo: number;
  kind: "output" | "tool" | "system";
  /** Source discriminator so output/event/tool spans never share a React key. */
  source: "output" | "event" | "tool";
  /** Unique row key matching the ledger ("out-N", "ev-N", "tool-N"). */
  key: string;
  error?: boolean;
}

interface FractionRange {
  start: number;
  end: number;
}

const LANE_LABELS = ["Output", "Tools", "System"] as const;

function orderedRange(a: number, b: number): FractionRange {
  return a <= b ? { start: a, end: b } : { start: b, end: a };
}

// Lane assignment for standalone event spans: diff/warning/compaction events
// render in the System lane; everything else lives in the Output lane. This
// derives from the shared kind registry's phases so the two views cannot
// classify the same event differently.
function eventSpanLane(phase: string): 0 | 2 {
  return phase === "diff" || phase === "warning" || phase === "compaction"
    ? 2
    : 0;
}

export function CommandEventTimelineOverview({
  outputs,
  events,
  selectedKey,
  onSelect,
  onRangeSelect,
  className,
}: CommandEventTimelineOverviewProps) {
  const { t } = useTranslation();
  const trackRef = useRef<HTMLDivElement>(null);
  const [selection, setSelection] = useState<FractionRange | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragAnchor = useRef<number | null>(null);

  const { spans } = useMemo(() => {
    const spans: Span[] = [];
    const pairs = pairToolCallEvents(events);

    // Output runs come from the shared merge implementation, so the span keys
    // match the ledger rows and the inspector's merged outputs exactly. Push
    // order (runs → tools → events) keeps the same tie-break at equal
    // timestamps as the previous ts-interleaved construction.
    for (const run of mergeOutputRuns(outputs, events)) {
      spans.push({
        lane: 0,
        start: run.startTs,
        end: Math.max(run.endTs, run.startTs + 1),
        seqNo: run.output.seqNo,
        kind: "output",
        source: "output",
        key: run.key,
      });
    }

    for (const pair of pairs) {
      const start = tsToMs(pair.started.timestamp);
      const end = pair.finished ? tsToMs(pair.finished.timestamp) : start + 1;
      const status =
        pair.finished?.payload.case === "toolCallFinished"
          ? pair.finished.payload.value.status
          : undefined;
      spans.push({
        lane: 1,
        start,
        end: Math.max(end, start + 1),
        seqNo: pair.started.seqNo,
        kind: "tool",
        source: "tool",
        key: `tool-${pair.started.seqNo}`,
        error: isToolCallError(status),
      });
    }

    for (const event of events) {
      if (
        event.type === CommandEventType.TOOL_CALL_STARTED ||
        event.type === CommandEventType.TOOL_CALL_FINISHED
      ) {
        continue;
      }
      const kind = getCommandEventKind(event.type);
      const phase = kind.phase;
      const lane = eventSpanLane(phase);
      const ts = tsToMs(event.timestamp);
      spans.push({
        lane,
        start: ts,
        end: ts + 1,
        seqNo: event.seqNo,
        kind: lane === 2 ? "system" : "output",
        source: "event",
        key: `ev-${event.seqNo}`,
        error: phase === "warning",
      });
    }

    return { spans };
  }, [outputs, events]);

  const fractionFromEvent = useCallback((clientX: number): number => {
    const el = trackRef.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return 0;
    return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
  }, []);

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const f = fractionFromEvent(e.clientX);
    dragAnchor.current = f;
    setSelection({ start: f, end: f });
    setDragging(true);
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!dragging || dragAnchor.current === null) return;
    const f = fractionFromEvent(e.clientX);
    setSelection(orderedRange(dragAnchor.current, f));
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!dragging) return;
    const f = fractionFromEvent(e.clientX);
    const range = orderedRange(dragAnchor.current ?? f, f);
    setDragging(false);
    dragAnchor.current = null;
    setSelection(range);

    // Collect every span inside the range and scroll the ledger to the first
    // one (the "start of the range").
    const ordered = [...spans].sort((a, b) => a.start - b.start);
    const step = 100 / ordered.length;
    const gap = Math.min(1.6, step / 4);
    const selectedKeys: string[] = [];
    let first: Span | undefined;
    for (let i = 0; i < ordered.length; i++) {
      const left = i * step;
      const width = Math.max(0.5, step - gap);
      const inRange =
        left < range.end * 100 && left + width > range.start * 100;
      if (inRange) {
        selectedKeys.push(ordered[i].key);
        if (!first) first = ordered[i];
      }
    }
    onRangeSelect?.(selectedKeys.length > 0 ? selectedKeys : null);
    if (first) onSelect?.(first.key);
  };

  const clearSelection = () => {
    setSelection(null);
    setDragging(false);
    dragAnchor.current = null;
    onRangeSelect?.(null);
  };

  if (spans.length === 0) {
    return (
      <div
        className={cn(
          "flex h-12 items-center justify-center rounded border border-control-border bg-background text-[11px] text-control-light",
          className
        )}
      >
        {t("command.waiting-events")}
      </div>
    );
  }

  const ordered = [...spans].sort((a, b) => a.start - b.start);
  const step = 100 / ordered.length;
  const gap = Math.min(1.6, step / 4);

  return (
    <div
      className={cn(
        "rounded border border-control-border bg-background px-2 py-1.5",
        className
      )}
    >
      <div className="grid grid-cols-[44px_minmax(0,1fr)] gap-1">
        <div className="flex flex-col justify-between py-0.5 text-right text-[9px] leading-3 text-control-light">
          {LANE_LABELS.map((label) => (
            <span key={label}>{label}</span>
          ))}
        </div>
        <div
          ref={trackRef}
          role="button"
          aria-label={t("command.timeline-drag-hint")}
          tabIndex={0}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={clearSelection}
          onContextMenu={(e) => {
            e.preventDefault();
            clearSelection();
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") clearSelection();
          }}
          className={cn(
            "relative h-9 cursor-crosshair touch-none overflow-hidden rounded bg-control-bg/40 select-none",
            dragging && "cursor-grabbing"
          )}
        >
          {selection && (
            <>
              <div
                aria-hidden="true"
                className="absolute inset-y-0 bg-accent/15"
                style={{
                  left: `${selection.start * 100}%`,
                  width: `${(selection.end - selection.start) * 100}%`,
                }}
              />
              <div
                aria-hidden="true"
                className="absolute inset-y-0 w-0.5 bg-accent"
                style={{ left: `${selection.start * 100}%` }}
              />
              <div
                aria-hidden="true"
                className="absolute inset-y-0 w-0.5 bg-accent"
                style={{ left: `${selection.end * 100}%` }}
              />
            </>
          )}
          {ordered.map((span, index) => {
            const left = index * step;
            const width = Math.max(0.5, step - gap);
            const selected = selectedKey === span.key;
            const inSelection =
              !selection ||
              (left >= selection.start * 100 &&
                left + width <= selection.end * 100);
            return (
              <button
                key={span.key}
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onRangeSelect?.(null);
                  onSelect?.(span.key);
                }}
                aria-label={`${span.kind} #${span.seqNo}`}
                className={cn(
                  "absolute h-2.5 rounded-[1px] transition-opacity",
                  span.lane === 0 && "top-0.5 bg-info/70",
                  span.lane === 1 && "top-[13px] bg-warning/80",
                  span.lane === 2 && "top-[25px] bg-control-light/70",
                  span.error && "bg-error/80",
                  !selected && "opacity-70 hover:opacity-100",
                  selected && "opacity-100 ring-1 ring-accent",
                  selection && !inSelection && "opacity-15"
                )}
                style={{ left: `${left}%`, width: `${width}%` }}
              />
            );
          })}
        </div>
      </div>
    </div>
  );
}
