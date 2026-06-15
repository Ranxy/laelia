import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { CommandTerminal } from "@/react/components/command-terminal";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import { cn } from "@/react/lib/utils";
import { useAppStore } from "@/react/stores";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandStatus,
  ExecutorKind,
} from "@/types/proto-es/v1/command_pb";

const statusLabels: Record<number, string> = {
  [CommandStatus.PENDING]: "Pending",
  [CommandStatus.RUNNING]: "Running",
  [CommandStatus.COMPLETED]: "Completed",
  [CommandStatus.FAILED]: "Failed",
  [CommandStatus.CANCELLED]: "Cancelled",
  [CommandStatus.TIMEOUT]: "Timeout",
};

const statusVariants: Record<
  number,
  "default" | "secondary" | "success" | "warning" | "destructive"
> = {
  [CommandStatus.PENDING]: "secondary",
  [CommandStatus.RUNNING]: "warning",
  [CommandStatus.COMPLETED]: "success",
  [CommandStatus.FAILED]: "destructive",
  [CommandStatus.CANCELLED]: "destructive",
  [CommandStatus.TIMEOUT]: "destructive",
};

const eventTypeLabels: Record<number, string> = {
  [CommandEventType.LIFECYCLE]: "Lifecycle",
  [CommandEventType.TEXT_DELTA]: "Text",
  [CommandEventType.TOOL_CALL_STARTED]: "Tool Started",
  [CommandEventType.TOOL_CALL_FINISHED]: "Tool Finished",
  [CommandEventType.DIFF_EMITTED]: "Diff",
  [CommandEventType.WARNING]: "Warning",
  [CommandEventType.RAW_ACP]: "Raw ACP",
  [CommandEventType.FINAL_SUMMARY]: "Final Summary",
  [CommandEventType.PERMISSION_REQUESTED]: "Permission Requested",
  [CommandEventType.PERMISSION_TIMED_OUT]: "Permission Timed Out",
  [CommandEventType.PERMISSION_DECIDED]: "Permission Decided",
};

const eventTypeColors: Record<number, string> = {
  [CommandEventType.LIFECYCLE]: "text-control-light",
  [CommandEventType.TOOL_CALL_STARTED]: "text-blue-400",
  [CommandEventType.TOOL_CALL_FINISHED]: "text-green-400",
  [CommandEventType.DIFF_EMITTED]: "text-purple-400",
  [CommandEventType.WARNING]: "text-amber-400",
  [CommandEventType.RAW_ACP]: "text-zinc-500",
  [CommandEventType.FINAL_SUMMARY]: "text-emerald-400",
  [CommandEventType.PERMISSION_REQUESTED]: "text-amber-400",
  [CommandEventType.PERMISSION_TIMED_OUT]: "text-red-400",
  [CommandEventType.PERMISSION_DECIDED]: "text-green-400",
};

function formatDuration(ms: number | bigint | undefined): string {
  if (ms === undefined || ms === 0n) return "-";
  const num = Number(ms);
  if (num < 1000) return `${num}ms`;
  if (num < 60000) return `${(num / 1000).toFixed(1)}s`;
  return `${(num / 60000).toFixed(1)}m`;
}

function formatTimestamp(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "-";
  return new Date(Number(ts.seconds) * 1000).toLocaleString();
}

function formatEventTimestamp(ev: CommandEvent): string {
  if (ev.timestamp?.seconds) {
    return new Date(Number(ev.timestamp.seconds) * 1000).toLocaleTimeString();
  }
  return "";
}

function isVisibleEvent(event: CommandEvent): boolean {
  return (
    event.type !== CommandEventType.TEXT_DELTA &&
    event.type !== CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
  );
}

function EventRow({ event }: { event: CommandEvent }) {
  const [expanded, setExpanded] = useState(false);
  const isRaw = event.type === CommandEventType.RAW_ACP;
  const showExpand = isRaw || event.type === CommandEventType.DIFF_EMITTED;

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
          {eventTypeLabels[event.type] ?? `Event ${event.type}`}
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
            {expanded ? "Collapse" : "Details"}
          </Button>
        )}
        <span className="text-xs text-control-light ml-auto">
          {formatEventTimestamp(event)}
        </span>
      </div>
      {expanded && event.payload.value && (
        <pre className="text-xs text-control-light font-mono bg-zinc-900 rounded p-2 mt-1 overflow-auto max-h-64 whitespace-pre-wrap">
          {JSON.stringify(event.payload.value, null, 2)}
        </pre>
      )}
    </div>
  );
}

export function CommandDetailPage() {
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
    executorKind: number;
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
      executorKind: c.executorKind,
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

  const isRunning =
    cmd &&
    (cmd.status === CommandStatus.PENDING ||
      cmd.status === CommandStatus.RUNNING);
  const isACP = cmd && cmd.executorKind === ExecutorKind.ACP;

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
    <div className="flex flex-col min-h-full">
      <div className="flex-1 p-6 flex flex-col gap-4 w-full">
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate(`/agents/${agentId}/commands`)}
          >
            &larr; Back
          </Button>
        </div>

        {cmd && (
          <>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <h1 className="text-lg font-mono font-semibold text-main truncate max-w-xl">
                  {cmd.instruction || cmd.command}
                </h1>
                {isACP && <Badge variant="secondary">ACP</Badge>}
                <Badge variant={statusVariants[cmd.status] ?? "default"}>
                  {statusLabels[cmd.status] ?? "Unknown"}
                </Badge>
              </div>
              {isRunning && (
                <Button
                  variant="outline"
                  onClick={handleCancel}
                  disabled={cancelling}
                >
                  {cancelling ? "Cancelling..." : "Cancel"}
                </Button>
              )}
            </div>

            {isACP && cmd.profile && (
              <div className="text-xs text-control-light">
                Profile: {cmd.profile}
              </div>
            )}

            <div className="flex gap-6 text-sm text-control-light">
              <span>Duration: {formatDuration(cmd.durationMs)}</span>
              {cmd.exitCode !== undefined && cmd.exitCode !== 0 && (
                <span>Exit code: {cmd.exitCode}</span>
              )}
              {cmd.principalName && <span>Sent by: {cmd.principalName}</span>}
              <span>{cmd.created}</span>
            </div>

            {cmd.errorMessage && (
              <div className="rounded bg-error/10 border border-control-border p-3 text-sm text-error">
                {cmd.errorMessage}
              </div>
            )}

            {cmd.finalSummary && (
              <div className="rounded bg-accent/10 border border-control-border p-3">
                <div className="text-xs font-medium text-control mb-1">
                  Final Summary
                </div>
                <div className="text-sm text-main whitespace-pre-wrap">
                  {cmd.finalSummary}
                </div>
              </div>
            )}
          </>
        )}

        <div className="flex flex-col gap-4 lg:flex-row">
          <div className="flex-1 flex flex-col gap-2">
            <h2 className="text-sm font-medium text-control">Output</h2>
            <CommandTerminal outputs={outputs} className="min-h-[300px]" />
          </div>

          {isACP && visibleEvents.length > 0 && (
            <div className="flex-1 flex flex-col gap-2">
              <h2 className="text-sm font-medium text-control">Events</h2>
              <div className="rounded border border-control-border p-2 flex flex-col gap-1 max-h-[400px] overflow-auto">
                {visibleEvents.map((ev) => (
                  <EventRow key={ev.seqNo} event={ev} />
                ))}
              </div>
            </div>
          )}

          {isACP &&
            visibleEvents.length === 0 &&
            cmd.status === CommandStatus.RUNNING && (
              <div className="flex-1 flex flex-col gap-2">
                <h2 className="text-sm font-medium text-control">Events</h2>
                <div className="rounded border border-control-border p-4 text-xs text-control-light">
                  Waiting for structured events...
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
                    "Permission required"}
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
