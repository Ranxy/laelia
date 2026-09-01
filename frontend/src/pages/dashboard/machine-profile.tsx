import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { Loader2, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { TransferOwnershipDialog } from "@/components/shared/transfer-ownership-dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  groupServiceClient,
  iamServiceClient,
  settingServiceClient,
} from "@/connect";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { usePolling } from "@/hooks/use-polling";
import { describeError } from "@/lib/connect-errors";
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
} from "@/types/proto-es/v1/agent_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type Machine } from "@/types/proto-es/v1/machine_pb";
import { MachineAddAgentSheet } from "./machine-add-agent-sheet";
import {
  MachineAccessCard,
  MachineAccessManageSheet,
  MachineAgentRoster,
  MachineIdentityCard,
  MachineProvidersCard,
  MachineTokenCard,
} from "./machine-profile-cards";

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

  // Self-upgrade state: the trigger is local, the progress comes from
  // machine.upgradeStatus refreshed by polling while an upgrade runs.
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeError, setUpgradeError] = useState("");

  // Add-agent sheet: only the open flag and the created-agent success dialog
  // live here. The ~20-field form state moved into MachineAddAgentSheet,
  // which remounts its inner form per open — the old resetAddForm() cascade
  // is gone (remount-per-open replaces manual resets).
  const [addOpen, setAddOpen] = useState(false);
  const [addedOpen, setAddedOpen] = useState(false);
  const [addedTitle, setAddedTitle] = useState("");
  // Workspace toggle deciding whether the add-agent sheet offers the
  // self-provided-key mode.
  const [selfProvidedKeysEnabled, setSelfProvidedKeysEnabled] = useState(false);
  const [listScrolled, setListScrolled] = useState(false);

  // Ownership transfer: the two-step flow lives in the shared
  // TransferOwnershipDialog (target + reason picker, then a confirm
  // AlertDialog); the page only controls when it opens and supplies the
  // store action plus its post-transfer reload.
  const [transferOpen, setTransferOpen] = useState(false);

  // Access (IAM) state: who may create agents on this machine. The policy and
  // its load/save flow stay page-level; the card + manage sheet rendering
  // lives in machine-profile-cards. The policy is loaded only for callers who
  // may manage it (machine.canManage).
  const [policyState, setPolicyState] = useState<{
    policy: IamPolicy;
    etag: string;
  } | null>(null);
  const [accessOpen, setAccessOpen] = useState(false);
  const [accessMembers, setAccessMembers] = useState<Set<string>>(new Set());
  const accessInitializedRef = useRef(false);
  const [accessSaving, setAccessSaving] = useState(false);
  const [accessError, setAccessError] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);

  // Machine-scoped providers (identity grid's add-sheet picker, providers
  // card); memoized so card props stay referentially stable.
  const availableProviders: AgentProviderInfo[] = useMemo(
    () => machine?.info?.availableProviders ?? [],
    [machine]
  );

  const reload = useCallback(async () => {
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
  }, [getMachine, machineName]);

  useEffect(() => {
    if (!machineId) return;
    void reload();
  }, [machineId, reload]);

  // Load the caller's accessible global API providers once per page view; the
  // store slice caches them for the add-agent sheet's provider/entry
  // dropdowns. Also read the workspace toggle that decides whether the
  // self-provided-key mode is offered in the sheet.
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

  // Access (who may create agents) logic. The machine IAM policy is
  // handler-gated server-side to the machine's creator or a workspace admin,
  // matching machine.canManage.
  const loadPolicy = useCallback(async () => {
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
      setAccessError(describeError(err));
    }
  }, [machineName]);

  useEffect(() => {
    if (!machineId) return;
    fetchUsers({ pageSize: 1000 });
    if (!machine?.canManage) return;
    void loadPolicy();
    void groupServiceClient
      .listGroups({ pageSize: 1000 })
      .then((res) => setGroups(res.groups ?? []));
  }, [machineId, machine?.canManage, fetchUsers, loadPolicy]);

  // agentCreatorMembers are the principals bound to the machineAgentCreator
  // role on this machine's IAM policy.
  const agentCreatorMembers = useMemo(() => {
    const binding = policyState?.policy.bindings.find(
      (b) => b.role === AGENT_CREATOR_ROLE
    );
    return binding?.members ?? [];
  }, [policyState]);

  // If the manage sheet is opened before the IAM policy finishes loading,
  // populate its member list as soon as the policy arrives. This avoids
  // showing an empty "current members" list for fast clicks while the policy
  // request is still in flight.
  useEffect(() => {
    if (!accessOpen) {
      accessInitializedRef.current = false;
      return;
    }
    if (!policyState || accessInitializedRef.current) return;
    accessInitializedRef.current = true;
    setAccessMembers(new Set(agentCreatorMembers));
  }, [accessOpen, policyState, agentCreatorMembers]);

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
  // Gated via enabled: only polls while an upgrade is in flight.
  usePolling(
    useCallback(async () => {
      const next = await getMachine(machineName);
      if (next) setMachine(next);
    }, [getMachine, machineName]),
    3000,
    { enabled: upgradeInProgress }
  );

  // userTitle resolves a user resource name (users/{id}) to the roster's
  // display title, falling back to the raw name so a stale/deleted user never
  // renders empty.
  const userTitle = useCallback(
    (name: string): string => {
      if (!name) return "";
      return users.find((u) => u.name === name)?.title || name;
    },
    [users]
  );

  const openUserProfile = useCallback(
    (userId: string) => navigate(`/members/users/${userId}`),
    [navigate]
  );
  const openAgentProfile = useCallback(
    (resourceId: string) => navigate(`/members/agents/${resourceId}`),
    [navigate]
  );
  const openTransferPicker = useCallback(() => {
    setTransferOpen(true);
  }, []);
  const openAddAgent = useCallback(() => {
    setAddOpen(true);
  }, []);
  const closeAddAgent = useCallback(() => setAddOpen(false), []);

  // The add-agent sheet calls this after a successful createAgent: the page
  // closes the sheet, shows the created dialog and refetches machine + roster
  // so the new agent appears and machine pickers refresh.
  const handleAgentCreated = useCallback(
    (title: string) => {
      setAddedTitle(title);
      setAddOpen(false);
      setAddedOpen(true);
      void (async () => {
        await reload();
        fetchMachines({ pageSize: 100 }, { silent: true });
      })();
    },
    [reload, fetchMachines]
  );

  // Provider refresh state lives in MachineProvidersCard; the flow itself is
  // page-level (refresh + reload).
  const handleRefreshProviders = useCallback(async () => {
    const refreshMachineProviders =
      useAppStore.getState().refreshMachineProviders;
    await refreshMachineProviders(machineName);
    await reload();
  }, [machineName, reload]);

  const handleUpgrade = useCallback(async () => {
    setUpgrading(true);
    setUpgradeError("");
    try {
      await useAppStore.getState().upgradeMachine(machineName);
      // Refresh immediately so the "requested" status shows, then the poll
      // effect above takes over.
      const next = await getMachine(machineName);
      if (next) setMachine(next);
    } catch (err) {
      setUpgradeError(describeError(err));
    } finally {
      setUpgrading(false);
    }
  }, [getMachine, machineName]);

  // Token card flows: the card owns its confirm dialogs and busy state; the
  // page owns the mutation + refetch ordering.
  const handleRevokeToken = useCallback(async () => {
    const revokeMachineToken = useAppStore.getState().revokeMachineToken;
    await revokeMachineToken(machineName);
    await reload();
  }, [machineName, reload]);

  const handleForceDisconnect = useCallback(async () => {
    const forceDisconnectMachine =
      useAppStore.getState().forceDisconnectMachine;
    await forceDisconnectMachine(machineName);
    await reload();
    fetchMachines({ pageSize: 100 }, { silent: true });
  }, [machineName, reload, fetchMachines]);

  // Transfer flow (rendered by TransferOwnershipDialog): on confirm,
  // TransferMachineOwnership reassigns the owner immediately and unilaterally;
  // the profile is refetched so the new owner's authority (and the old
  // owner's loss of it) reflects at once.
  const handleTransfer = useCallback(
    async (target: string, reason: string) => {
      const transferMachineOwnership =
        useAppStore.getState().transferMachineOwnership;
      await transferMachineOwnership(machineName, target, reason);
      await reload();
    },
    [machineName, reload]
  );

  function openAccess() {
    accessInitializedRef.current = false;
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
        setAccessError(describeError(err));
      }
    } finally {
      setAccessSaving(false);
    }
  }

  const closeAccessSheet = useCallback(() => {
    setAccessOpen(false);
    setAccessError("");
  }, []);

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
            <MachineIdentityCard
              machine={machine}
              userTitle={userTitle}
              onOpenUser={openUserProfile}
            />

            {/* Token & connection control */}
            <div>
              <MachineTokenCard
                machine={machine}
                canManage={canManage}
                onRevoke={handleRevokeToken}
                onForce={handleForceDisconnect}
                onTransfer={openTransferPicker}
              />
            </div>

            {/* Who can create agents on this machine */}
            {canManage && (
              <MachineAccessCard
                accessError={accessError}
                members={agentCreatorMembers}
                users={users}
                groups={groups}
                onManage={openAccess}
              />
            )}
          </div>

          {/* Providers + agent roster */}
          <div className="flex flex-col gap-6">
            <MachineProvidersCard
              providers={availableProviders}
              canManage={canManage}
              onRefresh={handleRefreshProviders}
            />

            <MachineAgentRoster
              agents={agents}
              agentsLoading={agentsLoading}
              canCreateAgent={canCreateAgent}
              isDesktop={isDesktop}
              onAddAgent={openAddAgent}
              onOpenAgent={openAgentProfile}
            />
          </div>
        </div>
      </div>

      {/* Add-agent sheet */}
      <MachineAddAgentSheet
        open={addOpen}
        machineName={machineName}
        machineTitle={machine.title}
        canCreateAgent={canCreateAgent}
        availableProviders={availableProviders}
        selfProvidedKeysEnabled={selfProvidedKeysEnabled}
        onCreated={handleAgentCreated}
        onClose={closeAddAgent}
      />

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
      <MachineAccessManageSheet
        open={accessOpen}
        machineTitle={machine.title}
        accessError={accessError}
        saving={accessSaving}
        members={accessMembers}
        users={users}
        groups={groups}
        onClose={closeAccessSheet}
        onAdd={handleAccessAdd}
        onRemove={handleAccessRemove}
        onSave={handleSaveAccess}
      />

      {/* Mobile add-agent FAB: mirrors the chat create-channel FAB on touch
          layouts; the roster footer button stays for desktop. */}
      {canCreateAgent && (
        <button
          type="button"
          onClick={openAddAgent}
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
      <TransferOwnershipDialog
        open={transferOpen}
        onOpenChange={(next) => !next && setTransferOpen(false)}
        users={users}
        excludeUserName={machine.createdBy}
        onTransfer={handleTransfer}
        labels={{
          pickerTitle: t("machine.transfer-owner-title"),
          pickerDescription: t("machine.transfer-owner-description"),
          targetLabel: t("machine.transfer-owner-target"),
          targetPlaceholder: t("machine.transfer-owner-target-placeholder"),
          reasonLabel: t("machine.transfer-owner-reason"),
          reasonPlaceholder: t("machine.transfer-owner-reason-placeholder"),
          confirmTitle: t("machine.transfer-owner-confirm-title"),
          confirmDescription: (targetTitle) =>
            t("machine.transfer-owner-confirm-description", {
              target: targetTitle,
            }),
          confirmAction: t("machine.transfer-owner-confirm"),
        }}
      />
    </div>
  );
}
