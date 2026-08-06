import { Check, Loader2, Pencil, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import {
  Card,
  entryLabel,
  Field,
  modelLabel,
  piAPIProviderIds,
  providerDisplayName,
  providerLabel,
} from "@/components/profile-common";
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
import { ModelCombobox } from "@/components/ui/combobox";
import {
  Dialog,
  DialogClose,
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
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAvatarEditor } from "@/composables/useAvatarEditor";
import { settingServiceClient } from "@/connect";
import {
  deleteAgentAvatar,
  uploadAgentAvatar,
  useAvatar,
} from "@/lib/avatar-cache";
import { agentResourceName, formatTimestamp } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import type { AgentACPConfigInput } from "@/stores/types";
import {
  type Agent,
  type AgentACPConfig,
  type AgentProviderInfo,
  type PiModel,
} from "@/types/proto-es/v1/agent_pb";
import { type McpServer, McpServerScope } from "@/types/proto-es/v1/mcp_pb";
import { agentLifecycle, lifecycleLabel } from "./agents";

function McpServerCheckboxRow({
  server,
  enabled,
  canEdit,
  onToggle,
}: {
  server: McpServer;
  enabled: boolean;
  canEdit: boolean;
  onToggle: () => void;
}) {
  return (
    <label className="flex items-center justify-between gap-2 py-1.5 px-2 rounded-xs hover:bg-control-bg cursor-pointer">
      <span className="flex flex-col gap-0.5 min-w-0">
        <span className="text-sm truncate">{server.title}</span>
        <span className="text-xs text-control-placeholder truncate">
          {server.transport.value?.url ?? ""}
        </span>
      </span>
      <input
        type="checkbox"
        checked={enabled}
        disabled={!canEdit}
        onChange={onToggle}
        className="accent-accent"
      />
    </label>
  );
}

export function AgentProfilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const getMachine = useAppStore((s) => s.getMachine);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);
  // The ACP-config/avatar/persona editors hit admin-only RPCs (agents.edit), so
  // they are gated on canEditAdminOnly even when canEdit is true for the agent's
  // owner. The allow_add_to_channel toggle below is gated on canEdit.
  const canEditAdminOnly = useHasPermission("laelia.agents.edit");
  // Whether users may self-provide an inline api key (workspace toggle). When
  // enabled, non-admin owners can configure their own key on their agents; the
  // legacy inline fields are then shown to them (with a masked key preview).
  // Admins always see them.
  const [selfProvidedKeysEnabled, setSelfProvidedKeysEnabled] = useState(false);
  const showLegacyInline = canEditAdminOnly || selfProvidedKeysEnabled;

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
  const [apiProvider, setApiProvider] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [globalProvider, setGlobalProvider] = useState("");
  const [globalProviderEntry, setGlobalProviderEntry] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [personaDraft, setPersonaDraft] = useState("");
  const [personaEditing, setPersonaEditing] = useState(false);
  const [saveStatus, setSaveStatus] = useState<
    "idle" | "saving" | "saved" | "error"
  >("idle");
  // Dynamic model list for the builtin-pi runtime: fetched from the provider's
  // model API via the manager (ListPiModels), cached per api_provider so
  // toggling providers does not refetch.
  const [piModels, setPiModels] = useState<PiModel[]>([]);
  const [piModelsLoading, setPiModelsLoading] = useState(false);
  const [piModelsError, setPiModelsError] = useState("");
  const piModelsCacheRef = useRef<Map<string, PiModel[]>>(new Map());
  // Debounce timer for the "fetch models when the user stops typing the api
  // key" trigger. The model list is fetched only on explicit user actions
  // (api key change / Refresh button), NEVER on page entry — see fetchPiModels.
  const apiKeyFetchDebounceRef = useRef<
    ReturnType<typeof setTimeout> | undefined
  >(undefined);
  // Global API providers the caller may use (handler-gated server-side), for
  // the builtin-pi runtime's provider/entry pickers.
  const apiProviders = useAppStore((s) => s.apiProviders);
  // MCP servers the caller may use (handler-gated server-side), for the agent's
  // enabled-MCP picker.
  const mcpServers = useAppStore((s) => s.mcpServers);
  const fetchMcpServers = useAppStore((s) => s.fetchMcpServers);
  const [selectedMcpServers, setSelectedMcpServers] = useState<string[]>([]);
  const [mcpSaving, setMcpSaving] = useState(false);
  const workspaceMcpServers = mcpServers.filter(
    (s) => s.scope !== McpServerScope.USER
  );
  const myMcpServers = mcpServers.filter(
    (s) => s.scope === McpServerScope.USER
  );

  const configRef = useRef({
    executable: "",
    args: [] as string[],
    allowEnv: [] as string[],
    provider: "",
    model: "",
    apiProvider: "",
    apiKey: "",
    globalProvider: "",
    globalProviderEntry: "",
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

  const agentAvatarName = agent?.avatar || undefined;
  const avatarSrc = useAvatar(agentAvatarName);

  const {
    busy: avatarBusy,
    onChange: handleAvatarChange,
    onRemove: handleAvatarRemove,
  } = useAvatarEditor({
    avatarName: agentAvatarName ?? null,
    upload: (file) => uploadAgentAvatar(agentName, file),
    remove: (name) => deleteAgentAvatar(name),
    refetch: async () => {
      setAgent(await getAgent(agentName));
    },
    messages: {
      uploadSuccess: t("agent.profile.avatar-uploaded"),
      uploadFailure: t("agent.profile.avatar-upload-failed"),
      removeSuccess: t("agent.profile.avatar-removed"),
      removeFailure: t("agent.profile.avatar-remove-failed"),
    },
  });
  const [allowAddSaving, setAllowAddSaving] = useState(false);
  const [followOwnerSaving, setFollowOwnerSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Ownership transfer state. The flow is deliberately two-step: the first
  // dialog picks the target user (and optional audit reason), then a second
  // AlertDialog confirms the risky, unilateral, immediately-effective transfer
  // before it is sent.
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferTarget, setTransferTarget] = useState("");
  const [transferReason, setTransferReason] = useState("");
  const [transferConfirmOpen, setTransferConfirmOpen] = useState(false);
  const [transferBusy, setTransferBusy] = useState(false);
  const [transferError, setTransferError] = useState("");

  async function saveMcpServers() {
    if (!canEdit) return;
    setMcpSaving(true);
    try {
      const updateAgentMcpConfig = useAppStore.getState().updateAgentMcpConfig;
      await updateAgentMcpConfig(agentName, selectedMcpServers);
      setAgent(await getAgent(agentName));
      toastManager.add({
        type: "success",
        title: t("agent.mcp-saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.mcp-save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setMcpSaving(false);
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

  // Load the user roster (once) so the ownership transfer target picker and the
  // owner/creator display can resolve users/{id} → display title.
  useEffect(() => {
    if (users.length === 0) {
      void fetchUsers({ pageSize: 100 }, { silent: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

  // Read the workspace LLM config toggle that decides whether the legacy
  // self-provided-key fields are shown to non-admin owners.
  useEffect(() => {
    void settingServiceClient.getLlmAgentConfig({}).then((res) => {
      setSelfProvidedKeysEnabled(res.config?.allowUserSelfProvidedKeys ?? true);
    });
  }, []);

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
      apiProvider: cfg?.apiProvider ?? "",
      // Seed the key from the persisted config so an editor can see/keep it.
      // Non-editors get an empty key server-side (redacted), which is fine —
      // they cannot save anyway. On save, an empty key means "keep existing".
      apiKey: cfg?.apiKey ?? "",
      globalProvider: cfg?.globalProvider ?? "",
      globalProviderEntry: cfg?.globalProviderEntry ?? "",
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
    setApiProvider(next.apiProvider);
    setApiKey(next.apiKey);
    setGlobalProvider(next.globalProvider);
    setGlobalProviderEntry(next.globalProviderEntry);
    setCustomEnvEntries(next.customEnvEntries);
    setPersonaDraft(cfg?.personaPrompt ?? "");
    setPersonaEditing(false);
    setSaveStatus("idle");
    setSelectedMcpServers(agent?.mcpServers ? [...agent.mcpServers] : []);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent?.name]);

  // Load the MCP server roster the caller may use (once) for the enabled-MCP
  // picker.
  useEffect(() => {
    if (mcpServers.length === 0) {
      void fetchMcpServers({ pageSize: 100 }, { silent: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Clear any pending debounce when the editor unmounts. Lives before the
  // `if (!agent)` early return so the hook order is stable (Rules of Hooks).
  useEffect(() => {
    return () => {
      if (apiKeyFetchDebounceRef.current) {
        clearTimeout(apiKeyFetchDebounceRef.current);
      }
    };
  }, []);

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

  // canEdit is server-resolved per-agent (Agent.canEdit): true for the agent's
  // owner or a workspace admin. It gates the allow_add_to_channel toggle.
  // The ACP-config/avatar/persona editors hit admin-only RPCs (agents.edit), so
  // they are gated on canEditAdminOnly to avoid offering a 403 to owners.
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
      apiProvider: draft.apiProvider.trim(),
      // Empty apiKey on save means "keep the existing stored key" server-side.
      apiKey: draft.apiKey,
      globalProvider: draft.globalProvider.trim(),
      globalProviderEntry: draft.globalProviderEntry.trim(),
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
      apiProvider: cfg?.apiProvider ?? "",
      // Preserve the stored key on a persona-only save.
      apiKey: cfg?.apiKey ?? "",
      globalProvider: cfg?.globalProvider ?? "",
      globalProviderEntry: cfg?.globalProviderEntry ?? "",
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
  const isPiProvider = provider === "builtin-pi";
  // Global-provider selection for the builtin-pi runtime: the provider (one the
  // caller may use) and the entry (one (key, model) pair) the agent will use.
  // The model resolves from the entry server-side.
  const selectedGlobalProvider = apiProviders.find(
    (p) => p.name === globalProvider
  );
  const globalProviderEntries = selectedGlobalProvider?.entries ?? [];
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === provider
  );
  const modelOptions = selectedProviderInfo?.models ?? [];
  const providerSupportsModel =
    !!selectedProviderInfo?.supportsModelConfigOption;
  // fetchPiModels loads the model list for an API provider from the manager
  // (ListPiModels). deepseek requires the api_key; openrouter is public. Results
  // are cached per provider so toggling back does not refetch.
  async function fetchPiModels(nextProvider: string, key: string) {
    if (!nextProvider) return;
    if (nextProvider === "deepseek" && key.trim() === "") return;
    const cached = piModelsCacheRef.current.get(nextProvider);
    if (cached) {
      setPiModels(cached);
      setPiModelsError("");
      return;
    }
    setPiModelsLoading(true);
    setPiModelsError("");
    try {
      const listPiModels = useAppStore.getState().listPiModels;
      const models = await listPiModels(nextProvider, key);
      piModelsCacheRef.current.set(nextProvider, models);
      setPiModels(models);
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t("agent.acp-config-pi-models-refresh-failed");
      setPiModelsError(msg);
      toastManager.add({
        type: "error",
        title: t("agent.acp-config-pi-models-refresh-failed"),
        description: msg,
      });
    } finally {
      setPiModelsLoading(false);
    }
  }

  // A config is saveable once a provider (built-in, custom, or builtin-pi) is
  // chosen. For the custom path an executable is still required; for a built-in
  // provider the command is derived from the registry, so executable stays
  // empty. When the provider exposes model selection, a model must also be
  // chosen. For builtin-pi, an api provider + model are required; the api key
  // is optional on save (empty means keep the existing stored key).
  function canSaveFor(draft: typeof configRef.current): boolean {
    if (draft.provider === "custom") return draft.executable.trim() !== "";
    if (draft.provider === "builtin-pi") {
      return draft.apiProvider.trim() !== "" && draft.model.trim() !== "";
    }
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
    if (!canEditAdminOnly) return;
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
    if (!canEditAdminOnly) return;
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

  // Toggle allow_add_to_channel via UpdateAgent, then refetch the agent and the
  // roster so the member picker (which filters on this flag) reflects it.
  async function handleToggleAllowAdd(next: boolean) {
    setAllowAddSaving(true);
    try {
      const updateAgent = useAppStore.getState().updateAgent;
      await updateAgent(agentName, { allowAddToChannel: next });
      setAgent(await getAgent(agentName));
      fetchAgents({ pageSize: 100 }, { silent: true });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.allow-add-to-channel-save-error"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setAllowAddSaving(false);
    }
  }

  // Toggle follow_owner_permissions via UpdateAgent, then refetch the agent so
  // the access model shown reflects the new setting.
  async function handleToggleFollowOwner(next: boolean) {
    setFollowOwnerSaving(true);
    try {
      const updateAgent = useAppStore.getState().updateAgent;
      await updateAgent(agentName, { followOwnerPermissions: next });
      setAgent(await getAgent(agentName));
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.follow-owner-permissions-save-error"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setFollowOwnerSaving(false);
    }
  }

  // userTitle resolves a user resource name (users/{id}) to the roster's display
  // title, falling back to the raw name so a stale/deleted user never renders
  // empty. Used for the owner/creator display rows.
  function userTitle(name: string): string {
    if (!name) return "";
    return users.find((u) => u.name === name)?.title || name;
  }

  // Transfer flow: first dialog picks the target + reason, then the second
  // AlertDialog confirms. On confirm, TransferAgentOwnership reassigns the owner
  // immediately and unilaterally; the profile and roster are refetched so the
  // new owner's authority (and the old owner's loss of it) reflects at once.
  function openTransferPicker() {
    setTransferTarget("");
    setTransferReason("");
    setTransferError("");
    setTransferOpen(true);
  }

  async function handleTransfer() {
    if (!agentName || !transferTarget) return;
    setTransferBusy(true);
    setTransferError("");
    try {
      const transferAgentOwnership =
        useAppStore.getState().transferAgentOwnership;
      await transferAgentOwnership(agentName, transferTarget, transferReason);
      setTransferConfirmOpen(false);
      setTransferOpen(false);
      setAgent(await getAgent(agentName));
      fetchAgents({ pageSize: 100 }, { silent: true });
      toastManager.add({
        type: "success",
        title: t("agent.transfer-owner-success"),
      });
    } catch (err) {
      setTransferError(err instanceof Error ? err.message : String(err));
      toastManager.add({
        type: "error",
        title: t("agent.transfer-owner-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setTransferBusy(false);
    }
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
                {agent.owner && (
                  <Field label={t("agent.detail-owner")}>
                    <span className="flex items-center gap-2">
                      {userTitle(agent.ownerName || agent.owner)}
                      {canEdit && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={openTransferPicker}
                        >
                          {t("agent.transfer-owner")}
                        </Button>
                      )}
                    </span>
                  </Field>
                )}
                {agent.createdBy && (
                  <Field label={t("agent.detail-created-by")}>
                    {userTitle(agent.createdBy)}
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
                      disabled={!canEditAdminOnly || avatarBusy}
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
                        disabled={!canEditAdminOnly || avatarBusy}
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

          {/* Channel access */}
          <div>
            <Card title={t("agent.profile.section-add-to-channel")}>
              <FieldRow
                label={t("agent.allow-add-to-channel")}
                hint={t("agent.allow-add-to-channel-hint")}
              >
                <Switch
                  checked={agent.allowAddToChannel ?? false}
                  disabled={!canEdit || allowAddSaving}
                  onCheckedChange={(next) => {
                    void handleToggleAllowAdd(next);
                  }}
                />
              </FieldRow>
              <FieldRow
                label={t("agent.follow-owner-permissions")}
                hint={t("agent.follow-owner-permissions-hint")}
              >
                <Switch
                  checked={agent.followOwnerPermissions ?? true}
                  disabled={!canEdit || followOwnerSaving}
                  onCheckedChange={(next) => {
                    void handleToggleFollowOwner(next);
                  }}
                />
              </FieldRow>
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
              <fieldset
                disabled={!canEditAdminOnly && !canEdit}
                className="contents"
              >
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium">
                      {t("agent.acp-config-provider")}
                    </label>
                    {availableProviders.length === 0 && !canEditAdminOnly ? (
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
                          // Reset model + pi fields when the provider changes —
                          // the previous values belong to the old runtime.
                          configRef.current = {
                            ...configRef.current,
                            provider: next,
                            model: "",
                            apiProvider: "",
                          };
                          setProvider(next);
                          setModel("");
                          setApiProvider("");
                          saveConfig();
                        }}
                      >
                        <SelectTrigger>
                          <SelectValue>
                            {(v: string | null) =>
                              v
                                ? v === "builtin-pi"
                                  ? t("agent.acp-config-provider-builtin-pi")
                                  : providerLabel(v, availableProviders)
                                : ""
                            }
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent>
                          {/* builtin-pi is always available — it is bundled with
                            laelia, not host-detected — so it shows on every
                            agent regardless of the machine's probe results. */}
                          <SelectItem value="builtin-pi">
                            {t("agent.acp-config-provider-builtin-pi")}
                          </SelectItem>
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

                  {isPiProvider && (
                    <>
                      <div className="flex flex-col gap-1">
                        <label className="text-sm font-medium">
                          {t("agent.acp-config-pi-global-provider")}
                        </label>
                        <Select
                          value={globalProvider}
                          onValueChange={(v) => {
                            const next = String(v ?? "");
                            configRef.current = {
                              ...configRef.current,
                              globalProvider: next,
                              globalProviderEntry: "",
                            };
                            setGlobalProvider(next);
                            setGlobalProviderEntry("");
                            saveConfig();
                          }}
                        >
                          <SelectTrigger>
                            <SelectValue>
                              {(v: string | null) =>
                                v
                                  ? (apiProviders.find((p) => p.name === v)
                                      ?.title ?? v)
                                  : ""
                              }
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            {apiProviders.length === 0 && (
                              <SelectItem value="__no_provider" disabled>
                                {t(
                                  "agent.acp-config-pi-global-providers-empty"
                                )}
                              </SelectItem>
                            )}
                            {apiProviders.map((p) => (
                              <SelectItem key={p.name} value={p.name}>
                                {p.title}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>

                      {globalProvider &&
                        (globalProviderEntries.length > 0 ? (
                          <div className="flex flex-col gap-1">
                            <label className="text-sm font-medium">
                              {t("agent.acp-config-pi-global-entry")}
                            </label>
                            <Select
                              value={globalProviderEntry}
                              onValueChange={(v) => {
                                const next = String(v ?? "");
                                configRef.current = {
                                  ...configRef.current,
                                  globalProviderEntry: next,
                                };
                                setGlobalProviderEntry(next);
                                saveConfig();
                              }}
                            >
                              <SelectTrigger>
                                <SelectValue>
                                  {(v: string | null) =>
                                    v
                                      ? entryLabel(
                                          globalProviderEntries.find(
                                            (e) => e.name === v
                                          )
                                        )
                                      : ""
                                  }
                                </SelectValue>
                              </SelectTrigger>
                              <SelectContent>
                                {globalProviderEntries.map((e) => (
                                  <SelectItem key={e.name} value={e.name}>
                                    {entryLabel(e)}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <p className="text-xs text-control-light">
                              {t("agent.acp-config-pi-global-entry-hint")}
                            </p>
                          </div>
                        ) : (
                          <p className="text-xs text-control-light">
                            {t("agent.acp-config-pi-global-entries-empty")}
                          </p>
                        ))}

                      {showLegacyInline && (
                        <>
                          <div className="flex flex-col gap-1">
                            <label className="text-sm font-medium">
                              {t("agent.acp-config-pi-api-provider")}
                            </label>
                            <Select
                              value={apiProvider}
                              onValueChange={(v) => {
                                const next = String(v ?? "");
                                // Reset model when the API provider changes — the
                                // previous model belongs to the old provider's set.
                                // Clear the cached model list too (it is per
                                // provider) and cancel a pending key-change fetch;
                                // the user clicks Refresh to load the new list.
                                configRef.current = {
                                  ...configRef.current,
                                  apiProvider: next,
                                  model: "",
                                };
                                if (apiKeyFetchDebounceRef.current) {
                                  clearTimeout(apiKeyFetchDebounceRef.current);
                                  apiKeyFetchDebounceRef.current = undefined;
                                }
                                setApiProvider(next);
                                setModel("");
                                setPiModels([]);
                                setPiModelsError("");
                                saveConfig();
                              }}
                            >
                              <SelectTrigger>
                                <SelectValue>
                                  {(v: string | null) => v ?? ""}
                                </SelectValue>
                              </SelectTrigger>
                              <SelectContent>
                                {piAPIProviderIds.map((id) => (
                                  <SelectItem key={id} value={id}>
                                    {id}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>

                          <div className="flex flex-col gap-1">
                            <label className="text-sm font-medium">
                              {t("agent.acp-config-model")}
                            </label>
                            <div className="flex items-center gap-2">
                              <ModelCombobox
                                className="flex-1"
                                value={model}
                                options={piModels}
                                loading={piModelsLoading}
                                placeholder={t(
                                  "agent.acp-config-pi-model-placeholder"
                                )}
                                disabled={!apiProvider}
                                emptyLabel={t(
                                  "agent.acp-config-pi-models-empty"
                                )}
                                onValueChange={(next) => {
                                  configRef.current = {
                                    ...configRef.current,
                                    model: next,
                                  };
                                  setModel(next);
                                }}
                              />
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                disabled={
                                  !apiProvider ||
                                  piModelsLoading ||
                                  (apiProvider === "deepseek" &&
                                    apiKey.trim() === "")
                                }
                                onClick={() => {
                                  // Force a refetch: drop the cache entry first
                                  // and cancel any pending key-change debounce.
                                  if (apiKeyFetchDebounceRef.current) {
                                    clearTimeout(
                                      apiKeyFetchDebounceRef.current
                                    );
                                    apiKeyFetchDebounceRef.current = undefined;
                                  }
                                  if (apiProvider) {
                                    piModelsCacheRef.current.delete(
                                      apiProvider
                                    );
                                  }
                                  void fetchPiModels(apiProvider, apiKey);
                                }}
                              >
                                {piModelsLoading ? (
                                  <Loader2 className="size-3.5 animate-spin" />
                                ) : (
                                  t("agent.acp-config-pi-models-refresh")
                                )}
                              </Button>
                            </div>
                            {piModelsError && (
                              <p className="text-xs text-danger">
                                {piModelsError}
                              </p>
                            )}
                          </div>

                          <div className="flex flex-col gap-1">
                            <label className="text-sm font-medium">
                              {t("agent.acp-config-pi-api-key")}
                            </label>
                            <Input
                              type="password"
                              // An LLM API key is not a login password: stop password
                              // managers from autofilling a generated password here
                              // (which would silently overwrite the real key on save).
                              autoComplete="off"
                              data-1p-ignore
                              data-lpignore="true"
                              placeholder={t(
                                "agent.acp-config-pi-api-key-placeholder"
                              )}
                              value={apiKey}
                              onChange={(e) => {
                                const next = e.target.value;
                                configRef.current = {
                                  ...configRef.current,
                                  apiKey: next,
                                };
                                setApiKey(next);
                                // Fetch the model list once the user stops typing
                                // the key (debounced) — this is the "user changed
                                // the api key" trigger. deepseek needs the key;
                                // fetchPiModels no-ops for deepseek + empty key.
                                if (apiKeyFetchDebounceRef.current) {
                                  clearTimeout(apiKeyFetchDebounceRef.current);
                                }
                                apiKeyFetchDebounceRef.current = setTimeout(
                                  () => {
                                    void fetchPiModels(apiProvider, next);
                                  },
                                  600
                                );
                              }}
                              onBlur={() => {
                                // Leaving the field: persist the key, and fetch
                                // immediately rather than waiting on the debounce.
                                if (apiKeyFetchDebounceRef.current) {
                                  clearTimeout(apiKeyFetchDebounceRef.current);
                                  apiKeyFetchDebounceRef.current = undefined;
                                }
                                saveConfig();
                                void fetchPiModels(apiProvider, apiKey);
                              }}
                            />
                            <p className="text-xs text-control-light">
                              {t("agent.acp-config-pi-api-key-hint")}
                            </p>
                          </div>
                        </>
                      )}
                    </>
                  )}

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

                  {isCustomProvider && !isPiProvider && (
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

                  {selectedProviderInfo &&
                    !isCustomProvider &&
                    !isPiProvider && (
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
                          disabled={!canEditAdminOnly}
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
                            disabled={!canEditAdminOnly}
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

                  {!isPiProvider && (
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
                  )}

                  {!isPiProvider && (
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
                  )}
                </div>
              </fieldset>
            </Card>
          </div>

          {/* Managed MCP servers */}
          <div>
            <Card
              title={t("agent.mcp-section-title")}
              actions={
                mcpSaving ? (
                  <span className="flex items-center gap-1 text-xs text-control-light">
                    <Loader2 className="size-3 animate-spin" />
                    {t("agent.mcp-saving")}
                  </span>
                ) : null
              }
            >
              <div className="flex flex-col gap-3">
                <p className="text-xs text-control-light">
                  {t("agent.mcp-section-hint")}
                </p>
                {mcpServers.length === 0 ? (
                  <p className="text-xs text-control-placeholder">
                    {t("agent.mcp-empty")}
                  </p>
                ) : (
                  <div className="flex flex-col gap-3">
                    {workspaceMcpServers.length > 0 && (
                      <div className="flex flex-col gap-1.5">
                        <div className="text-xs font-medium text-main">
                          {t("agent.mcp-workspace-section")}
                        </div>
                        {workspaceMcpServers.map((server) => {
                          const enabled = selectedMcpServers.includes(
                            server.name
                          );
                          return (
                            <McpServerCheckboxRow
                              key={server.name}
                              server={server}
                              enabled={enabled}
                              canEdit={canEdit}
                              onToggle={() =>
                                setSelectedMcpServers((prev) =>
                                  enabled
                                    ? prev.filter((n) => n !== server.name)
                                    : [...prev, server.name]
                                )
                              }
                            />
                          );
                        })}
                      </div>
                    )}
                    {myMcpServers.length > 0 && (
                      <div className="flex flex-col gap-1.5">
                        <div className="text-xs font-medium text-main">
                          {t("agent.mcp-my-section")}
                        </div>
                        {myMcpServers.map((server) => {
                          const enabled = selectedMcpServers.includes(
                            server.name
                          );
                          return (
                            <McpServerCheckboxRow
                              key={server.name}
                              server={server}
                              enabled={enabled}
                              canEdit={canEdit}
                              onToggle={() =>
                                setSelectedMcpServers((prev) =>
                                  enabled
                                    ? prev.filter((n) => n !== server.name)
                                    : [...prev, server.name]
                                )
                              }
                            />
                          );
                        })}
                      </div>
                    )}
                  </div>
                )}
                {canEdit && mcpServers.length > 0 && (
                  <div className="flex justify-end">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={mcpSaving}
                      onClick={() => void saveMcpServers()}
                    >
                      {t("agent.mcp-save")}
                    </Button>
                  </div>
                )}
              </div>
            </Card>
          </div>
        </div>
      </div>

      {/* Ownership transfer: pick target + reason, then a second risky-action
          confirm. The transfer is unilateral and effective immediately. */}
      <Dialog
        open={transferOpen}
        onOpenChange={(next) => !next && setTransferOpen(false)}
      >
        <DialogContent>
          <DialogTitle>{t("agent.transfer-owner-title")}</DialogTitle>
          <DialogDescription>
            {t("agent.transfer-owner-description")}
          </DialogDescription>
          <div className="mt-4 flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {t("agent.transfer-owner-target")}
              </label>
              <Select
                value={transferTarget}
                onValueChange={(v) => v && setTransferTarget(v)}
              >
                <SelectTrigger>
                  <SelectValue
                    placeholder={t("agent.transfer-owner-target-placeholder")}
                  >
                    {(v: string | null) => (v ? userTitle(v) : "")}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {users
                    .filter((u) => u.name !== agent.owner)
                    .map((u) => (
                      <SelectItem key={u.name} value={u.name}>
                        {u.title}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {t("agent.transfer-owner-reason")}
              </label>
              <Input
                value={transferReason}
                onChange={(e) => setTransferReason(e.target.value)}
                placeholder={t("agent.transfer-owner-reason-placeholder")}
              />
            </div>
            {transferError && (
              <Alert variant="error" description={transferError} />
            )}
          </div>
          <div className="mt-6 flex justify-end gap-2">
            <DialogClose>
              <Button variant="outline">{t("common.cancel")}</Button>
            </DialogClose>
            <Button
              disabled={!transferTarget}
              onClick={() => {
                setTransferError("");
                setTransferOpen(false);
                setTransferConfirmOpen(true);
              }}
            >
              {t("common.next")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={transferConfirmOpen}
        onOpenChange={(next) => !next && setTransferConfirmOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.transfer-owner-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.transfer-owner-confirm-description", {
              target: userTitle(transferTarget),
            })}
          </AlertDialogDescription>
          {transferError && (
            <Alert variant="error" description={transferError} />
          )}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={transferBusy}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={transferBusy}
              onClick={() => void handleTransfer()}
            >
              {transferBusy
                ? t("common.saving")
                : t("agent.transfer-owner-confirm")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
