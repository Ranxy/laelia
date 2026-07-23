import { Loader2, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  deleteAgentAvatar,
  invalidateAvatar,
  uploadAgentAvatar,
  useAvatar,
} from "@/lib/avatar-cache";
import { agentResourceName, formatTimestamp } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import {
  type Agent,
  type AgentProviderInfo,
} from "@/types/proto-es/v1/agent_pb";
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

// Field renders a labeled value row in the identity grid. The label is muted
// and right-aligned on a fixed column so values line up vertically.
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

export function AgentProfilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const getMachine = useAppStore((s) => s.getMachine);
  const fetchAgents = useAppStore((s) => s.fetchAgents);

  const agentName = agentResourceName(agentId);
  // Hold the full GetAgent result in local state, fetched fresh on entry and
  // re-fetched after each mutation. canEdit/acp_config are per-caller and
  // mutable, so they are never cached in the store — this avoids a stale
  // canEdit surviving a user switch (admin → normal user).
  const [agent, setAgent] = useState<Agent | undefined>(undefined);

  // ACP config editor local state, seeded from the agent's persisted config.
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState<string[]>([]);
  const [allowEnv, setAllowEnv] = useState<string[]>([]);
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [personaPrompt, setPersonaPrompt] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  // Available providers are machine-scoped: the owning machine probes its host
  // and exposes them on Machine.info.availableProviders. We fetch the machine
  // (by agent.machine) so the provider/model selectors here read the same list
  // the machine profile page manages. Refresh happens on the machine profile.
  const [machineProviders, setMachineProviders] = useState<AgentProviderInfo[]>(
    []
  );

  const [avatarBusy, setAvatarBusy] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const agentAvatarName = agent?.avatar || undefined;
  const avatarSrc = useAvatar(agentAvatarName);

  async function handleAvatarChange(file: File | undefined) {
    if (!file || !agentName) return;
    setAvatarBusy(true);
    try {
      await uploadAgentAvatar(agentName, file);
      if (agentAvatarName) invalidateAvatar(agentAvatarName);
      setAgent(await getAgent(agentName));
      toastManager.add({
        type: "success",
        title: t("agent.profile.avatar-uploaded"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.profile.avatar-upload-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setAvatarBusy(false);
    }
  }

  async function handleAvatarRemove() {
    if (!agentName || !agentAvatarName) return;
    setAvatarBusy(true);
    try {
      await deleteAgentAvatar(agentAvatarName);
      invalidateAvatar(agentAvatarName);
      setAgent(await getAgent(agentName));
      toastManager.add({
        type: "success",
        title: t("agent.profile.avatar-removed"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.profile.avatar-remove-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setAvatarBusy(false);
    }
  }

  useEffect(() => {
    if (!agentId) return;
    getAgent(agentName).then(setAgent);
  }, [agentId, agentName, getAgent]);

  // Load the owning machine's available providers whenever the agent's machine
  // binding is known. Providers are machine-scoped (the machine probes its host);
  // the agent profile reads them from the machine rather than the agent.
  useEffect(() => {
    const machineName = agent?.machine;
    if (!machineName) {
      setMachineProviders([]);
      return;
    }
    getMachine(machineName).then((m) =>
      setMachineProviders(m?.info?.availableProviders ?? [])
    );
  }, [agent?.machine, getMachine]);

  // Re-seed the editor whenever the persisted config reference changes (e.g.
  // after a save round-trip), so the form reflects the latest server state
  // instead of stale local edits.
  useEffect(() => {
    const cfg = agent?.info?.acpConfig;
    setExecutable(cfg?.executable ?? "");
    setArgs(cfg?.args ? [...cfg.args] : []);
    setAllowEnv(cfg?.allowEnv ? [...cfg.allowEnv] : []);
    setProvider(cfg?.provider ?? "");
    setModel(cfg?.model ?? "");
    setPersonaPrompt(cfg?.personaPrompt ?? "");
    setCustomEnvEntries(
      cfg?.customEnv
        ? Object.entries(cfg.customEnv).map(([key, value]) => ({ key, value }))
        : []
    );
    setSaveError("");
  }, [agent?.name, agent?.info?.acpConfig]);

  if (!agent) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      </div>
    );
  }

  // Profile mutations are gated to the agent's creator (via the agentEditor IAM
  // binding) or a workspace admin (all-permissions union), enforced server-side
  // by the agents.edit permission. The server resolves this per-agent and
  // surfaces it as Agent.canEdit, so the UI does not need to re-derive it from
  // the creator's name. Hide/disable the editors for everyone else so the UI
  // never offers a 403.
  const canEdit = agent.canEdit;

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
        personaPrompt: personaPrompt.trim(),
        customEnv,
      });
      setAgent(await getAgent(agentName));
      fetchAgents({ pageSize: 100 }, { silent: true });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : t("agent.acp-config-save-failed");
      setSaveError(msg);
    } finally {
      setSaving(false);
    }
  }

  // Available providers are machine-scoped: the owning machine probes its host
  // and exposes them on Machine.info.availableProviders. The "custom" escape
  // hatch lets an admin hand-type a command for any provider the machine does
  // not know about.
  const availableProviders: AgentProviderInfo[] = machineProviders;
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

  const lifecycle = agentLifecycle(agent);

  const machineResourceID = agent.machine
    ? agent.machine.replace(/^machines\//, "")
    : "";

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
        {!canEdit && (
          <Alert
            variant="info"
            description={t("agent.profile.edit-not-allowed")}
          />
        )}
        {lifecycle === "waiting-connection" && (
          <Alert
            variant="info"
            description={t("agent.waiting-connection-hint")}
          />
        )}
        {lifecycle === "pending-config" && (
          <Alert variant="info" description={t("agent.pending-config-hint")} />
        )}

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-12">
          {/* Identity & status */}
          <div className="lg:col-span-4">
            <Card title={t("agent.profile.section-identity")}>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                <Field label={t("agent.detail-name")}>{agent.title}</Field>
                <Field label={t("agent.detail-status")}>
                  <ConnectionBadge state={agent.status?.state} />
                </Field>
                <Field label={t("agent.detail-configuration")}>
                  {lifecycleLabel(t, lifecycle)}
                </Field>
                {agent.machine && (
                  <Field label={t("agent.detail-machine")}>
                    <button
                      type="button"
                      className="text-sm text-link hover:underline"
                      onClick={() =>
                        machineResourceID &&
                        navigate(`/machines/${machineResourceID}`)
                      }
                    >
                      {agent.machine}
                    </button>
                  </Field>
                )}
                {agent.status?.connectedTime && (
                  <Field label={t("agent.detail-connected")}>
                    {formatTimestamp(agent.status.connectedTime)}
                  </Field>
                )}
                {agent.status?.lastHeartbeatTime && (
                  <Field label={t("agent.detail-last-heartbeat")}>
                    {formatTimestamp(agent.status.lastHeartbeatTime)}
                  </Field>
                )}
                {agent.createdAt && (
                  <Field label={t("agent.detail-created")}>
                    {formatTimestamp(agent.createdAt)}
                  </Field>
                )}
              </dl>

              {/* Avatar */}
              <div className="flex items-center gap-4 pt-2 border-t border-control-border">
                <Avatar seed={agentId || agent.title} src={avatarSrc} />
                <div className="min-w-0 flex-1">
                  <div className="text-xs font-medium text-control">
                    {t("agent.profile.avatar")}
                  </div>
                  <p className="mt-0.5 text-xs text-control-placeholder">
                    {t("agent.profile.avatar-hint")}
                  </p>
                  <div className="mt-2 flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => fileInputRef.current?.click()}
                      disabled={!canEdit || avatarBusy}
                    >
                      {avatarBusy ? (
                        <Loader2 className="size-3.5 animate-spin" />
                      ) : (
                        <Upload className="size-3.5" />
                      )}
                      {avatarBusy
                        ? t("agent.profile.avatar-uploading")
                        : t("agent.profile.avatar-upload")}
                    </Button>
                    {agent.avatar && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleAvatarRemove}
                        disabled={!canEdit || avatarBusy}
                      >
                        <Trash2 className="size-3.5" />
                        {t("agent.profile.avatar-remove")}
                      </Button>
                    )}
                    <input
                      ref={fileInputRef}
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/gif"
                      className="hidden"
                      onChange={(e) => {
                        void handleAvatarChange(e.target.files?.[0]);
                        e.target.value = "";
                      }}
                    />
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* ACP config */}
          <div className="lg:col-span-8">
            <Card
              title={t("agent.acp-config")}
              footer={
                <div className="flex items-center justify-end gap-2">
                  <Button
                    disabled={saving || !canSave || !canEdit}
                    onClick={handleSaveACPConfig}
                  >
                    {saving ? t("common.saving") : t("common.save")}
                  </Button>
                </div>
              }
            >
              {saveError && <Alert variant="error" description={saveError} />}
              <fieldset disabled={!canEdit} className="contents">
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium">
                      {t("agent.acp-config-provider")}
                    </label>
                    {availableProviders.length === 0 ? (
                      <p className="text-xs text-control-light">
                        {machineResourceID
                          ? t("agent.acp-config-no-providers-machine")
                          : t("agent.acp-config-no-providers")}
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
                    {machineResourceID && (
                      <p className="text-xs text-control-light">
                        <button
                          type="button"
                          className="text-link hover:underline"
                          onClick={() =>
                            navigate(`/machines/${machineResourceID}`)
                          }
                        >
                          {t("agent.acp-config-manage-providers")}
                        </button>
                      </p>
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
                          placeholder={t(
                            "agent.acp-config-executable-placeholder"
                          )}
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

                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium">
                      {t("agent.acp-config-persona-prompt")}
                    </label>
                    <Textarea
                      className="font-mono text-sm min-h-[160px]"
                      placeholder={t(
                        "agent.acp-config-persona-prompt-placeholder"
                      )}
                      value={personaPrompt}
                      onChange={(e) => {
                        setPersonaPrompt(e.target.value);
                        setSaveError("");
                      }}
                    />
                  </div>

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
              </fieldset>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}
