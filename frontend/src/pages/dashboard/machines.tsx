import { Plus, Trash } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  matchPath,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from "react-router-dom";
import { MachineConnectionBadge } from "@/components/machine-connection-badge";
import { ProvisioningPhaseBadge } from "@/components/provisioning-phase-badge";
import { RailRow } from "@/components/rail-row";
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
import { TwoPaneShell } from "@/components/ui/two-pane-shell";
import { usePolling } from "@/hooks/use-polling";
import { useWorkspacePolicy } from "@/hooks/use-workspace-policy";
import { describeError } from "@/lib/connect-errors";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import {
  MachineStatus_ConnectionState,
  ProvisioningPhase,
} from "@/types/proto-es/v1/machine_pb";
export function MachinesPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId: selectedMachineId } = useParams<{ machineId: string }>();
  const location = useLocation();
  // The create-machine route has no :machineId param, but its page (install +
  // setup commands) renders in the same detail pane as a selected machine and
  // must count as an open detail, or the pane stays hidden on touch layouts.
  const detailOpen =
    Boolean(selectedMachineId) ||
    matchPath("/machines/new", location.pathname) != null;
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const machines = useAppStore((s) => s.machines);
  const loading = useAppStore((s) => s.machinesLoading);
  // Gate the create entry on the exact permission its flow requires
  // (laelia.machines.create) or the workspace policy that lets ordinary users
  // create their own machines; per-machine canDelete (creator or
  // laelia.machines.delete) is populated by ListMachines.
  const hasCreatePermission = useHasPermission("laelia.machines.create");
  // The machine-creation policy is public workspace info, so ordinary users
  // can read it without admin settings access. Defaults to allowed while the
  // request is in flight (shared cache — see use-workspace-policy).
  const { userCreateMachineDisallowed } = useWorkspacePolicy();
  const allowUserCreateMachine = !userCreateMachineDisallowed;
  const canCreate = hasCreatePermission || allowUserCreateMachine;
  const [listScrolled, setListScrolled] = useState(false);
  const [actionError, setActionError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{
    name: string;
    title: string;
  } | null>(null);
  const [deleting, setDeleting] = useState(false);
  // undefined = not provisioned / still resolving; true/false mirrors the
  // bound provisioner's retain-data policy once GetProvisioner responds.
  const [deleteRetainData, setDeleteRetainData] = useState<boolean | undefined>(
    undefined
  );
  const [deleteProvisioned, setDeleteProvisioned] = useState(false);

  // A provisioned machine's delete confirmation states the data-volume
  // consequence per the provisioner's retention policy (design §10.3). The
  // summary row only carries the provisioning phase, so resolving the policy
  // needs the full machine (for the provisioner binding) + the provisioner
  // (for status.retainData); without the permission or on failure the dialog
  // keeps a generic workload-teardown line. Runs once per opened target.
  useEffect(() => {
    let cancelled = false;
    setDeleteRetainData(undefined);
    setDeleteProvisioned(false);
    if (!deleteTarget) return;
    const summary = useAppStore
      .getState()
      .machines.find((m) => m.name === deleteTarget.name);
    const phase = summary?.provisioning?.phase;
    if (!phase || phase === ProvisioningPhase.DELETED) {
      return;
    }
    setDeleteProvisioned(true);
    void (async () => {
      const machine = await useAppStore
        .getState()
        .getMachine(deleteTarget.name);
      if (cancelled || !machine?.provisioner) return;
      const provisioner = await useAppStore
        .getState()
        .getProvisioner(machine.provisioner);
      if (cancelled) return;
      setDeleteRetainData(provisioner?.status?.retainData);
    })();
    return () => {
      cancelled = true;
    };
  }, [deleteTarget]);

  const load = useCallback(() => {
    fetchMachines({ pageSize: 100 });
  }, [fetchMachines]);

  useEffect(() => {
    load();
  }, [load]);

  // Refresh while any machine is not yet online so the list flips to "online"
  // promptly once the machine app connects. Silent refreshes skip the loading
  // flag and skip the state update when nothing changed.
  const anyNonOnline = machines.some(
    (m) => m.status?.state !== MachineStatus_ConnectionState.ONLINE
  );
  // Machine connection-state transitions are not time-critical; 10s (was 3s)
  // still flips the list to "online" promptly once the machine app connects
  // while keeping the poll traffic during an outage at ~6 req/min instead of
  // ~20. Gated via enabled: only polls while any machine is offline.
  usePolling(
    () => {
      void fetchMachines({ pageSize: 100 }, { silent: true });
    },
    10000,
    { enabled: anyNonOnline }
  );

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    const deleteMachine = useAppStore.getState().deleteMachine;
    setDeleting(true);
    try {
      await deleteMachine(deleteTarget.name);
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } catch (err) {
      setActionError(describeError(err));
    } finally {
      setDeleting(false);
    }
  }

  return (
    <>
      <TwoPaneShell
        detailOpen={detailOpen}
        width="w-56"
        railClassName="overflow-hidden"
        rail={
          <>
            {/* Left rail: machine list. */}
            <div className="hidden lg:flex items-center justify-between gap-2 border-b border-control-border px-3 py-3 shrink-0">
              <h1 className="hidden lg:block text-sm font-semibold text-main truncate">
                {t("machine.title")}
              </h1>
              {canCreate && (
                <Button size="sm" onClick={() => navigate("/machines/new")}>
                  {t("machine.create")}
                </Button>
              )}
            </div>

            <div
              className="flex-1 overflow-y-auto py-1"
              onScroll={(e) => setListScrolled(e.currentTarget.scrollTop > 8)}
            >
              {loading ? (
                <p className="px-3 py-2 text-sm text-control-light">
                  {t("common.loading")}
                </p>
              ) : machines.length === 0 ? (
                <p className="px-3 py-2 text-sm text-control-light">
                  {t("common.no-data")}
                </p>
              ) : (
                <ul className="flex flex-col divide-y divide-control-border/50">
                  {machines.map((machine) => {
                    const resourceId = machine.name.replace(/^machines\//, "");
                    const selected = resourceId === selectedMachineId;
                    // In-flight provisioning marker for provisioned machines;
                    // once the machine connects the connection badge alone
                    // tells the story.
                    const provisioningPhase = machine.provisioning?.phase;
                    const showProvisioningBadge =
                      provisioningPhase === ProvisioningPhase.PENDING ||
                      provisioningPhase === ProvisioningPhase.PROVISIONING ||
                      provisioningPhase === ProvisioningPhase.FAILED;
                    return (
                      <li key={machine.name}>
                        <RailRow
                          selected={selected}
                          label={t("machine.row-open-detail", {
                            title: machine.title,
                          })}
                          onSelect={() => navigate(`/machines/${resourceId}`)}
                        >
                          <div className="min-w-0 flex-1 flex flex-col gap-1">
                            <span className="truncate text-sm font-medium text-main">
                              {machine.title}
                            </span>
                            <div className="flex items-center gap-2">
                              <MachineConnectionBadge
                                state={machine.status?.state}
                              />
                              {showProvisioningBadge && (
                                <ProvisioningPhaseBadge
                                  phase={provisioningPhase}
                                />
                              )}
                              {machine.upgradeAvailable && (
                                <Badge variant="warning" className="text-xs">
                                  {t("machine.upgrade-badge")}
                                </Badge>
                              )}
                              <span className="text-xs text-control-light">
                                {t("machine.agent-count", {
                                  count: machine.agentCount,
                                })}
                              </span>
                            </div>
                          </div>
                          {machine.canDelete && (
                            <Button
                              variant="ghost"
                              size="sm"
                              className="size-6 shrink-0 p-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                              aria-label={t("common.delete")}
                              onClick={(e) => {
                                e.stopPropagation();
                                setDeleteTarget({
                                  name: machine.name,
                                  title: machine.title,
                                });
                                setDeleteOpen(true);
                              }}
                            >
                              <Trash className="size-3.5" />
                            </Button>
                          )}
                        </RailRow>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>

            {/* Mobile create-machine FAB: mirrors the chat create-channel FAB on
                touch layouts; the header button stays for desktop. */}
            {canCreate && (
              <button
                type="button"
                onClick={() => navigate("/machines/new")}
                aria-label={t("machine.create")}
                data-testid="create-machine-fab"
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
                    {t("machine.fab-label")}
                  </span>
                )}
              </button>
            )}
          </>
        }
      >
        {/* Right pane: machine detail (or empty state). */}
        <Outlet />
      </TwoPaneShell>

      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("machine.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("machine.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
            {deleteProvisioned && (
              <>
                <br />
                {deleteRetainData === undefined
                  ? t("machine.delete-provisioned-generic")
                  : deleteRetainData
                    ? t("machine.delete-provisioned-retain")
                    : t("machine.delete-provisioned-delete")}
              </>
            )}
          </AlertDialogDescription>
          {actionError && (
            <Alert variant="error" description={actionError} className="mt-2" />
          )}
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
    </>
  );
}
