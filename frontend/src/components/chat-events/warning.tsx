import { AlertTriangle } from "lucide-react";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

interface ChatWarningProps {
  event: CommandEvent;
}

export function ChatWarning({ event }: ChatWarningProps) {
  if (event.payload.case !== "warning") return null;
  const { message } = event.payload.value;

  return (
    <div className="rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 flex items-center gap-2 text-xs">
      <AlertTriangle className="size-3.5 shrink-0 text-warning" />
      <span className="text-warning">{message}</span>
    </div>
  );
}
