import { Timestamp } from "@bufbuild/protobuf/wkt";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { ConnectionBadge } from "@/react/components/connection-badge";
import { Button } from "@/react/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/react/components/ui/dialog";
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
import { useAppStore } from "@/react/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";

export function AgentsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const agents = useAppStore((s) => s.agents);
  const loading = useAppStore((s) => s.agentsLoading);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const load = useCallback(() => {
    fetchAgents({ pageSize: 100 });
  }, [fetchAgents]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleCreate() {
    if (!name.trim()) return;
    setCreating(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      const agent = await createAgent(name.trim());
      if (agent.bootstrapToken) {
        setToken(agent.bootstrapToken);
        setTokenOpen(true);
      }
      setName("");
      setCreateOpen(false);
      load();
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(name: string) {
    const deleteAgent = useAppStore.getState().deleteAgent;
    try {
      await deleteAgent(name);
      load();
    } catch {
      // handled by api client
    }
  }

  function handleRowClick(agent: Agent) {
    setSelectedAgent(agent);
    setDetailOpen(true);
  }

  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">{t("agent.title")}</h1>
        <Button onClick={() => setCreateOpen(true)}>{t("agent.create")}</Button>
      </div>

      <Sheet
        open={createOpen}
        onOpenChange={(next) => !next && setCreateOpen(false)}
      >
        <SheetContent width="narrow">
          <SheetTitle>{t("agent.create-title")}</SheetTitle>
          <SheetDescription>{t("agent.create-description")}</SheetDescription>
          <div className="flex flex-col gap-4 pt-4">
            <Input
              placeholder={t("agent.create-name-placeholder")}
              value={name}
              onChange={(e) => setName((e.target as HTMLInputElement).value)}
            />
            <Button disabled={creating || !name.trim()} onClick={handleCreate}>
              {creating ? t("common.creating") : t("common.create")}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <Dialog
        open={tokenOpen}
        onOpenChange={(next) => !next && setTokenOpen(false)}
      >
        <DialogContent className="max-w-lg">
          <DialogTitle>{t("agent.created-title")}</DialogTitle>
          <DialogDescription>
            {t("agent.created-description")}
          </DialogDescription>
          <div className="mt-4 space-y-3">
            <p className="text-sm text-control-light">
              {t("agent.created-run-hint")}
            </p>
            <div className="rounded bg-white border border-control-border p-3 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
              {token &&
                `laelia-agent run --manager ${getManagerURL()} --token ${formatToken(token)}`}
            </div>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => {
                if (token) {
                  const cmd = `laelia-agent run --manager ${getManagerURL()} --token ${token}`;
                  navigator.clipboard.writeText(cmd).catch(() => {});
                }
              }}
            >
              {t("common.copy")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog
        open={detailOpen}
        onOpenChange={(next) => !next && setDetailOpen(false)}
      >
        <DialogContent className="max-w-md">
          <DialogTitle>{t("agent.detail-title")}</DialogTitle>
          {selectedAgent && (
            <div className="flex flex-col gap-2 text-sm mt-2">
              <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                <span className="text-control-light whitespace-nowrap">
                  {t("agent.detail-name")}
                </span>
                <span>{selectedAgent.title}</span>

                <span className="text-control-light whitespace-nowrap">
                  {t("agent.detail-status")}
                </span>
                <span>
                  <ConnectionBadge state={selectedAgent.status?.state} />
                </span>

                {selectedAgent.info && (
                  <>
                    {selectedAgent.info.hostname && (
                      <>
                        <span className="text-control-light whitespace-nowrap">
                          {t("agent.detail-hostname")}
                        </span>
                        <span>{selectedAgent.info.hostname}</span>
                      </>
                    )}
                    {selectedAgent.info.os && (
                      <>
                        <span className="text-control-light whitespace-nowrap">
                          {t("agent.detail-os")}
                        </span>
                        <span>
                          {selectedAgent.info.os}/{selectedAgent.info.arch}
                        </span>
                      </>
                    )}
                    {selectedAgent.info.ip && (
                      <>
                        <span className="text-control-light whitespace-nowrap">
                          {t("agent.detail-ip")}
                        </span>
                        <span>{selectedAgent.info.ip}</span>
                      </>
                    )}
                    {selectedAgent.info.version && (
                      <>
                        <span className="text-control-light whitespace-nowrap">
                          {t("agent.detail-version")}
                        </span>
                        <span>{selectedAgent.info.version}</span>
                      </>
                    )}
                  </>
                )}
                {selectedAgent.status?.connectedTime && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      {t("agent.detail-connected")}
                    </span>
                    <span>
                      {formatTimestamp(selectedAgent.status.connectedTime)}
                    </span>
                  </>
                )}
                {selectedAgent.status?.lastHeartbeatTime && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      {t("agent.detail-last-heartbeat")}
                    </span>
                    <span>
                      {formatTimestamp(selectedAgent.status.lastHeartbeatTime)}
                    </span>
                  </>
                )}
                {selectedAgent.createdAt && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      {t("agent.detail-created")}
                    </span>
                    <span>{formatTimestamp(selectedAgent.createdAt)}</span>
                  </>
                )}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {loading ? (
        <p className="text-control-light">{t("common.loading")}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("agent.header-name")}</TableHead>
              <TableHead>{t("agent.header-status")}</TableHead>
              <TableHead>{t("agent.header-hostname")}</TableHead>
              <TableHead>{t("agent.header-os")}</TableHead>
              <TableHead>{t("agent.header-ip")}</TableHead>
              <TableHead>{t("agent.header-actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className="text-center text-control-light"
                >
                  {t("common.no-data")}
                </TableCell>
              </TableRow>
            ) : (
              agents.map((agent) => (
                <TableRow
                  key={agent.name}
                  className="cursor-pointer"
                  onClick={() => handleRowClick(agent)}
                >
                  <TableCell>{agent.title}</TableCell>
                  <TableCell>
                    <ConnectionBadge state={agent.status?.state} />
                  </TableCell>
                  <TableCell>{agent.info?.hostname ?? "-"}</TableCell>
                  <TableCell>
                    {agent.info?.os
                      ? `${agent.info.os}/${agent.info.arch ?? ""}`
                      : "-"}
                  </TableCell>
                  <TableCell>{agent.info?.ip ?? "-"}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          const resourceId = agent.name.replace(
                            /^agents\//,
                            ""
                          );
                          navigate(`/agents/${resourceId}/chat`);
                        }}
                      >
                        Chat
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          const resourceId = agent.name.replace(
                            /^agents\//,
                            ""
                          );
                          navigate(`/agents/${resourceId}/commands`);
                        }}
                      >
                        Commands
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          if (
                            window.confirm(
                              t("common.confirm-delete", {
                                name: agent.title,
                              })
                            )
                          ) {
                            handleDelete(agent.name);
                          }
                        }}
                      >
                        {t("common.delete")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function formatTimestamp(ts: Timestamp): string {
  const seconds = Number(ts.seconds);
  const date = new Date(seconds * 1000);
  const y = date.getFullYear();
  const mo = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  const h = String(date.getHours()).padStart(2, "0");
  const mi = String(date.getMinutes()).padStart(2, "0");
  const s = String(date.getSeconds()).padStart(2, "0");
  return `${y}-${mo}-${d} ${h}:${mi}:${s}`;
}

function formatToken(token: string): string {
  if (token.length <= 20) {
    return token.slice(0, 6) + "*".repeat(token.length - 6);
  }
  return `${token.slice(0, 10)}${"*".repeat(20)}${token.slice(-6)}`;
}

function getManagerURL(): string {
  return (import.meta.env.VITE_API_BASE_URL || window.location.origin).replace(
    /\/+$/,
    ""
  );
}
