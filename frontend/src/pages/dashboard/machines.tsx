import { Plus, Trash } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useNavigate, useParams } from "react-router-dom";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { settingServiceClient } from "@/connect";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

export function MachinesPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId: selectedMachineId } = useParams<{ machineId: string }>();
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const machines = useAppStore((s) => s.machines);
  const loading = useAppStore((s) => s.machinesLoading);
  // Gate the create entry on the exact permission its flow requires
  // (laelia.machines.create) or the workspace policy that lets ordinary users
  // create their own machines; per-machine canDelete (creator or
  // laelia.machines.delete) is populated by ListMachines.
  const hasCreatePermission = useHasPermission("laelia.machines.create");
  const [allowUserCreateMachine, setAllowUserCreateMachine] = useState(true);
  const canCreate = hasCreatePermission || allowUserCreateMachine;
  const [listScrolled, setListScrolled] = useState(false);
  const [actionError, setActionError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{
    name: string;
    title: string;
  } | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(() => {
    fetchMachines({ pageSize: 100 });
  }, [fetchMachines]);

  useEffect(() => {
    load();
  }, [load]);

  // The machine-creation policy is public workspace info, so ordinary users
  // can read it without admin settings access. Default to enabled while the
  // request is in flight.
  useEffect(() => {
    let cancelled = false;
    void settingServiceClient
      .getWorkspaceInfo({})
      .then((res) => {
        if (!cancelled) {
          setAllowUserCreateMachine(!res.disallowUserCreateMachine);
        }
      })
      .catch(() => {
        if (!cancelled) setAllowUserCreateMachine(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Refresh while any machine is not yet online so the list flips to "online"
  // promptly once the machine app connects. Silent refreshes skip the loading
  // flag and skip the state update when nothing changed.
  const anyNonOnline = machines.some(
    (m) => m.status?.state !== MachineStatus_ConnectionState.ONLINE
  );
  // Machine connection-state transitions are not time-critical; 10s (was 3s)
  // still flips the list to "online" promptly once the machine app connects
  // while keeping the poll traffic during an outage at ~6 req/min instead of
  // ~20.
  useEffect(() => {
    if (!anyNonOnline) return;
    const id = setInterval(
      () => fetchMachines({ pageSize: 100 }, { silent: true }),
      10000
    );
    return () => clearInterval(id);
  }, [anyNonOnline, fetchMachines]);

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
      setActionError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Left rail: machine list. */}
      <aside
        className={cn(
          "shrink-0 flex-col border-r border-control-border overflow-hidden",
          selectedMachineId ? "hidden lg:flex lg:w-56" : "flex w-full lg:w-56"
        )}
      >
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
                return (
                  <li key={machine.name}>
                    <div
                      role="button"
                      tabIndex={0}
                      aria-label={t("machine.row-open-detail", {
                        title: machine.title,
                      })}
                      className={cn(
                        "group flex cursor-pointer items-center gap-2 px-3 py-2 transition-colors",
                        "border-l-2",
                        selected
                          ? "border-l-accent bg-control-bg"
                          : "border-l-transparent hover:bg-control-bg/60"
                      )}
                      onClick={() => navigate(`/machines/${resourceId}`)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          navigate(`/machines/${resourceId}`);
                        }
                      }}
                    >
                      <div className="min-w-0 flex-1 flex flex-col gap-1">
                        <span className="truncate text-sm font-medium text-main">
                          {machine.title}
                        </span>
                        <div className="flex items-center gap-2">
                          <MachineConnectionBadge
                            state={machine.status?.state}
                          />
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
                    </div>
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
      </aside>

      {/* Right pane: machine detail (or empty state). */}
      <div
        className={cn(
          "min-w-0 flex-1 overflow-hidden",
          !selectedMachineId && "hidden lg:block"
        )}
      >
        <Outlet />
      </div>

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
    </div>
  );
}
