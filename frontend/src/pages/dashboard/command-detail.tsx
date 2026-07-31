import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { FinalSummary } from "@/components/command-terminal";
import { CommandTimeline } from "@/components/command-timeline";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  commandEventTypeToI18nKey,
  formatDuration,
  formatTimeOfDay,
  formatTimestamp,
} from "@/lib/command-status";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandStatus,
} from "@/types/proto-es/v1/command_pb";

const eventTypeColors: Record<number, string> = {
  [CommandEventType.LIFECYCLE]: "text-control-light",
  [CommandEventType.TOOL_CALL_STARTED]: "text-info",
  [CommandEventType.TOOL_CALL_FINISHED]: "text-success",
  [CommandEventType.DIFF_EMITTED]: "text-accent",
  [CommandEventType.WARNING]: "text-warning",
  [CommandEventType.RAW_ACP]: "text-control-light",
  [CommandEventType.FINAL_SUMMARY]: "text-success",
  [CommandEventType.PERMISSION_REQUESTED]: "text-warning",
  [CommandEventType.PERMISSION_TIMED_OUT]: "text-error",
  [CommandEventType.PERMISSION_DECIDED]: "text-success",
  [CommandEventType.CONTEXT_COMPACTION_STARTED]: "text-warning",
  [CommandEventType.CONTEXT_COMPACTION_FINISHED]: "text-success",
  [CommandEventType.CONTEXT_USAGE_UPDATE]: "text-info",
};

function isVisibleEvent(event: CommandEvent): boolean {
  return (
    event.type !== CommandEventType.TEXT_DELTA &&
    event.type !== CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
  );
}

function EventRow({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const isRaw = event.type === CommandEventType.RAW_ACP;
  const showExpand = isRaw || event.type === CommandEventType.DIFF_EMITTED;

  const labelKey =
    commandEventTypeToI18nKey[event.type] ?? "command.event-unknown";

  return (
    <div
      className={cn(
        "flex flex-col gap-1 py-1.5 px-3 rounded border border-control-border",
        isRaw && "opacity-60"
      )}
    >
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "text-xs font-medium",
            eventTypeColors[event.type] ?? "text-control"
          )}
        >
          {t(labelKey)}
        </span>
        <span className="text-xs text-control-light">#{event.seqNo}</span>
        {event.summary && (
          <span className="text-xs text-control truncate flex-1">
            {event.summary.slice(0, 120)}
          </span>
        )}
        {showExpand && (
          <Button
            variant="ghost"
            size="sm"
            className="text-xs"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? t("command.collapse") : t("command.details")}
          </Button>
        )}
        <span className="text-xs text-control-light ml-auto">
          {formatTimeOfDay(event.timestamp)}
        </span>
      </div>
      {expanded && event.payload.value && (
        <pre className="text-xs text-matrix-green font-mono bg-dark-bg rounded p-2 mt-1 overflow-auto max-h-64 whitespace-pre-wrap break-all min-w-0">
          {JSON.stringify(event.payload.value, null, 2)}
        </pre>
      )}
    </div>
  );
}

// Renders a tool-call pair as a structured card (title + input + output +
// status) inside an EventRow-style header. Used for TOOL_CALL_STARTED (with
// its paired finished) and orphan TOOL_CALL_FINISHED events. The card itself
// comes from the shared ChatToolCall component so command-detail and chat
// render tool calls identically.
function ToolEventRow({
  event,
  startedEvent,
  finishedEvent,
}: {
  event: CommandEvent;
  startedEvent?: CommandEvent;
  finishedEvent?: CommandEvent;
}) {
  const { t } = useTranslation();
  const labelKey =
    commandEventTypeToI18nKey[event.type] ?? "command.event-unknown";

  return (
    <div className="flex flex-col gap-1 py-1.5 px-1">
      <div className="flex items-center gap-2 px-2">
        <span
          className={cn(
            "text-xs font-medium",
            eventTypeColors[event.type] ?? "text-control"
          )}
        >
          {t(labelKey)}
        </span>
        <span className="text-xs text-control-light">#{event.seqNo}</span>
        <span className="text-xs text-control-light ml-auto">
          {formatTimeOfDay(event.timestamp)}
        </span>
      </div>
      <ChatToolCall startedEvent={startedEvent} finishedEvent={finishedEvent} />
    </div>
  );
}

export function CommandDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId, commandId } = useParams<{
    agentId: string;
    commandId: string;
  }>();
  const getCommand = useAppStore((s) => s.getCommand);
  const cancelCommand = useAppStore((s) => s.cancelCommand);
  const watchCommand = useAppStore((s) => s.watchCommand);
  const watchCommandEvents = useAppStore((s) => s.watchCommandEvents);
  const activeOutputs = useAppStore((s) => s.activeOutputs);
  const activeEvents = useAppStore((s) => s.activeEvents);
  const abortRef = useRef<AbortController | null>(null);

  const [cmd, setCmd] = useState<{
    name: string;
    command: string;
    instruction: string;
    profile: string;
    finalSummary: string;
    status: number;
    exitCode: number;
    durationMs: bigint;
    principalName: string;
    errorMessage: string;
    created: string;
  } | null>(null);
  const [cancelling, setCancelling] = useState(false);

  const cmdName = `agents/${agentId}/commands/${commandId}`;

  const load = useCallback(async () => {
    if (!cmdName) return;
    const c = await getCommand(cmdName);
    if (!c) return;
    setCmd({
      name: c.name,
      command: c.command,
      instruction: c.instruction,
      profile: c.profile,
      finalSummary: c.finalSummary,
      status: c.status,
      exitCode: c.exitCode,
      durationMs: c.durationMs,
      principalName: c.principalName,
      errorMessage: c.errorMessage,
      created: formatTimestamp(c.createdAt),
    });
  }, [cmdName, getCommand]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!cmdName) return;

    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    watchCommand(cmdName, controller.signal).catch(() => {});
    watchCommandEvents(cmdName, controller.signal).catch(() => {});

    return () => {
      controller.abort();
    };
  }, [cmdName, watchCommand, watchCommandEvents]);

  const outputs = activeOutputs[cmdName] ?? [];
  const events = activeEvents[cmdName] ?? [];
  const visibleEvents = events.filter(isVisibleEvent);

  // Pair TOOL_CALL_STARTED with TOOL_CALL_FINISHED so the Events panel shows
  // one structured card per tool call (input + output) instead of two
  // disconnected rows whose payloads are never expanded today.
  const toolPairs = useMemo(
    () => pairToolCallEvents(visibleEvents),
    [visibleEvents]
  );
  const toolPairByStartedSeqNo = useMemo(() => {
    const m = new Map<number, ToolCallPair>();
    for (const p of toolPairs) m.set(p.started.seqNo, p);
    return m;
  }, [toolPairs]);
  const pairedFinishedSeqNos = useMemo(() => {
    const s = new Set<number>();
    for (const p of toolPairs) if (p.finished) s.add(p.finished.seqNo);
    return s;
  }, [toolPairs]);

  const renderEvent = (ev: CommandEvent) => {
    if (ev.type === CommandEventType.TOOL_CALL_STARTED) {
      const pair = toolPairByStartedSeqNo.get(ev.seqNo);
      return (
        <ToolEventRow
          key={ev.seqNo}
          event={ev}
          startedEvent={ev}
          finishedEvent={pair?.finished}
        />
      );
    }
    if (ev.type === CommandEventType.TOOL_CALL_FINISHED) {
      // Paired finished events are already rendered inside the started card.
      if (pairedFinishedSeqNos.has(ev.seqNo)) return null;
      return (
        <ToolEventRow
          key={ev.seqNo}
          event={ev}
          startedEvent={undefined}
          finishedEvent={ev}
        />
      );
    }
    return <EventRow key={ev.seqNo} event={ev} />;
  };

  const isRunning =
    cmd &&
    (cmd.status === CommandStatus.PENDING ||
      cmd.status === CommandStatus.RUNNING);

  const pendingPermission = useMemo(() => {
    if (!isRunning) return null;

    const requested = events.filter(
      (ev) => ev.type === CommandEventType.PERMISSION_REQUESTED
    );
    if (requested.length === 0) return null;

    const latest = requested[requested.length - 1];

    const decided = events.some(
      (ev) =>
        (ev.type === CommandEventType.PERMISSION_TIMED_OUT ||
          ev.type === CommandEventType.PERMISSION_DECIDED) &&
        ev.seqNo > latest.seqNo
    );
    if (decided) return null;

    return latest;
  }, [events, isRunning]);

  const respondPermission = useAppStore((s) => s.respondPermission);
  const [permissionResponded, setPermissionResponded] = useState(false);

  const handleRespondPermission = async (optionId: string) => {
    if (!cmdName) return;
    setPermissionResponded(true);
    try {
      await respondPermission(cmdName, optionId);
    } catch {
      setPermissionResponded(false);
    }
  };

  useEffect(() => {
    setPermissionResponded(false);
  }, [cmdName]);

  const handleCancel = async () => {
    if (!cmdName) return;
    setCancelling(true);
    try {
      await cancelCommand(cmdName);
      load();
    } finally {
      setCancelling(false);
    }
  };

  return (
    <div className="flex flex-col h-full overflow-y-auto">
      <div className="flex-1 p-4 flex flex-col gap-4 w-full">
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate(`/agents/${agentId}/commands`)}
          >
            &larr; {t("command.back")}
          </Button>
        </div>

        {cmd && (
          <>
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-3 min-w-0">
                <h1 className="text-lg font-mono font-semibold text-main truncate max-w-xl">
                  {cmd.instruction || cmd.command}
                </h1>
                <Badge variant="secondary">{t("command.executor-acp")}</Badge>
                <CommandStatusBadge status={cmd.status} />
              </div>
              {isRunning && (
                <Button
                  variant="outline"
                  onClick={handleCancel}
                  disabled={cancelling}
                >
                  {cancelling ? t("command.cancelling") : t("common.cancel")}
                </Button>
              )}
            </div>

            <div className="flex gap-6 text-sm text-control-light flex-wrap">
              <span>
                {t("command.duration")}: {formatDuration(cmd.durationMs)}
              </span>
              {cmd.exitCode !== undefined && cmd.exitCode !== 0 && (
                <span>
                  {t("command.exit-code")}: {cmd.exitCode}
                </span>
              )}
              {cmd.principalName && (
                <span>
                  {t("command.sent-by")}: {cmd.principalName}
                </span>
              )}
              <span>{cmd.created}</span>
            </div>

            {cmd.errorMessage && (
              <div className="rounded bg-error/10 border border-control-border p-3 text-sm text-error">
                {cmd.errorMessage}
              </div>
            )}

            {cmd.finalSummary && (
              <div className="rounded bg-accent/10 border border-control-border p-3">
                <div className="text-xs font-medium text-control mb-2">
                  {t("command.final-summary")}
                </div>
                <FinalSummary content={cmd.finalSummary} />
              </div>
            )}
          </>
        )}

        <div className="flex-1 min-h-0 flex flex-col gap-4 lg:grid lg:grid-cols-[minmax(0,1fr)_380px] lg:grid-rows-[minmax(0,1fr)]">
          <div className="flex flex-col gap-2 min-w-0 min-h-0">
            <h2 className="text-sm font-medium text-control">
              {t("command.output")}
            </h2>
            <CommandTimeline
              outputs={outputs}
              events={visibleEvents}
              className="min-h-[300px] flex-1 min-h-0"
            />
          </div>

          {visibleEvents.length > 0 && (
            <div className="flex flex-col gap-2 min-w-0 min-h-0">
              <h2 className="text-sm font-medium text-control">
                {t("command.events")}
              </h2>
              <div className="rounded border border-control-border p-2 flex flex-col gap-1 flex-1 min-h-0 overflow-auto max-h-[400px] lg:max-h-none">
                {visibleEvents.map((ev) => renderEvent(ev))}
              </div>
            </div>
          )}

          {cmd &&
            visibleEvents.length === 0 &&
            cmd.status === CommandStatus.RUNNING && (
              <div className="flex flex-col gap-2 min-w-0 min-h-0">
                <h2 className="text-sm font-medium text-control">
                  {t("command.events")}
                </h2>
                <div className="rounded border border-control-border p-4 text-xs text-control-light">
                  {t("command.waiting-events")}
                </div>
              </div>
            )}
        </div>
      </div>

      {pendingPermission &&
        pendingPermission.payload.case === "permissionRequested" &&
        !permissionResponded && (
          <div className="sticky bottom-0 bg-background border-t border-control-border p-3">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <Badge variant="warning" className="shrink-0">
                  {pendingPermission.payload.value.kind}
                </Badge>
                <span className="text-sm text-main truncate">
                  {pendingPermission.payload.value.title ||
                    t("command.permission-required")}
                </span>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {pendingPermission.payload.value.options.map((opt) => (
                  <Button
                    key={opt.optionId}
                    variant={
                      opt.kind === "allow_once" || opt.kind === "allow_always"
                        ? "default"
                        : "outline"
                    }
                    size="sm"
                    onClick={() => handleRespondPermission(opt.optionId)}
                  >
                    {opt.name}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        )}
    </div>
  );
}
