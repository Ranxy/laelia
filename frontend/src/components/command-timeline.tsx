import { useMemo, useRef } from "react";
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
}

type TimelineItem =
  | { kind: "output"; ts: number; output: CommandOutput }
  | { kind: "tool"; ts: number; pair: ToolCallPair };

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
}: CommandTimelineProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const { onScroll } = useAutoScroll(scrollRef, [outputs, events]);

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
      {items.map((item) => {
        if (item.kind === "output") {
          const o = item.output;
          return (
            <div
              key={`out-${o.commandId}-${o.seqNo}`}
              className={cn("whitespace-pre-wrap break-all", {
                "text-matrix-green": o.type === CommandOutput_StreamType.STDOUT,
                "text-error": o.type === CommandOutput_StreamType.STDERR,
                "text-warning": o.type === CommandOutput_StreamType.SYSTEM,
              })}
            >
              {o.content}
            </div>
          );
        }
        return (
          <div
            key={`tool-${item.pair.started.seqNo}`}
            className="my-2 not-italic"
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
