import { Download, Search, X } from "lucide-react";
import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { auditLogServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import { type AuditLog } from "@/types/proto-es/v1/audit_log_service_pb";

function formatTime(ts?: { seconds?: bigint; nanos?: number }): string {
  if (!ts) return "";
  const ms =
    Number(ts.seconds ?? 0n) * 1000 + Math.floor(Number(ts.nanos ?? 0) / 1e6);
  return new Date(ms).toLocaleString();
}

function buildFilter(method: string, actor: string, status: string): string {
  const parts: string[] = [];
  if (method) parts.push(`method = "${method}"`);
  if (actor) parts.push(`actor = "${actor}"`);
  if (status) parts.push(`status = "${status}"`);
  return parts.join(" && ");
}

function downloadCsv(content: string, filename: string) {
  const blob = new Blob([content], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export function SettingsAuditPage() {
  const { t } = useTranslation();
  const canView = useHasPermission("laelia.auditLogs.search");

  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [nextPageToken, setNextPageToken] = useState("");
  const [method, setMethod] = useState("");
  const [actor, setActor] = useState("");
  const [status, setStatus] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [exporting, setExporting] = useState(false);

  const load = useCallback(
    async (pageToken: string) => {
      setLoading(true);
      try {
        const res = await auditLogServiceClient.searchAuditLogs({
          pageSize: 50,
          pageToken,
          filter: buildFilter(method, actor, status),
        });
        if (pageToken) {
          setLogs((prev) => [...prev, ...(res.auditLogs ?? [])]);
        } else {
          setLogs(res.auditLogs ?? []);
        }
        setNextPageToken(res.nextPageToken ?? "");
      } catch (err) {
        toastManager.add({
          type: "error",
          title: t("settings.audit.load-failed"),
          description: describeError(err),
        });
      } finally {
        setLoading(false);
      }
    },
    [method, actor, status, t]
  );

  // Debounced load of the filtered results. The first run fires immediately so
  // the list appears without waiting; later filter edits (which change `load`'s
  // identity) refetch once the query settles — without this, typing a filter
  // fired a searchAuditLogs request per keystroke. The explicit Filter button
  // still calls load("") directly.
  const initialLoadDone = useRef(false);
  useEffect(() => {
    if (!canView) {
      setLoading(false);
      return;
    }
    if (!initialLoadDone.current) {
      initialLoadDone.current = true;
      void load("");
      return;
    }
    const timer = setTimeout(() => void load(""), 250);
    return () => clearTimeout(timer);
  }, [canView, load]);

  const exportCsv = async () => {
    setExporting(true);
    try {
      const res = await auditLogServiceClient.exportAuditLogs({
        filter: buildFilter(method, actor, status),
        limit: 10000,
      });
      downloadCsv(
        res.content,
        `audit-logs-${new Date().toISOString().slice(0, 10)}.csv`
      );
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.audit.export-failed"),
        description: describeError(err),
      });
    } finally {
      setExporting(false);
    }
  };

  const clearFilters = () => {
    setMethod("");
    setActor("");
    setStatus("");
  };

  const togglePayload = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  if (!canView) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">
          {t("settings.audit.not-allowed")}
        </p>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-5 w-full">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold text-main">
            {t("settings.audit.title")}
          </h1>
          <p className="text-sm text-control-light">
            {t("settings.audit.description")}
          </p>
        </div>
        <Button variant="outline" onClick={exportCsv} disabled={exporting}>
          <Download className="w-4 h-4" />
          {t("settings.audit.export")}
        </Button>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <FieldRow label={t("settings.audit.filter-method")}>
          <Input
            value={method}
            onChange={(e) => setMethod(e.target.value)}
            placeholder={t("settings.audit.filter-method-placeholder")}
            className="w-80"
          />
        </FieldRow>
        <FieldRow label={t("settings.audit.filter-actor")}>
          <Input
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            placeholder={t("settings.audit.filter-actor-placeholder")}
            className="w-56"
          />
        </FieldRow>
        <FieldRow label={t("settings.audit.filter-status")}>
          <Input
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            placeholder={t("settings.audit.filter-status-placeholder")}
            className="w-32"
          />
        </FieldRow>
        <div className="flex gap-2">
          <Button onClick={() => load("")}>
            <Search className="w-4 h-4" />
            {t("settings.audit.filter")}
          </Button>
          <Button variant="ghost" onClick={clearFilters}>
            <X className="w-4 h-4" />
            {t("settings.audit.clear")}
          </Button>
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("settings.audit.header-time")}</TableHead>
            <TableHead>{t("settings.audit.header-method")}</TableHead>
            <TableHead>{t("settings.audit.header-actor")}</TableHead>
            <TableHead>{t("settings.audit.header-status")}</TableHead>
            <TableHead>{t("settings.audit.header-resource")}</TableHead>
            <TableHead>{t("settings.audit.header-payload")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {logs.map((log) => {
            const isExpanded = expanded.has(log.name ?? "");
            return (
              <Fragment key={log.name}>
                <TableRow key={log.name}>
                  <TableCell className="whitespace-nowrap text-control-light">
                    {formatTime(log.createTime)}
                  </TableCell>
                  <TableCell className="max-w-64 truncate font-mono text-xs">
                    {log.method}
                  </TableCell>
                  <TableCell>
                    {log.actorId || t("settings.audit.actor-unknown")}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        log.status === "ok" ? "secondary" : "destructive"
                      }
                    >
                      {log.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-40 truncate font-mono text-xs">
                    {log.resource}
                  </TableCell>
                  <TableCell>
                    {log.payload ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => togglePayload(log.name ?? "")}
                      >
                        {isExpanded
                          ? t("settings.audit.hide-payload")
                          : t("settings.audit.show-payload")}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
                {isExpanded && (
                  <TableRow key={`${log.name}-payload`}>
                    <TableCell colSpan={6}>
                      <pre className="text-xs whitespace-pre-wrap break-all bg-control-bg text-control rounded p-3">
                        {log.payload}
                      </pre>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            );
          })}
          {logs.length === 0 && !loading && (
            <TableRow>
              <TableCell
                colSpan={6}
                className="text-center text-control-light py-8"
              >
                {t("settings.audit.no-logs")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      {nextPageToken && (
        <div className="flex justify-center">
          <Button
            variant="outline"
            onClick={() => load(nextPageToken)}
            disabled={loading}
          >
            {t("settings.audit.load-more")}
          </Button>
        </div>
      )}
    </div>
  );
}
