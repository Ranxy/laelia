import { useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Expand, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  COMMAND_LIST_QUERY_ROOT,
  useCommandListPage,
} from "@/composables/use-command-list";
import { FinalSummary } from "@/lib/markdown";
import { agentResourceName, commandIdFromName } from "@/lib/resource";
import { formatDuration, formatTimestamp } from "@/lib/time-format";
import { useAppStore } from "@/stores";
import type { Command } from "@/types/proto-es/v1/command_pb";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";

type StatusFilter = "all" | "pending" | "running" | "done" | "failed";

// 服务端 ListCommandsRequest.status 为单个 CommandStatus。`failed` tab 仅按
// FAILED 过滤，CANCELLED/TIMEOUT 不在此 tab 内（避免后端改动为 repeated）。
const statusFilterToStatusValue: Record<StatusFilter, CommandStatus> = {
  all: CommandStatus.COMMAND_STATUS_UNSPECIFIED,
  pending: CommandStatus.PENDING,
  running: CommandStatus.RUNNING,
  done: CommandStatus.COMPLETED,
  failed: CommandStatus.FAILED,
};

export function CommandListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = agentResourceName(agentId);

  const sendChatMessage = useAppStore((s) => s.sendChatMessage);

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  // pageTokens[i] is the page_token to ENTER page i; page 0 is "" (offset 0).
  // The token stack is pagination UI state; the pages themselves live in the
  // Query cache, one entry per (agent, status, token) — use-command-list.ts
  // owns it (same split as the activity feed).
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [pageIndex, setPageIndex] = useState(0);

  // Reset the pagination stack when the agent or the filter tab changes so
  // the page re-enters at page 0 (render-time adjust: no extra effect pass).
  const resetKey = `${agent}|${statusFilter}`;
  const [prevResetKey, setPrevResetKey] = useState(resetKey);
  if (prevResetKey !== resetKey) {
    setPrevResetKey(resetKey);
    setPageTokens([""]);
    setPageIndex(0);
  }

  const pageToken = pageTokens[pageIndex] ?? "";
  const page = useCommandListPage({
    agent,
    status: statusFilterToStatusValue[statusFilter],
    pageToken,
  });
  const rows = page.data?.commands;
  const nextPageToken = page.data?.nextPageToken ?? "";
  const loading = page.isPending;
  // A user-initiated fetch is in flight (first page / page turn / post-send
  // reload); it disables the pager so canNext can't double-trigger.
  const refreshing = page.isFetching;

  const [sendOpen, setSendOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [sending, setSending] = useState(false);
  const [expandedSummary, setExpandedSummary] = useState<Command | null>(null);

  const handleStatusFilterChange = (f: StatusFilter) => {
    if (f === statusFilter) return;
    setStatusFilter(f);
    // The resetKey change above drives the stack back to page 1.
  };

  const handleNextPage = () => {
    // Truncate any forward history (after going back a page) and push the
    // cursor for the page we are about to enter.
    const cut = pageTokens.slice(0, pageIndex + 1);
    cut.push(nextPageToken);
    setPageTokens(cut);
    setPageIndex(pageIndex + 1);
  };

  const handlePrevPage = () => {
    setPageIndex((i) => Math.max(0, i - 1));
  };

  const handleSend = async () => {
    if (!agent || !instruction.trim()) return;
    setSending(true);
    try {
      await sendChatMessage(agent, instruction.trim());
      setInstruction("");
      setSendOpen(false);
      // The new command heads page 1: re-enter at page 0 and refetch it.
      setPageTokens([""]);
      setPageIndex(0);
      void queryClient.invalidateQueries({ queryKey: COMMAND_LIST_QUERY_ROOT });
    } finally {
      setSending(false);
    }
  };

  function handleRowClick(cmd: Command) {
    if (!cmd.name) return;
    const commandId = commandIdFromName(cmd.name);
    if (!commandId) return;
    navigate(`/members/agents/${agentId}/commands/${commandId}`);
  }

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 border-b border-control-border px-4 py-3">
        <div className="mx-auto max-w-5xl flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-1 overflow-x-auto">
            {(
              ["all", "pending", "running", "done", "failed"] as StatusFilter[]
            ).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => handleStatusFilterChange(f)}
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
          <div className="ml-auto">
            <Button onClick={() => setSendOpen(true)} size="sm">
              <Plus className="size-3.5" />
              {t("tasks.new")}
            </Button>
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-4 py-4">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-24 whitespace-nowrap">
                    {t("tasks.header-status")}
                  </TableHead>
                  <TableHead>{t("tasks.header-final-summary")}</TableHead>
                  <TableHead className="w-24 whitespace-nowrap">
                    {t("tasks.header-duration")}
                  </TableHead>
                  <TableHead className="w-44 whitespace-nowrap">
                    {t("tasks.header-created")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && (
                  <TableRow>
                    <TableCell
                      colSpan={4}
                      className="text-center text-control-light py-8"
                    >
                      {t("common.loading")}
                    </TableCell>
                  </TableRow>
                )}
                {!loading && (rows ?? []).length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className="text-center py-12">
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
                {!loading &&
                  (rows ?? []).map((cmd) => (
                    <TableRow
                      key={cmd.name}
                      className="cursor-pointer"
                      tabIndex={0}
                      aria-label={t("command.row-open-detail")}
                      onClick={() => handleRowClick(cmd)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          handleRowClick(cmd);
                        }
                      }}
                    >
                      <TableCell className="whitespace-nowrap">
                        <CommandStatusBadge status={cmd.status} />
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {cmd.finalSummary ? (
                          <div className="flex items-center gap-2 min-w-0">
                            <span className="truncate flex-1 min-w-0 max-w-md">
                              {cmd.finalSummary}
                            </span>
                            <Button
                              variant="ghost"
                              size="xs"
                              className="shrink-0 px-1"
                              aria-label={t("tasks.show-final-summary")}
                              onClick={(e) => {
                                e.stopPropagation();
                                setExpandedSummary(cmd);
                              }}
                            >
                              <Expand className="size-3.5" />
                            </Button>
                          </div>
                        ) : (
                          <span className="text-control-light">-</span>
                        )}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-control-light text-xs">
                        {formatDuration(cmd.durationMs)}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-control-light text-xs">
                        {formatTimestamp(cmd.createdAt)}
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </div>
        </div>
      </div>

      <div className="shrink-0 border-t border-control-border px-4 py-2">
        <div className="mx-auto max-w-5xl flex items-center justify-between text-xs text-control-light">
          <span>{t("tasks.page", { n: pageIndex + 1 })}</span>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={pageIndex === 0 || refreshing}
              onClick={handlePrevPage}
            >
              <ChevronLeft className="size-3.5" />
              {t("tasks.prev")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={nextPageToken === "" || refreshing}
              onClick={handleNextPage}
            >
              {t("tasks.next")}
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      </div>

      <NewTaskSheet
        open={sendOpen}
        onOpenChange={(next) => !next && setSendOpen(false)}
        agentId={agentId ?? ""}
        instruction={instruction}
        setInstruction={setInstruction}
        sending={sending}
        onSend={handleSend}
      />

      <Sheet
        open={!!expandedSummary}
        onOpenChange={(next) => !next && setExpandedSummary(null)}
      >
        <SheetContent width="standard">
          <SheetHeader>
            <SheetTitle>{t("tasks.final-summary")}</SheetTitle>
          </SheetHeader>
          <SheetBody className="overflow-y-auto">
            {expandedSummary?.finalSummary ? (
              <FinalSummary content={expandedSummary.finalSummary} />
            ) : null}
          </SheetBody>
        </SheetContent>
      </Sheet>
    </div>
  );
}

interface NewTaskSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  instruction: string;
  setInstruction: (v: string) => void;
  sending: boolean;
  onSend: () => void;
}

function NewTaskSheet({
  open,
  onOpenChange,
  agentId,
  instruction,
  setInstruction,
  sending,
  onSend,
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
