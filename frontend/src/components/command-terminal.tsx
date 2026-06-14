import { useEffect, useRef } from "react";
import { cn } from "@/react/lib/utils";
import type { CommandOutput } from "@/types/proto-es/v1/command_pb";
import { CommandOutput_StreamType } from "@/types/proto-es/v1/command_pb";

interface CommandTerminalProps {
  outputs: CommandOutput[];
  className?: string;
}

export function CommandTerminal({ outputs, className }: CommandTerminalProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(true);

  useEffect(() => {
    if (autoScrollRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [outputs]);

  const handleScroll = () => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 40;
  };

  if (outputs.length === 0) {
    return (
      <div
        className={cn(
          "rounded bg-zinc-950 p-4 font-mono text-xs text-zinc-500",
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
      onScroll={handleScroll}
      className={cn(
        "rounded bg-zinc-950 p-4 font-mono text-xs overflow-auto max-h-96",
        className
      )}
    >
      {outputs.map((o) => (
        <div
          key={`${o.commandId}-${o.seqNo}`}
          className={cn("whitespace-pre-wrap break-all", {
            "text-green-400": o.type === CommandOutput_StreamType.STDOUT,
            "text-red-400": o.type === CommandOutput_StreamType.STDERR,
            "text-yellow-400": o.type === CommandOutput_StreamType.SYSTEM,
          })}
        >
          {o.content}
        </div>
      ))}
    </div>
  );
}
