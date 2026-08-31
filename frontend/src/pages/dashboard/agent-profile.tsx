import {
  Loader2,
  Pencil,
  Play,
  RotateCcw,
  Square,
  Trash2,
  Upload,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import {
  AcpConfigEditor,
  type AcpConfigEditorHandle,
} from "@/components/agent/acp-config-editor";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Card, Field } from "@/components/profile-common";
import { TransferOwnershipDialog } from "@/components/shared/transfer-ownership-dialog";
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
import { FieldRow } from "@/components/ui/field-row";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { persistedToInput } from "@/composables/use-acp-config-draft";
import { useAvatarEditor } from "@/composables/useAvatarEditor";
import { settingServiceClient } from "@/connect";
import {
  deleteAgentAvatar,
  uploadAgentAvatar,
  useAvatar,
} from "@/lib/avatar-cache";
import { agentResourceName, formatTimestamp } from "@/lib/command-status";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import type { AgentACPConfigInput } from "@/stores/types";
import {
  type Agent,
  type AgentModelOption,
  type AgentProviderInfo,
} from "@/types/proto-es/v1/agent_pb";
import { agentLifecycle, lifecycleLabel } from "./agents";
export function AgentProfilePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const getMachine = useAppStore((s) => s.getMachine);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);
  // The runtime-config/avatar/persona editors hit admin-only RPCs (agents.edit), so
  // they are gated on canEditAdminOnly even when canEdit is true for the agent's
  // owner. The allow_add_to_channel toggle below is gated on canEdit.
  const canEditAdminOnly = useHasPermission("laelia.agents.edit");
  // Whether users may self-provide an inline api key (workspace toggle). When
  // enabled, non-admin owners can configure their own key on their agents; the
  // legacy inline fields are then shown to them (with a masked key preview).
  // Admins always see them.
  const [selfProvidedKeysEnabled, setSelfProvidedKeysEnabled] = useState(false);

  const agentName = agentResourceName(agentId);
  // Hold the full GetAgent result in local state, fetched fresh on entry and
  // re-fetched after each mutation. canEdit/acp_config are per-caller and
  // mutable, so they are never cached in the store — this avoids a stale
  // canEdit surviving a user switch (admin → normal user).
  const [agent, setAgent] = useState<Agent | undefined>(undefined);
  // loadError distinguishes a failed/missing fetch from an in-progress load so
  // the profile does not strand the user on a perpetual "Loading…" screen.
  const [loadError, setLoadError] = useState(false);

  // Persona/description editors keep their own page-level state; the runtime
  // config form state (provider, model, pi key sources, env editors) moved
  // into AcpConfigEditor + useAcpConfigDraft, seeded per agent via key remount.
  const [personaDraft, setPersonaDraft] = useState("");
  const [personaEditing, setPersonaEditing] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const [descriptionEditing, setDescriptionEditing] = useState(false);
  const [savingDescription, setSavingDescription] = useState(false);
  const [saveStatus, setSaveStatus] = useState<
    "idle" | "saving" | "saved" | "error"
  >("idle");
  // Global API providers the caller may use (handler-gated server-side), for
  // the builtin-pi runtime's provider/entry pickers.
  const apiProviders = useAppStore((s) => s.apiProviders);
  // agentRef mirrors the latest fetched agent so saves can read the persisted
  // config/persona snapshot; editorRef reads the config editor's CURRENT draft
  // synchronously (replacing the page's old state+configRef dual-write).
  const agentRef = useRef<Agent | undefined>(undefined);
  agentRef.current = agent;
  const editorRef = useRef<AcpConfigEditorHandle | null>(null);
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
  // The machine display name needs no round-trip: GetAgent resolves it into
  // agent.machineTitle.
  const [machineProviders, setMachineProviders] = useState<AgentProviderInfo[]>(
    []
  );
  // Available providers are machine-scoped: the owning machine probes its host
  // and exposes them on Machine.info.availableProviders. The "custom" escape
  // hatch lets an admin hand-type a command for any provider the machine does
  // not know about (the editor adds it to the picker unconditionally).
  const availableProviders: AgentProviderInfo[] = machineProviders;

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
  const [canManageMembersSaving, setCanManageMembersSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Ownership transfer: the two-step flow lives in the shared
  // TransferOwnershipDialog (target + reason picker, then a confirm
  // AlertDialog); the page only controls when it opens and supplies the
  // store action plus its success/error side effects.
  const [transferOpen, setTransferOpen] = useState(false);
  const [stopOpen, setStopOpen] = useState(false);
  const [stopBusy, setStopBusy] = useState(false);
  const [stopError, setStopError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [restartOpen, setRestartOpen] = useState(false);
  const [restartBusy, setRestartBusy] = useState(false);
  const [restartError, setRestartError] = useState("");

  async function handleStop() {
    setStopBusy(true);
    setStopError("");
    try {
      await useAppStore.getState().stopAgent(agentName);
      setStopOpen(false);
      await loadAgent();
      toastManager.add({
        type: "success",
        title: t("agent.stopped-toast"),
      });
    } catch (err) {
      setStopError(describeError(err));
    } finally {
      setStopBusy(false);
    }
  }

  async function handleStart() {
    try {
      await useAppStore.getState().startAgent(agentName);
      await loadAgent();
      toastManager.add({
        type: "success",
        title: t("agent.started-toast"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        description: describeError(err),
      });
    }
  }

  async function handleRestart() {
    setRestartBusy(true);
    setRestartError("");
    try {
      await useAppStore.getState().restartAgent(agentName);
      setRestartOpen(false);
      toastManager.add({
        type: "success",
        title: t("agent.restarted-toast"),
      });
    } catch (err) {
      setRestartError(describeError(err));
    } finally {
      setRestartBusy(false);
    }
  }

  async function handleDelete() {
    setDeleteBusy(true);
    setDeleteError("");
    try {
      await useAppStore.getState().deleteAgent(agentName);
      setDeleteOpen(false);
      toastManager.add({
        type: "success",
        title: t("agent.deleted-toast"),
      });
      navigate("/members/agents");
    } catch (err) {
      setDeleteError(describeError(err));
    } finally {
      setDeleteBusy(false);
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

  // Load the caller's accessible global API providers once per page view. The
  // store slice caches them for the builtin-pi runtime's managed provider and
  // entry pickers; without this the agent detail page would show the raw
  // "apiProviders/{id}" resource name and an empty provider dropdown.
  useEffect(() => {
    void useAppStore.getState().fetchApiProviders(undefined, { silent: true });
  }, []);

  // Load the owning machine's available providers whenever the agent's machine
  // binding is known. Providers are machine-scoped (the machine probes its host);
  // the agent profile reads them from the machine rather than the agent. The
  // machine display name comes from GetAgent (machineTitle), so no second
  // round-trip is needed for it.
  useEffect(() => {
    const machineName = agent?.machine;
    if (!machineName) {
      setMachineProviders([]);
      return;
    }
    getMachine(machineName).then((m) => {
      setMachineProviders(m?.info?.availableProviders ?? []);
    });
  }, [agent?.machine, getMachine]);

  // Read the workspace LLM config toggle that decides whether the legacy
  // self-provided-key fields are shown to non-admin owners.
  useEffect(() => {
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

  // Reset the agent-scoped page state once per agent (on load / agent switch).
  // Deliberately keyed on agent.name only — NOT on acpConfig — so the refetch
  // that follows each auto-save does not clobber in-progress edits. The
  // runtime-config editor seeds its own draft the same way: the page remounts
  // it via key={agent.name}, so only the persona/description editors and the
  // save-status indicator (which still live here) are reset in this effect.
  useEffect(() => {
    setPersonaDraft(agent?.info?.acpConfig?.personaPrompt ?? "");
    setPersonaEditing(false);
    setDescriptionDraft(agent?.description ?? "");
    setDescriptionEditing(false);
    setSavingDescription(false);
    setSaveStatus("idle");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent?.name]);

  // The config editor is memoized, so its callbacks must keep their identity
  // across page re-renders (persona/description typing, store loads) — both
  // read only refs and stable store actions inside. (Rules of Hooks: these
  // live before the `if (!agent)` early return.)
  const handleRefreshModels = useCallback(
    (input: AgentACPConfigInput): Promise<AgentModelOption[]> =>
      useAppStore.getState().refreshAgentModels(agentName, input),
    [agentName]
  );
  function isConfigDirty(): boolean {
    // Skip a save when the live draft matches what the server already holds
    // (e.g. focus→blur with no edit), to avoid redundant writes. The persona
    // used for the comparison comes from the persisted config, never the draft.
    return (
      editorRef.current?.isDirtyAgainst(agentRef.current?.info?.acpConfig) ??
      false
    );
  }
  function saveConfig() {
    if (!canEditAdminOnly) return;
    const editor = editorRef.current;
    if (!editor) return;
    if (!editor.canSave(availableProviders)) return;
    if (!isConfigDirty()) {
      setSaveStatus("idle");
      return;
    }
    // Read the persisted persona inside the build closure (at execution time,
    // after any earlier saves in the queue have refetched) so a config save
    // queued behind a persona save never reverts the persona. The draft is
    // read through the editor's synchronous ref mirror at execution time too,
    // so the last queued draft always wins.
    const { toInput } = editor;
    enqueueSave(() =>
      toInput(agentRef.current?.info?.acpConfig?.personaPrompt ?? "")
    );
  }
  // Stable identity wrapper for the memoized editor's onAutoSave; the editor
  // fires it exactly where the page used to call saveConfig().
  const saveConfigRef = useRef<() => void>(() => {});
  saveConfigRef.current = saveConfig;
  const handleAutoSave = useCallback(() => saveConfigRef.current(), []);

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
  // The runtime-config/avatar/persona editors hit admin-only RPCs (agents.edit), so
  // they are gated on canEditAdminOnly to avoid offering a 403 to owners.
  const canEdit = agent.canEdit;

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
        void showErrorToast(err, t("agent.acp-config-save-failed"));
      }
    });
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
      persistedToInput(agentRef.current?.info?.acpConfig, personaDraft.trim())
    );
  }

  // Save the public description via UpdateAgent, then refetch the agent and the
  // roster so pickers/rosters show the updated intro.
  async function saveDescription() {
    if (!canEdit) return;
    setSavingDescription(true);
    try {
      const updateAgent = useAppStore.getState().updateAgent;
      await updateAgent(agentName, { description: descriptionDraft.trim() });
      setAgent(await getAgent(agentName));
      fetchAgents({ pageSize: 100 }, { silent: true });
      setDescriptionEditing(false);
    } catch (err) {
      void showErrorToast(err, t("agent.profile.description-save-failed"));
    } finally {
      setSavingDescription(false);
    }
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
      void showErrorToast(err, t("agent.allow-add-to-channel-save-error"));
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
      void showErrorToast(err, t("agent.follow-owner-permissions-save-error"));
    } finally {
      setFollowOwnerSaving(false);
    }
  }

  // Toggle can_manage_channel_members via UpdateAgent, then refetch the agent so
  // the member-management access model shown reflects the new setting.
  async function handleToggleCanManageMembers(next: boolean) {
    setCanManageMembersSaving(true);
    try {
      const updateAgent = useAppStore.getState().updateAgent;
      await updateAgent(agentName, { canManageChannelMembers: next });
      setAgent(await getAgent(agentName));
    } catch (err) {
      void showErrorToast(
        err,
        t("agent.can-manage-channel-members-save-error")
      );
    } finally {
      setCanManageMembersSaving(false);
    }
  }

  // userTitle resolves a user resource name (users/{id}) to the roster's display
  // title, falling back to the raw name so a stale/deleted user never renders
  // empty. Used for the owner/creator display rows.
  function userTitle(name: string): string {
    if (!name) return "";
    return users.find((u) => u.name === name)?.title || name;
  }

  // Transfer flow (rendered by TransferOwnershipDialog): on confirm,
  // TransferAgentOwnership reassigns the owner immediately and unilaterally;
  // the profile and roster are refetched so the new owner's authority (and
  // the old owner's loss of it) reflects at once. Re-throwing lets the dialog
  // surface the error inline next to the toast.
  async function handleTransfer(target: string, reason: string) {
    try {
      const transferAgentOwnership =
        useAppStore.getState().transferAgentOwnership;
      await transferAgentOwnership(agentName, target, reason);
      setAgent(await getAgent(agentName));
      fetchAgents({ pageSize: 100 }, { silent: true });
      toastManager.add({
        type: "success",
        title: t("agent.transfer-owner-success"),
      });
    } catch (err) {
      void showErrorToast(err, t("agent.transfer-owner-failed"));
      throw err;
    }
  }

  const lifecycle = agentLifecycle(agent);

  const machineResourceID = agent.machine
    ? agent.machine.replace(/^machines\//, "")
    : "";

  // machine_title rides along on GetAgent, so the identity grid shows the
  // machine's display name immediately; the raw machines/{id} is the
  // last-resort fallback (e.g. for a machine that no longer exists).
  const machineDisplay = agent.machineTitle || agent.machine;

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
                <Field label={t("agent.detail-name")}>
                  {agent.title}{" "}
                  <span className="font-mono text-control-light">
                    @{agent.handle}
                  </span>
                </Field>
                <Field label={t("agent.detail-status")}>
                  <ConnectionBadge
                    state={agent.status?.state}
                    enabled={agent.enabled}
                  />
                </Field>
                <Field label={t("agent.detail-configuration")}>
                  {lifecycleLabel(t, lifecycle)}
                </Field>
                {agent.machine && (
                  <Field label={t("agent.detail-machine")}>
                    {canEdit ? (
                      <button
                        type="button"
                        className="text-sm text-link hover:underline"
                        onClick={() =>
                          machineResourceID &&
                          navigate(`/machines/${machineResourceID}`)
                        }
                      >
                        {machineDisplay}
                      </button>
                    ) : (
                      <span className="text-sm">{machineDisplay}</span>
                    )}
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
                          onClick={() => setTransferOpen(true)}
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

              {/* Public description */}
              <div className="flex flex-col gap-1 pt-2 border-t border-control-border">
                <div className="flex items-center gap-2">
                  <div className="text-xs font-semibold uppercase tracking-widest text-control-light">
                    {t("agent.profile.description")}
                  </div>
                  {!descriptionEditing && (
                    <button
                      type="button"
                      aria-label={t("agent.profile.edit-description")}
                      title={t("agent.profile.edit-description")}
                      className="text-control-light hover:text-control transition-colors disabled:cursor-not-allowed disabled:opacity-40"
                      disabled={!canEdit}
                      onClick={() => setDescriptionEditing(true)}
                    >
                      <Pencil className="size-3" />
                    </button>
                  )}
                </div>
                {descriptionEditing ? (
                  <div className="flex flex-col gap-2">
                    <Textarea
                      className="text-sm min-h-[100px]"
                      placeholder={t("agent.profile.description-placeholder")}
                      value={descriptionDraft}
                      onChange={(e) => setDescriptionDraft(e.target.value)}
                    />
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        disabled={!canEdit || savingDescription}
                        onClick={saveDescription}
                      >
                        {savingDescription ? (
                          <Loader2 className="size-3.5 animate-spin" />
                        ) : (
                          t("common.save")
                        )}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setDescriptionDraft(agent?.description ?? "");
                          setDescriptionEditing(false);
                        }}
                      >
                        {t("common.cancel")}
                      </Button>
                    </div>
                  </div>
                ) : (
                  <p className="text-sm text-main whitespace-pre-wrap">
                    {descriptionDraft.trim() ? (
                      descriptionDraft
                    ) : (
                      <span className="italic text-control-light">
                        {t("agent.profile.description-empty")}
                      </span>
                    )}
                  </p>
                )}
              </div>

              {/* Persona prompt */}
              <div className="flex flex-col gap-1 pt-2 border-t border-control-border">
                <div className="flex items-center gap-2">
                  <div className="text-xs font-semibold uppercase tracking-widest text-control-light">
                    {t("agent.profile.persona-prompt")}
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
                        "agent.profile.persona-prompt-placeholder"
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
                            agentRef.current?.info?.acpConfig?.personaPrompt ??
                              ""
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
                        {t("agent.profile.persona-empty")}
                      </span>
                    )}
                  </p>
                )}
              </div>

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
              <FieldRow
                label={t("agent.can-manage-channel-members")}
                hint={t("agent.can-manage-channel-members-hint")}
              >
                <Switch
                  checked={agent.canManageChannelMembers ?? true}
                  disabled={!canEdit || canManageMembersSaving}
                  onCheckedChange={(next) => {
                    void handleToggleCanManageMembers(next);
                  }}
                />
              </FieldRow>
            </Card>
          </div>

          {/* Actions */}
          {canEdit && (
            <div>
              <Card title={t("common.actions")}>
                <div className="flex flex-col gap-2">
                  {agent.enabled ? (
                    <Button
                      variant="outline"
                      size="md"
                      className="w-full justify-center"
                      onClick={() => {
                        setStopError("");
                        setStopOpen(true);
                      }}
                    >
                      <Square className="size-4" />
                      {t("common.stop")}
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="md"
                      className="w-full justify-center"
                      onClick={handleStart}
                    >
                      <Play className="size-4" />
                      {t("common.start")}
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    size="md"
                    className="w-full justify-center"
                    onClick={() => {
                      setRestartError("");
                      setRestartOpen(true);
                    }}
                  >
                    <RotateCcw className="size-4" />
                    {t("common.restart")}
                  </Button>
                  <Button
                    variant="outline"
                    size="md"
                    className="w-full justify-center border-error/30 bg-error/10 text-error hover:bg-error/15"
                    onClick={() => {
                      setDeleteError("");
                      setDeleteOpen(true);
                    }}
                  >
                    <Trash2 className="size-4" />
                    {t("common.delete")}
                  </Button>
                </div>
              </Card>
            </div>
          )}

          {/* Runtime config. The editor owns the form draft (remounted per
              agent via key={agent.name}, which re-seeds it only on agent
              change — refetches keep the same key and never clobber in-flight
              edits); the page keeps the save chain and serializes saves. */}
          <div>
            <AcpConfigEditor
              key={agent.name}
              ref={editorRef}
              acpConfig={agent.info?.acpConfig}
              agentName={agentName}
              availableProviders={availableProviders}
              apiProviders={apiProviders}
              canEdit={canEdit}
              canEditAdminOnly={canEditAdminOnly}
              canSelfProvide={selfProvidedKeysEnabled}
              machineResourceID={machineResourceID}
              saveStatus={saveStatus}
              onAutoSave={handleAutoSave}
              onRefreshModels={handleRefreshModels}
            />
          </div>
        </div>
      </div>

      {/* Restart-agent confirm */}
      <AlertDialog
        open={restartOpen}
        onOpenChange={(next) => !next && setRestartOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.restart-agent-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.restart-agent-confirm-description", {
              title: agent.title,
            })}
          </AlertDialogDescription>
          {restartError && <Alert variant="error" description={restartError} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={restartBusy}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={restartBusy}
              onClick={handleRestart}
            >
              {restartBusy ? t("common.saving") : t("common.restart")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Stop-agent confirm */}
      <AlertDialog
        open={stopOpen}
        onOpenChange={(next) => !next && setStopOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.stop-agent-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.stop-agent-confirm-description", { title: agent.title })}
          </AlertDialogDescription>
          {stopError && <Alert variant="error" description={stopError} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={stopBusy}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={stopBusy}
              onClick={handleStop}
            >
              {stopBusy ? t("common.saving") : t("common.stop")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete-agent confirm */}
      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => !next && setDeleteOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("agent.delete-agent-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.delete-agent-confirm-description", {
              title: agent.title,
            })}
          </AlertDialogDescription>
          {deleteError && <Alert variant="error" description={deleteError} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleteBusy}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={deleteBusy}
              onClick={handleDelete}
            >
              {deleteBusy ? t("common.saving") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Ownership transfer: pick target + reason, then a second risky-action
          confirm. The transfer is unilateral and effective immediately. */}
      <TransferOwnershipDialog
        open={transferOpen}
        onOpenChange={setTransferOpen}
        users={users}
        excludeUserName={agent.owner}
        onTransfer={handleTransfer}
        labels={{
          pickerTitle: t("agent.transfer-owner-title"),
          pickerDescription: t("agent.transfer-owner-description"),
          targetLabel: t("agent.transfer-owner-target"),
          targetPlaceholder: t("agent.transfer-owner-target-placeholder"),
          reasonLabel: t("agent.transfer-owner-reason"),
          reasonPlaceholder: t("agent.transfer-owner-reason-placeholder"),
          confirmTitle: t("agent.transfer-owner-confirm-title"),
          confirmDescription: (targetTitle) =>
            t("agent.transfer-owner-confirm-description", {
              target: targetTitle,
            }),
          confirmAction: t("agent.transfer-owner-confirm"),
        }}
      />
    </div>
  );
}
