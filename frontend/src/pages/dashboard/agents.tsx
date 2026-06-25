import { Timestamp } from "@bufbuild/protobuf/wkt";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { ConnectionBadge } from "@/react/components/connection-badge";
import { Alert } from "@/react/components/ui/alert";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/react/components/ui/alert-dialog";
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
import { useAppStore } from "@/react/stores";
import {
  type Agent,
  AgentStatus_ConnectionState,
} from "@/types/proto-es/v1/agent_pb";

type Lifecycle =
  | "waiting-connection"
  | "pending-config"
  | "ready"
  | "configured-offline";

function agentLifecycle(agent: Agent): Lifecycle {
  const online = agent.status?.state === AgentStatus_ConnectionState.ONLINE;
  const configured = !!agent.info?.acpConfig?.executable;
  if (online && configured) return "ready";
  if (online && !configured) return "pending-config";
  if (!online && configured) return "configured-offline";
  return "waiting-connection";
}

function lifecycleLabel(state: Lifecycle): string {
  switch (state) {
    case "ready":
      return "Ready";
    case "pending-config":
      return "Connected — pending configuration";
    case "configured-offline":
      return "Configured (offline)";
    case "waiting-connection":
      return "Waiting for connection";
  }
}

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
  const [tokenFromRotation, setTokenFromRotation] = useState(false);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [acpConfigOpen, setAcpConfigOpen] = useState(false);
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState<string[]>([]);
  const [allowEnv, setAllowEnv] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [rotateOpen, setRotateOpen] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [actionError, setActionError] = useState("");

  const load = useCallback(() => {
    fetchAgents({ pageSize: 100 });
  }, [fetchAgents]);

  useEffect(() => {
    load();
  }, [load]);

  // Refresh while any agent is not yet ready (waiting for connection or
  // pending configuration), so the page flips to "pending config" / "ready"
  // promptly once the agent connects or gets configured. Silent refreshes skip
  // the loading flag and skip the state update when nothing changed, so polls
  // cause no re-render/flicker unless the data actually changed.
  const anyNonReady = agents.some((a) => agentLifecycle(a) !== "ready");
  useEffect(() => {
    if (!anyNonReady) return;
    const id = setInterval(
      () => fetchAgents({ pageSize: 100 }, { silent: true }),
      3000
    );
    return () => clearInterval(id);
  }, [anyNonReady, fetchAgents]);

  // Keep the open detail dialog in sync with refreshed agent data.
  useEffect(() => {
    if (!detailOpen || !selectedAgent?.name) return;
    const latest = agents.find((a) => a.name === selectedAgent.name);
    if (latest && latest !== selectedAgent) setSelectedAgent(latest);
  }, [agents, detailOpen, selectedAgent]);

  async function handleCreate() {
    if (!name.trim()) return;
    setCreating(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      const agent = await createAgent(name.trim());
      if (agent.bootstrapToken) {
        setToken(agent.bootstrapToken);
        setTokenFromRotation(false);
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

  function handleEditACPConfig(agent: Agent) {
    setSelectedAgent(agent);
    setExecutable(agent.info?.acpConfig?.executable ?? "");
    setArgs(agent.info?.acpConfig?.args ? [...agent.info.acpConfig.args] : []);
    setAllowEnv(
      agent.info?.acpConfig?.allowEnv ? [...agent.info.acpConfig.allowEnv] : []
    );
    setSaveError("");
    setAcpConfigOpen(true);
  }

  async function handleRotateToken() {
    if (!selectedAgent?.name) return;
    setRotating(true);
    setActionError("");
    try {
      const rotateAgentToken = useAppStore.getState().rotateAgentToken;
      const res = await rotateAgentToken(selectedAgent.name);
      if (res.bootstrapToken) {
        setToken(res.bootstrapToken);
        setTokenFromRotation(true);
        setTokenOpen(true);
      }
      setRotateOpen(false);
      load();
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.rotate-token-error");
      setActionError(msg);
    } finally {
      setRotating(false);
    }
  }

  async function handleRevokeToken() {
    if (!selectedAgent?.name) return;
    setRevoking(true);
    setActionError("");
    try {
      const revokeAgentToken = useAppStore.getState().revokeAgentToken;
      await revokeAgentToken(selectedAgent.name);
      setRevokeOpen(false);
      load();
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.revoke-token-error");
      setActionError(msg);
    } finally {
      setRevoking(false);
    }
  }

  async function handleSaveACPConfig() {
    if (!selectedAgent?.name) return;
    setSaving(true);
    setSaveError("");
    try {
      const acpConfig = {
        executable: executable.trim(),
        args: args.map((a) => a.trim()).filter((a) => a !== ""),
        allowEnv: allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
      };
      const updateAgentACPConfig = useAppStore.getState().updateAgentACPConfig;
      await updateAgentACPConfig(selectedAgent.name, acpConfig);
      setAcpConfigOpen(false);
      load();
      const getAgent = useAppStore.getState().getAgent;
      const updated = await getAgent(selectedAgent.name, { force: true });
      if (updated) {
        setSelectedAgent(updated);
      }
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Failed to save ACP config";
      setSaveError(msg);
    } finally {
      setSaving(false);
    }
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
          <DialogTitle>
            {tokenFromRotation
              ? t("agent.rotate-token-success-title")
              : t("agent.created-title")}
          </DialogTitle>
          <DialogDescription>
            {tokenFromRotation
              ? t("agent.rotate-token-success-description")
              : t("agent.created-description")}
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

                <span className="text-control-light whitespace-nowrap">
                  Configuration
                </span>
                <span>{lifecycleLabel(agentLifecycle(selectedAgent))}</span>

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
                <span className="text-control-light whitespace-nowrap">
                  {t("agent.detail-token-version")}
                </span>
                <span>{selectedAgent.tokenVersion ?? "-"}</span>
                {selectedAgent.lastTokenRotatedAt && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      {t("agent.detail-last-rotated")}
                    </span>
                    <span>
                      {formatTimestamp(selectedAgent.lastTokenRotatedAt)}
                    </span>
                  </>
                )}
              </div>
              {agentLifecycle(selectedAgent) === "waiting-connection" && (
                <Alert
                  variant="info"
                  description="Run the bootstrap command on the host to connect this agent, then configure its executable."
                  className="mt-1"
                />
              )}
              {agentLifecycle(selectedAgent) === "pending-config" && (
                <Alert
                  variant="info"
                  description="Agent is connected. Set its executable to make it usable."
                  className="mt-1"
                />
              )}
              <div className="pt-3 border-t border-control-border flex flex-col gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleEditACPConfig(selectedAgent)}
                >
                  ACP Config
                </Button>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setActionError("");
                      setRotateOpen(true);
                    }}
                  >
                    {t("agent.rotate-token")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setActionError("");
                      setRevokeOpen(true);
                    }}
                  >
                    {t("agent.revoke-token")}
                  </Button>
                </div>
                {actionError && (
                  <Alert
                    variant="error"
                    description={actionError}
                    className="mt-2"
                  />
                )}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Sheet
        open={acpConfigOpen}
        onOpenChange={(next) => !next && setAcpConfigOpen(false)}
      >
        <SheetContent width="standard">
          <SheetHeader>
            <SheetTitle>ACP Config — {selectedAgent?.title ?? ""}</SheetTitle>
            <SheetDescription>
              Configure the LLM agent to run. Everything else uses built-in
              defaults.
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {saveError && (
              <Alert variant="error" description={saveError} className="mb-4" />
            )}
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">Executable</label>
                <Input
                  placeholder="npx"
                  value={executable}
                  onChange={(e) => {
                    setExecutable((e.target as HTMLInputElement).value);
                    setSaveError("");
                  }}
                />
              </div>

              <StringListEditor
                label="Args"
                placeholder="@agentclientprotocol/claude-agent-acp@latest"
                values={args}
                onChange={(next) => {
                  setArgs(next);
                  setSaveError("");
                }}
              />

              <StringListEditor
                label="Allow env"
                placeholder="MY_CUSTOM_VAR"
                values={allowEnv}
                onChange={(next) => {
                  setAllowEnv(next);
                  setSaveError("");
                }}
              />
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setAcpConfigOpen(false)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button
              disabled={saving || !executable.trim()}
              onClick={handleSaveACPConfig}
            >
              {saving ? "Saving..." : "Save"}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={rotateOpen}
        onOpenChange={(next) => !next && setRotateOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.rotate-token-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.rotate-token-confirm-description")}
          </AlertDialogDescription>
          {actionError && (
            <Alert variant="error" description={actionError} className="mt-2" />
          )}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={rotating}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button disabled={rotating} onClick={handleRotateToken}>
              {rotating ? t("common.creating") : t("agent.rotate-token")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={revokeOpen}
        onOpenChange={(next) => !next && setRevokeOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.revoke-token-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.revoke-token-confirm-description")}
          </AlertDialogDescription>
          {actionError && (
            <Alert variant="error" description={actionError} className="mt-2" />
          )}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={revoking}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button disabled={revoking} onClick={handleRevokeToken}>
              {revoking ? t("common.creating") : t("agent.revoke-token")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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

function StringListEditor({
  label,
  placeholder,
  values,
  onChange,
}: {
  label: string;
  placeholder: string;
  values: string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium">{label}</label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onChange([...values, ""])}
        >
          Add
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        {values.map((value, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              placeholder={placeholder}
              value={value}
              onChange={(e) => {
                const next = [...values];
                next[index] = (e.target as HTMLInputElement).value;
                onChange(next);
              }}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onChange(values.filter((_, i) => i !== index))}
            >
              Remove
            </Button>
          </div>
        ))}
      </div>
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
