import { Loader2, Plus, Shield, User as UserIcon, X } from "lucide-react";
import { memo, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ConnectionBadge } from "@/components/connection-badge";
import { CopyableCommand } from "@/components/copyable-command";
import { MachineConnectionBadge } from "@/components/machine-connection-badge";
import { MemberPicker } from "@/components/member-picker";
import { Card, Field, providerDisplayName } from "@/components/profile-common";
import { ProvisioningPhaseBadge } from "@/components/provisioning-phase-badge";
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
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { describeError } from "@/lib/connect-errors";
import {
  buildMachineInstallCommand,
  buildMachineSetupCommand,
  machineInstallOSFromInfo,
} from "@/lib/machine-token";
import { formatTimestamp } from "@/lib/time-format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type {
  AgentProviderInfo,
  AgentSummary,
} from "@/types/proto-es/v1/agent_pb";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import {
  type Machine,
  MachineStatus_ConnectionState,
} from "@/types/proto-es/v1/machine_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// ---------------------------------------------------------------------------
// Static-ish machine profile cards, extracted (03 goal tree) so form typing in
// the add-agent sheet re-renders only the sheet. Each card is memoized; the
// page keeps data loading, the dialogs cluster and the transfer flow.
// ---------------------------------------------------------------------------

// TOKEN_ACTION_BTN sizes the Token & Connection action buttons: large
// full-width touch targets on phones, the compact sm row on sm+.
const TOKEN_ACTION_BTN =
  "h-9 w-full px-3 text-sm leading-5 sm:h-7 sm:w-auto sm:px-2 sm:text-xs sm:leading-4";

// useMemberLabel resolves an IAM member reference to its display label
// (users → title/email, groups → title/email, allUsers → localized label).
// The machine page's semantics differ from lib/members' (email-first) labels,
// so this page-local variant is kept verbatim here.
function useMemberLabel(users: User[], groups: Group[]) {
  const { t } = useTranslation();
  return useCallback(
    (member: string): string => {
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
        return g
          ? g.title || g.email || member
          : member.slice("groups/".length);
      }
      return member;
    },
    [t, users, groups]
  );
}

// ---- Provisioning (provisioned machines) -------------------------------------
// The lifecycle card for machines created through a provisioner: workload
// locator, phase pill, timestamps and the last error. The provisioner's
// display title/backend are resolved client-side via GetProvisioner; without
// laelia.provisioners.get the raw resource name still renders.

interface MachineProvisioningCardProps {
  machine: Machine;
}

export const MachineProvisioningCard = memo(function MachineProvisioningCard({
  machine,
}: MachineProvisioningCardProps) {
  const { t } = useTranslation();
  const getProvisioner = useAppStore((s) => s.getProvisioner);
  const provisioning = machine.provisioning;
  const [provTitle, setProvTitle] = useState("");
  const [backend, setBackend] = useState("");
  const [retainData, setRetainData] = useState<boolean | undefined>(undefined);

  // Resolve the provisioner's friendly identity once per bound provisioner;
  // a failed lookup (no permission, provisioner deleted) falls back to the
  // raw resource name rather than hiding the card.
  useEffect(() => {
    let cancelled = false;
    setProvTitle("");
    setBackend("");
    setRetainData(undefined);
    if (!machine.provisioner) return;
    void getProvisioner(machine.provisioner).then((p) => {
      if (cancelled || !p) return;
      setProvTitle(p.title || machine.provisioner);
      setBackend(p.status?.backend || p.backend);
      setRetainData(p.status?.retainData);
    });
    return () => {
      cancelled = true;
    };
  }, [machine.provisioner, getProvisioner]);

  return (
    <Card title={t("machine.provisioning.title")}>
      <div className="flex flex-col gap-3">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <Field label={t("machine.provisioning.provisioner")}>
            {provTitle || machine.provisioner}
          </Field>
          {backend && (
            <Field label={t("machine.provisioning.backend")}>{backend}</Field>
          )}
          <Field label={t("machine.provisioning.phase")}>
            <ProvisioningPhaseBadge phase={provisioning?.phase} />
          </Field>
          {provisioning?.workloadName && (
            <Field label={t("machine.provisioning.workload")}>
              <span className="font-mono text-xs">
                {provisioning.workloadName}
              </span>
            </Field>
          )}
          {provisioning?.pendingAt && (
            <Field label={t("machine.provisioning.pending-at")}>
              {formatTimestamp(provisioning.pendingAt)}
            </Field>
          )}
          {provisioning?.provisionedAt && (
            <Field label={t("machine.provisioning.provisioned-at")}>
              {formatTimestamp(provisioning.provisionedAt)}
            </Field>
          )}
          {provisioning?.failedAt && (
            <Field label={t("machine.provisioning.failed-at")}>
              {formatTimestamp(provisioning.failedAt)}
            </Field>
          )}
        </dl>
        {retainData !== undefined && (
          <p className="text-xs text-control-light">
            {retainData
              ? t("machine.provisioning.retain-data")
              : t("machine.provisioning.delete-data")}
          </p>
        )}
        {provisioning?.error && (
          <Alert variant="error" description={provisioning.error} />
        )}
      </div>
    </Card>
  );
});

// ---- Identity & host info ---------------------------------------------------

interface MachineIdentityCardProps {
  machine: Machine;
  userTitle: (name: string) => string;
  onOpenUser: (userId: string) => void;
}

export const MachineIdentityCard = memo(function MachineIdentityCard({
  machine,
  userTitle,
  onOpenUser,
}: MachineIdentityCardProps) {
  const { t } = useTranslation();
  const info = machine.info;

  return (
    <Card title={t("machine.profile.section-identity")}>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
        <Field label={t("machine.detail-name")}>{machine.title}</Field>
        {machine.createdBy && (
          <Field label={t("machine.detail-owner")}>
            <button
              type="button"
              className="text-sm text-link hover:underline"
              onClick={() =>
                onOpenUser(machine.createdBy.replace(/^users\//, ""))
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
          <Field label={t("machine.detail-hostname")}>{info.hostname}</Field>
        )}
        {info?.os && (
          <Field label={t("machine.detail-os")}>
            {info.os}/{info.arch ?? ""}
          </Field>
        )}
        {info?.ip && <Field label={t("machine.detail-ip")}>{info.ip}</Field>}
        {info?.version && (
          <Field label={t("machine.detail-version")}>{info.version}</Field>
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
  );
});

// ---- Token & connection control ---------------------------------------------
// The card owns its confirm dialogs (revoke/force) and the copy buttons; the
// page supplies the token-revocation / force-disconnect flows.

interface MachineTokenCardProps {
  machine: Machine;
  canManage: boolean;
  onRevoke: () => Promise<void>;
  onForce: () => Promise<void>;
  onTransfer: () => void;
}

export const MachineTokenCard = memo(function MachineTokenCard({
  machine,
  canManage,
  onRevoke,
  onForce,
  onTransfer,
}: MachineTokenCardProps) {
  const { t } = useTranslation();
  const [actionError, setActionError] = useState("");
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [forceOpen, setForceOpen] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [forcing, setForcing] = useState(false);
  const [installCopied, setInstallCopied] = useState(false);
  const [setupCopied, setSetupCopied] = useState(false);

  // Offline reconnection commands. They mirror the new-machine page: the
  // install command depends on the machine's reported OS, while the setup
  // command is the same everywhere.
  const installOS = machineInstallOSFromInfo(machine.info?.os);
  const installCommand = installOS ? buildMachineInstallCommand(installOS) : "";
  const setupCommand = buildMachineSetupCommand();
  const isOffline =
    machine.status?.state === MachineStatus_ConnectionState.OFFLINE;
  // Provisioned machines are created and managed by a provisioner: they
  // authenticate via a provisioner-seeded credential (no device-code flow), so
  // the manual reconnection commands and the token/connection actions are
  // meaningless and must not be offered.
  const isProvisioned =
    machine.provisioner !== "" && machine.provisioner !== undefined;

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

  async function handleRevoke() {
    setRevoking(true);
    setActionError("");
    try {
      await onRevoke();
      setRevokeOpen(false);
    } catch (err) {
      setActionError(describeError(err));
    } finally {
      setRevoking(false);
    }
  }

  async function handleForce() {
    setForcing(true);
    setActionError("");
    try {
      await onForce();
      setForceOpen(false);
    } catch (err) {
      setActionError(describeError(err));
    } finally {
      setForcing(false);
    }
  }

  return (
    <>
      <Card title={t("machine.profile.section-token")}>
        {actionError && <Alert variant="error" description={actionError} />}
        {!canManage ? (
          <p className="text-xs text-control-light">
            {t("machine.profile.edit-not-allowed")}
          </p>
        ) : (
          <div className="flex flex-col gap-4">
            {isProvisioned ? (
              <p className="text-sm text-control-light">
                {t("machine.profile.provisioned-managed-note")}
              </p>
            ) : (
              isOffline && (
                <div className="flex flex-col gap-4">
                  {installCommand && (
                    <div className="flex flex-col gap-2">
                      <p className="text-sm text-control-light">
                        {t("machine.profile.offline-install-note")}
                      </p>
                      <p className="text-sm text-control-light">
                        {t("machine.profile.offline-install-hint")}
                      </p>
                      <CopyableCommand
                        command={installCommand}
                        copied={installCopied}
                        onCopy={() => void handleCopyInstall()}
                      />
                    </div>
                  )}
                  <div className="flex flex-col gap-2">
                    <p className="text-sm text-control-light">
                      {t("machine.profile.offline-command-hint")}
                    </p>
                    <CopyableCommand
                      command={setupCommand}
                      copied={setupCopied}
                      onCopy={() => void handleCopySetup()}
                    />
                  </div>
                </div>
              )
            )}
            {/* Management actions. On touch layouts the buttons stack
                full-width (large, well-separated targets) with the two
                destructive ones error-tinted; from sm up they share one
                compact row. Provisioned machines only offer ownership
                transfer — token revocation and force-disconnect are
                provisioner-managed. */}
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              {!isProvisioned && (
                <Button
                  variant="destructive-outline"
                  className={TOKEN_ACTION_BTN}
                  onClick={() => {
                    setActionError("");
                    setRevokeOpen(true);
                  }}
                >
                  {t("machine.revoke-token")}
                </Button>
              )}
              {!isProvisioned && (
                <Button
                  variant="destructive-outline"
                  className={TOKEN_ACTION_BTN}
                  onClick={() => {
                    setActionError("");
                    setForceOpen(true);
                  }}
                >
                  {t("machine.force-disconnect")}
                </Button>
              )}
              <Button
                variant="outline"
                className={TOKEN_ACTION_BTN}
                onClick={onTransfer}
              >
                {t("machine.transfer-owner")}
              </Button>
            </div>
          </div>
        )}
      </Card>

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
            <Button disabled={revoking} onClick={() => void handleRevoke()}>
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
            <Button disabled={forcing} onClick={() => void handleForce()}>
              {forcing ? t("common.loading") : t("machine.force-disconnect")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
});

// ---- Who can create agents on this machine ----------------------------------

interface MachineAccessCardProps {
  accessError: string;
  members: string[];
  users: User[];
  groups: Group[];
  onManage: () => void;
}

export const MachineAccessCard = memo(function MachineAccessCard({
  accessError,
  members,
  users,
  groups,
  onManage,
}: MachineAccessCardProps) {
  const { t } = useTranslation();
  const memberLabel = useMemberLabel(users, groups);

  return (
    <Card
      title={t("machine.access-title")}
      footer={
        <div className="flex items-center justify-end">
          <Button variant="outline" size="sm" onClick={onManage}>
            {t("machine.access-manage")}
          </Button>
        </div>
      }
    >
      {accessError && <Alert variant="error" description={accessError} />}
      {members.length === 0 ? (
        <p className="text-xs text-control-light">
          {t("machine.access-no-members")}
        </p>
      ) : (
        <ul className="flex flex-wrap gap-1.5">
          {members.map((m) => (
            <li key={m}>
              <Badge variant="secondary">{memberLabel(m)}</Badge>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
});

// ---- Providers ---------------------------------------------------------------

interface MachineProvidersCardProps {
  providers: AgentProviderInfo[];
  canManage: boolean;
  onRefresh: () => Promise<void>;
}

export const MachineProvidersCard = memo(function MachineProvidersCard({
  providers,
  canManage,
  onRefresh,
}: MachineProvidersCardProps) {
  const { t } = useTranslation();
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState("");

  async function handleRefresh() {
    setRefreshing(true);
    setRefreshError("");
    try {
      await onRefresh();
    } catch (err) {
      setRefreshError(describeError(err));
    } finally {
      setRefreshing(false);
    }
  }

  return (
    <Card
      title={t("machine.providers")}
      footer={
        <div className="flex items-center justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={refreshing || !canManage}
            onClick={() => void handleRefresh()}
          >
            {refreshing ? <Loader2 className="size-3.5 animate-spin" /> : null}
            {refreshing ? t("common.loading") : t("machine.refresh-providers")}
          </Button>
        </div>
      }
    >
      {refreshError && <Alert variant="error" description={refreshError} />}
      {providers.length === 0 ? (
        <p className="text-xs text-control-light">
          {t("machine.no-providers")}
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {providers.map((p) => (
            <li key={p.providerId} className="text-sm text-main">
              {providerDisplayName(p)}
              {p.compatible === false && p.incompatibilityReason
                ? ` — ${p.incompatibilityReason}`
                : ""}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
});

// ---- Agent roster -------------------------------------------------------------

interface MachineAgentRosterProps {
  agents: AgentSummary[];
  agentsLoading: boolean;
  canCreateAgent: boolean;
  isDesktop: boolean;
  onAddAgent: () => void;
  onOpenAgent: (resourceId: string) => void;
}

export const MachineAgentRoster = memo(function MachineAgentRoster({
  agents,
  agentsLoading,
  canCreateAgent,
  isDesktop,
  onAddAgent,
  onOpenAgent,
}: MachineAgentRosterProps) {
  const { t } = useTranslation();

  return (
    <Card
      title={t("machine.agent-roster")}
      footer={
        isDesktop ? (
          <div className="flex items-center justify-end gap-2">
            <Button size="sm" disabled={!canCreateAgent} onClick={onAddAgent}>
              <Plus className="size-3.5" />
              {t("machine.add-agent")}
            </Button>
          </div>
        ) : undefined
      }
    >
      {agentsLoading ? (
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      ) : agents.length === 0 ? (
        <p className="text-sm text-control-light">{t("machine.no-agents")}</p>
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
                  onClick={() => onOpenAgent(resourceId)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onOpenAgent(resourceId);
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
  );
});

// ---- Manage access (who may create agents) -----------------------------------
// Presentational sheet: the member-selection state and the etag-guarded save
// flow stay in the page (machine-profile.tsx).

interface MachineAccessManageSheetProps {
  open: boolean;
  machineTitle: string;
  accessError: string;
  saving: boolean;
  members: Set<string>;
  users: User[];
  groups: Group[];
  onClose: () => void;
  onAdd: (member: string) => void;
  onRemove: (member: string) => void;
  onSave: () => void;
}

export const MachineAccessManageSheet = memo(function MachineAccessManageSheet({
  open,
  machineTitle,
  accessError,
  saving,
  members,
  users,
  groups,
  onClose,
  onAdd,
  onRemove,
  onSave,
}: MachineAccessManageSheetProps) {
  const { t } = useTranslation();
  const memberLabel = useMemberLabel(users, groups);

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <SheetContent width="medium">
        <SheetHeader>
          <SheetTitle>{t("machine.access-manage-title")}</SheetTitle>
          <SheetDescription>
            {t("machine.access-manage-description", { title: machineTitle })}
          </SheetDescription>
        </SheetHeader>
        <SheetBody>
          {accessError && (
            <Alert variant="error" description={accessError} className="mb-2" />
          )}
          <div className="flex flex-col gap-5">
            {/* Current members */}
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-semibold uppercase tracking-wide text-control">
                {t("machine.access-current-members")}
              </label>
              <div className="max-h-64 overflow-y-auto pr-1">
                {members.size === 0 ? (
                  <p className="text-sm text-control-light py-4 text-center border border-dashed border-control-border rounded-xs">
                    {t("machine.access-no-members")}
                  </p>
                ) : (
                  <div className="flex flex-col gap-2">
                    {[...members]
                      .sort((a, b) =>
                        (memberLabel(a) ?? a).localeCompare(memberLabel(b) ?? b)
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
                              onClick={() => onRemove(member)}
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
                users={users.filter((u) => !members.has(u.name ?? ""))}
                groups={groups.filter(
                  (g) =>
                    !members.has(g.name ?? "") &&
                    !(g.email && members.has(`groups/${g.email}`))
                )}
                value=""
                onSelect={onAdd}
              />
            </div>
          </div>
        </SheetBody>
        <SheetFooter>
          <Button variant="outline" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button disabled={saving} onClick={onSave}>
            {saving ? <Loader2 className="size-4 animate-spin" /> : null}
            {t("common.save")}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
});
