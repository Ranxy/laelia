import { Check, Loader2, Pencil, Trash2, Upload } from "lucide-react";
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
import type { AgentACPConfigInput } from "@/stores/types";
import {
  type Agent,
  type AgentACPConfig,
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
  actions,
}: {
  title: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <section className="flex flex-col rounded-lg border border-control-border bg-background shadow-xs">
      <header className="flex items-center justify-between border-b border-control-border px-5 py-3">
        <h2 className="text-sm font-semibold text-control">{title}</h2>
        {actions}
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
  // loadError distinguishes a failed/missing fetch from an in-progress load so
  // the profile does not strand the user on a perpetual "Loading…" screen.
  const [loadError, setLoadError] = useState(false);

  // ACP config editor local state, seeded from the agent's persisted config.
  // All fields except personaPrompt auto-persist (selects + add/remove
  // immediately, text inputs on blur); personaPrompt has its own explicit
  // inline edit→save cycle. configRef mirrors the live fields synchronously so
  // the async save always reads current values; agentRef mirrors the latest
  // fetched agent so saves can read the persisted config/persona snapshot.
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState<string[]>([]);
  const [allowEnv, setAllowEnv] = useState<string[]>([]);
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [personaDraft, setPersonaDraft] = useState("");
  const [personaEditing, setPersonaEditing] = useState(false);
  const [saveStatus, setSaveStatus] = useState<
    "idle" | "saving" | "saved" | "error"
  >("idle");

  const configRef = useRef({
    executable: "",
    args: [] as string[],
    allowEnv: [] as string[],
    provider: "",
    model: "",
    customEnvEntries: [] as { key: string; value: string }[],
  });
  const agentRef = useRef<Agent | undefined>(undefined);
  agentRef.current = agent;
  // Saves are serialized through this chain so config auto-saves and persona
  // saves never overlap. Each save refetches the agent, which updates the
  // persisted snapshot for the next save — last write wins, no revert races.
  const saveChainRef = useRef<Promise<void>>(Promise.resolve());
  const savedTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined
  );

  // Available providers are machine-scoped: the owning machine probes its host
  // and exposes them on Machine.info.availableProviders. We fetch the machine
  // (by agent.machine) so the provider/model selectors here read the same list
  // the machine profile page manages. Refresh happens on the machine profile.
  const [machineProviders, setMachineProviders] = useState<AgentProviderInfo[]>(
    []
  );
  // machineTitle is the owning machine's display title (resolved from
  // agent.machine so the identity grid shows a name, not the raw id).
  const [machineTitle, setMachineTitle] = useState("");

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

  async function loadAgent() {
    if (!agentId) return;
    const a = await getAgent(agentName);
    setAgent(a);
    setLoadError(!a);
  }

  useEffect(() => {
    if (!agentId) return;
    void loadAgent();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, agentName, getAgent]);

  // Load the owning machine's available providers whenever the agent's machine
  // binding is known. Providers are machine-scoped (the machine probes its host);
  // the agent profile reads them from the machine rather than the agent.
  useEffect(() => {
    const machineName = agent?.machine;
    if (!machineName) {
      setMachineProviders([]);
      setMachineTitle("");
      return;
    }
    getMachine(machineName).then((m) => {
      setMachineProviders(m?.info?.availableProviders ?? []);
      setMachineTitle(m?.title ?? "");
    });
  }, [agent?.machine, getMachine]);

  // Seed the editor once per agent (on load / agent switch). Deliberately keyed
  // on agent.name only — NOT on acpConfig — so the refetch that follows each
  // auto-save does not clobber in-progress edits. The server does not push
  // config changes to us, so there is no external drift to re-sync against.
  useEffect(() => {
    const cfg = agent?.info?.acpConfig;
    const next = {
      executable: cfg?.executable ?? "",
      args: cfg?.args ? [...cfg.args] : [],
      allowEnv: cfg?.allowEnv ? [...cfg.allowEnv] : [],
      provider: cfg?.provider ?? "",
      model: cfg?.model ?? "",
      customEnvEntries: cfg?.customEnv
        ? Object.entries(cfg.customEnv).map(([key, value]) => ({ key, value }))
        : [],
    };
    configRef.current = next;
    setExecutable(next.executable);
    setArgs(next.args);
    setAllowEnv(next.allowEnv);
    setProvider(next.provider);
    setModel(next.model);
    setCustomEnvEntries(next.customEnvEntries);
    setPersonaDraft(cfg?.personaPrompt ?? "");
    setPersonaEditing(false);
    setSaveStatus("idle");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent?.name]);

  if (!agent) {
    return (
      <div className="h-full overflow-y-auto p-6">
        {loadError ? (
          <div className="flex flex-col gap-3">
            <Alert
              variant="error"
              description={t("agent.profile.load-failed")}
            />
            <Button variant="outline" onClick={() => void loadAgent()}>
              {t("common.retry")}
            </Button>
          </div>
        ) : (
          <p className="text-sm text-control-light">{t("common.loading")}</p>
        )}
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

  // Fold the key-value editor entries into a map, dropping entries with empty
  // keys (empty-value entries are kept so a user can set FOO="").
  function foldCustomEnv(
    entries: { key: string; value: string }[]
  ): Record<string, string> {
    const customEnv: Record<string, string> = {};
    for (const entry of entries) {
      const key = entry.key.trim();
      if (!key) continue;
      customEnv[key] = entry.value;
    }
    return customEnv;
  }

  // Build a full-replace config payload from the live draft, carrying the given
  // persona (the persisted persona for config auto-saves, so an unsaved persona
  // draft is never persisted by a config save).
  function buildFromDraft(
    draft: typeof configRef.current,
    personaPrompt: string
  ): AgentACPConfigInput {
    return {
      executable: draft.executable.trim(),
      args: draft.args.map((a) => a.trim()).filter((a) => a !== ""),
      allowEnv: draft.allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
      provider: draft.provider.trim(),
      model: draft.model.trim(),
      customEnv: foldCustomEnv(draft.customEnvEntries),
      personaPrompt,
    };
  }

  // Build a full-replace config payload from the persisted server config,
  // overriding only persona — so a persona save never touches (possibly
  // mid-edit, possibly invalid) config draft state.
  function buildFromPersisted(
    cfg: AgentACPConfig | undefined,
    personaPrompt: string
  ): AgentACPConfigInput {
    return {
      executable: cfg?.executable ?? "",
      args: cfg?.args ? [...cfg.args] : [],
      allowEnv: cfg?.allowEnv ? [...cfg.allowEnv] : [],
      provider: cfg?.provider ?? "",
      model: cfg?.model ?? "",
      customEnv: { ...(cfg?.customEnv ?? {}) },
      personaPrompt,
    };
  }

  // Serialize saves: each save awaits the previous, then refetches the agent so
  // the persisted snapshot used by the next save is current. Errors surface as a
  // toast plus the "error" status; success shows a fleeting "saved" status.
  function enqueueSave(
    build: () => AgentACPConfigInput,
    opts?: { silent?: boolean }
  ) {
    saveChainRef.current = saveChainRef.current.then(async () => {
      setSaveStatus("saving");
      try {
        const updateAgentACPConfig =
          useAppStore.getState().updateAgentACPConfig;
        await updateAgentACPConfig(agentName, build());
        setAgent(await getAgent(agentName));
        fetchAgents({ pageSize: 100 }, { silent: true });
        if (!opts?.silent) {
          setSaveStatus("saved");
          if (savedTimerRef.current) clearTimeout(savedTimerRef.current);
          savedTimerRef.current = setTimeout(() => setSaveStatus("idle"), 1500);
        }
      } catch (err) {
        setSaveStatus("error");
        toastManager.add({
          type: "error",
          title: t("agent.acp-config-save-failed"),
          description: err instanceof Error ? err.message : String(err),
        });
      }
    });
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
  // A config is saveable once a provider (built-in or custom) is chosen. For
  // the custom path an executable is still required; for a built-in provider
  // the command is derived from the registry, so executable stays empty. When
  // the provider exposes model selection, a model must also be chosen.
  function canSaveFor(draft: typeof configRef.current): boolean {
    if (draft.provider === "custom") return draft.executable.trim() !== "";
    const info = availableProviders.find(
      (p) => p.providerId === draft.provider
    );
    const needsModel =
      !!info?.supportsModelConfigOption && (info?.models ?? []).length > 0;
    return draft.provider !== "" && (!needsModel || draft.model.trim() !== "");
  }

  // Skip a save when the live draft matches what the server already holds
  // (e.g. focus→blur with no edit), to avoid redundant writes.
  function isConfigDirty(): boolean {
    const cfg = agentRef.current?.info?.acpConfig;
    const draft = buildFromDraft(configRef.current, cfg?.personaPrompt ?? "");
    const persisted = buildFromPersisted(cfg, cfg?.personaPrompt ?? "");
    return JSON.stringify(draft) !== JSON.stringify(persisted);
  }

  function saveConfig() {
    if (!canEdit) return;
    if (!canSaveFor(configRef.current)) return;
    if (!isConfigDirty()) {
      setSaveStatus("idle");
      return;
    }
    // Read the persisted persona inside the build closure (at execution time,
    // after any earlier saves in the queue have refetched) so a config save
    // queued behind a persona save never reverts the persona.
    enqueueSave(() =>
      buildFromDraft(
        configRef.current,
        agentRef.current?.info?.acpConfig?.personaPrompt ?? ""
      )
    );
  }

  function savePersona() {
    if (!canEdit) return;
    const persistedPersona =
      agentRef.current?.info?.acpConfig?.personaPrompt ?? "";
    if (personaDraft.trim() === persistedPersona.trim()) {
      setPersonaEditing(false);
      return;
    }
    setPersonaEditing(false);
    // Read the persisted config inside the build closure (at execution time)
    // so a persona save picks up the latest server config rather than a stale
    // snapshot captured at click time.
    enqueueSave(() =>
      buildFromPersisted(agentRef.current?.info?.acpConfig, personaDraft.trim())
    );
  }

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

        <div className="flex flex-col gap-6">
          {/* Identity & status */}
          <div>
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
                      {machineTitle || agent.machine}
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
          <div>
            <Card
              title={t("agent.acp-config")}
              actions={
                saveStatus === "saving" ? (
                  <span className="flex items-center gap-1 text-xs text-control-light">
                    <Loader2 className="size-3 animate-spin" />
                    {t("agent.acp-config-saving")}
                  </span>
                ) : saveStatus === "saved" ? (
                  <span className="flex items-center gap-1 text-xs text-control-light">
                    <Check className="size-3" />
                    {t("agent.acp-config-saved")}
                  </span>
                ) : saveStatus === "error" ? (
                  <span className="flex items-center gap-1 text-xs text-error">
                    <span className="size-1.5 rounded-full bg-error" />
                    {t("agent.acp-config-save-error")}
                  </span>
                ) : null
              }
            >
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
                          const next = String(v ?? "");
                          // Reset model when the provider changes — the previous
                          // value belongs to the old provider's option set.
                          configRef.current = {
                            ...configRef.current,
                            provider: next,
                            model: "",
                          };
                          setProvider(next);
                          setModel("");
                          saveConfig();
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
                            const next = String(v ?? "");
                            configRef.current = {
                              ...configRef.current,
                              model: next,
                            };
                            setModel(next);
                            saveConfig();
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
                            const next = e.target.value;
                            configRef.current = {
                              ...configRef.current,
                              executable: next,
                            };
                            setExecutable(next);
                          }}
                          onBlur={() => saveConfig()}
                        />
                      </div>

                      <StringListEditor
                        label={t("agent.acp-config-args")}
                        placeholder={t("agent.acp-config-args-placeholder")}
                        values={args}
                        onChange={(next) => {
                          configRef.current = {
                            ...configRef.current,
                            args: next,
                          };
                          setArgs(next);
                        }}
                        onCommit={(next) => {
                          configRef.current = {
                            ...configRef.current,
                            args: next,
                          };
                          setArgs(next);
                          saveConfig();
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
                    <div className="flex items-center gap-2">
                      <div className="text-xs font-semibold uppercase tracking-widest text-control-light">
                        {t("agent.acp-config-persona-prompt")}
                      </div>
                      {!personaEditing && (
                        <button
                          type="button"
                          aria-label={t("common.edit")}
                          title={t("common.edit")}
                          className="text-control-light hover:text-control transition-colors disabled:cursor-not-allowed disabled:opacity-40"
                          disabled={!canEdit}
                          onClick={() => setPersonaEditing(true)}
                        >
                          <Pencil className="size-3" />
                        </button>
                      )}
                    </div>
                    {personaEditing ? (
                      <div className="flex flex-col gap-2">
                        <Textarea
                          className="font-mono text-sm min-h-[160px]"
                          placeholder={t(
                            "agent.acp-config-persona-prompt-placeholder"
                          )}
                          value={personaDraft}
                          onChange={(e) => setPersonaDraft(e.target.value)}
                        />
                        <div className="flex items-center gap-2">
                          <Button
                            size="sm"
                            disabled={!canEdit}
                            onClick={savePersona}
                          >
                            {t("common.save")}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setPersonaDraft(
                                agentRef.current?.info?.acpConfig
                                  ?.personaPrompt ?? ""
                              );
                              setPersonaEditing(false);
                            }}
                          >
                            {t("common.cancel")}
                          </Button>
                        </div>
                      </div>
                    ) : (
                      <p className="text-sm text-main whitespace-pre-wrap">
                        {personaDraft.trim() ? (
                          personaDraft
                        ) : (
                          <span className="italic text-control-light">
                            {t("agent.acp-config-persona-empty")}
                          </span>
                        )}
                      </p>
                    )}
                  </div>

                  <KeyValueEnvEditor
                    label={t("agent.acp-config-custom-env")}
                    entries={customEnvEntries}
                    onChange={(next) => {
                      configRef.current = {
                        ...configRef.current,
                        customEnvEntries: next,
                      };
                      setCustomEnvEntries(next);
                    }}
                    onCommit={(next) => {
                      configRef.current = {
                        ...configRef.current,
                        customEnvEntries: next,
                      };
                      setCustomEnvEntries(next);
                      saveConfig();
                    }}
                  />

                  <StringListEditor
                    label={t("agent.acp-config-allow-env")}
                    placeholder={t("agent.acp-config-allow-env-placeholder")}
                    values={allowEnv}
                    onChange={(next) => {
                      configRef.current = {
                        ...configRef.current,
                        allowEnv: next,
                      };
                      setAllowEnv(next);
                    }}
                    onCommit={(next) => {
                      configRef.current = {
                        ...configRef.current,
                        allowEnv: next,
                      };
                      setAllowEnv(next);
                      saveConfig();
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
