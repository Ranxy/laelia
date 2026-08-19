import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  Check,
  Copy,
  Loader2,
  Plus,
  Shield,
  User as UserIcon,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { KeyValueEnvEditor } from "@/components/agent/key-value-env-editor";
import { StringListEditor } from "@/components/agent/string-list-editor";
import { ConnectionBadge } from "@/components/connection-badge";
import { MachineConnectionBadge } from "@/components/machine-connection-badge";
import { MemberPicker } from "@/components/member-picker";
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
import { Badge } from "@/components/ui/badge";
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
import { SecretInput } from "@/components/ui/secret-input";
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
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  groupServiceClient,
  iamServiceClient,
  settingServiceClient,
} from "@/connect";
import { formatTimestamp } from "@/lib/command-status";
import {
  buildMachineInstallCommand,
  buildMachineSetupCommand,
  machineInstallOSFromInfo,
} from "@/lib/machine-token";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import {
  type AgentProviderInfo,
  type AgentSummary,
  type PiModel,
} from "@/types/proto-es/v1/agent_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import {
  type Machine,
  MachineStatus_ConnectionState,
} from "@/types/proto-es/v1/machine_pb";

// AGENT_CREATOR_ROLE is the machine-scope IAM role bound on a machine's IAM
// policy to grant creating agents on that machine. Only this role's bindings
// are surfaced on the machine profile's Access card.
const AGENT_CREATOR_ROLE = "roles/machineAgentCreator";

export function MachineProfilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId } = useParams<{ machineId: string }>();
  const getMachine = useAppStore((s) => s.getMachine);
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const isDesktop = useIsDesktop();

  const machineName = `machines/${machineId ?? ""}`;

  const [machine, setMachine] = useState<Machine | undefined>(undefined);
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [agentsLoading, setAgentsLoading] = useState(false);
  // loadError distinguishes a failed/missing fetch from an in-progress load so
  // the profile does not strand the user on a perpetual "Loading…" screen.
  const [loadError, setLoadError] = useState(false);

  // Token / control action state.
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [forceOpen, setForceOpen] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [forcing, setForcing] = useState(false);
  const [actionError, setActionError] = useState("");
  const [installCopied, setInstallCopied] = useState(false);
  const [setupCopied, setSetupCopied] = useState(false);

  // Ownership transfer state. The flow is deliberately two-step: the first
  // dialog picks the target + reason, the second AlertDialog confirms the
  // risky, unilateral, immediately-effective transfer.
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferTarget, setTransferTarget] = useState("");
  const [transferReason, setTransferReason] = useState("");
  const [transferConfirmOpen, setTransferConfirmOpen] = useState(false);
  const [transferBusy, setTransferBusy] = useState(false);
  const [transferError, setTransferError] = useState("");

  // Provider refresh state.
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");

  // Self-upgrade state: the trigger is local, the progress comes from
  // machine.upgradeStatus refreshed by polling while an upgrade runs.
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeError, setUpgradeError] = useState("");

  // Add-agent sheet state. The sheet carries the full ACP config (provider,
  // model, persona, env, custom command) so an agent can be fully configured at
  // creation time instead of requiring a second visit to the agent profile.
  const [addOpen, setAddOpen] = useState(false);
  const [listScrolled, setListScrolled] = useState(false);
  const [agentName, setAgentName] = useState("");
  const [agentDescription, setAgentDescription] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [globalProvider, setGlobalProvider] = useState("");
  const [globalProviderEntry, setGlobalProviderEntry] = useState("");
  // Self-provided-key mode for the builtin-pi runtime: whether the sheet offers
  // "use my own API key" (api_provider + key + model) in addition to the managed
  // global providers, and the inline fields when that mode is active.
  const [piMode, setPiMode] = useState<"global" | "self">("global");
  const [selfProvidedKeysEnabled, setSelfProvidedKeysEnabled] = useState(false);
  const [apiProvider, setApiProvider] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [apiBaseUrl, setApiBaseUrl] = useState("");
  const [piModels, setPiModels] = useState<PiModel[]>([]);
  const [piModelsLoading, setPiModelsLoading] = useState(false);
  const piModelsCacheRef = useRef<Map<string, PiModel[]>>(new Map());
  const [personaPrompt, setPersonaPrompt] = useState("");
  const [customEnvEntries, setCustomEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [allowEnv, setAllowEnv] = useState<string[]>([]);
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState<string[]>([]);
  const [allowAddToChannel, setAllowAddToChannel] = useState(false);
  const [adding, setAdding] = useState(false);
  const [addError, setAddError] = useState("");
  const [addedOpen, setAddedOpen] = useState(false);
  const [addedTitle, setAddedTitle] = useState("");
  // Global API providers the caller may use, for the builtin-pi runtime's
  // provider + entry pickers. Handler-gated server-side: non-admins see only
  // the providers they may use.
  const apiProviders = useAppStore((s) => s.apiProviders);

  // Access (IAM) state: who may create agents on this machine. The policy is
  // loaded only for callers who may manage it (machine.canManage).
  const [policyState, setPolicyState] = useState<{
    policy: IamPolicy;
    etag: string;
  } | null>(null);
  const [accessOpen, setAccessOpen] = useState(false);
  const [accessMembers, setAccessMembers] = useState<Set<string>>(new Set());
  const [accessSaving, setAccessSaving] = useState(false);
  const [accessError, setAccessError] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);

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

  // Load the caller's accessible global API providers once per page view; the
  // store slice caches them for the sheet's provider/entry dropdowns. Also read
  // the workspace toggle that decides whether the self-provided-key mode is
  // offered in the sheet.
  useEffect(() => {
    void useAppStore.getState().fetchApiProviders(undefined, { silent: true });
    void settingServiceClient
      .getSetting({ name: "settings/llm_agent_config" })
      .then((res) => {
        const v = res.value?.value;
        setSelfProvidedKeysEnabled(
          v?.case === "llmAgentConfig"
            ? v.value.allowUserSelfProvidedKeys
            : true
        );
      });
  }, []);

  // fetchPiModels loads the model list for a self-provided LLM API provider
  // (ListPiModels). deepseek requires the api_key; openrouter is public.
  async function fetchPiModels(
    nextProvider: string,
    key: string,
    baseUrl = ""
  ) {
    if (!nextProvider) return;
    if (nextProvider === "deepseek" && key.trim() === "") return;
    if (nextProvider === "custom" && baseUrl.trim() === "") return;
    const cacheKey = `${nextProvider}/${baseUrl}`;
    const cached = piModelsCacheRef.current.get(cacheKey);
    if (cached) {
      setPiModels(cached);
      return;
    }
    setPiModelsLoading(true);
    setAddError("");
    try {
      const models = await useAppStore
        .getState()
        .listPiModels(nextProvider, key, baseUrl);
      piModelsCacheRef.current.set(cacheKey, models);
      setPiModels(models);
    } catch (err) {
      setAddError(
        err instanceof Error
          ? err.message
          : t("agent.acp-config-pi-models-refresh-failed")
      );
    } finally {
      setPiModelsLoading(false);
    }
  }

  // Debounced: fetch the model list once the user stops typing in the
  // self-provided mode (deepseek needs the key before the list is useful).
  useEffect(() => {
    if (provider !== "builtin-pi" || piMode !== "self" || !apiProvider) return;
    if (apiProvider === "deepseek" && apiKey.trim() === "") return;
    if (apiProvider === "custom" && apiBaseUrl.trim() === "") return;
    const timer = setTimeout(
      () => void fetchPiModels(apiProvider, apiKey, apiBaseUrl),
      400
    );
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [provider, piMode, apiProvider, apiKey, apiBaseUrl]);

  // Access (who may create agents) logic. The machine IAM policy is
  // handler-gated server-side to the machine's creator or a workspace admin,
  // matching machine.canManage.
  async function loadPolicy() {
    try {
      const res = await iamServiceClient.getMachineIamPolicy({
        name: machineName,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      setAccessError("");
    } catch (err) {
      setAccessError(err instanceof Error ? err.message : String(err));
    }
  }

  useEffect(() => {
    if (!machineId) return;
    fetchUsers({ pageSize: 1000 });
    if (!machine?.canManage) return;
    void loadPolicy();
    void groupServiceClient
      .listGroups({ pageSize: 1000 })
      .then((res) => setGroups(res.groups ?? []));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machineId, machineName, machine?.canManage, fetchUsers]);

  // agentCreatorMembers are the principals bound to the machineAgentCreator
  // role on this machine's IAM policy.
  const agentCreatorMembers = useMemo(() => {
    const binding = policyState?.policy.bindings.find(
      (b) => b.role === AGENT_CREATOR_ROLE
    );
    return binding?.members ?? [];
  }, [policyState]);

  // The active upgrade stage reported by the machine, polled from
  // machine.upgradeStatus while a triggered upgrade is in flight.
  const upgradeStage = machine?.upgradeStatus?.stage ?? "";
  const upgradeInProgress = [
    "requested",
    "downloading",
    "installing",
    "restarting",
  ].includes(upgradeStage);

  // Poll the machine while an upgrade runs: the machine briefly goes offline
  // and reconnects on the new version, so the page keeps refetching until the
  // stage reaches a terminal value or the reported version catches up.
  useEffect(() => {
    if (!upgradeInProgress) return;
    const id = setInterval(() => {
      void (async () => {
        const next = await getMachine(machineName);
        if (next) setMachine(next);
      })();
    }, 3000);
    return () => clearInterval(id);
  }, [upgradeInProgress, getMachine, machineName]);

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
  const canCreateAgent = machine.canCreateAgent;
  const canManage = machine.canManage;
  // hasAnyAction suppresses the "not allowed" notice for users who hold at least
  // one capability on this machine (e.g. a granted agent creator).
  const hasAnyAction = canEdit || canCreateAgent || canManage;
  const info = machine.info;
  const availableProviders: AgentProviderInfo[] =
    info?.availableProviders ?? [];

  // Offline reconnection commands. They mirror the new-machine page: the
  // install command depends on the machine's reported OS, while the setup
  // command is the same everywhere.
  const installOS = machineInstallOSFromInfo(info?.os);
  const installCommand = installOS ? buildMachineInstallCommand(installOS) : "";
  const setupCommand = buildMachineSetupCommand();
  const isOffline =
    machine.status?.state === MachineStatus_ConnectionState.OFFLINE;

  // Add-agent form derived state. Provider is required; model is required only
  // when the selected provider exposes a model config option with advertised
  // models (a provider that does not expose model selection via the protocol
  // does not require a model). The "custom" provider hand-types a command and
  // never exposes model selection, so it requires an executable instead.
  const isCustomProvider = provider === "custom";
  const isPiProvider = provider === "builtin-pi";
  const selectedProviderInfo = availableProviders.find(
    (p) => p.providerId === provider
  );
  const modelOptions = selectedProviderInfo?.models ?? [];
  const modelRequired =
    !!selectedProviderInfo?.supportsModelConfigOption &&
    modelOptions.length > 0;
  // The global-provider selection for the builtin-pi runtime: the provider
  // (one the caller may use) and the entry (one (key, model) pair) the agent
  // will use. The model resolves from the entry server-side.
  const selectedGlobalProvider = apiProviders.find(
    (p) => p.name === globalProvider
  );
  const globalProviderEntries = selectedGlobalProvider?.entries ?? [];
  // resetAddForm clears the create-agent sheet inputs so reopening it starts
  // from a blank state instead of the previous submission's values.
  function resetAddForm() {
    setAgentName("");
    setAgentDescription("");
    setProvider("");
    setModel("");
    setGlobalProvider("");
    setGlobalProviderEntry("");
    setPiMode("global");
    setApiProvider("");
    setApiKey("");
    setPiModels([]);
    setPersonaPrompt("");
    setCustomEnvEntries([]);
    setAllowEnv([]);
    setExecutable("");
    setArgs([]);
    setAllowAddToChannel(false);
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

  async function handleUpgrade() {
    setUpgrading(true);
    setUpgradeError("");
    try {
      await useAppStore.getState().upgradeMachine(machineName);
      // Refresh immediately so the "requested" status shows, then the poll
      // effect above takes over.
      const next = await getMachine(machineName);
      if (next) setMachine(next);
    } catch (err) {
      setUpgradeError(
        err instanceof Error ? err.message : t("machine.upgrade-failed")
      );
    } finally {
      setUpgrading(false);
    }
  }

  async function handleCopyInstall() {
    if (!installCommand) return;
    try {
      await navigator.clipboard.writeText(installCommand);
      setInstallCopied(true);
      setTimeout(() => setInstallCopied(false), 2000);
    } catch {
      // Clipboard unavailable; the command is visible for manual copy.
    }
  }

  async function handleCopySetup() {
    try {
      await navigator.clipboard.writeText(setupCommand);
      setSetupCopied(true);
      setTimeout(() => setSetupCopied(false), 2000);
    } catch {
      // Clipboard unavailable; the command is visible for manual copy.
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

  // Transfer flow: first dialog picks the target + reason, then the second
  // AlertDialog confirms. On confirm, TransferMachineOwnership reassigns the
  // owner immediately and unilaterally; the profile is refetched so the new
  // owner's authority (and the old owner's loss of it) reflects at once.
  function openTransferPicker() {
    setTransferTarget("");
    setTransferReason("");
    setTransferError("");
    setTransferOpen(true);
  }

  async function handleTransfer() {
    if (!machineName || !transferTarget) return;
    setTransferBusy(true);
    setTransferError("");
    try {
      const transferMachineOwnership =
        useAppStore.getState().transferMachineOwnership;
      await transferMachineOwnership(
        machineName,
        transferTarget,
        transferReason
      );
      setTransferConfirmOpen(false);
      setTransferOpen(false);
      await reload();
    } catch (err) {
      setTransferError(err instanceof Error ? err.message : String(err));
    } finally {
      setTransferBusy(false);
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
    if (isPiProvider) {
      if (piMode === "global") {
        if (!globalProvider.trim()) {
          setAddError(t("machine.add-agent-provider-required"));
          return;
        }
        if (!globalProviderEntry.trim()) {
          setAddError(t("machine.add-agent-global-entry-required"));
          return;
        }
      } else {
        if (!apiProvider.trim()) {
          setAddError(t("machine.add-agent-provider-required"));
          return;
        }
        if (!model.trim()) {
          setAddError(t("machine.add-agent-model-required"));
          return;
        }
        if (!apiKey.trim()) {
          setAddError(t("machine.add-agent-api-key-required"));
          return;
        }
        if (apiProvider === "custom" && !apiBaseUrl.trim()) {
          setAddError(t("machine.add-agent-api-base-url-required"));
          return;
        }
      }
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
      await createAgent(
        name,
        machineName,
        {
          executable: executable.trim(),
          args: args.map((a) => a.trim()).filter((a) => a !== ""),
          allowEnv: allowEnv.map((e) => e.trim()).filter((e) => e !== ""),
          provider: provider.trim(),
          model: model.trim(),
          protocol: "",
          personaPrompt: personaPrompt.trim(),
          customEnv,
          globalProvider: globalProvider.trim(),
          globalProviderEntry: globalProviderEntry.trim(),
          apiProvider: apiProvider.trim(),
          apiKey: apiKey.trim(),
          apiBaseUrl: apiBaseUrl.trim(),
        },
        undefined,
        allowAddToChannel,
        agentDescription.trim()
      );
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

  // userTitle resolves a user resource name (users/{id}) to the roster's
  // display title, falling back to the raw name so a stale/deleted user never
  // renders empty.
  function userTitle(name: string): string {
    if (!name) return "";
    return users.find((u) => u.name === name)?.title || name;
  }

  function memberLabel(member: string): string {
    if (member === "allUsers") return t("machine.access-member-all-users");
    if (member.startsWith("users/")) {
      const u = users.find((u) => u.name === member);
      return u ? u.title || u.email || member : member;
    }
    if (member.startsWith("groups/")) {
      const g = groups.find(
        (grp) =>
          grp.name === member ||
          (grp.email ? `groups/${grp.email}` === member : false)
      );
      return g ? g.title || g.email || member : member.slice("groups/".length);
    }
    return member;
  }

  function openAccess() {
    setAccessMembers(new Set(agentCreatorMembers));
    setAccessError("");
    setAccessOpen(true);
  }

  function handleAccessAdd(member: string) {
    if (!member || accessMembers.has(member)) return;
    setAccessMembers((prev) => new Set(prev).add(member));
  }

  function handleAccessRemove(member: string) {
    setAccessMembers((prev) => {
      const next = new Set(prev);
      next.delete(member);
      return next;
    });
  }

  // handleSaveAccess replaces only the machineAgentCreator binding (members set
  // to the sheet's selection), leaving any other bindings untouched, and writes
  // it back etag-guarded.
  async function handleSaveAccess() {
    if (!policyState) return;
    setAccessSaving(true);
    setAccessError("");
    try {
      const bindings: Binding[] = [];
      let found = false;
      for (const b of policyState.policy.bindings) {
        if (b.role === AGENT_CREATOR_ROLE) {
          found = true;
          if (accessMembers.size > 0) {
            bindings.push(
              create(BindingSchema, {
                role: AGENT_CREATOR_ROLE,
                members: [...accessMembers],
              })
            );
          }
        } else {
          bindings.push(
            create(BindingSchema, { role: b.role, members: [...b.members] })
          );
        }
      }
      if (!found && accessMembers.size > 0) {
        bindings.push(
          create(BindingSchema, {
            role: AGENT_CREATOR_ROLE,
            members: [...accessMembers],
          })
        );
      }
      const policy = create(IamPolicySchema, { bindings });
      const res = await iamServiceClient.setMachineIamPolicy({
        name: machineName,
        policy,
        etag: policyState.etag,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      setAccessOpen(false);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        // Etag mismatch: another writer changed the policy. Reload the latest
        // state so the admin can review and retry.
        setAccessError(t("machine.access-etag-mismatch"));
        await loadPolicy();
      } else {
        setAccessError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setAccessSaving(false);
    }
  }

  return (
    <div
      className="h-full overflow-y-auto p-6"
      onScroll={(e) => setListScrolled(e.currentTarget.scrollTop > 8)}
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
        {!hasAnyAction && (
          <Alert
            variant="info"
            description={t("machine.profile.edit-not-allowed")}
          />
        )}

        {machine.upgradeAvailable && !upgradeInProgress && (
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <Alert
              variant="warning"
              title={t("machine.upgrade-available-title")}
              description={t("machine.upgrade-available-description", {
                current: info?.version ?? "-",
                latest: machine.latestVersion,
              })}
            />
            {canManage && (
              <Button
                onClick={() => void handleUpgrade()}
                disabled={upgrading}
                className="shrink-0"
              >
                {upgrading ? <Loader2 className="size-4 animate-spin" /> : null}
                {t("machine.upgrade-cta")}
              </Button>
            )}
          </div>
        )}

        {upgradeInProgress && (
          <Alert
            variant="info"
            title={t("machine.upgrade-in-progress-title")}
            description={t("machine.upgrade-stage", {
              stage: upgradeStage,
              version: machine.upgradeStatus?.version ?? "",
            })}
          />
        )}

        {upgradeStage === "failed" && (
          <Alert
            variant="error"
            title={t("machine.upgrade-failed")}
            description={machine.upgradeStatus?.error || ""}
          />
        )}

        {upgradeError && <Alert variant="error" description={upgradeError} />}

        <div className="flex flex-col gap-6">
          {/* Identity & host info */}
          <div className="flex flex-col gap-6">
            <Card title={t("machine.profile.section-identity")}>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                <Field label={t("machine.detail-name")}>{machine.title}</Field>
                {machine.createdBy && (
                  <Field label={t("machine.detail-owner")}>
                    <button
                      type="button"
                      className="text-sm text-link hover:underline"
                      onClick={() =>
                        navigate(
                          `/members/users/${machine.createdBy.replace(
                            /^users\//,
                            ""
                          )}`
                        )
                      }
                    >
                      {userTitle(machine.createdBy)}
                    </button>
                  </Field>
                )}
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
                {info?.labels?.["git_commit"] && (
                  <Field label={t("machine.detail-hash")}>
                    {info.labels["git_commit"]}
                  </Field>
                )}
                {info?.labels?.["build_time"] && (
                  <Field label={t("machine.detail-build-time")}>
                    {info.labels["build_time"]}
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
            <div>
              <Card title={t("machine.profile.section-token")}>
                {actionError && (
                  <Alert variant="error" description={actionError} />
                )}
                {!canManage ? (
                  <p className="text-xs text-control-light">
                    {t("machine.profile.edit-not-allowed")}
                  </p>
                ) : (
                  <div className="flex flex-col gap-4">
                    {isOffline && (
                      <div className="flex flex-col gap-4">
                        {installCommand && (
                          <div className="flex flex-col gap-2">
                            <p className="text-sm text-control-light">
                              {t("machine.profile.offline-install-note")}
                            </p>
                            <p className="text-sm text-control-light">
                              {t("machine.profile.offline-install-hint")}
                            </p>
                            <div className="flex items-center gap-2">
                              <code className="flex-1 rounded bg-white border border-control-border px-3 py-2 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
                                {installCommand}
                              </code>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => void handleCopyInstall()}
                              >
                                {installCopied ? (
                                  <Check className="size-4 text-success" />
                                ) : (
                                  <Copy className="size-4" />
                                )}
                                {installCopied
                                  ? t("common.copied")
                                  : t("common.copy")}
                              </Button>
                            </div>
                          </div>
                        )}
                        <div className="flex flex-col gap-2">
                          <p className="text-sm text-control-light">
                            {t("machine.profile.offline-command-hint")}
                          </p>
                          <div className="flex items-center gap-2">
                            <code className="flex-1 rounded bg-white border border-control-border px-3 py-2 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
                              {setupCommand}
                            </code>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => void handleCopySetup()}
                            >
                              {setupCopied ? (
                                <Check className="size-4 text-success" />
                              ) : (
                                <Copy className="size-4" />
                              )}
                              {setupCopied
                                ? t("common.copied")
                                : t("common.copy")}
                            </Button>
                          </div>
                        </div>
                      </div>
                    )}
                    <div className="flex flex-wrap items-center gap-2">
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
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={openTransferPicker}
                      >
                        {t("machine.transfer-owner")}
                      </Button>
                    </div>
                  </div>
                )}
              </Card>
            </div>

            {/* Who can create agents on this machine */}
            {canManage && (
              <Card
                title={t("machine.access-title")}
                footer={
                  <div className="flex items-center justify-end">
                    <Button variant="outline" size="sm" onClick={openAccess}>
                      {t("machine.access-manage")}
                    </Button>
                  </div>
                }
              >
                {accessError && (
                  <Alert variant="error" description={accessError} />
                )}
                {agentCreatorMembers.length === 0 ? (
                  <p className="text-xs text-control-light">
                    {t("machine.access-no-members")}
                  </p>
                ) : (
                  <ul className="flex flex-wrap gap-1.5">
                    {agentCreatorMembers.map((m) => (
                      <li key={m}>
                        <Badge variant="secondary">{memberLabel(m)}</Badge>
                      </li>
                    ))}
                  </ul>
                )}
              </Card>
            )}
          </div>

          {/* Providers + agent roster */}
          <div className="flex flex-col gap-6">
            <Card
              title={t("machine.providers")}
              footer={
                <div className="flex items-center justify-end gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={refreshing || !canManage}
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
                isDesktop ? (
                  <div className="flex items-center justify-end gap-2">
                    <Button
                      size="sm"
                      disabled={!canCreateAgent}
                      onClick={() => {
                        resetAddForm();
                        setAddOpen(true);
                      }}
                    >
                      <Plus className="size-3.5" />
                      {t("machine.add-agent")}
                    </Button>
                  </div>
                ) : undefined
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
                          onClick={() =>
                            navigate(`/members/agents/${resourceId}`)
                          }
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              navigate(`/members/agents/${resourceId}`);
                            }
                          }}
                        >
                          <div className="min-w-0 flex-1 flex flex-col gap-1">
                            <span className="truncate text-sm font-medium text-main">
                              {agent.title}
                            </span>
                            <ConnectionBadge
                              state={agent.status?.state}
                              enabled={agent.enabled}
                            />
                          </div>
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
                  {t("agent.profile.description")}
                </label>
                <Textarea
                  className="text-sm min-h-[80px]"
                  placeholder={t("agent.profile.description-placeholder")}
                  value={agentDescription}
                  onChange={(e) => {
                    setAgentDescription(e.target.value);
                    setAddError("");
                  }}
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("agent.acp-config-provider")}
                </label>
                <Select
                  value={provider}
                  onValueChange={(v) => {
                    setProvider(String(v ?? ""));
                    // Reset model + pi fields when the provider changes — the
                    // previous values belong to the old runtime.
                    setModel("");
                    setGlobalProvider("");
                    setGlobalProviderEntry("");
                    setAddError("");
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
                {availableProviders.length === 0 && (
                  <p className="text-xs text-control-light">
                    {t("machine.add-agent-no-providers")}
                  </p>
                )}
              </div>

              {isPiProvider && (
                <>
                  {selfProvidedKeysEnabled && (
                    <div className="flex flex-col gap-1">
                      <label className="text-sm font-medium">
                        {t("agent.acp-config-pi-mode")}
                      </label>
                      <Select
                        value={piMode}
                        onValueChange={(v) => {
                          setPiMode(v === "self" ? "self" : "global");
                          setAddError("");
                        }}
                      >
                        <SelectTrigger>
                          <SelectValue>
                            {(v: string | null) =>
                              v === "self"
                                ? t("agent.acp-config-pi-mode-self")
                                : t("agent.acp-config-pi-mode-managed")
                            }
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="global">
                            {t("agent.acp-config-pi-mode-managed")}
                          </SelectItem>
                          <SelectItem value="self">
                            {t("agent.acp-config-pi-mode-self")}
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  {piMode === "global" && (
                    <>
                      <div className="flex flex-col gap-1">
                        <label className="text-sm font-medium">
                          {t("agent.acp-config-pi-global-provider")}
                        </label>
                        <Select
                          value={globalProvider}
                          onValueChange={(v) => {
                            const next = String(v ?? "");
                            setGlobalProvider(next);
                            setGlobalProviderEntry("");
                            setAddError("");
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
                        {apiProviders.length === 0 && (
                          <p className="text-xs text-control-light">
                            {t("machine.add-agent-no-providers")}
                          </p>
                        )}
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
                                setGlobalProviderEntry(String(v ?? ""));
                                setAddError("");
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
                    </>
                  )}

                  {piMode === "self" && (
                    <>
                      <div className="flex flex-col gap-1">
                        <label className="text-sm font-medium">
                          {t("agent.acp-config-pi-api-provider")}
                        </label>
                        <Select
                          value={apiProvider}
                          onValueChange={(v) => {
                            const next = String(v ?? "");
                            setApiProvider(next);
                            setModel("");
                            setApiBaseUrl(next === "custom" ? apiBaseUrl : "");
                            setAddError("");
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

                      {apiProvider === "custom" && (
                        <div className="flex flex-col gap-1">
                          <label className="text-sm font-medium">
                            {t("agent.acp-config-pi-api-base-url")}
                          </label>
                          <Input
                            value={apiBaseUrl}
                            onChange={(e) => {
                              setApiBaseUrl(e.target.value);
                              setAddError("");
                            }}
                            onBlur={() => {
                              if (apiBaseUrl.trim()) {
                                void fetchPiModels(
                                  apiProvider,
                                  apiKey,
                                  apiBaseUrl
                                );
                              }
                            }}
                            placeholder={t(
                              "agent.acp-config-pi-api-base-url-placeholder"
                            )}
                            spellCheck={false}
                          />
                          <p className="text-xs text-control-light">
                            {t("agent.acp-config-pi-api-base-url-hint")}
                          </p>
                        </div>
                      )}

                      <div className="flex flex-col gap-1">
                        <label className="text-sm font-medium">
                          {t("agent.acp-config-pi-api-key")}
                        </label>
                        <SecretInput
                          placeholder={t(
                            "agent.acp-config-pi-api-key-placeholder"
                          )}
                          value={apiKey}
                          onChange={(e) => {
                            setApiKey(e.target.value);
                            setAddError("");
                          }}
                        />
                        <p className="text-xs text-control-light">
                          {t("agent.acp-config-pi-api-key-hint")}
                        </p>
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
                            emptyLabel={t("agent.acp-config-pi-models-empty")}
                            onValueChange={(next) => {
                              setModel(next);
                              setAddError("");
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
                                apiKey.trim() === "") ||
                              (apiProvider === "custom" &&
                                apiBaseUrl.trim() === "")
                            }
                            onClick={() => {
                              if (apiProvider) {
                                piModelsCacheRef.current.delete(
                                  `${apiProvider}/${apiBaseUrl}`
                                );
                              }
                              void fetchPiModels(
                                apiProvider,
                                apiKey,
                                apiBaseUrl
                              );
                            }}
                          >
                            {piModelsLoading ? (
                              <Loader2 className="size-3.5 animate-spin" />
                            ) : (
                              t("agent.acp-config-pi-models-refresh")
                            )}
                          </Button>
                        </div>
                        {apiProvider &&
                          !piModelsLoading &&
                          piModels.length === 0 && (
                            <p className="text-xs text-control-light">
                              {t("agent.acp-config-pi-models-empty")}
                            </p>
                          )}
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

              {isCustomProvider && !isPiProvider && (
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

              {selectedProviderInfo && !isCustomProvider && !isPiProvider && (
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

              {!isPiProvider && (
                <KeyValueEnvEditor
                  label={t("agent.acp-config-custom-env")}
                  entries={customEnvEntries}
                  onChange={(next) => {
                    setCustomEnvEntries(next);
                    setAddError("");
                  }}
                />
              )}

              {!isPiProvider && (
                <StringListEditor
                  label={t("agent.acp-config-allow-env")}
                  placeholder={t("agent.acp-config-allow-env-placeholder")}
                  values={allowEnv}
                  onChange={(next) => {
                    setAllowEnv(next);
                    setAddError("");
                  }}
                />
              )}

              <FieldRow
                label={t("agent.allow-add-to-channel")}
                hint={t("agent.allow-add-to-channel-hint")}
              >
                <Switch
                  checked={allowAddToChannel}
                  onCheckedChange={setAllowAddToChannel}
                />
              </FieldRow>
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
                (modelRequired && !model.trim()) ||
                (isPiProvider &&
                  (piMode === "global"
                    ? !globalProvider.trim() || !globalProviderEntry.trim()
                    : !apiProvider.trim() ||
                      !model.trim() ||
                      !apiKey.trim() ||
                      (apiProvider === "custom" && !apiBaseUrl.trim())))
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

      {/* Manage access (who may create agents) */}
      <Sheet
        open={accessOpen}
        onOpenChange={(next) => {
          setAccessOpen(next);
          if (!next) setAccessError("");
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{t("machine.access-manage-title")}</SheetTitle>
            <SheetDescription>
              {t("machine.access-manage-description", { title: machine.title })}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {accessError && (
              <Alert
                variant="error"
                description={accessError}
                className="mb-2"
              />
            )}
            <div className="flex flex-col gap-5">
              {/* Current members */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("machine.access-current-members")}
                </label>
                <div className="max-h-64 overflow-y-auto pr-1">
                  {accessMembers.size === 0 ? (
                    <p className="text-sm text-control-light py-4 text-center border border-dashed border-control-border rounded-xs">
                      {t("machine.access-no-members")}
                    </p>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {[...accessMembers]
                        .sort((a, b) =>
                          (memberLabel(a) ?? a).localeCompare(
                            memberLabel(b) ?? b
                          )
                        )
                        .map((member) => {
                          const user = users.find((u) => u.name === member);
                          const group = member.startsWith("groups/")
                            ? groups.find(
                                (g) =>
                                  g.name === member ||
                                  (g.email
                                    ? `groups/${g.email}` === member
                                    : false)
                              )
                            : undefined;
                          return (
                            <div
                              key={member}
                              className="flex items-center gap-3 rounded-xs border border-control-border bg-background p-3"
                            >
                              <div className="flex size-9 items-center justify-center rounded-full shrink-0 bg-accent/10 text-accent">
                                {group ? (
                                  <Shield className="size-4.5" />
                                ) : (
                                  <UserIcon className="size-4.5" />
                                )}
                              </div>
                              <div className="flex flex-col gap-0.5 min-w-0 flex-1">
                                <span className="text-sm font-medium text-main truncate">
                                  {memberLabel(member)}
                                </span>
                                {user?.title && (
                                  <span className="text-xs text-control-light truncate">
                                    {user.title}
                                  </span>
                                )}
                                {group && (
                                  <span className="text-xs text-control-light truncate">
                                    {t("machine.access-member-group-count", {
                                      count: group.members?.length ?? 0,
                                    })}
                                  </span>
                                )}
                              </div>
                              <Button
                                variant="ghost"
                                size="xs"
                                onClick={() => handleAccessRemove(member)}
                                aria-label={t("machine.access-remove-member", {
                                  email: memberLabel(member),
                                })}
                                className="shrink-0 text-control-light hover:text-error"
                              >
                                <X className="size-4" />
                              </Button>
                            </div>
                          );
                        })}
                    </div>
                  )}
                </div>
              </div>

              {/* Add member */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("machine.access-add-member")}
                </label>
                <p className="text-xs text-control-placeholder">
                  {t("machine.access-add-member-hint")}
                </p>
                <MemberPicker
                  users={users.filter((u) => !accessMembers.has(u.name ?? ""))}
                  groups={groups.filter(
                    (g) =>
                      !accessMembers.has(g.name ?? "") &&
                      !(g.email && accessMembers.has(`groups/${g.email}`))
                  )}
                  value=""
                  onSelect={handleAccessAdd}
                />
              </div>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setAccessOpen(false)}
              disabled={accessSaving}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={accessSaving} onClick={handleSaveAccess}>
              {accessSaving ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

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

      {/* Mobile add-agent FAB: mirrors the chat create-channel FAB on touch
          layouts; the roster footer button stays for desktop. */}
      {canCreateAgent && (
        <button
          type="button"
          onClick={() => {
            resetAddForm();
            setAddOpen(true);
          }}
          aria-label={t("machine.add-agent")}
          data-testid="add-agent-fab"
          className={cn(
            "fixed right-4 z-chrome flex h-14 items-center justify-center gap-1.5 overflow-hidden",
            "bottom-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+0.75rem)]",
            "rounded-full bg-accent text-accent-text shadow-lg transition-all duration-200",
            "focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2",
            "lg:hidden",
            listScrolled ? "w-14" : "w-32"
          )}
        >
          <Plus className="size-6 shrink-0" strokeWidth={2.25} />
          {!listScrolled && (
            <span className="text-sm font-semibold whitespace-nowrap">
              {t("machine.add-agent-fab-label")}
            </span>
          )}
        </button>
      )}

      {/* Ownership transfer: pick target + reason, then a second risky-action
          confirm. The transfer is unilateral and effective immediately. */}
      <Dialog
        open={transferOpen}
        onOpenChange={(next) => !next && setTransferOpen(false)}
      >
        <DialogContent>
          <DialogTitle>{t("machine.transfer-owner-title")}</DialogTitle>
          <DialogDescription>
            {t("machine.transfer-owner-description")}
          </DialogDescription>
          <div className="mt-4 flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {t("machine.transfer-owner-target")}
              </label>
              <Select
                value={transferTarget}
                onValueChange={(v) => v && setTransferTarget(v)}
              >
                <SelectTrigger>
                  <SelectValue
                    placeholder={t("machine.transfer-owner-target-placeholder")}
                  >
                    {(v: string | null) =>
                      v ? users.find((u) => u.name === v)?.title || v : ""
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {users
                    .filter((u) => u.name !== machine.createdBy)
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
                {t("machine.transfer-owner-reason")}
              </label>
              <Input
                value={transferReason}
                onChange={(e) => setTransferReason(e.target.value)}
                placeholder={t("machine.transfer-owner-reason-placeholder")}
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
            {t("machine.transfer-owner-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.transfer-owner-confirm-description", {
              target:
                users.find((u) => u.name === transferTarget)?.title ||
                transferTarget,
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
                : t("machine.transfer-owner-confirm")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
