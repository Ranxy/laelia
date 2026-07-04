import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import { Alert } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { formatTimestamp } from "@/lib/command-status";
import { useAppStore } from "@/stores";
import {
  type Agent,
  type AgentProviderInfo,
  AgentStatus_ConnectionState,
} from "@/types/proto-es/v1/agent_pb";

type Lifecycle =
  | "waiting-connection"
  | "pending-config"
  | "ready"
  | "configured-offline";

function agentLifecycle(agent: Agent): Lifecycle {
  const online = agent.status?.state === AgentStatus_ConnectionState.ONLINE;
  const cfg = agent.info?.acpConfig;
  // An agent is "configured" when it has either a selected provider or a
  // custom executable. A built-in provider derives its command from the
  // registry, so executable is empty for it.
  const configured = !!cfg?.provider || !!cfg?.executable;
  if (online && configured) return "ready";
  if (online && !configured) return "pending-config";
  if (!online && configured) return "configured-offline";
  return "waiting-connection";
}

function lifecycleLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  state: Lifecycle
): string {
  return t(`agent.lifecycle.${state}`);
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
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");
  const [rotateOpen, setRotateOpen] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [actionError, setActionError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{
    name: string;
    title: string;
  } | null>(null);
  const [deleting, setDeleting] = useState(false);

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

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await handleDelete(deleteTarget.name);
      setDeleteOpen(false);
      setDeleteTarget(null);
    } finally {
      setDeleting(false);
    }
  }

  function handleRowClick(agent: Agent) {
    setSelectedAgent(agent);
    setDetailOpen(true);
  }

  function handleEditACPConfig(agent: Agent) {
    setSelectedAgent(agent);
    const cfg = agent.info?.acpConfig;
    setExecutable(cfg?.executable ?? "");
    setArgs(cfg?.args ? [...cfg.args] : []);
    setAllowEnv(cfg?.allowEnv ? [...cfg.allowEnv] : []);
    setProvider(cfg?.provider ?? "");
    setModel(cfg?.model ?? "");
    setCustomEnvEntries(
      cfg?.customEnv
        ? Object.entries(cfg.customEnv).map(([key, value]) => ({ key, value }))
        : []
    );
    setSaveError("");
    setRefreshError("");
    setAcpConfigOpen(true);
  }

  async function handleRefreshProviders() {
    if (!selectedAgent?.name) return;
    setRefreshing(true);
    setRefreshError("");
    try {
      const refreshAgentProviders =
        useAppStore.getState().refreshAgentProviders;
      await refreshAgentProviders(selectedAgent.name);
      // refreshAgentProviders force-refreshes the cache; pull the updated agent
      // so the Sheet's availableProviders list re-renders.
      const updated = await useAppStore
        .getState()
        .getAgent(selectedAgent.name, {
          force: true,
        });
      if (updated) setSelectedAgent(updated);
      load();
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t("agent.acp-config-refresh-failed");
      setRefreshError(msg);
    } finally {
      setRefreshing(false);
    }
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
      // Fold the key-value editor entries into a map, dropping entries with
      // empty keys (empty-value entries are kept so a user can set FOO="").
      const customEnv: Record<string, string> = {};
      for (const entry of customEnvEntries) {
        const key = entry.key.trim();
        if (!key) continue;
        customEnv[key] = entry.value;
      }
      const acpConfig = {
        executable: executable.trim(),
        args: args.map((a) => a.trim()).filter((a) => a !== ""),
        allowEnv: allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
        provider: provider.trim(),
        model: model.trim(),
        customEnv,
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
        err instanceof Error ? err.message : t("agent.acp-config-save-failed");
      setSaveError(msg);
    } finally {
      setSaving(false);
    }
  }

  // Available providers are agent-reported (agent.info.availableProviders).
  // The "custom" escape hatch lets an admin hand-type a command for any
  // provider the daemon doesn't know about.
  const availableProviders: AgentProviderInfo[] =
    selectedAgent?.info?.availableProviders ?? [];
  const isCustomProvider = provider === "custom";
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === provider
  );
  const modelOptions = selectedProviderInfo?.models ?? [];
  const providerSupportsModel =
    !!selectedProviderInfo?.supportsModelConfigOption;
  // Save is allowed once a provider (built-in or custom) is chosen. For the
  // custom path an executable is still required; for a built-in provider the
  // command is derived from the registry, so executable stays empty.
  const canSave = isCustomProvider ? executable.trim() !== "" : provider !== "";

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
              onChange={(e) => setName(e.target.value)}
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
                  {t("agent.detail-configuration")}
                </span>
                <span>{lifecycleLabel(t, agentLifecycle(selectedAgent))}</span>

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
                  description={t("agent.waiting-connection-hint")}
                  className="mt-1"
                />
              )}
              {agentLifecycle(selectedAgent) === "pending-config" && (
                <Alert
                  variant="info"
                  description={t("agent.pending-config-hint")}
                  className="mt-1"
                />
              )}
              <div className="pt-3 border-t border-control-border flex flex-col gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleEditACPConfig(selectedAgent)}
                >
                  {t("agent.acp-config")}
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
            <SheetTitle>
              {t("agent.acp-config-title", {
                title: selectedAgent?.title ?? "",
              })}
            </SheetTitle>
            <SheetDescription>
              {t("agent.acp-config-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {saveError && (
              <Alert variant="error" description={saveError} className="mb-4" />
            )}
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-1">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-provider")}
                  </label>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={refreshing}
                    onClick={handleRefreshProviders}
                  >
                    {refreshing
                      ? t("common.loading")
                      : t("agent.acp-config-refresh-providers")}
                  </Button>
                </div>
                {refreshError && (
                  <Alert
                    variant="error"
                    description={refreshError}
                    className="mt-1"
                  />
                )}
                {availableProviders.length === 0 ? (
                  <p className="text-xs text-control-light">
                    {t("agent.acp-config-no-providers")}
                  </p>
                ) : (
                  <Select
                    value={provider}
                    onValueChange={(v) => {
                      setProvider(String(v ?? ""));
                      // Reset model when the provider changes — the previous
                      // value belongs to the old provider's option set.
                      setModel("");
                      setSaveError("");
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {(v: string | null) =>
                          v ? providerLabel(v, availableProviders) : ""
                        }
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {availableProviders.map((p) => (
                        <SelectItem key={p.providerId} value={p.providerId}>
                          {providerDisplayName(p)}
                        </SelectItem>
                      ))}
                      <SelectItem value="custom">
                        {t("agent.acp-config-provider-custom")}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                )}
              </div>

              {selectedProviderInfo && (
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-model")}
                  </label>
                  {providerSupportsModel && modelOptions.length > 0 ? (
                    <Select
                      value={model}
                      onValueChange={(v) => {
                        setModel(String(v ?? ""));
                        setSaveError("");
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue>
                          {(v: string | null) =>
                            v ? modelLabel(v, modelOptions) : ""
                          }
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {modelOptions.map((m) => (
                          <SelectItem key={m.value} value={m.value}>
                            {m.name || m.value}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <p className="text-xs text-control-light">
                      {t("agent.acp-config-model-unsupported")}
                    </p>
                  )}
                </div>
              )}

              {isCustomProvider && (
                <>
                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium">
                      {t("agent.acp-config-executable")}
                    </label>
                    <Input
                      placeholder={t("agent.acp-config-executable-placeholder")}
                      value={executable}
                      onChange={(e) => {
                        setExecutable(e.target.value);
                        setSaveError("");
                      }}
                    />
                  </div>

                  <StringListEditor
                    label={t("agent.acp-config-args")}
                    placeholder={t("agent.acp-config-args-placeholder")}
                    values={args}
                    onChange={(next) => {
                      setArgs(next);
                      setSaveError("");
                    }}
                  />
                </>
              )}

              {selectedProviderInfo && !isCustomProvider && (
                <p className="text-xs text-control-light">
                  {t("agent.acp-config-derived-command-hint")}
                </p>
              )}

              <KeyValueEnvEditor
                label={t("agent.acp-config-custom-env")}
                entries={customEnvEntries}
                onChange={(next) => {
                  setCustomEnvEntries(next);
                  setSaveError("");
                }}
              />

              <StringListEditor
                label={t("agent.acp-config-allow-env")}
                placeholder={t("agent.acp-config-allow-env-placeholder")}
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
              {t("common.cancel")}
            </Button>
            <Button disabled={saving || !canSave} onClick={handleSaveACPConfig}>
              {saving ? t("common.saving") : t("common.save")}
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

      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>{t("agent.delete-confirm-title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={deleting}
              onClick={handleConfirmDelete}
            >
              {deleting ? t("common.saving") : t("common.delete")}
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
                  tabIndex={0}
                  aria-label={t("agent.row-open-detail", {
                    title: agent.title,
                  })}
                  onClick={() => handleRowClick(agent)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      handleRowClick(agent);
                    }
                  }}
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
                        onClick={async (e) => {
                          e.stopPropagation();
                          const resourceId = agent.name.replace(
                            /^agents\//,
                            ""
                          );
                          // Lazily create (or fetch) the 1:1 conversation with
                          // this agent, then open it in the unified chat page.
                          try {
                            const convName = await useAppStore
                              .getState()
                              .getOrCreateConversation(`agents/${resourceId}`);
                            const convId = convName.split("/").pop();
                            if (convId) navigate(`/chat/${convId}`);
                          } catch {
                            // open failed — fall back to the agent workspace
                            navigate(`/agents/${resourceId}/commands`);
                          }
                        }}
                      >
                        {t("agent.action-chat")}
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
                        {t("agent.action-commands")}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          setDeleteTarget({
                            name: agent.name,
                            title: agent.title,
                          });
                          setDeleteOpen(true);
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
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium">{label}</label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onChange([...values, ""])}
        >
          {t("common.add")}
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
                next[index] = e.target.value;
                onChange(next);
              }}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onChange(values.filter((_, i) => i !== index))}
            >
              {t("common.remove")}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}

function providerDisplayName(p: AgentProviderInfo): string {
  if (p.displayName) {
    return p.version ? `${p.displayName} (${p.version})` : p.displayName;
  }
  return p.providerId;
}

function providerLabel(id: string, providers: AgentProviderInfo[]): string {
  if (id === "custom") return "";
  const p = providers.find((it) => it.providerId === id);
  return p ? providerDisplayName(p) : id;
}

function modelLabel(value: string, models: { value: string; name: string }[]) {
  const m = models.find((it) => it.value === value);
  return m ? m.name || m.value : value;
}

function KeyValueEnvEditor({
  label,
  entries,
  onChange,
}: {
  label: string;
  entries: { key: string; value: string }[];
  onChange: (next: { key: string; value: string }[]) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium">{label}</label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onChange([...entries, { key: "", value: "" }])}
        >
          {t("common.add")}
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        {entries.map((entry, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              placeholder={t("agent.acp-config-custom-env-key-placeholder")}
              value={entry.key}
              onChange={(e) => {
                const next = [...entries];
                next[index] = { ...next[index], key: e.target.value };
                onChange(next);
              }}
            />
            <Input
              placeholder={t("agent.acp-config-custom-env-value-placeholder")}
              value={entry.value}
              onChange={(e) => {
                const next = [...entries];
                next[index] = { ...next[index], value: e.target.value };
                onChange(next);
              }}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onChange(entries.filter((_, i) => i !== index))}
            >
              {t("common.remove")}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
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
