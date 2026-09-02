import { Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmActionDialog } from "@/components/settings/confirm-action-dialog";
import { ResourceSheet } from "@/components/settings/resource-sheet";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
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
  Dialog,
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { provisionerServiceClient } from "@/connect";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { describeError } from "@/lib/connect-errors";
import { formatTimestamp } from "@/lib/time-format";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";

// BACKEND_OPTIONS lists the registry's backend types: kubernetes is the only
// implemented backend today; the docker stub shows disabled with a hint
// (design §9 — new backend types plug in without manager changes).
const BACKEND_OPTIONS = [
  {
    value: "kubernetes",
    labelKey: "settings.provisioners.backend-kubernetes",
    disabled: false,
  },
  {
    value: "docker",
    labelKey: "settings.provisioners.backend-docker",
    disabled: true,
  },
];

interface ProvisionerForm {
  title: string;
  backend: string;
  description: string;
}

// TokenDialog is the copy-once display for create/rotate responses: the
// backend keeps only the token's hash, so this is the single chance to copy
// it into the provisioner's config file.
function TokenDialog({
  token,
  title,
  onClose,
}: {
  token: string;
  title: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard unavailable; the token stays selectable in the input.
    }
  }

  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogTitle>{t("settings.provisioners.token-title")}</DialogTitle>
        <DialogDescription>
          {t("settings.provisioners.token-description", { title })}
        </DialogDescription>
        <div className="flex items-center gap-2">
          <SecretInput
            readOnly
            value={token}
            onFocus={(e) => e.target.select()}
          />
          <Button variant="outline" size="sm" onClick={() => void handleCopy()}>
            <Copy className="size-4" />
            {copied ? t("common.copied") : t("common.copy")}
          </Button>
        </div>
        <Alert
          variant="warning"
          description={t("settings.provisioners.token-warning")}
        />
      </DialogContent>
    </Dialog>
  );
}

export function SettingsProvisionersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.provisioners.get");
  const canCreate = useHasPermission("laelia.provisioners.create");
  const canRotateOrDelete = useHasPermission("laelia.provisioners.delete");

  const provisionersQuery = useResourceQuery<Provisioner>({
    enabled: canList,
    queryKey: ["settings", "provisioners"],
    queryFn: async (signal) =>
      (
        await provisionerServiceClient.listProvisioners(
          { pageSize: 1000 },
          { signal }
        )
      ).provisioners ?? [],
    failureTitle: t("settings.provisioners.load-failed"),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  // The one-time token from create/rotate, held with the provisioner title
  // for the copy-once dialog's wording.
  const [tokenShown, setTokenShown] = useState<{
    token: string;
    title: string;
  } | null>(null);

  const [rotateTarget, setRotateTarget] = useState<Provisioner | null>(null);
  const [rotating, setRotating] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<Provisioner | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");

  async function handleCreate(form: ProvisionerForm) {
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.provisioners.title-required"),
      });
      return;
    }
    setCreating(true);
    try {
      const { provisioner, token } = await useAppStore
        .getState()
        .createProvisioner({
          title: form.title.trim(),
          backend: form.backend,
          description: form.description.trim(),
        });
      setCreateOpen(false);
      if (token) {
        setTokenShown({
          token,
          title: provisioner?.title || form.title.trim(),
        });
      }
      void provisionersQuery.reload();
    } catch (err) {
      void showErrorToast(err, t("settings.provisioners.create-failed"));
    } finally {
      setCreating(false);
    }
  }

  async function handleRotate() {
    const target = rotateTarget;
    if (!target) return;
    setRotating(true);
    try {
      const token = await useAppStore
        .getState()
        .rotateProvisionerToken(target.name);
      setRotateTarget(null);
      setTokenShown({ token, title: target.title });
    } catch (err) {
      void showErrorToast(err, t("settings.provisioners.rotate-failed"));
    } finally {
      setRotating(false);
    }
  }

  // The machines-bound refusal must be read while the dialog is open, so the
  // delete confirm is a hand-rolled AlertDialog with inline error presentation
  // (ConfirmActionDialog is failure-dumb by contract).
  async function handleDelete() {
    const target = deleteTarget;
    if (!target) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await useAppStore.getState().deleteProvisioner(target.name);
      setDeleteTarget(null);
      void provisionersQuery.reload();
      toastManager.add({
        type: "success",
        title: t("settings.provisioners.deleted"),
      });
    } catch (err) {
      setDeleteError(describeError(err));
    } finally {
      setDeleting(false);
    }
  }

  if (!canList) {
    return (
      <PermissionNotice message={t("settings.provisioners.not-allowed")} />
    );
  }

  return (
    <SettingsPage
      title={t("settings.provisioners.title")}
      description={t("settings.provisioners.description")}
      actions={
        canCreate && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            {t("settings.provisioners.create")}
          </Button>
        )
      }
    >
      {provisionersQuery.initialLoading ? (
        <PageLoading />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("settings.provisioners.header-title")}</TableHead>
              <TableHead>{t("settings.provisioners.header-backend")}</TableHead>
              <TableHead>{t("settings.provisioners.header-status")}</TableHead>
              <TableHead>{t("settings.provisioners.header-version")}</TableHead>
              <TableHead>
                {t("settings.provisioners.header-machines")}
              </TableHead>
              <TableHead>{t("settings.provisioners.header-created")}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {provisionersQuery.items.map((p) => (
              <TableRow key={p.name}>
                <TableCell className="font-medium text-main">
                  <div className="flex flex-col">
                    <span className="truncate">{p.title}</span>
                    {p.description && (
                      <span className="truncate text-xs text-control-light">
                        {p.description}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">{p.backend}</Badge>
                </TableCell>
                <TableCell>
                  <div className="flex flex-col gap-0.5">
                    {p.status?.connected ? (
                      <Badge variant="success">
                        {t("settings.provisioners.status-connected")}
                      </Badge>
                    ) : (
                      <Badge variant="default">
                        {t("settings.provisioners.status-offline")}
                      </Badge>
                    )}
                    {p.status?.autoUpgrade && (
                      <span className="text-xs text-control-light">
                        {t("settings.provisioners.auto-upgrade")}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell>{p.status?.version || "-"}</TableCell>
                <TableCell>{p.machineCount}</TableCell>
                <TableCell>
                  {p.createdAt ? formatTimestamp(p.createdAt) : "-"}
                </TableCell>
                <TableCell className="text-right">
                  {canRotateOrDelete && (
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setDeleteError("");
                          setRotateTarget(p);
                        }}
                        aria-label={t("settings.provisioners.rotate")}
                        title={t("settings.provisioners.rotate")}
                      >
                        <KeyRound className="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-error"
                        onClick={() => {
                          setDeleteError("");
                          setDeleteTarget(p);
                        }}
                        aria-label={t("common.delete")}
                        title={t("common.delete")}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {provisionersQuery.items.length === 0 &&
              !provisionersQuery.refreshing && (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="py-8 text-center text-control-light"
                  >
                    {t("settings.provisioners.no-provisioners")}
                  </TableCell>
                </TableRow>
              )}
          </TableBody>
        </Table>
      )}

      {/* Add provisioner: ends in the copy-once token dialog. */}
      <ResourceSheet
        open={createOpen}
        entity={null}
        title={t("settings.provisioners.create-title")}
        description={t("settings.provisioners.create-description")}
        submitting={creating}
        submitLabel={t("common.create")}
        onClose={() => setCreateOpen(false)}
        renderForm={({ formId }) => (
          <ProvisionerCreateForm formId={formId} onSubmit={handleCreate} />
        )}
      />

      {/* Rotate confirm: kills the old token at its next use. */}
      <ConfirmActionDialog
        open={rotateTarget !== null}
        onClose={() => setRotateTarget(null)}
        busy={rotating}
        title={t("settings.provisioners.rotate-confirm-title")}
        description={t("settings.provisioners.rotate-confirm-description", {
          title: rotateTarget?.title ?? "",
        })}
        confirmLabel={t("settings.provisioners.rotate")}
        busyLabel={t("common.saving")}
        onConfirm={() => {
          void handleRotate();
        }}
      />

      {/* Delete confirm: the machines-bound refusal renders inline. */}
      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(next) => {
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.provisioners.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.provisioners.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          {deleteError && (
            <Alert variant="error" description={deleteError} className="mt-2" />
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
              onClick={() => void handleDelete()}
            >
              {deleting ? t("common.deleting") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Copy-once token (create + rotate). */}
      {tokenShown && (
        <TokenDialog
          token={tokenShown.token}
          title={tokenShown.title}
          onClose={() => setTokenShown(null)}
        />
      )}
    </SettingsPage>
  );
}

// ProvisionerCreateForm is the inner form of the add drawer; it mounts fresh
// per open (ResourceSheet keys on the open sequence).
function ProvisionerCreateForm({
  formId,
  onSubmit,
}: {
  formId: string;
  onSubmit: (form: ProvisionerForm) => void;
}) {
  const { t } = useTranslation();
  const [title, setTitle] = useState("");
  const [backend, setBackend] = useState("kubernetes");
  const [description, setDescription] = useState("");

  return (
    <form
      id={formId}
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit({ title, backend, description });
      }}
    >
      <FieldRow
        label={t("settings.provisioners.field-title")}
        htmlFor="provisioner-title"
        required
      >
        <Input
          id="provisioner-title"
          value={title}
          placeholder={t("settings.provisioners.field-title-placeholder")}
          onChange={(e) => setTitle(e.target.value)}
        />
      </FieldRow>
      <FieldRow
        label={t("settings.provisioners.field-backend")}
        htmlFor="provisioner-backend"
        required
      >
        <Select
          value={backend}
          onValueChange={(value) => {
            if (value) setBackend(value);
          }}
        >
          <SelectTrigger id="provisioner-backend" className="w-full">
            <SelectValue>
              {(value) =>
                BACKEND_OPTIONS.find((o) => o.value === value)?.labelKey
                  ? t(BACKEND_OPTIONS.find((o) => o.value === value)!.labelKey)
                  : value
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {BACKEND_OPTIONS.map((opt) => (
              <SelectItem
                key={opt.value}
                value={opt.value}
                disabled={opt.disabled}
              >
                {t(opt.labelKey)}
                {opt.disabled
                  ? ` — ${t("settings.provisioners.backend-unavailable")}`
                  : ""}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FieldRow>
      <FieldRow
        label={t("settings.provisioners.field-description")}
        htmlFor="provisioner-description"
      >
        <Textarea
          id="provisioner-description"
          value={description}
          rows={3}
          placeholder={t("settings.provisioners.field-description-placeholder")}
          onChange={(e) => setDescription(e.target.value)}
        />
      </FieldRow>
    </form>
  );
}
