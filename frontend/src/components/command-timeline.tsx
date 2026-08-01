import { useEffect, useMemo, useRef } from "react";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { useAutoScroll } from "@/lib/use-auto-scroll";
import { cn } from "@/lib/utils";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import { CommandOutput_StreamType } from "@/types/proto-es/v1/command_pb";

interface CommandTimelineProps {
  outputs: CommandOutput[];
  events: CommandEvent[];
  className?: string;
  // scrollToSeqNo jumps the timeline to the tool card whose started event has
  // this seq_no (used by the events panel to locate the corresponding output).
  scrollToSeqNo?: number | null;
  // activeSeqNo highlights the tool card with the matching started seq_no.
  activeSeqNo?: number | null;
  onToolCardClick?: (seqNo: number) => void;
  // active is false while the containing tab is hidden (keepMounted). When the
  // panel becomes visible again, re-apply the auto-scroll position so content
  // that arrived while hidden does not leave the user at the top.
  active?: boolean;
}

type TimelineItem =
  | { kind: "output"; ts: number; output: CommandOutput }
  | { kind: "tool"; ts: number; pair: ToolCallPair };

// A run of consecutive same-stream output chunks merged into one text block.
// Chunks arrive at flush boundaries (per-token for pi, per-~4KB for ACP); each
// renders as its own block-level div, so unmerged output splits words across
// lines. LLM tokens carry their own whitespace, so concatenating chunk contents
// reproduces the original text exactly (same as the backend outputBuffer does
// before flushing). Tool cards and stream-type changes break a run.
type MergedOutput = {
  type: CommandOutput_StreamType;
  content: string;
  key: string;
};

type RenderItem =
  | { kind: "output"; merged: MergedOutput }
  | { kind: "tool"; pair: ToolCallPair };

function tsToMs(ts: { seconds?: bigint; nanos?: number } | undefined): number {
  if (!ts?.seconds) return 0;
  return Number(ts.seconds) * 1000 + (ts.nanos ?? 0) / 1_000_000;
}

// Renders the command's stdout/stderr/system stream interleaved with the
// structured tool-call cards (TOOL_CALL_STARTED/FINISHED), ordered by
// timestamp so the page reads like a terminal log of "agent ran X → output Y"
// rather than two disconnected panels.
export function CommandTimeline({
  outputs,
  events,
  className,
  scrollToSeqNo,
  activeSeqNo,
  onToolCardClick,
  active = true,
}: CommandTimelineProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const { onScroll, autoScrollRef } = useAutoScroll(scrollRef, [
    outputs,
    events,
  ]);

  useEffect(() => {
    if (!active || !scrollRef.current || !autoScrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [active, autoScrollRef]);

  useEffect(() => {
    if (scrollToSeqNo == null || !scrollRef.current) return;
    const el = scrollRef.current.querySelector<HTMLElement>(
      `[data-tool-seq="${scrollToSeqNo}"]`
    );
    if (!el) return;
    // Jumping is a user-initiated navigation: stop pinning to the bottom so
    // subsequent output chunks do not yank the viewport back.
    autoScrollRef.current = false;
    const container = scrollRef.current;
    const targetTop =
      el.getBoundingClientRect().top -
      container.getBoundingClientRect().top +
      container.scrollTop;
    container.scrollTo({
      top: Math.max(0, targetTop - 12),
      behavior: "smooth",
    });
  }, [scrollToSeqNo, autoScrollRef]);

  const items = useMemo<TimelineItem[]>(() => {
    const pairs = pairToolCallEvents(events);
    const toolItems: TimelineItem[] = pairs.map((pair) => ({
      kind: "tool" as const,
      ts: tsToMs(pair.started.timestamp),
      pair,
    }));
    const outputItems: TimelineItem[] = outputs.map((o) => ({
      kind: "output" as const,
      ts: tsToMs(o.timestamp),
      output: o,
    }));
    return [...outputItems, ...toolItems].sort((a, b) => a.ts - b.ts);
  }, [outputs, events]);

  const renderItems = useMemo<RenderItem[]>(() => {
    const out: RenderItem[] = [];
    for (const item of items) {
      if (item.kind === "tool") {
        out.push({ kind: "tool", pair: item.pair });
        continue;
      }
      const o = item.output;
      const last = out[out.length - 1];
      if (last && last.kind === "output" && last.merged.type === o.type) {
        last.merged.content += o.content;
        continue;
      }
      out.push({
        kind: "output",
        merged: {
          type: o.type,
          content: o.content,
          key: `out-${o.commandId}-${o.seqNo}`,
        },
      });
    }
    return out;
  }, [items]);

  if (outputs.length === 0 && items.length === 0) {
    return (
      <div
        className={cn(
          "rounded bg-dark-bg p-4 font-mono text-xs text-matrix-green/50",
          className
        )}
      >
        Waiting for output...
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className={cn(
        "rounded bg-dark-bg p-4 font-mono text-xs overflow-auto min-w-0",
        className
      )}
    >
      {renderItems.map((item) => {
        if (item.kind === "output") {
          const m = item.merged;
          return (
            <div
              key={m.key}
              className={cn("whitespace-pre-wrap break-all", {
                "text-matrix-green": m.type === CommandOutput_StreamType.STDOUT,
                "text-error": m.type === CommandOutput_StreamType.STDERR,
                "text-warning": m.type === CommandOutput_StreamType.SYSTEM,
              })}
            >
              {m.content}
            </div>
          );
        }
        return (
          <div
            key={`tool-${item.pair.started.seqNo}`}
            data-tool-seq={item.pair.started.seqNo}
            onClick={
              onToolCardClick
                ? () => onToolCardClick(item.pair.started.seqNo)
                : undefined
            }
            className={cn(
              "my-2 not-italic",
              onToolCardClick && "cursor-pointer",
              activeSeqNo === item.pair.started.seqNo &&
                "ring-2 ring-accent rounded-lg"
            )}
          >
            <ChatToolCall
              startedEvent={item.pair.started}
              finishedEvent={item.pair.finished}
            />
          </div>
        );
      })}
    </div>
  );
}
