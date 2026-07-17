import { Key, Loader2, Save } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { Checkbox } from "@/components/ui/checkbox";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { roleServiceClient } from "@/connect";
import {
  ALL_PERMISSIONS,
  PERMISSION_GROUPS,
  permissionLabel,
} from "@/lib/permissions";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/auth";
import { type Role } from "@/types/proto-es/v1/role_service_pb";

// roleIDFromName extracts the bare id from `roles/{id}`.
function roleIDFromName(name: string | undefined): string {
  if (!name) return "";
  return name.startsWith("roles/") ? name.slice("roles/".length) : name;
}

// slugify turns a free-form title into a role resource-id slug (lowercase,
// alnum + dash). The role id is immutable after creation, so we derive it from
// the title unless the user edits it directly.
function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

interface RoleForm {
  resourceID: string;
  title: string;
  description: string;
  permissions: Record<string, boolean>;
}

function emptyForm(): RoleForm {
  return { resourceID: "", title: "", description: "", permissions: {} };
}

export function SettingsRolesPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.roles.list");
  const canCreate = useHasPermission("laelia.roles.create");
  const canUpdate = useHasPermission("laelia.roles.update");
  const canDelete = useHasPermission("laelia.roles.delete");

  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<RoleForm>(emptyForm());
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Role | null>(null);
  const [editForm, setEditForm] = useState<RoleForm>(emptyForm());
  const [saving, setSaving] = useState(false);
  const [editError, setEditError] = useState("");

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [viewOpen, setViewOpen] = useState(false);
  const [viewTarget, setViewTarget] = useState<Role | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await roleServiceClient.listRoles({});
      setRoles(res.roles ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.roles.load-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (canList) load();
    else setLoading(false);
  }, [canList, load]);

  function resetCreate() {
    setCreateForm(emptyForm());
    setCreateError("");
  }

  function openCreate() {
    resetCreate();
    setCreateOpen(true);
  }

  function openEdit(role: Role) {
    setEditTarget(role);
    const perms: Record<string, boolean> = {};
    for (const p of role.permissions) perms[p] = true;
    setEditForm({
      resourceID: roleIDFromName(role.name),
      title: role.title,
      description: role.description,
      permissions: perms,
    });
    setEditError("");
    setEditOpen(true);
  }

  function openView(role: Role) {
    setViewTarget(role);
    setViewOpen(true);
  }

  function openEditFromView() {
    if (!viewTarget) return;
    const target = viewTarget;
    setViewOpen(false);
    openEdit(target);
  }

  function openDeleteFromView() {
    if (!viewTarget) return;
    setDeleteTarget(viewTarget);
    setViewOpen(false);
    setDeleteOpen(true);
  }

  function togglePermission(
    form: RoleForm,
    perm: string
  ): Record<string, boolean> {
    const next = { ...form.permissions, [perm]: !form.permissions[perm] };
    if (!next[perm]) delete next[perm];
    return next;
  }

  // viewPermissions builds a permission map for the read-only view sheet from a
  // role's permission list.
  function viewPermissions(role: Role | null): Record<string, boolean> {
    const perms: Record<string, boolean> = {};
    if (!role) return perms;
    for (const p of role.permissions) perms[p] = true;
    return perms;
  }

  async function handleCreate() {
    setCreateError("");
    const id = createForm.resourceID.trim();
    if (!id) {
      setCreateError(t("settings.roles.id-required"));
      return;
    }
    if (!createForm.title.trim()) {
      setCreateError(t("settings.roles.title-required"));
      return;
    }
    setCreating(true);
    try {
      await roleServiceClient.createRole({
        role: {
          name: `roles/${id}`,
          title: createForm.title.trim(),
          description: createForm.description.trim(),
          permissions: Object.keys(createForm.permissions),
        },
      });
      toastManager.add({ type: "success", title: t("settings.roles.created") });
      setCreateOpen(false);
      resetCreate();
      load();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  async function handleSaveEdit() {
    if (!editTarget?.name) return;
    setEditError("");
    const maskPaths: string[] = [];
    if (editForm.title !== editTarget.title) maskPaths.push("title");
    if (editForm.description !== editTarget.description)
      maskPaths.push("description");
    const original = new Set(editTarget.permissions);
    const current = new Set(Object.keys(editForm.permissions));
    if (!setEqual(original, current)) maskPaths.push("permissions");
    if (maskPaths.length === 0) {
      setEditOpen(false);
      return;
    }
    setSaving(true);
    try {
      await roleServiceClient.updateRole({
        role: {
          name: editTarget.name,
          title: editForm.title,
          description: editForm.description,
          permissions: Object.keys(editForm.permissions),
        },
        updateMask: { paths: maskPaths },
      });
      toastManager.add({ type: "success", title: t("settings.roles.updated") });
      setEditOpen(false);
      setEditTarget(null);
      load();
    } catch (err) {
      setEditError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function handleConfirmDelete() {
    if (!deleteTarget?.name) return;
    setDeleting(true);
    try {
      await roleServiceClient.deleteRole({ name: deleteTarget.name });
      toastManager.add({ type: "success", title: t("settings.roles.deleted") });
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.roles.delete-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setDeleting(false);
    }
  }

  if (!canList) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">
          {t("settings.roles.not-allowed")}
        </p>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-5 w-full">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold text-main flex items-center gap-2">
            <Key className="size-5 text-accent" />
            {t("settings.roles.title")}
          </h1>
        </div>
        {canCreate && (
          <Button onClick={openCreate}>{t("common.create")}</Button>
        )}
      </div>

      {loading ? (
        <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
          <Loader2 className="size-4 animate-spin" />
          {t("common.loading")}
        </div>
      ) : (
        <div className="rounded-xs border border-control-border bg-background shadow-xs overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[25%]">
                  {t("settings.roles.header-title")}
                </TableHead>
                <TableHead>{t("settings.roles.header-description")}</TableHead>
                <TableHead className="w-[15%]">
                  {t("settings.roles.header-permissions")}
                </TableHead>
                <TableHead className="w-[15%]">
                  {t("settings.roles.header-type")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {roles.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className="text-center text-control-light py-12"
                  >
                    {t("common.no-data")}
                  </TableCell>
                </TableRow>
              ) : (
                roles.map((role) => (
                  <TableRow
                    key={role.name}
                    className="cursor-pointer hover:bg-control-hover/40"
                    onClick={() => openView(role)}
                  >
                    <TableCell className="font-medium align-top">
                      {role.title}
                    </TableCell>
                    <TableCell className="text-control-light align-top">
                      {role.description || "-"}
                    </TableCell>
                    <TableCell className="align-top">
                      <span className="text-sm text-control-light">
                        {t("settings.roles.permission-count", {
                          count: role.permissions.length,
                        })}
                      </span>
                    </TableCell>
                    <TableCell className="align-top">
                      {role.predefined ? (
                        <Badge variant="success">
                          {t("settings.roles.type-predefined")}
                        </Badge>
                      ) : (
                        <Badge variant="warning">
                          {t("settings.roles.type-custom")}
                        </Badge>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {/* View role (read-only permissions; edit/delete for custom roles) */}
      <Sheet
        open={viewOpen}
        onOpenChange={(next) => {
          setViewOpen(next);
          if (!next) setViewTarget(null);
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{viewTarget?.title ?? ""}</SheetTitle>
            <SheetDescription>
              {viewTarget?.description ||
                t("settings.roles.view-no-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            <div className="flex flex-col gap-5">
              <div className="flex items-center gap-2">
                {viewTarget?.predefined ? (
                  <Badge variant="success">
                    {t("settings.roles.type-predefined")}
                  </Badge>
                ) : (
                  <Badge variant="warning">
                    {t("settings.roles.type-custom")}
                  </Badge>
                )}
                <span className="font-mono text-xs text-control-light">
                  {viewTarget ? roleIDFromName(viewTarget.name) : ""}
                </span>
              </div>
              <PermissionGrid
                permissions={viewPermissions(viewTarget)}
                onToggle={() => {}}
                disabled
              />
            </div>
          </SheetBody>
          <SheetFooter>
            <Button variant="outline" onClick={() => setViewOpen(false)}>
              {t("common.close")}
            </Button>
            {viewTarget && !viewTarget.predefined && canUpdate && (
              <Button onClick={openEditFromView}>{t("common.edit")}</Button>
            )}
            {viewTarget && !viewTarget.predefined && canDelete && (
              <Button variant="destructive" onClick={openDeleteFromView}>
                {t("common.delete")}
              </Button>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Create role */}
      <Sheet
        open={createOpen}
        onOpenChange={(next) => {
          setCreateOpen(next);
          if (!next) resetCreate();
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{t("settings.roles.create-title")}</SheetTitle>
            <SheetDescription>
              {t("settings.roles.create-description")}
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
                label={t("settings.roles.field-title")}
                htmlFor="role-title"
              >
                <Input
                  id="role-title"
                  value={createForm.title}
                  placeholder={t("settings.roles.field-title-placeholder")}
                  onChange={(e) => {
                    const title = e.target.value;
                    setCreateForm((prev) => ({
                      ...prev,
                      title,
                      resourceID: prev.resourceID || slugify(title),
                    }));
                  }}
                />
              </FieldRow>
              <FieldRow
                label={t("settings.roles.field-id")}
                hint={t("settings.roles.field-id-hint")}
                htmlFor="role-id"
              >
                <Input
                  id="role-id"
                  value={createForm.resourceID}
                  placeholder={t("settings.roles.field-id-placeholder")}
                  onChange={(e) =>
                    setCreateForm((prev) => ({
                      ...prev,
                      resourceID: slugify(e.target.value),
                    }))
                  }
                />
              </FieldRow>
              <FieldRow
                label={t("settings.roles.field-description")}
                htmlFor="role-description"
              >
                <Textarea
                  id="role-description"
                  className="min-h-[60px]"
                  value={createForm.description}
                  placeholder={t(
                    "settings.roles.field-description-placeholder"
                  )}
                  onChange={(e) =>
                    setCreateForm((prev) => ({
                      ...prev,
                      description: e.target.value,
                    }))
                  }
                />
              </FieldRow>
              <PermissionGrid
                permissions={createForm.permissions}
                onToggle={(perm) =>
                  setCreateForm((prev) => ({
                    ...prev,
                    permissions: togglePermission(prev, perm),
                  }))
                }
              />
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => {
                setCreateOpen(false);
                resetCreate();
              }}
              disabled={creating}
            >
              {t("common.cancel")}
            </Button>
            <Button
              disabled={creating || !createForm.title.trim()}
              onClick={handleCreate}
            >
              {creating ? t("common.creating") : t("common.create")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Edit role */}
      <Sheet
        open={editOpen}
        onOpenChange={(next) => {
          setEditOpen(next);
          if (!next) setEditTarget(null);
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("settings.roles.edit-title", {
                title: editTarget?.title ?? "",
              })}
            </SheetTitle>
            <SheetDescription>
              {t("settings.roles.edit-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {editError && (
              <Alert variant="error" description={editError} className="mb-2" />
            )}
            <div className="flex flex-col gap-5">
              <FieldRow
                label={t("settings.roles.field-title")}
                htmlFor="edit-title"
              >
                <Input
                  id="edit-title"
                  value={editForm.title}
                  onChange={(e) =>
                    setEditForm((prev) => ({ ...prev, title: e.target.value }))
                  }
                />
              </FieldRow>
              <FieldRow
                label={t("settings.roles.field-description")}
                htmlFor="edit-description"
              >
                <Textarea
                  id="edit-description"
                  className="min-h-[60px]"
                  value={editForm.description}
                  onChange={(e) =>
                    setEditForm((prev) => ({
                      ...prev,
                      description: e.target.value,
                    }))
                  }
                />
              </FieldRow>
              <PermissionGrid
                permissions={editForm.permissions}
                onToggle={(perm) =>
                  setEditForm((prev) => ({
                    ...prev,
                    permissions: togglePermission(prev, perm),
                  }))
                }
              />
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setEditOpen(false)}
              disabled={saving}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} onClick={handleSaveEdit}>
              {saving ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Delete role */}
      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.roles.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.roles.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
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

function FieldRow({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label
        htmlFor={htmlFor}
        className="text-xs font-semibold uppercase tracking-wide text-control"
      >
        {label}
      </label>
      {children}
      {hint && <span className="text-xs text-control-placeholder">{hint}</span>}
    </div>
  );
}

// PermissionGrid renders the catalog grouped by resource, each permission a
// checkbox. The full set is always shown so an admin can see and grant any
// permission, including ones the role does not yet hold.
function PermissionGrid({
  permissions,
  onToggle,
  disabled = false,
}: {
  permissions: Record<string, boolean>;
  onToggle: (perm: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-3">
      <span className="text-xs font-semibold uppercase tracking-wide text-control">
        {t("settings.roles.field-permissions")}
      </span>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {PERMISSION_GROUPS.map((group) => (
          <div
            key={group.resource}
            className="rounded-md border border-control-border p-3"
          >
            <p className="mb-2 text-xs font-semibold text-control-light">
              {group.resource}
            </p>
            <div className="flex flex-col gap-1.5">
              {group.permissions.map((perm) => (
                <label
                  key={perm}
                  className="flex items-center gap-2 text-sm text-main"
                  title={perm}
                >
                  <Checkbox
                    checked={!!permissions[perm]}
                    onCheckedChange={() => onToggle(perm)}
                    size="sm"
                    disabled={disabled}
                  />
                  <span className="font-mono text-xs">
                    {permissionLabel(perm)}
                  </span>
                </label>
              ))}
            </div>
          </div>
        ))}
      </div>
      <p className="text-xs text-control-placeholder">
        {t("settings.roles.field-permissions-hint", {
          count: ALL_PERMISSIONS.length,
        })}
      </p>
    </div>
  );
}

function setEqual(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}
