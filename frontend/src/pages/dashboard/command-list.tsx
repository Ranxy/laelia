import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import { Input } from "@/react/components/ui/input";
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
import {
  type Command,
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

const executorKindLabels: Record<number, string> = {
  [ExecutorKind.EXECUTOR_KIND_UNSPECIFIED]: "",
  [ExecutorKind.SHELL]: "Shell",
  [ExecutorKind.ACP]: "ACP",
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
  const [instruction, setInstruction] = useState("");
  const [profile, setProfile] = useState("");
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
    if (!agent || !instruction.trim()) return;
    setSending(true);
    try {
      await sendCommand(agent, instruction.trim(), {
        executorKind: ExecutorKind.ACP,
        instruction: instruction.trim(),
        profile,
      });
      setInstruction("");
      setProfile("");
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

  function displayTaskText(cmd: Command): string {
    if (cmd.instruction) return cmd.instruction;
    return cmd.command;
  }

  return (
    <div className="p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">Tasks</h1>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => navigate(`/agents/${agentId}/chat`)}
          >
            Chat
          </Button>
          <Button onClick={() => setSendOpen(true)}>New Task</Button>
        </div>
      </div>

      <Sheet
        open={sendOpen}
        onOpenChange={(next) => !next && setSendOpen(false)}
      >
        <SheetContent width="standard">
          <SheetTitle>New Task</SheetTitle>
          <SheetDescription>
            Send an ACP task to agent {agentId}
          </SheetDescription>
          <div className="flex flex-col gap-4 pt-4">
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                Instruction (natural language)
              </span>
              <Textarea
                className="font-mono text-sm min-h-[120px]"
                placeholder="e.g. Read config.yaml and list all the port numbers in use"
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                Profile (optional — leave empty for agent default)
              </span>
              <Input
                placeholder="e.g. default-acp"
                value={profile}
                onChange={(e) => setProfile(e.target.value)}
              />
            </div>
            <Button
              disabled={sending || !instruction.trim()}
              onClick={handleSend}
            >
              {sending ? "Sending..." : "Send"}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-20">Type</TableHead>
            <TableHead className="w-24">Status</TableHead>
            <TableHead>Task</TableHead>
            <TableHead className="w-28">Duration</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading && (
            <TableRow>
              <TableCell colSpan={4} className="text-center text-control-light">
                Loading...
              </TableCell>
            </TableRow>
          )}
          {!loading && commands.length === 0 && (
            <TableRow>
              <TableCell colSpan={4} className="text-center text-control-light">
                No tasks yet
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
                <span className="text-xs text-control-light">
                  {executorKindLabels[cmd.executorKind] ?? "ACP"}
                </span>
              </TableCell>
              <TableCell>
                <Badge variant={statusVariants[cmd.status] ?? "default"}>
                  {statusLabels[cmd.status] ?? "Unknown"}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs truncate max-w-md">
                {displayTaskText(cmd)}
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
