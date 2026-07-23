import { Loader2, Plus, Trash } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
import { ConnectionBadge } from "@/components/connection-badge";
import { MachineConnectionBadge } from "@/components/machine-connection-badge";
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
import { FieldRow } from "@/components/ui/field-row";
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
import { Textarea } from "@/components/ui/textarea";
import { formatTimestamp } from "@/lib/command-status";
import { buildMachineRunCommand } from "@/lib/machine-token";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import {
  type AgentProviderInfo,
  type AgentSummary,
} from "@/types/proto-es/v1/agent_pb";
import { type Machine } from "@/types/proto-es/v1/machine_pb";

// Field renders a labeled value row in the identity grid.
function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <dt className="text-xs text-control-light whitespace-nowrap pt-0.5">
        {label}
      </dt>
      <dd className="text-sm text-main min-w-0 break-words">{children}</dd>
    </>
  );
}

function Card({
  title,
  children,
  footer,
}: {
  title: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <section className="flex flex-col rounded-lg border border-control-border bg-background shadow-xs">
      <header className="border-b border-control-border px-5 py-3">
        <h2 className="text-sm font-semibold text-control">{title}</h2>
      </header>
      <div className="flex flex-col gap-4 p-5">{children}</div>
      {footer && (
        <footer className="border-t border-control-border px-5 py-3">
          {footer}
        </footer>
      )}
    </section>
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

export function MachineProfilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId } = useParams<{ machineId: string }>();
  const getMachine = useAppStore((s) => s.getMachine);
  const fetchMachines = useAppStore((s) => s.fetchMachines);

  const machineName = `machines/${machineId ?? ""}`;

  const [machine, setMachine] = useState<Machine | undefined>(undefined);
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [agentsLoading, setAgentsLoading] = useState(false);
  // loadError distinguishes a failed/missing fetch from an in-progress load so
  // the profile does not strand the user on a perpetual "Loading…" screen.
  const [loadError, setLoadError] = useState(false);

  // Token / control action state.
  const [rotateOpen, setRotateOpen] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [forceOpen, setForceOpen] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [forcing, setForcing] = useState(false);
  const [actionError, setActionError] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);

  // Provider refresh state.
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");

  // Add-agent sheet state. The sheet carries the full ACP config (provider,
  // model, persona, env, custom command) so an agent can be fully configured at
  // creation time instead of requiring a second visit to the agent profile.
  const [addOpen, setAddOpen] = useState(false);
  const [agentName, setAgentName] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [personaPrompt, setPersonaPrompt] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [allowEnv, setAllowEnv] = useState<string[]>([]);
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState<string[]>([]);
  const [adding, setAdding] = useState(false);
  const [addError, setAddError] = useState("");
  const [addedOpen, setAddedOpen] = useState(false);
  const [addedTitle, setAddedTitle] = useState("");

  // Remove-agent state.
  const [removeTarget, setRemoveTarget] = useState<AgentSummary | null>(null);
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState("");

  async function reload() {
    const m = await getMachine(machineName);
    setMachine(m);
    setLoadError(!m);
    setAgentsLoading(true);
    try {
      const listMachineAgents = useAppStore.getState().listMachineAgents;
      setAgents(await listMachineAgents(machineName));
    } finally {
      setAgentsLoading(false);
    }
  }

  useEffect(() => {
    if (!machineId) return;
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machineId, machineName]);

  if (!machine) {
    return (
      <div className="h-full overflow-y-auto p-6">
        {loadError ? (
          <div className="flex flex-col gap-3">
            <Alert
              variant="error"
              description={t("machine.profile.load-failed")}
            />
            <Button variant="outline" onClick={() => void reload()}>
              {t("common.retry")}
            </Button>
          </div>
        ) : (
          <p className="text-sm text-control-light">{t("common.loading")}</p>
        )}
      </div>
    );
  }

  const canEdit = machine.canEdit;
  const info = machine.info;
  const availableProviders: AgentProviderInfo[] =
    info?.availableProviders ?? [];

  // Add-agent form derived state. Provider is required; model is required only
  // when the selected provider exposes a model config option with advertised
  // models (a provider that does not expose model selection via the protocol
  // does not require a model). The "custom" provider hand-types a command and
  // never exposes model selection, so it requires an executable instead.
  const isCustomProvider = provider === "custom";
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === provider
  );
  const modelOptions = selectedProviderInfo?.models ?? [];
  const modelRequired =
    !!selectedProviderInfo?.supportsModelConfigOption &&
    modelOptions.length > 0;

  // resetAddForm clears the create-agent sheet inputs so reopening it starts
  // from a blank state instead of the previous submission's values.
  function resetAddForm() {
    setAgentName("");
    setProvider("");
    setModel("");
    setPersonaPrompt("");
    setCustomEnvEntries([]);
    setAllowEnv([]);
    setExecutable("");
    setArgs([]);
    setAddError("");
  }

  async function handleRefreshProviders() {
    setRefreshing(true);
    setRefreshError("");
    try {
      const refreshMachineProviders =
        useAppStore.getState().refreshMachineProviders;
      await refreshMachineProviders(machineName);
      await reload();
    } catch (err) {
      setRefreshError(
        err instanceof Error
          ? err.message
          : t("machine.providers-refresh-failed")
      );
    } finally {
      setRefreshing(false);
    }
  }

  async function handleRotateToken() {
    setRotating(true);
    setActionError("");
    try {
      const rotateMachineToken = useAppStore.getState().rotateMachineToken;
      const res = await rotateMachineToken(machineName);
      if (res.registrationToken) {
        setToken(res.registrationToken);
        setTokenOpen(true);
      }
      setRotateOpen(false);
      await reload();
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : t("machine.rotate-token-error")
      );
    } finally {
      setRotating(false);
    }
  }

  async function handleRevokeToken() {
    setRevoking(true);
    setActionError("");
    try {
      const revokeMachineToken = useAppStore.getState().revokeMachineToken;
      await revokeMachineToken(machineName);
      setRevokeOpen(false);
      await reload();
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : t("machine.revoke-token-error")
      );
    } finally {
      setRevoking(false);
    }
  }

  async function handleForceDisconnect() {
    setForcing(true);
    setActionError("");
    try {
      const forceDisconnectMachine =
        useAppStore.getState().forceDisconnectMachine;
      await forceDisconnectMachine(machineName);
      setForceOpen(false);
      await reload();
      fetchMachines({ pageSize: 100 }, { silent: true });
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : t("machine.force-disconnect-error")
      );
    } finally {
      setForcing(false);
    }
  }

  async function handleAddAgent() {
    setAddError("");
    const name = agentName.trim();
    if (!name) {
      setAddError(t("machine.add-agent-name-required"));
      return;
    }
    if (!provider) {
      setAddError(t("machine.add-agent-provider-required"));
      return;
    }
    if (isCustomProvider && !executable.trim()) {
      setAddError(t("machine.add-agent-executable-required"));
      return;
    }
    if (modelRequired && !model.trim()) {
      setAddError(t("machine.add-agent-model-required"));
      return;
    }
    // Fold the key-value editor entries into a map, dropping entries with empty
    // keys (empty-value entries are kept so a user can set FOO="").
    const customEnv: Record<string, string> = {};
    for (const entry of customEnvEntries) {
      const key = entry.key.trim();
      if (!key) continue;
      customEnv[key] = entry.value;
    }
    setAdding(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      await createAgent(name, machineName, {
        executable: executable.trim(),
        args: args.map((a) => a.trim()).filter((a) => a !== ""),
        allowEnv: allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
        provider: provider.trim(),
        model: model.trim(),
        personaPrompt: personaPrompt.trim(),
        customEnv,
      });
      setAddedTitle(name);
      setAddOpen(false);
      resetAddForm();
      setAddedOpen(true);
      await reload();
      fetchMachines({ pageSize: 100 }, { silent: true });
    } catch (err) {
      setAddError(err instanceof Error ? err.message : String(err));
    } finally {
      setAdding(false);
    }
  }

  async function handleConfirmRemoveAgent() {
    if (!removeTarget) return;
    setRemoving(true);
    setRemoveError("");
    try {
      const deleteAgent = useAppStore.getState().deleteAgent;
      await deleteAgent(removeTarget.name);
      setRemoveTarget(null);
      await reload();
      fetchMachines({ pageSize: 100 }, { silent: true });
    } catch (err) {
      setRemoveError(err instanceof Error ? err.message : String(err));
    } finally {
      setRemoving(false);
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
        {!canEdit && (
          <Alert
            variant="info"
            description={t("machine.profile.edit-not-allowed")}
          />
        )}

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-12">
          {/* Identity & host info */}
          <div className="lg:col-span-4">
            <Card title={t("machine.profile.section-identity")}>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                <Field label={t("machine.detail-name")}>{machine.title}</Field>
                <Field label={t("machine.detail-status")}>
                  <MachineConnectionBadge state={machine.status?.state} />
                </Field>
                {info?.hostname && (
                  <Field label={t("machine.detail-hostname")}>
                    {info.hostname}
                  </Field>
                )}
                {info?.os && (
                  <Field label={t("machine.detail-os")}>
                    {info.os}/{info.arch ?? ""}
                  </Field>
                )}
                {info?.ip && (
                  <Field label={t("machine.detail-ip")}>{info.ip}</Field>
                )}
                {info?.version && (
                  <Field label={t("machine.detail-version")}>
                    {info.version}
                  </Field>
                )}
                {machine.status?.connectedTime && (
                  <Field label={t("machine.detail-connected")}>
                    {formatTimestamp(machine.status.connectedTime)}
                  </Field>
                )}
                {machine.status?.lastHeartbeatTime && (
                  <Field label={t("machine.detail-last-heartbeat")}>
                    {formatTimestamp(machine.status.lastHeartbeatTime)}
                  </Field>
                )}
                {machine.createdAt && (
                  <Field label={t("machine.detail-created")}>
                    {formatTimestamp(machine.createdAt)}
                  </Field>
                )}
              </dl>
            </Card>

            {/* Token & connection control */}
            <div className="mt-6">
              <Card title={t("machine.profile.section-token")}>
                {actionError && (
                  <Alert variant="error" description={actionError} />
                )}
                {!canEdit ? (
                  <p className="text-xs text-control-light">
                    {t("machine.profile.edit-not-allowed")}
                  </p>
                ) : (
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setActionError("");
                        setRotateOpen(true);
                      }}
                    >
                      {t("machine.rotate-token")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setActionError("");
                        setRevokeOpen(true);
                      }}
                    >
                      {t("machine.revoke-token")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setActionError("");
                        setForceOpen(true);
                      }}
                    >
                      {t("machine.force-disconnect")}
                    </Button>
                  </div>
                )}
              </Card>
            </div>
          </div>

          {/* Providers + agent roster */}
          <div className="lg:col-span-8 flex flex-col gap-6">
            <Card
              title={t("machine.providers")}
              footer={
                <div className="flex items-center justify-end gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={refreshing || !canEdit}
                    onClick={handleRefreshProviders}
                  >
                    {refreshing ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : null}
                    {refreshing
                      ? t("common.loading")
                      : t("machine.refresh-providers")}
                  </Button>
                </div>
              }
            >
              {refreshError && (
                <Alert variant="error" description={refreshError} />
              )}
              {availableProviders.length === 0 ? (
                <p className="text-xs text-control-light">
                  {t("machine.no-providers")}
                </p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {availableProviders.map((p) => (
                    <li key={p.providerId} className="text-sm text-main">
                      {providerDisplayName(p)}
                    </li>
                  ))}
                </ul>
              )}
            </Card>

            <Card
              title={t("machine.agent-roster")}
              footer={
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="sm"
                    disabled={!canEdit}
                    onClick={() => {
                      resetAddForm();
                      setAddOpen(true);
                    }}
                  >
                    <Plus className="size-3.5" />
                    {t("machine.add-agent")}
                  </Button>
                </div>
              }
            >
              {agentsLoading ? (
                <p className="text-sm text-control-light">
                  {t("common.loading")}
                </p>
              ) : agents.length === 0 ? (
                <p className="text-sm text-control-light">
                  {t("machine.no-agents")}
                </p>
              ) : (
                <ul className="flex flex-col">
                  {agents.map((agent) => {
                    const resourceId = agent.name.replace(/^agents\//, "");
                    return (
                      <li key={agent.name}>
                        <div
                          role="button"
                          tabIndex={0}
                          className={cn(
                            "group flex cursor-pointer items-center gap-2 -mx-2 px-2 py-2 rounded-md transition-colors",
                            "hover:bg-control-bg/60"
                          )}
                          onClick={() => navigate(`/agents/${resourceId}`)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              navigate(`/agents/${resourceId}`);
                            }
                          }}
                        >
                          <div className="min-w-0 flex-1 flex flex-col gap-1">
                            <span className="truncate text-sm font-medium text-main">
                              {agent.title}
                            </span>
                            <ConnectionBadge state={agent.status?.state} />
                          </div>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="size-6 shrink-0 p-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                            aria-label={t("common.delete")}
                            disabled={!canEdit}
                            onClick={(e) => {
                              e.stopPropagation();
                              setRemoveTarget(agent);
                            }}
                          >
                            <Trash className="size-3.5" />
                          </Button>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </Card>
          </div>
        </div>
      </div>

      {/* Rotated registration token dialog */}
      <Dialog
        open={tokenOpen}
        onOpenChange={(next) => !next && setTokenOpen(false)}
      >
        <DialogContent className="max-w-lg">
          <DialogTitle>{t("machine.rotate-token-success-title")}</DialogTitle>
          <DialogDescription>
            {t("machine.rotate-token-success-description")}
          </DialogDescription>
          <div className="mt-4 space-y-3">
            <p className="text-sm text-control-light">
              {t("machine.created-run-hint")}
            </p>
            <div className="rounded bg-white border border-control-border p-3 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
              {token && buildMachineRunCommand(token, true)}
            </div>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => {
                if (token) {
                  navigator.clipboard
                    .writeText(buildMachineRunCommand(token, false))
                    .catch(() => {});
                }
              }}
            >
              {t("common.copy")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Add-agent sheet */}
      <Sheet
        open={addOpen}
        onOpenChange={(next) => {
          setAddOpen(next);
          if (!next) setAddError("");
        }}
      >
        <SheetContent width="wide">
          <SheetHeader>
            <SheetTitle>{t("machine.add-agent-title")}</SheetTitle>
            <SheetDescription>
              {t("machine.add-agent-description", { title: machine.title })}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {addError && (
              <Alert variant="error" description={addError} className="mb-2" />
            )}
            <div className="flex flex-col gap-4">
              <FieldRow
                label={t("machine.field-agent-name")}
                htmlFor="add-agent-name"
              >
                <Input
                  id="add-agent-name"
                  value={agentName}
                  placeholder={t("machine.add-agent-name-placeholder")}
                  onChange={(e) => {
                    setAgentName(e.target.value);
                    setAddError("");
                  }}
                />
              </FieldRow>

              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("agent.acp-config-provider")}
                </label>
                <Select
                  value={provider}
                  onValueChange={(v) => {
                    setProvider(String(v ?? ""));
                    // Reset model when the provider changes — the previous value
                    // belongs to the old provider's option set.
                    setModel("");
                    setAddError("");
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
                {availableProviders.length === 0 && (
                  <p className="text-xs text-control-light">
                    {t("machine.add-agent-no-providers")}
                  </p>
                )}
              </div>

              {selectedProviderInfo && (
                <div className="flex flex-col gap-1">
                  <label className="text-sm font-medium">
                    {t("agent.acp-config-model")}
                  </label>
                  {modelRequired ? (
                    <Select
                      value={model}
                      onValueChange={(v) => {
                        setModel(String(v ?? ""));
                        setAddError("");
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
                        setAddError("");
                      }}
                    />
                  </div>

                  <StringListEditor
                    label={t("agent.acp-config-args")}
                    placeholder={t("agent.acp-config-args-placeholder")}
                    values={args}
                    onChange={(next) => {
                      setArgs(next);
                      setAddError("");
                    }}
                  />
                </>
              )}

              {selectedProviderInfo && !isCustomProvider && (
                <p className="text-xs text-control-light">
                  {t("agent.acp-config-derived-command-hint")}
                </p>
              )}

              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("agent.acp-config-persona-prompt")}
                </label>
                <Textarea
                  className="font-mono text-sm min-h-[120px]"
                  placeholder={t("agent.acp-config-persona-prompt-placeholder")}
                  value={personaPrompt}
                  onChange={(e) => {
                    setPersonaPrompt(e.target.value);
                    setAddError("");
                  }}
                />
              </div>

              <KeyValueEnvEditor
                label={t("agent.acp-config-custom-env")}
                entries={customEnvEntries}
                onChange={(next) => {
                  setCustomEnvEntries(next);
                  setAddError("");
                }}
              />

              <StringListEditor
                label={t("agent.acp-config-allow-env")}
                placeholder={t("agent.acp-config-allow-env-placeholder")}
                values={allowEnv}
                onChange={(next) => {
                  setAllowEnv(next);
                  setAddError("");
                }}
              />
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setAddOpen(false)}
              disabled={adding}
            >
              {t("common.cancel")}
            </Button>
            <Button
              disabled={
                adding ||
                !agentName.trim() ||
                !provider ||
                (isCustomProvider && !executable.trim()) ||
                (modelRequired && !model.trim())
              }
              onClick={handleAddAgent}
            >
              {adding ? t("common.creating") : t("common.create")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Agent created (picked up automatically) dialog */}
      <Dialog
        open={addedOpen}
        onOpenChange={(next) => !next && setAddedOpen(false)}
      >
        <DialogContent className="max-w-lg">
          <DialogTitle>{t("machine.agent-created-title")}</DialogTitle>
          <DialogDescription>
            {t("machine.agent-created-description", {
              title: addedTitle,
              machine: machine.title,
            })}
          </DialogDescription>
        </DialogContent>
      </Dialog>

      {/* Remove-agent confirm */}
      <AlertDialog
        open={!!removeTarget}
        onOpenChange={(next) => !next && setRemoveTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("machine.remove-agent-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.remove-agent-confirm-description", {
              title: removeTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          {removeError && <Alert variant="error" description={removeError} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={removing}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={removing}
              onClick={handleConfirmRemoveAgent}
            >
              {removing ? t("common.saving") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Rotate confirm */}
      <AlertDialog
        open={rotateOpen}
        onOpenChange={(next) => !next && setRotateOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("machine.rotate-token-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.rotate-token-confirm-description")}
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
              {rotating ? t("common.creating") : t("machine.rotate-token")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Revoke confirm */}
      <AlertDialog
        open={revokeOpen}
        onOpenChange={(next) => !next && setRevokeOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("machine.revoke-token-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.revoke-token-confirm-description")}
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
              {revoking ? t("common.creating") : t("machine.revoke-token")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Force-disconnect confirm */}
      <AlertDialog
        open={forceOpen}
        onOpenChange={(next) => !next && setForceOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("machine.force-disconnect-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.force-disconnect-confirm-description")}
          </AlertDialogDescription>
          {actionError && (
            <Alert variant="error" description={actionError} className="mt-2" />
          )}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={forcing}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button disabled={forcing} onClick={handleForceDisconnect}>
              {forcing ? t("common.loading") : t("machine.force-disconnect")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
