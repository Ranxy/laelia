import { Trash } from "lucide-react";
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
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { buildMachineRunCommand } from "@/lib/machine-token";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

export function MachinesPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId: selectedMachineId } = useParams<{ machineId: string }>();
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const machines = useAppStore((s) => s.machines);
  const loading = useAppStore((s) => s.machinesLoading);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");
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

  // Refresh while any machine is not yet online so the list flips to "online"
  // promptly once the machine app connects. Silent refreshes skip the loading
  // flag and skip the state update when nothing changed.
  const anyNonOnline = machines.some(
    (m) => m.status?.state !== MachineStatus_ConnectionState.ONLINE
  );
  useEffect(() => {
    if (!anyNonOnline) return;
    const id = setInterval(
      () => fetchMachines({ pageSize: 100 }, { silent: true }),
      3000
    );
    return () => clearInterval(id);
  }, [anyNonOnline, fetchMachines]);

  async function handleCreate() {
    setCreateError("");
    if (!name.trim()) {
      setCreateError(t("machine.create-name-required"));
      return;
    }
    setCreating(true);
    try {
      const createMachine = useAppStore.getState().createMachine;
      const res = await createMachine(name.trim());
      setToken(res.registrationToken);
      setTokenOpen(true);
      setName("");
      setCreateOpen(false);
      load();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

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
      setCreateError(err instanceof Error ? err.message : String(err));
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
        <div className="flex items-center justify-between gap-2 border-b border-control-border px-3 py-3 shrink-0">
          <h1 className="text-sm font-semibold text-main truncate">
            {t("machine.title")}
          </h1>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            {t("machine.create")}
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto py-1">
          {loading ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("common.loading")}
            </p>
          ) : machines.length === 0 ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("common.no-data")}
            </p>
          ) : (
            <ul className="flex flex-col">
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
                          ? "border-accent bg-control-bg"
                          : "border-transparent hover:bg-control-bg/60"
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
                          <span className="text-xs text-control-light">
                            {t("machine.agent-count", {
                              count: machine.agentCount,
                            })}
                          </span>
                        </div>
                      </div>
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
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
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

      <Sheet
        open={createOpen}
        onOpenChange={(next) => {
          setCreateOpen(next);
          if (!next) setCreateError("");
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{t("machine.create-title")}</SheetTitle>
            <SheetDescription>
              {t("machine.create-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {createError && (
              <Alert
                variant="error"
                description={createError}
                className="mb-2"
              />
            )}
            <div className="flex flex-col gap-5">
              <FieldRow
                label={t("machine.field-name")}
                htmlFor="create-machine-name"
              >
                <Input
                  id="create-machine-name"
                  value={name}
                  placeholder={t("machine.create-name-placeholder")}
                  onChange={(e) => {
                    setName(e.target.value);
                    setCreateError("");
                  }}
                />
              </FieldRow>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setCreateOpen(false)}
              disabled={creating}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={creating || !name.trim()} onClick={handleCreate}>
              {creating ? t("common.creating") : t("common.create")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <Dialog
        open={tokenOpen}
        onOpenChange={(next) => !next && setTokenOpen(false)}
      >
        <DialogContent className="max-w-lg">
          <DialogTitle>{t("machine.created-title")}</DialogTitle>
          <DialogDescription>
            {t("machine.created-description")}
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
          {createError && (
            <Alert variant="error" description={createError} className="mt-2" />
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
