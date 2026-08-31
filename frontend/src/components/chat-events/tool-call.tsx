import { ChevronDown, ChevronRight, Wrench } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { isToolCallError } from "@/lib/command-events-model";
import { safeStringify } from "@/lib/safe-stringify";
import { cn } from "@/lib/utils";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

interface ChatToolCallProps {
  startedEvent?: CommandEvent;
  finishedEvent?: CommandEvent;
}

export function ChatToolCall({
  startedEvent,
  finishedEvent,
}: ChatToolCallProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);

  const title =
    startedEvent?.payload.case === "toolCallStarted"
      ? startedEvent.payload.value.title
      : t("chat.tool-call");

  const status =
    finishedEvent?.payload.case === "toolCallFinished"
      ? finishedEvent.payload.value.status
      : undefined;

  const rawInput =
    startedEvent?.payload.case === "toolCallStarted"
      ? startedEvent.payload.value.rawInput
      : undefined;

  const rawOutput =
    finishedEvent?.payload.case === "toolCallFinished"
      ? finishedEvent.payload.value.rawOutput
      : undefined;

  const isFinished = !!finishedEvent;

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
        <Wrench className="size-3.5 shrink-0 text-accent" />
        <span className="font-medium text-control truncate flex-1 text-left">
          {title}
        </span>
        {isFinished ? (
          // The one shared error predicate (08 F-R3): a failed tool call now
          // renders as an error badge in chat too, instead of a grey
          // "finished" pill that misrepresents the result.
          <Badge
            size="sm"
            variant={isToolCallError(status) ? "error" : "success"}
          >
            {isToolCallError(status)
              ? t("chat.tool-error")
              : t("chat.tool-finished")}
          </Badge>
        ) : (
          <Badge size="sm" variant="warning">
            {t("chat.tool-started")}
          </Badge>
        )}
      </button>
      {expanded && (
        <div className="border-t border-control-border px-3 py-2 flex flex-col gap-2">
          {rawInput !== undefined && (
            <div>
              <div className="text-[10px] font-medium text-control-light mb-1">
                {t("chat.tool-input")}
              </div>
              <pre className="text-[11px] font-mono text-control bg-background/50 rounded p-2 overflow-auto max-h-40 whitespace-pre-wrap break-all min-w-0">
                {safeStringify(rawInput)}
              </pre>
            </div>
          )}
          {rawInput === undefined && (
            <div>
              <div className="text-[10px] font-medium text-control-light mb-1">
                {t("chat.tool-input")}
              </div>
              <div className="text-[11px] text-control-light italic">
                {t("chat.input-not-captured")}
              </div>
            </div>
          )}
          {rawOutput !== undefined && (
            <div>
              <div className="text-[10px] font-medium text-control-light mb-1">
                {t("chat.tool-output")}
              </div>
              <pre
                className={cn(
                  "text-[11px] font-mono text-control bg-background/50 rounded p-2 overflow-auto max-h-40 whitespace-pre-wrap break-all min-w-0"
                )}
              >
                {safeStringify(rawOutput)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
