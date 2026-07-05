import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
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
import { buildAgentRunCommand } from "@/lib/agent-token";
import { agentResourceName, formatTimestamp } from "@/lib/command-status";
import { useAppStore } from "@/stores";
import { type AgentProviderInfo } from "@/types/proto-es/v1/agent_pb";
import { agentLifecycle, lifecycleLabel } from "./agents";

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

export function AgentProfilePage() {
  const { t } = useTranslation();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const agentCache = useAppStore((s) => s.agentCache);
  const fetchAgents = useAppStore((s) => s.fetchAgents);

  const agentName = agentResourceName(agentId);
  const agent = agentCache[agentName];

  // ACP config editor local state, seeded from the agent's persisted config.
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

  // Token action state.
  const [rotateOpen, setRotateOpen] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [actionError, setActionError] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [tokenFromRotation, setTokenFromRotation] = useState(false);

  useEffect(() => {
    if (!agentId) return;
    getAgent(agentName);
  }, [agentId, agentName, getAgent]);

  // Re-seed the editor whenever the persisted config reference changes (e.g.
  // after refresh-providers or a save round-trip), so the form reflects the
  // latest server state instead of stale local edits.
  useEffect(() => {
    const cfg = agent?.info?.acpConfig;
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
  }, [agent?.name, agent?.info?.acpConfig]);

  if (!agent) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      </div>
    );
  }

  async function handleRefreshProviders() {
    setRefreshing(true);
    setRefreshError("");
    try {
      const refreshAgentProviders =
        useAppStore.getState().refreshAgentProviders;
      await refreshAgentProviders(agentName);
      await getAgent(agentName, { force: true });
      fetchAgents({ pageSize: 100 }, { silent: true });
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

  async function handleSaveACPConfig() {
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
      const updateAgentACPConfig = useAppStore.getState().updateAgentACPConfig;
      await updateAgentACPConfig(agentName, {
        executable: executable.trim(),
        args: args.map((a) => a.trim()).filter((a) => a !== ""),
        allowEnv: allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
        provider: provider.trim(),
        model: model.trim(),
        customEnv,
      });
      await getAgent(agentName, { force: true });
      fetchAgents({ pageSize: 100 }, { silent: true });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.acp-config-save-failed");
      setSaveError(msg);
    } finally {
      setSaving(false);
    }
  }

  async function handleRotateToken() {
    setRotating(true);
    setActionError("");
    try {
      const rotateAgentToken = useAppStore.getState().rotateAgentToken;
      const res = await rotateAgentToken(agentName);
      if (res.bootstrapToken) {
        setToken(res.bootstrapToken);
        setTokenFromRotation(true);
        setTokenOpen(true);
      }
      setRotateOpen(false);
      fetchAgents({ pageSize: 100 }, { silent: true });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.rotate-token-error");
      setActionError(msg);
    } finally {
      setRotating(false);
    }
  }

  async function handleRevokeToken() {
    setRevoking(true);
    setActionError("");
    try {
      const revokeAgentToken = useAppStore.getState().revokeAgentToken;
      await revokeAgentToken(agentName);
      setRevokeOpen(false);
      fetchAgents({ pageSize: 100 }, { silent: true });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.revoke-token-error");
      setActionError(msg);
    } finally {
      setRevoking(false);
    }
  }

  // Available providers are agent-reported (agent.info.availableProviders).
  // The "custom" escape hatch lets an admin hand-type a command for any
  // provider the daemon doesn't know about.
  const availableProviders: AgentProviderInfo[] =
    agent.info?.availableProviders ?? [];
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
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-6 w-full max-w-2xl">
      {/* Identity & status */}
      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-semibold text-control">
          {t("agent.profile.section-identity")}
        </h2>
        <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <span className="text-control-light whitespace-nowrap">
            {t("agent.detail-name")}
          </span>
          <span>{agent.title}</span>

          <span className="text-control-light whitespace-nowrap">
            {t("agent.detail-status")}
          </span>
          <span>
            <ConnectionBadge state={agent.status?.state} />
          </span>

          <span className="text-control-light whitespace-nowrap">
            {t("agent.detail-configuration")}
          </span>
          <span>{lifecycleLabel(t, agentLifecycle(agent))}</span>

          {agent.info?.hostname && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-hostname")}
              </span>
              <span>{agent.info.hostname}</span>
            </>
          )}
          {agent.info?.os && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-os")}
              </span>
              <span>
                {agent.info.os}/{agent.info.arch ?? ""}
              </span>
            </>
          )}
          {agent.info?.ip && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-ip")}
              </span>
              <span>{agent.info.ip}</span>
            </>
          )}
          {agent.info?.version && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-version")}
              </span>
              <span>{agent.info.version}</span>
            </>
          )}
          {agent.status?.connectedTime && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-connected")}
              </span>
              <span>{formatTimestamp(agent.status.connectedTime)}</span>
            </>
          )}
          {agent.status?.lastHeartbeatTime && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-last-heartbeat")}
              </span>
              <span>{formatTimestamp(agent.status.lastHeartbeatTime)}</span>
            </>
          )}
          {agent.createdAt && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-created")}
              </span>
              <span>{formatTimestamp(agent.createdAt)}</span>
            </>
          )}
          <span className="text-control-light whitespace-nowrap">
            {t("agent.detail-token-version")}
          </span>
          <span>{agent.tokenVersion ?? "-"}</span>
          {agent.lastTokenRotatedAt && (
            <>
              <span className="text-control-light whitespace-nowrap">
                {t("agent.detail-last-rotated")}
              </span>
              <span>{formatTimestamp(agent.lastTokenRotatedAt)}</span>
            </>
          )}
        </div>
        {agentLifecycle(agent) === "waiting-connection" && (
          <Alert
            variant="info"
            description={t("agent.waiting-connection-hint")}
            className="mt-1"
          />
        )}
        {agentLifecycle(agent) === "pending-config" && (
          <Alert
            variant="info"
            description={t("agent.pending-config-hint")}
            className="mt-1"
          />
        )}
      </section>

      {/* ACP config */}
      <section className="flex flex-col gap-3 pt-4 border-t border-control-border">
        <h2 className="text-sm font-semibold text-control">
          {t("agent.acp-config")}
        </h2>
        {saveError && <Alert variant="error" description={saveError} />}
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
                  // Reset model when the provider changes — the previous value
                  // belongs to the old provider's option set.
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
        <div className="flex justify-end">
          <Button disabled={saving || !canSave} onClick={handleSaveACPConfig}>
            {saving ? t("common.saving") : t("common.save")}
          </Button>
        </div>
      </section>

      {/* Token actions */}
      <section className="flex flex-col gap-2 pt-4 border-t border-control-border">
        <h2 className="text-sm font-semibold text-control">
          {t("agent.profile.section-token")}
        </h2>
        {actionError && <Alert variant="error" description={actionError} />}
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
      </section>

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
              {token && buildAgentRunCommand(token, true)}
            </div>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => {
                if (token) {
                  navigator.clipboard
                    .writeText(buildAgentRunCommand(token, false))
                    .catch(() => {});
                }
              }}
            >
              {t("common.copy")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

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
    </div>
  );
}
