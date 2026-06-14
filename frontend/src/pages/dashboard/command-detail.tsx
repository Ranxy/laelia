import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { CommandTerminal } from "@/react/components/command-terminal";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import { useAppStore } from "@/react/stores";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

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

export function CommandDetailPage() {
  const navigate = useNavigate();
  const { agentId, commandId } = useParams<{
    agentId: string;
    commandId: string;
  }>();
  const getCommand = useAppStore((s) => s.getCommand);
  const cancelCommand = useAppStore((s) => s.cancelCommand);
  const watchCommand = useAppStore((s) => s.watchCommand);
  const activeOutputs = useAppStore((s) => s.activeOutputs);
  const abortRef = useRef<AbortController | null>(null);

  const [cmd, setCmd] = useState<{
    name: string;
    command: string;
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

    watchCommand(cmdName, controller.signal).catch(() => {
      // stream aborted or failed — expected on unmount
    });

    return () => {
      controller.abort();
    };
  }, [cmdName, watchCommand]);

  const outputs = activeOutputs[cmdName] ?? [];

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

  const isRunning =
    cmd &&
    (cmd.status === CommandStatus.PENDING ||
      cmd.status === CommandStatus.RUNNING);

  return (
    <div className="p-6 flex flex-col gap-4 w-full">
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
                {cmd.command}
              </h1>
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

          <div className="flex gap-6 text-sm text-control-light">
            <span>Duration: {formatDuration(cmd.durationMs)}</span>
            {cmd.exitCode !== 0 &&
              cmd.status === CommandStatus.FAILED &&
              cmd.exitCode !== undefined && (
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
        </>
      )}

      <CommandTerminal outputs={outputs} className="min-h-[400px]" />
    </div>
  );
}
