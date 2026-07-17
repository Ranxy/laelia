import { ChevronLeft, ChevronRight } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ReminderStatusBadge } from "@/components/reminder-status-badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { agentResourceName, formatTimestamp } from "@/lib/command-status";
import { useAppStore } from "@/stores";
import type { Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";

type StatusFilter =
  | "all"
  | "pending"
  | "due"
  | "completed"
  | "cancelled"
  | "missed"
  | "failed";

const PAGE_SIZE = 50;

// statusFilterToValues maps a tab to the ReminderStatus values it shows. `all`
// is an empty filter (server returns every status). The non-terminal tab
// groups PENDING + DUE (active work).
const statusFilterToValues: Record<StatusFilter, ReminderStatus[]> = {
  all: [],
  pending: [ReminderStatus.PENDING, ReminderStatus.DUE],
  due: [ReminderStatus.DUE],
  completed: [ReminderStatus.COMPLETED],
  cancelled: [ReminderStatus.CANCELLED],
  missed: [ReminderStatus.MISSED],
  failed: [ReminderStatus.FAILED],
};

// fireAtDate renders a reminder's fire_at as a local string, or "-" when
// absent (one-shot terminal reminders may have a stale fire_at; recurring ones
// carry the next cron fire).
function fireAtDate(r: Reminder): string {
  return formatTimestamp(r.fireAt);
}

// scheduleSummary renders the trigger mechanism: a cron expression for
// recurring reminders, or "once" for one-shot, alongside the tz.
function scheduleSummary(r: Reminder): string {
  if (r.cronExpr) {
    return r.tz && r.tz !== "UTC" ? `${r.cronExpr} (${r.tz})` : r.cronExpr;
  }
  return "once";
}

export function ReminderListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const agent = agentResourceName(agentId);

  const reminders = useAppStore((s) => s.reminders);
  const loading = useAppStore((s) => s.remindersLoading);
  const listReminders = useAppStore((s) => s.listReminders);

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  // 分页：pageTokens 为已访问页的 cursor 栈，pageIndex 指向当前页。
  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [pageIndex, setPageIndex] = useState(0);
  const [nextPageToken, setNextPageToken] = useState("");

  const pageToken = pageTokens[pageIndex] ?? "";
  const canPrev = pageIndex > 0;
  const canNext = nextPageToken !== "";

  const load = useCallback(
    async (silent = false) => {
      if (!agent) return;
      const res = await listReminders(agent, {
        pageSize: PAGE_SIZE,
        pageToken,
        statusFilter: statusFilterToValues[statusFilter],
        silent,
      });
      setNextPageToken(res?.nextPageToken ?? "");
    },
    [agent, listReminders, pageToken, statusFilter]
  );

  // Track whether the initial load has completed so background polls can be
  // silent (no loading spinner flash).
  const initialLoadDone = useRef(false);

  useEffect(() => {
    // Initial load and filter/page changes always show loading.
    load(false).then(() => {
      initialLoadDone.current = true;
    });
    // Background polls are silent — they don't toggle the loading flag,
    // avoiding visual flicker when the data hasn't changed.
    const handle = setInterval(() => load(true), 2000);
    return () => clearInterval(handle);
  }, [load]);

  const handleStatusFilterChange = (f: StatusFilter) => {
    setStatusFilter(f);
    setPageTokens([""]);
    setPageIndex(0);
    setNextPageToken("");
  };

  const onNext = () => {
    if (!canNext) return;
    setPageTokens((tok) => [...tok, nextPageToken]);
    setPageIndex((i) => i + 1);
  };

  const onPrev = () => {
    if (!canPrev) return;
    setPageIndex((i) => Math.max(0, i - 1));
  };

  function handleRowClick(r: Reminder) {
    if (!r.name) return;
    navigate(`/agents/${agentId}/reminders/${r.name.split("/").pop()}`);
  }

  return (
    <div className="flex h-full flex-col">
      <div className="shrink-0 border-b border-control-border px-4 py-3">
        <div className="mx-auto max-w-5xl flex items-center gap-1">
          {(
            [
              "all",
              "pending",
              "due",
              "completed",
              "cancelled",
              "missed",
              "failed",
            ] as StatusFilter[]
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
              {t(`reminders.filter-${f}`)}
            </button>
          ))}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-4 py-4">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-24 whitespace-nowrap">
                  {t("reminders.header-status")}
                </TableHead>
                <TableHead>{t("reminders.header-task")}</TableHead>
                <TableHead className="w-40 whitespace-nowrap">
                  {t("reminders.header-schedule")}
                </TableHead>
                <TableHead className="w-44 whitespace-nowrap">
                  {t("reminders.header-fire-at")}
                </TableHead>
                <TableHead className="w-32 whitespace-nowrap">
                  {t("reminders.header-assignee")}
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
              {!loading && reminders.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-center py-12">
                    <p className="text-control-light text-sm">
                      {t("reminders.empty")}
                    </p>
                  </TableCell>
                </TableRow>
              )}
              {!loading &&
                reminders.map((r) => (
                  <TableRow
                    key={r.name}
                    className="cursor-pointer"
                    tabIndex={0}
                    aria-label={t("reminders.row-open-detail")}
                    onClick={() => handleRowClick(r)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        handleRowClick(r);
                      }
                    }}
                  >
                    <TableCell className="whitespace-nowrap">
                      <ReminderStatusBadge status={r.status} />
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      <span className="line-clamp-2 max-w-md">
                        {r.taskContent || "-"}
                      </span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs text-control-light font-mono">
                      {scheduleSummary(r)}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-control-light text-xs">
                      {fireAtDate(r)}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-control-light text-xs">
                      {r.assigneeName || "-"}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <div className="shrink-0 border-t border-control-border px-4 py-2">
        <div className="mx-auto max-w-5xl flex items-center justify-between text-xs text-control-light">
          <span>{t("reminders.page", { n: pageIndex + 1 })}</span>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!canPrev || loading}
              onClick={onPrev}
            >
              <ChevronLeft className="size-3.5" />
              {t("reminders.prev")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!canNext || loading}
              onClick={onNext}
            >
              {t("reminders.next")}
              <ChevronRight className="size-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
