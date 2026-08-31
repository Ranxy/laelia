import { Key } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { useCrudDialog } from "@/composables/use-crud-dialog";
import { useResourceQuery } from "@/composables/use-resource-query";
import { roleServiceClient } from "@/connect";
import { roleIDFromName } from "@/lib/command-status";
import { describeError } from "@/lib/connect-errors";
import {
  ALL_PERMISSIONS,
  PERMISSION_GROUPS,
  permissionLabel,
} from "@/lib/permissions";
import { slugify } from "@/lib/slug";
import { showErrorToast } from "@/lib/toast-errors";
import { useHasPermission } from "@/stores/permissions";
import { type Role } from "@/types/proto-es/v1/role_service_pb";

interface RoleForm {
  resourceID: string;
  title: string;
  description: string;
  permissions: Record<string, boolean>;
}

function emptyForm(): RoleForm {
  return { resourceID: "", title: "", description: "", permissions: {} };
}

function roleToForm(role: Role): RoleForm {
  const perms: Record<string, boolean> = {};
  for (const p of role.permissions) perms[p] = true;
  return {
    resourceID: roleIDFromName(role.name),
    title: role.title,
    description: role.description,
    permissions: perms,
  };
}

export function SettingsRolesPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.roles.list");
  const canCreate = useHasPermission("laelia.roles.create");
  const canUpdate = useHasPermission("laelia.roles.update");
  const canDelete = useHasPermission("laelia.roles.delete");

  const rolesQuery = useResourceQuery<Role>({
    enabled: canList,
    queryKey: ["settings", "roles"],
    queryFn: async (signal) =>
      (await roleServiceClient.listRoles({ pageSize: 1000 }, { signal }))
        .roles ?? [],
    failureTitle: t("settings.roles.load-failed"),
  });

  const roles = rolesQuery.items;

  const crud = useCrudDialog<Role>({
    // Post-mutation refresh of the resource list.
    onChanged: () => void rolesQuery.reload(),
  });

  // Create/edit failures render as an inline Alert inside the sheet body
  // (old behavior), so the error text must outlive the submit call.
  const [createError, setCreateError] = useState("");
  const [editError, setEditError] = useState("");

  const [viewOpen, setViewOpen] = useState(false);
  const [viewTarget, setViewTarget] = useState<Role | null>(null);

  const openCreate = () => {
    setCreateError("");
    crud.openCreate();
  };

  function openView(role: Role) {
    setViewTarget(role);
    setViewOpen(true);
  }

  function openEditFromView() {
    if (!viewTarget) return;
    const target = viewTarget;
    setViewOpen(false);
    setEditError("");
    crud.openEdit(target);
  }

  function openDeleteFromView() {
    if (!viewTarget) return;
    const target = viewTarget;
    setViewOpen(false);
    crud.openDelete(target);
  }

  // viewPermissions builds a permission map for the read-only view sheet from a
  // role's permission list.
  function viewPermissions(role: Role | null): Record<string, boolean> {
    const perms: Record<string, boolean> = {};
    if (!role) return perms;
    for (const p of role.permissions) perms[p] = true;
    return perms;
  }

  const handleCreateForm = async (form: RoleForm) => {
    setCreateError("");
    const id = form.resourceID.trim();
    if (!id) {
      setCreateError(t("settings.roles.id-required"));
      return;
    }
    if (!form.title.trim()) {
      setCreateError(t("settings.roles.title-required"));
      return;
    }
    await crud.runCreate(
      async () => {
        await roleServiceClient.createRole({
          role: {
            name: `roles/${id}`,
            title: form.title.trim(),
            description: form.description.trim(),
            permissions: Object.keys(form.permissions),
          },
        });
      },
      {
        successTitle: t("settings.roles.created"),
        onError: (err) => {
          setCreateError(describeError(err));
        },
      }
    );
  };

  const handleSaveForm = async (form: RoleForm) => {
    const target = crud.editTarget;
    if (!target?.name) return;
    setEditError("");
    const maskPaths: string[] = [];
    if (form.title !== target.title) maskPaths.push("title");
    if (form.description !== target.description) maskPaths.push("description");
    const original = new Set(target.permissions);
    const current = new Set(Object.keys(form.permissions));
    if (!setEqual(original, current)) maskPaths.push("permissions");
    if (maskPaths.length === 0) {
      crud.closeEdit();
      return;
    }
    await crud.runSave(
      async () => {
        await roleServiceClient.updateRole({
          role: {
            name: target.name,
            title: form.title,
            description: form.description,
            permissions: Object.keys(form.permissions),
          },
          updateMask: { paths: maskPaths },
        });
      },
      {
        successTitle: t("settings.roles.updated"),
        onError: (err) => {
          setEditError(describeError(err));
        },
      }
    );
  };

  const handleDelete = async () => {
    const target = crud.deleteTarget;
    if (!target?.name) return;
    await crud.runDelete(
      async () => {
        await roleServiceClient.deleteRole({ name: target.name });
      },
      {
        successTitle: t("settings.roles.deleted"),
        onError: (err) => {
          void showErrorToast(err, t("settings.roles.delete-failed"));
        },
      }
    );
  };

  if (!canList) {
    return <PermissionNotice message={t("settings.roles.not-allowed")} />;
  }

  return (
    <SettingsPage
      title={
        <span className="flex items-center gap-2">
          <Key className="size-5 text-accent" />
          {t("settings.roles.title")}
        </span>
      }
      actions={
        canCreate && <Button onClick={openCreate}>{t("common.create")}</Button>
      }
    >
      {rolesQuery.initialLoading ? (
        <PageLoading />
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
              {roles.map((role) => (
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
              ))}
              {roles.length === 0 && !rolesQuery.refreshing && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className="text-center text-control-light py-12"
                  >
                    {t("common.no-data")}
                  </TableCell>
                </TableRow>
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
      <ResourceSheet
        open={crud.createOpen}
        entity={null}
        title={t("settings.roles.create-title")}
        description={t("settings.roles.create-description")}
        submitting={crud.creating}
        submitLabel={t("common.create")}
        width="medium"
        onClose={crud.closeCreate}
        renderForm={({ formId }) => (
          <>
            {createError && (
              <Alert
                variant="error"
                description={createError}
                className="mb-2"
              />
            )}
            <RoleFormFields
              entity={null}
              formId={formId}
              onSubmit={(form) => {
                void handleCreateForm(form);
              }}
            />
          </>
        )}
      />

      {/* Edit role */}
      <ResourceSheet
        open={crud.editOpen}
        entity={crud.editTarget}
        title={(target) =>
          t("settings.roles.edit-title", { title: target?.title ?? "" })
        }
        description={t("settings.roles.edit-description")}
        submitting={crud.saving}
        width="medium"
        onClose={crud.closeEdit}
        renderForm={({ entity, formId }) =>
          entity ? (
            <>
              {editError && (
                <Alert
                  variant="error"
                  description={editError}
                  className="mb-2"
                />
              )}
              <RoleFormFields
                entity={entity}
                formId={formId}
                onSubmit={(form) => {
                  void handleSaveForm(form);
                }}
              />
            </>
          ) : null
        }
      />

      {/* Delete role */}
      <ConfirmActionDialog
        open={crud.deleteOpen}
        onClose={crud.closeDelete}
        busy={crud.deleting}
        title={t("settings.roles.delete-confirm-title")}
        description={t("settings.roles.delete-confirm-description", {
          title: crud.deleteTarget?.title ?? "",
        })}
        onConfirm={() => {
          void handleDelete();
        }}
      />
    </SettingsPage>
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

// Inner form of the role drawer. Mounts fresh per open (ResourceSheet keys
// on the open sequence), so useState seeds read the current entity.
function RoleFormFields({ entity, formId, onSubmit }: RoleFormFieldsProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<RoleForm>(() =>
    entity ? roleToForm(entity) : emptyForm()
  );
  const createMode = entity === null;

  return (
    <form
      id={formId}
      className="flex flex-col gap-5"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(form);
      }}
    >
      <FieldRow
        label={t("settings.roles.field-title")}
        htmlFor={createMode ? "role-title" : "edit-title"}
      >
        <Input
          id={createMode ? "role-title" : "edit-title"}
          value={form.title}
          placeholder={
            createMode ? t("settings.roles.field-title-placeholder") : undefined
          }
          onChange={(e) => {
            // The resource id is derived from the title unless the user edits
            // it directly; the role id is immutable after creation.
            const title = e.target.value;
            setForm((prev) =>
              createMode
                ? {
                    ...prev,
                    title,
                    resourceID: prev.resourceID || slugify(title),
                  }
                : { ...prev, title }
            );
          }}
        />
      </FieldRow>
      {createMode && (
        <FieldRow
          label={t("settings.roles.field-id")}
          hint={t("settings.roles.field-id-hint")}
          htmlFor="role-id"
        >
          <Input
            id="role-id"
            value={form.resourceID}
            placeholder={t("settings.roles.field-id-placeholder")}
            onChange={(e) =>
              setForm((prev) => ({
                ...prev,
                resourceID: slugify(e.target.value),
              }))
            }
          />
        </FieldRow>
      )}
      <FieldRow
        label={t("settings.roles.field-description")}
        htmlFor={createMode ? "role-description" : "edit-description"}
      >
        <Textarea
          id={createMode ? "role-description" : "edit-description"}
          className="min-h-[60px]"
          value={form.description}
          placeholder={
            createMode
              ? t("settings.roles.field-description-placeholder")
              : undefined
          }
          onChange={(e) =>
            setForm((prev) => ({ ...prev, description: e.target.value }))
          }
        />
      </FieldRow>
      <PermissionGrid
        permissions={form.permissions}
        onToggle={(perm) =>
          setForm((prev) => ({
            ...prev,
            permissions: togglePermission(prev, perm),
          }))
        }
      />
    </form>
  );
}

interface RoleFormFieldsProps {
  // null seeds an empty create form; a role seeds its edit form.
  entity: Role | null;
  formId: string;
  onSubmit: (form: RoleForm) => void;
}

function togglePermission(
  form: RoleForm,
  perm: string
): Record<string, boolean> {
  const next = { ...form.permissions, [perm]: !form.permissions[perm] };
  if (!next[perm]) delete next[perm];
  return next;
}

function setEqual(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}
