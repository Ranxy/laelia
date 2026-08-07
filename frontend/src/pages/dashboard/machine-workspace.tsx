import { FolderTree, Loader2, RefreshCw, Trash } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { formatBytes } from "@/components/chat/file-card";
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatTimestamp } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { MACHINE_ROUTE_PROFILE } from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type {
  Machine,
  MachineWorkspaceSummary,
} from "@/types/proto-es/v1/machine_pb";

// MachineWorkspacePage lists every agent workspace directory on the machine
// with usage stats and a destructive delete action (owner/admin only, gated on
// machine.canManage like the layout's tab).
export function MachineWorkspacePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId } = useParams<{ machineId: string }>();
  const machineName = `machines/${machineId ?? ""}`;
  const getMachine = useAppStore((s) => s.getMachine);
  const listMachineWorkspaces = useAppStore((s) => s.listMachineWorkspaces);
  const deleteMachineWorkspace = useAppStore((s) => s.deleteMachineWorkspace);

  const [machine, setMachine] = useState<Machine | undefined>(undefined);
  const [checking, setChecking] = useState(true);
  const [workspaces, setWorkspaces] = useState<MachineWorkspaceSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [confirmTarget, setConfirmTarget] =
    useState<MachineWorkspaceSummary | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!machineId) return;
    getMachine(machineName).then((m) => {
      if (cancelled) return;
      setMachine(m);
      setChecking(false);
      if (!m?.canManage) {
        navigate(resolvePath(MACHINE_ROUTE_PROFILE, { machineId }), {
          replace: true,
        });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [machineId, machineName, getMachine, navigate]);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(false);
    try {
      setWorkspaces(await listMachineWorkspaces(machineName));
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, [machineName, listMachineWorkspaces]);

  useEffect(() => {
    if (machine?.canManage) void load();
  }, [machine?.canManage, load]);

  async function confirmDelete() {
    if (!confirmTarget) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await deleteMachineWorkspace(machineName, confirmTarget.directoryName);
      toastManager.add({
        type: "success",
        title: t("workspace.deleted"),
      });
      setConfirmTarget(null);
      await load();
    } catch (err) {
      setDeleteError(
        err instanceof Error ? err.message : t("workspace.delete-error")
      );
    } finally {
      setDeleting(false);
    }
  }

  if (checking || !machine?.canManage) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-5 animate-spin text-control-light" />
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 text-base font-semibold text-main">
            <FolderTree className="size-4" />
            {t("machine.tab-workspace")}
          </h2>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void load()}
            disabled={loading}
          >
            <RefreshCw className="size-4" />
            {t("workspace.refresh")}
          </Button>
        </div>
        {loadError ? (
          <Alert variant="error" description={t("workspace.load-error")} />
        ) : loading ? (
          <div className="flex items-center gap-2 py-6 text-sm text-control-light">
            <Loader2 className="size-4 animate-spin" />
            {t("workspace.loading")}
          </div>
        ) : workspaces.length === 0 ? (
          <p className="py-6 text-sm text-control-light">
            {t("workspace.no-workspaces")}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("workspace.directory")}</TableHead>
                <TableHead>{t("workspace.size")}</TableHead>
                <TableHead>{t("workspace.file-count")}</TableHead>
                <TableHead>{t("workspace.last-modified")}</TableHead>
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {workspaces.map((ws) => (
                <TableRow key={ws.directoryName}>
                  <TableCell className="font-mono">
                    {ws.directoryName}
                  </TableCell>
                  <TableCell>{formatBytes(ws.totalSizeBytes)}</TableCell>
                  <TableCell>{ws.fileCount.toString()}</TableCell>
                  <TableCell>{formatTimestamp(ws.lastModified)}</TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-danger"
                      onClick={() => {
                        setDeleteError("");
                        setConfirmTarget(ws);
                      }}
                    >
                      <Trash className="size-4" />
                      {t("workspace.delete")}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <AlertDialog
        open={!!confirmTarget}
        onOpenChange={(open) => !open && setConfirmTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>{t("workspace.delete-confirm")}</AlertDialogTitle>
          <AlertDialogDescription>
            {confirmTarget?.directoryName}
          </AlertDialogDescription>
          {deleteError && <Alert variant="error" description={deleteError} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={deleting}
              onClick={() => void confirmDelete()}
            >
              {deleting && <Loader2 className="size-4 animate-spin" />}
              {t("workspace.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
