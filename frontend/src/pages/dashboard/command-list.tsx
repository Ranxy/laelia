import { Plus, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { CommandStatusBadge } from "@/react/components/command-status-badge";
import { Button } from "@/react/components/ui/button";
import { Input } from "@/react/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/react/components/ui/select";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
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
import {
  agentResourceName,
  executorKindToI18nKey,
  formatDuration,
  formatTimestamp,
} from "@/react/lib/command-status";
import { useAppStore } from "@/react/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import type { Command } from "@/types/proto-es/v1/command_pb";
import { CommandStatus, ExecutorKind } from "@/types/proto-es/v1/command_pb";

type StatusFilter = "all" | "pending" | "running" | "done" | "failed";

const statusFilterToStatuses: Record<StatusFilter, CommandStatus[]> = {
  all: [],
  pending: [CommandStatus.PENDING],
  running: [CommandStatus.RUNNING],
  done: [CommandStatus.COMPLETED],
  failed: [
    CommandStatus.FAILED,
    CommandStatus.CANCELLED,
    CommandStatus.TIMEOUT,
  ],
};

export function CommandListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = agentResourceName(agentId);

  const commands = useAppStore((s) => s.commands);
  const loading = useAppStore((s) => s.commandsLoading);
  const listCommands = useAppStore((s) => s.listCommands);
  const sendCommand = useAppStore((s) => s.sendCommand);
  const getAgent = useAppStore((s) => s.getAgent);

  const [sendOpen, setSendOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [profile, setProfile] = useState("");
  const [sending, setSending] = useState(false);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [search, setSearch] = useState("");
  const [agentDetail, setAgentDetail] = useState<Agent | undefined>(undefined);

  const load = useCallback(() => {
    if (!agent) return;
    listCommands(agent, { pageSize: 100 });
  }, [agent, listCommands]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!agentId) return;
    getAgent(agent).then(setAgentDetail);
  }, [agentId, agent, getAgent]);

  const availableProfiles = useMemo(() => {
    const caps = agentDetail?.info?.capability;
    return caps?.availableProfiles ?? [];
  }, [agentDetail]);

  const defaultProfile = useMemo(() => {
    const caps = agentDetail?.info?.capability;
    return caps?.defaultProfile ?? "";
  }, [agentDetail]);

  const filteredCommands = useMemo(() => {
    const statuses = statusFilterToStatuses[statusFilter];
    const searchLower = search.trim().toLowerCase();
    return commands.filter((cmd) => {
      if (statuses.length > 0 && !statuses.includes(cmd.status)) return false;
      if (searchLower) {
        const text = (cmd.instruction || cmd.command || "").toLowerCase();
        if (!text.includes(searchLower)) return false;
      }
      return true;
    });
  }, [commands, statusFilter, search]);

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
    return cmd.instruction || cmd.command || "";
  }

  const executorLabel = (cmd: Command) => {
    const key = executorKindToI18nKey[cmd.executorKind as ExecutorKind];
    if (!key) return "ACP";
    const label = t(key);
    return label || "ACP";
  };

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 border-b border-control-border px-4 py-3">
        <div className="mx-auto max-w-5xl flex items-center gap-3">
          <div className="flex items-center gap-1">
            {(
              ["all", "pending", "running", "done", "failed"] as StatusFilter[]
            ).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setStatusFilter(f)}
                className={
                  statusFilter === f
                    ? "rounded-xs px-2.5 py-1 text-xs font-medium bg-accent text-accent-foreground"
                    : "rounded-xs px-2.5 py-1 text-xs font-medium text-control-light hover:bg-control-bg transition-colors"
                }
              >
                {t(`tasks.filter-${f}`)}
              </button>
            ))}
          </div>
          <div className="relative flex-1 max-w-xs ml-auto">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-control-light" />
            <Input
              className="pl-8 h-8 text-xs"
              placeholder={t("tasks.search")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <Button onClick={() => setSendOpen(true)} size="sm">
            <Plus className="size-3.5" />
            {t("tasks.new")}
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-4 py-4">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-20">{t("tasks.header-type")}</TableHead>
                <TableHead className="w-24">
                  {t("tasks.header-status")}
                </TableHead>
                <TableHead>{t("tasks.header-task")}</TableHead>
                <TableHead className="w-24">
                  {t("tasks.header-duration")}
                </TableHead>
                <TableHead className="w-32">
                  {t("tasks.header-created")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-center text-control-light py-8"
                  >
                    {t("common.loading")}
                  </TableCell>
                </TableRow>
              )}
              {!loading && filteredCommands.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center py-12">
                    <div className="flex flex-col items-center gap-3">
                      <p className="text-control-light text-sm">
                        {t("tasks.empty")}
                      </p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setSendOpen(true)}
                      >
                        <Plus className="size-3.5" />
                        {t("tasks.empty-cta")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )}
              {filteredCommands.map((cmd) => (
                <TableRow
                  key={cmd.name}
                  className="cursor-pointer"
                  onClick={() => handleRowClick(cmd)}
                >
                  <TableCell>
                    <span className="text-xs text-control-light">
                      {executorLabel(cmd)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <CommandStatusBadge status={cmd.status} />
                  </TableCell>
                  <TableCell className="font-mono text-xs truncate max-w-md">
                    {displayTaskText(cmd)}
                  </TableCell>
                  <TableCell className="text-control-light text-xs">
                    {formatDuration(cmd.durationMs)}
                  </TableCell>
                  <TableCell className="text-control-light text-xs">
                    {formatTimestamp(cmd.createdAt)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <NewTaskSheet
        open={sendOpen}
        onOpenChange={(next) => !next && setSendOpen(false)}
        agentId={agentId ?? ""}
        instruction={instruction}
        setInstruction={setInstruction}
        profile={profile}
        setProfile={setProfile}
        sending={sending}
        onSend={handleSend}
        availableProfiles={availableProfiles}
        defaultProfile={defaultProfile}
      />
    </div>
  );
}

interface NewTaskSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  instruction: string;
  setInstruction: (v: string) => void;
  profile: string;
  setProfile: (v: string) => void;
  sending: boolean;
  onSend: () => void;
  availableProfiles: string[];
  defaultProfile: string;
}

function NewTaskSheet({
  open,
  onOpenChange,
  agentId,
  instruction,
  setInstruction,
  profile,
  setProfile,
  sending,
  onSend,
  availableProfiles,
  defaultProfile,
}: NewTaskSheetProps) {
  const { t } = useTranslation();

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent width="standard">
        <SheetHeader>
          <SheetTitle>{t("tasks.new")}</SheetTitle>
          <SheetDescription>
            {t("tasks.new-description", { name: agentId })}
          </SheetDescription>
        </SheetHeader>
        <SheetBody className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <span className="text-sm text-control-light">
              {t("tasks.instruction-label")}
            </span>
            <Textarea
              className="font-mono text-sm min-h-[140px]"
              placeholder={t("tasks.instruction-placeholder")}
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-sm text-control-light">
              {t("tasks.profile-label")}
            </span>
            {availableProfiles.length > 0 ? (
              <Select
                value={profile || defaultProfile}
                onValueChange={(v) =>
                  setProfile(v && v !== defaultProfile ? String(v) : "")
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {availableProfiles.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Input
                placeholder={t("tasks.profile-placeholder")}
                value={profile}
                onChange={(e) => setProfile(e.target.value)}
              />
            )}
          </div>
        </SheetBody>
        <SheetFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={sending}
          >
            {t("common.cancel")}
          </Button>
          <Button disabled={sending || !instruction.trim()} onClick={onSend}>
            {sending ? t("common.loading") : t("common.send")}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
