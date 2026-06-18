import { ChevronDown, ChevronRight, FileDiff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

interface ChatDiffProps {
  event: CommandEvent;
}

export function ChatDiff({ event }: ChatDiffProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  if (event.payload.case !== "diffEmitted") return null;

  const { path, oldText, newText } = event.payload.value;

  const oldLines = oldText.split("\n");
  const newLines = newText.split("\n");

  return (
    <div className="rounded-lg border border-control-border bg-control-bg/50 overflow-hidden">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-3 py-2 text-xs hover:bg-link-hover transition-colors"
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0 text-control-light" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0 text-control-light" />
        )}
        <FileDiff className="size-3.5 shrink-0 text-accent" />
        <span className="font-medium text-control truncate flex-1 text-left font-mono">
          {path}
        </span>
        <span className="text-[10px] text-control-light shrink-0">
          {t("chat.diff")}
        </span>
      </button>
      {expanded && (
        <div className="border-t border-control-border overflow-auto max-h-64">
          <div className="grid grid-cols-2 text-[11px] font-mono">
            <div className="border-r border-control-border">
              <div className="px-2 py-1 text-[10px] font-medium text-control-light bg-error/5 border-b border-control-border">
                Old
              </div>
              {oldLines.map((line, i) => (
                <div
                  key={i}
                  className="px-2 py-0.5 text-error/70 whitespace-pre-wrap break-all"
                >
                  <span className="text-control-light/50 select-none mr-1">
                    -
                  </span>
                  {line}
                </div>
              ))}
            </div>
            <div>
              <div className="px-2 py-1 text-[10px] font-medium text-control-light bg-success/5 border-b border-control-border">
                New
              </div>
              {newLines.map((line, i) => (
                <div
                  key={i}
                  className="px-2 py-0.5 text-success/70 whitespace-pre-wrap break-all"
                >
                  <span className="text-control-light/50 select-none mr-1">
                    +
                  </span>
                  {line}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
