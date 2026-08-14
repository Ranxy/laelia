import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { pairToolCallEvents } from "@/lib/tool-call-events";

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

function tsToMs(ts: { seconds?: bigint; nanos?: number } | undefined): number {
  if (!ts?.seconds) return 0;
  return Number(ts.seconds) * 1000 + (ts.nanos ?? 0) / 1_000_000;
}

function orderedRange(a: number, b: number): FractionRange {
  return a <= b ? { start: a, end: b } : { start: b, end: a };
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

    type Item =
      | { kind: "output"; ts: number; output: CommandOutput }
      | { kind: "tool"; ts: number; pair: (typeof pairs)[number] }
      | { kind: "event"; ts: number; event: CommandEvent };
    const items: Item[] = [];

    for (const output of outputs) {
      items.push({ kind: "output", ts: tsToMs(output.timestamp), output });
    }
    for (const pair of pairs) {
      items.push({ kind: "tool", ts: tsToMs(pair.started.timestamp), pair });
    }
    for (const event of events) {
      if (
        event.type === CommandEventType.TOOL_CALL_STARTED ||
        event.type === CommandEventType.TOOL_CALL_FINISHED ||
        event.type === CommandEventType.CONTEXT_USAGE_UPDATE ||
        event.type === CommandEventType.RAW_ACP
      ) {
        continue;
      }
      items.push({ kind: "event", ts: tsToMs(event.timestamp), event });
    }
    items.sort((a, b) => a.ts - b.ts);

    let run: {
      type: number;
      start: number;
      end: number;
      seqNo: number;
      key: string;
    } | null = null;
    const flushRun = () => {
      if (!run) return;
      spans.push({
        lane: 0,
        start: run.start,
        end: Math.max(run.end, run.start + 1),
        seqNo: run.seqNo,
        kind: "output",
        source: "output",
        key: run.key,
      });
      run = null;
    };

    for (const item of items) {
      if (item.kind === "output") {
        const ts = item.ts;
        if (run && run.type === item.output.type && run.end <= ts) {
          run.end = ts;
          continue;
        }
        flushRun();
        run = {
          type: item.output.type,
          start: ts,
          end: ts,
          seqNo: item.output.seqNo,
          key: `out-${item.output.seqNo}`,
        };
        continue;
      }

      flushRun();

      if (item.kind === "tool") {
        const start = item.ts;
        const end = item.pair.finished
          ? tsToMs(item.pair.finished.timestamp)
          : start + 1;
        const status =
          item.pair.finished?.payload.case === "toolCallFinished"
            ? item.pair.finished.payload.value.status
            : undefined;
        spans.push({
          lane: 1,
          start,
          end: Math.max(end, start + 1),
          seqNo: item.pair.started.seqNo,
          kind: "tool",
          source: "tool",
          key: `tool-${item.pair.started.seqNo}`,
          error: status === "error" || status === "failed",
        });
        continue;
      }

      const ts = item.ts;
      const lane: 0 | 1 | 2 =
        item.event.type === CommandEventType.DIFF_EMITTED ||
        item.event.type === CommandEventType.WARNING ||
        item.event.type === CommandEventType.CONTEXT_COMPACTION_STARTED ||
        item.event.type === CommandEventType.CONTEXT_COMPACTION_FINISHED
          ? 2
          : 0;
      const kind: Span["kind"] =
        item.event.type === CommandEventType.DIFF_EMITTED ||
        item.event.type === CommandEventType.WARNING ||
        item.event.type === CommandEventType.CONTEXT_COMPACTION_STARTED ||
        item.event.type === CommandEventType.CONTEXT_COMPACTION_FINISHED
          ? "system"
          : "output";
      spans.push({
        lane,
        start: ts,
        end: ts + 1,
        seqNo: item.event.seqNo,
        kind,
        source: "event",
        key: `ev-${item.event.seqNo}`,
        error: item.event.type === CommandEventType.WARNING,
      });
    }
    flushRun();

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
