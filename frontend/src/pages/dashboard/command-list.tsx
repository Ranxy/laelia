import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/react/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/react/components/ui/table";
import { Textarea } from "@/react/components/ui/textarea";
import { useAppStore } from "@/react/stores";
import { type Command, CommandStatus } from "@/types/proto-es/v1/command_pb";

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

export function CommandListPage() {
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const commands = useAppStore((s) => s.commands);
  const loading = useAppStore((s) => s.commandsLoading);
  const listCommands = useAppStore((s) => s.listCommands);
  const sendCommand = useAppStore((s) => s.sendCommand);

  const [sendOpen, setSendOpen] = useState(false);
  const [cmdText, setCmdText] = useState("");
  const [sending, setSending] = useState(false);

  const agent = `agents/${agentId}`;

  const load = useCallback(() => {
    if (!agent) return;
    listCommands(agent, { pageSize: 100 });
  }, [agent, listCommands]);

  useEffect(() => {
    load();
  }, [load]);

  const handleSend = async () => {
    if (!cmdText.trim() || !agent) return;
    setSending(true);
    try {
      await sendCommand(agent, cmdText.trim());
      setCmdText("");
      setSendOpen(false);
      load();
    } finally {
      setSending(false);
    }
  };

  function handleRowClick(cmd: Command) {
    if (!cmd.name) return;
    navigate(`/agents/${agentId}/commands/${cmd.name.split("/").pop()}`);
  }

  return (
    <div className="p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">Commands</h1>
        <Button onClick={() => setSendOpen(true)}>Send Command</Button>
      </div>

      <Sheet
        open={sendOpen}
        onOpenChange={(next) => !next && setSendOpen(false)}
      >
        <SheetContent width="standard">
          <SheetTitle>Send Command</SheetTitle>
          <SheetDescription>
            Send a bash command to agent {agentId}
          </SheetDescription>
          <div className="flex flex-col gap-4 pt-4">
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">Command</span>
              <Textarea
                className="font-mono text-sm min-h-[120px]"
                placeholder="e.g. apt-get update -y"
                value={cmdText}
                onChange={(e) => setCmdText(e.target.value)}
              />
            </div>
            <Button disabled={sending || !cmdText.trim()} onClick={handleSend}>
              {sending ? "Sending..." : "Send"}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-24">Status</TableHead>
            <TableHead>Command</TableHead>
            <TableHead className="w-28">Duration</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading && (
            <TableRow>
              <TableCell colSpan={3} className="text-center text-control-light">
                Loading...
              </TableCell>
            </TableRow>
          )}
          {!loading && commands.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="text-center text-control-light">
                No commands yet
              </TableCell>
            </TableRow>
          )}
          {commands.map((cmd) => (
            <TableRow
              key={cmd.name}
              className="cursor-pointer"
              onClick={() => handleRowClick(cmd)}
            >
              <TableCell>
                <Badge variant={statusVariants[cmd.status] ?? "default"}>
                  {statusLabels[cmd.status] ?? "Unknown"}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs truncate max-w-md">
                {cmd.command}
              </TableCell>
              <TableCell className="text-control-light text-xs">
                {formatDuration(cmd.durationMs)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
