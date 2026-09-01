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

// DOM scale cap: beyond this many spans the per-span buttons stop being
// distinguishable (and affordable), so the overview keeps the most recent
// MAX_TIMELINE_SPANS of them and marks the truncated prefix with a "+N" chip
// at the left edge. The ledger remains the complete, virtualized surface.
const MAX_TIMELINE_SPANS = 500;
// Zero-length events (and the last span) stay clickable with a minimum
// visual width instead of collapsing to nothing.
const MIN_SPAN_WIDTH_PERCENT = 0.5;
// Pointer travel (px) before a background press counts as a drag rather than
// a plain click that clears the selection.
const DRAG_THRESHOLD_PX = 4;

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
  const pressX = useRef(0);

  const { spans, droppedCount } = useMemo(() => {
    const all: Span[] = [];
    const pairs = pairToolCallEvents(events);

    // Output runs come from the shared merge implementation, so the span keys
    // match the ledger rows and the inspector's merged outputs exactly. Push
    // order (runs → tools → events) keeps the same tie-break at equal
    // timestamps as the previous ts-interleaved construction.
    for (const run of mergeOutputRuns(outputs, events)) {
      all.push({
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
      all.push({
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
      all.push({
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

    all.sort((a, b) => a.start - b.start || a.seqNo - b.seqNo);
    if (all.length <= MAX_TIMELINE_SPANS) {
      return { spans: all, droppedCount: 0 };
    }
    return {
      spans: all.slice(all.length - MAX_TIMELINE_SPANS),
      droppedCount: all.length - MAX_TIMELINE_SPANS,
    };
  }, [outputs, events]);

  // This overview intentionally uses ordinal slots instead of wall-clock
  // positioning. Most system events (for example Context Usage, Final
  // Summary, and Token Usage) are instantaneous markers represented as
  // `end = start + 1` millisecond. Mapping those markers to a real-time axis
  // makes their bars effectively zero-width and turns the gaps between event
  // timestamps into a mostly empty timeline. Keep the track densely filled;
  // the ledger shows the exact timestamps and durations for time-based detail.
  const ordered = [...spans].sort(
    (a, b) => a.start - b.start || a.seqNo - b.seqNo
  );
  const step = 100 / ordered.length;
  const gap = Math.min(1.6, step / 4);

  const fractionFromEvent = useCallback((clientX: number): number => {
    const el = trackRef.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return 0;
    return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
  }, []);

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    // Span buttons handle their own click; pressing one must not start a drag.
    if ((e.target as HTMLElement).closest("button")) return;
    e.preventDefault();
    pressX.current = e.clientX;
    dragAnchor.current = fractionFromEvent(e.clientX);
    trackRef.current?.setPointerCapture?.(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragAnchor.current === null) return;
    if (!dragging) {
      if (Math.abs(e.clientX - pressX.current) < DRAG_THRESHOLD_PX) return;
      setDragging(true);
    }
    const f = fractionFromEvent(e.clientX);
    setSelection(orderedRange(dragAnchor.current, f));
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragAnchor.current === null) return;
    const anchor = dragAnchor.current;
    dragAnchor.current = null;
    // Judge by traveled distance, not by whether a move event flipped
    // `dragging`: a fast press→release can jump past the threshold without
    // any intermediate pointermove.
    const moved = Math.abs(e.clientX - pressX.current) >= DRAG_THRESHOLD_PX;
    if (!moved) {
      // A plain background click clears the selection instead of re-selecting
      // whatever happens to sit under the point.
      setDragging(false);
      setSelection(null);
      onRangeSelect?.(null);
      return;
    }
    const range = orderedRange(anchor, fractionFromEvent(e.clientX));
    setDragging(false);
    setSelection(range);

    // Use the same ordinal slots as the rendered bars so range selection
    // remains aligned with the visual timeline instead of wall-clock gaps.
    const selectedKeys: string[] = [];
    let first: Span | undefined;
    for (let i = 0; i < ordered.length; i++) {
      const left = i * step;
      const width = Math.max(MIN_SPAN_WIDTH_PERCENT, step - gap);
      const inRange =
        left < range.end * 100 && left + width > range.start * 100;
      if (inRange) {
        selectedKeys.push(ordered[i]!.key);
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

  const selectionStart = selection?.start ?? 0;
  const selectionEnd = selection?.end ?? 0;

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
          {droppedCount > 0 && (
            <span
              title={t("command.timeline-more", { count: droppedCount })}
              className="absolute left-0.5 top-1/2 z-10 -translate-y-1/2 rounded bg-control-bg px-1 py-0.5 text-[9px] text-control-light"
            >
              +{droppedCount}
            </span>
          )}
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
            const width = Math.max(MIN_SPAN_WIDTH_PERCENT, step - gap);
            const selected = selectedKey === span.key;
            const inSelection =
              !selection ||
              (left < selectionEnd * 100 &&
                left + width > selectionStart * 100);
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
