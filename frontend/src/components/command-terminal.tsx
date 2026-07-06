import MarkdownRender from "markstream-react";
import { useRef } from "react";
import { useAutoScroll } from "@/lib/use-auto-scroll";
import { cn } from "@/lib/utils";
import type { CommandOutput } from "@/types/proto-es/v1/command_pb";
import { CommandOutput_StreamType } from "@/types/proto-es/v1/command_pb";

interface CommandTerminalProps {
  outputs: CommandOutput[];
  className?: string;
}

export function CommandTerminal({ outputs, className }: CommandTerminalProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const { onScroll } = useAutoScroll(scrollRef, [outputs]);

  if (outputs.length === 0) {
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
        "rounded bg-dark-bg p-4 font-mono text-xs overflow-auto max-h-96",
        className
      )}
    >
      {outputs.map((o) => (
        <div
          key={`${o.commandId}-${o.seqNo}`}
          className={cn("whitespace-pre-wrap break-all", {
            "text-matrix-green": o.type === CommandOutput_StreamType.STDOUT,
            "text-error": o.type === CommandOutput_StreamType.STDERR,
            "text-warning": o.type === CommandOutput_StreamType.SYSTEM,
          })}
        >
          {o.content}
        </div>
      ))}
    </div>
  );
}

interface FinalSummaryProps {
  content: string;
  className?: string;
}

export function FinalSummary({ content, className }: FinalSummaryProps) {
  return (
    <div className={cn("markstream-chat", className)}>
      <MarkdownRender
        customId="command-summary"
        content={content}
        final
        smoothStreaming={false}
        fade
      />
    </div>
  );
}
