import { Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
import { PermissionNotice, SettingsPage } from "@/components/settings-page";
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
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import {
  apiProviderServiceClient,
  groupServiceClient,
  userServiceClient,
} from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import type { PiModel } from "@/types/proto-es/v1/agent_pb";
import type { ApiProvider } from "@/types/proto-es/v1/api_provider_service_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

// providerTypeOptions is the phase-1 pi runtime support set.
const PROVIDER_TYPE_OPTIONS = [
  { value: "deepseek", labelKey: "settings.api-providers.type-deepseek" },
  { value: "openrouter", labelKey: "settings.api-providers.type-openrouter" },
];

interface EntryForm {
  name: string; // existing entry resource name; "" for a new entry
  label: string;
  model: string;
  maskedApiKey: string;
  apiKey: string; // input: set to replace (existing) or required (new)
}

interface ProviderForm {
  title: string;
  providerType: string;
  baseUrl: string;
  description: string;
  members: string[];
  entries: EntryForm[];
}

function emptyForm(): ProviderForm {
  return {
    title: "",
    providerType: "deepseek",
    baseUrl: "",
    description: "",
    members: [],
    entries: [],
  };
}

function providerToForm(p: ApiProvider): ProviderForm {
  return {
    title: p.title,
    providerType: p.providerType,
    baseUrl: p.baseUrl,
    description: p.description,
    members: [...p.members],
    entries: (p.entries ?? []).map((e) => ({
      name: e.name,
      label: e.label,
      model: e.model,
      maskedApiKey: e.maskedApiKey,
      apiKey: "",
    })),
  };
}

function memberLabel(member: string, users: User[], groups: Group[]): string {
  if (member === "allUsers") return "allUsers";
  if (member.startsWith("users/")) {
    return users.find((u) => u.name === member)?.email ?? member;
  }
  if (member.startsWith("groups/")) {
    const token = member.slice("groups/".length);
    return (
      groups.find((g) => g.email === token || g.name === `groups/${token}`)
        ?.title ?? token
    );
  }
  return member;
}

export function SettingsApiProvidersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.apiProviders.list");
  const canCreate = useHasPermission("laelia.apiProviders.create");
  const canUpdate = useHasPermission("laelia.apiProviders.update");

  const [providers, setProviders] = useState<ApiProvider[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<ProviderForm>(emptyForm());
  const [creating, setCreating] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<ApiProvider | null>(null);
  const [editForm, setEditForm] = useState<ProviderForm>(emptyForm());
  const [saving, setSaving] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ApiProvider | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [providerRes, userRes, groupRes] = await Promise.all([
        apiProviderServiceClient.listApiProviders({ pageSize: 1000 }),
        userServiceClient.listUsers({ pageSize: 1000 }),
        groupServiceClient.listGroups({ pageSize: 1000 }),
      ]);
      setProviders(providerRes.apiProviders ?? []);
      setUsers(userRes.users ?? []);
      setGroups(groupRes.groups ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (canList) load();
    else setLoading(false);
  }, [canList, load]);

  const create = async () => {
    if (!createForm.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.title-required"),
      });
      return;
    }
    for (const e of createForm.entries) {
      if (!e.model.trim() || !e.apiKey.trim()) {
        toastManager.add({
          type: "error",
          title: t("settings.api-providers.entry-incomplete"),
        });
        return;
      }
    }
    setCreating(true);
    try {
      await apiProviderServiceClient.createApiProvider({
        apiProvider: {
          title: createForm.title.trim(),
          providerType: createForm.providerType,
          baseUrl: createForm.baseUrl.trim(),
          description: createForm.description.trim(),
          members: createForm.members,
          entries: createForm.entries.map((e) => ({
            label: e.label.trim(),
            model: e.model.trim(),
            apiKey: e.apiKey.trim(),
          })),
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.api-providers.created"),
      });
      setCreateOpen(false);
      setCreateForm(emptyForm());
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.create-failed"),
        description: describeError(err),
      });
    } finally {
      setCreating(false);
    }
  };

  const save = async () => {
    if (!editTarget) return;
    if (!editForm.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.title-required"),
      });
      return;
    }
    for (const e of editForm.entries) {
      if (!e.model.trim()) {
        toastManager.add({
          type: "error",
          title: t("settings.api-providers.entry-incomplete"),
        });
        return;
      }
      // A new entry must carry a real key; an existing entry keeps its stored
      // key when the input is empty.
      if (!e.name && !e.apiKey.trim()) {
        toastManager.add({
          type: "error",
          title: t("settings.api-providers.entry-key-required"),
        });
        return;
      }
    }
    setSaving(true);
    try {
      await apiProviderServiceClient.updateApiProvider({
        apiProvider: {
          name: editTarget.name,
          title: editForm.title.trim(),
          providerType: editForm.providerType,
          baseUrl: editForm.baseUrl.trim(),
          description: editForm.description.trim(),
          members: editForm.members,
          entries: editForm.entries.map((e) => ({
            name: e.name || undefined,
            label: e.label.trim(),
            model: e.model.trim(),
            apiKey: e.apiKey.trim(),
          })),
        },
        updateMask: {
          paths: ["title", "base_url", "description", "entries", "members"],
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.api-providers.updated"),
      });
      setEditOpen(false);
      setEditTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.update-failed"),
        description: describeError(err),
      });
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await apiProviderServiceClient.deleteApiProvider({
        name: deleteTarget.name,
      });
      toastManager.add({
        type: "success",
        title: t("settings.api-providers.deleted"),
      });
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.delete-failed"),
        description: describeError(err),
      });
    } finally {
      setDeleting(false);
    }
  };

  if (!canList) {
    return (
      <PermissionNotice message={t("settings.api-providers.not-allowed")} />
    );
  }

  return (
    <SettingsPage
      title={t("settings.api-providers.title")}
      description={t("settings.api-providers.description")}
      actions={
        canCreate && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="w-4 h-4" />
            {t("settings.api-providers.create")}
          </Button>
        )
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("settings.api-providers.header-title")}</TableHead>
            <TableHead>{t("settings.api-providers.header-type")}</TableHead>
            <TableHead>{t("settings.api-providers.header-entries")}</TableHead>
            <TableHead>{t("settings.api-providers.header-members")}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {providers.map((p) => (
            <TableRow key={p.name}>
              <TableCell className="font-medium text-main">{p.title}</TableCell>
              <TableCell>
                <Badge variant="secondary">{p.providerType}</Badge>
              </TableCell>
              <TableCell>{p.entries?.length ?? 0}</TableCell>
              <TableCell>{p.members?.length ?? 0}</TableCell>
              <TableCell className="text-right">
                <div className="flex justify-end gap-2">
                  {canUpdate && (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setEditTarget(p);
                          setEditForm(providerToForm(p));
                          setEditOpen(true);
                        }}
                        aria-label={t("common.edit")}
                        title={t("common.edit")}
                      >
                        <Pencil className="w-4 h-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-danger"
                        onClick={() => {
                          setDeleteTarget(p);
                          setDeleteOpen(true);
                        }}
                        aria-label={t("common.delete")}
                        title={t("common.delete")}
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </>
                  )}
                </div>
              </TableCell>
            </TableRow>
          ))}
          {providers.length === 0 && !loading && (
            <TableRow>
              <TableCell
                colSpan={5}
                className="text-center text-control-light py-8"
              >
                {t("settings.api-providers.no-providers")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <ProviderSheet
        open={createOpen}
        title={t("settings.api-providers.create-title")}
        description={t("settings.api-providers.create-description")}
        form={createForm}
        users={users}
        groups={groups}
        submitting={creating}
        onClose={() => setCreateOpen(false)}
        onFormChange={setCreateForm}
        onSubmit={create}
      />

      <ProviderSheet
        open={editOpen}
        title={t("settings.api-providers.edit-title", {
          title: editTarget?.title ?? "",
        })}
        description={t("settings.api-providers.edit-description")}
        form={editForm}
        users={users}
        groups={groups}
        submitting={saving}
        onClose={() => setEditOpen(false)}
        onFormChange={setEditForm}
        onSubmit={save}
      />

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.api-providers.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.api-providers.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button variant="destructive" disabled={deleting} onClick={remove}>
              {deleting ? t("common.saving") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsPage>
  );
}

interface ProviderSheetProps {
  open: boolean;
  title: string;
  description: string;
  form: ProviderForm;
  users: User[];
  groups: Group[];
  submitting: boolean;
  onClose: () => void;
  onFormChange: (f: ProviderForm) => void;
  onSubmit: () => void;
}

function ProviderSheet({
  open,
  title,
  description,
  form,
  users,
  groups,
  submitting,
  onClose,
  onFormChange,
  onSubmit,
}: ProviderSheetProps) {
  const { t } = useTranslation();

  const [fetchKey, setFetchKey] = useState("");
  const [models, setModels] = useState<PiModel[]>([]);
  const [fetching, setFetching] = useState(false);
  const [fetchError, setFetchError] = useState("");

  const usedMembers = useMemo(() => new Set(form.members), [form.members]);
  const addedModels = useMemo(
    () => new Set(form.entries.map((e) => e.model)),
    [form.entries]
  );

  const fetchModels = async () => {
    if (!fetchKey.trim()) {
      setFetchError(t("settings.api-providers.fetch-key-required"));
      return;
    }
    setFetching(true);
    setFetchError("");
    try {
      const res = await apiProviderServiceClient.listApiProviderModels({
        providerType: form.providerType,
        apiKey: fetchKey.trim(),
        baseUrl: form.baseUrl.trim(),
      });
      setModels(res.models ?? []);
    } catch (err) {
      setFetchError(describeError(err));
    } finally {
      setFetching(false);
    }
  };

  const toggleModel = (model: PiModel) => {
    if (addedModels.has(model.id)) {
      onFormChange({
        ...form,
        entries: form.entries.filter((e) => e.model !== model.id),
      });
      return;
    }
    onFormChange({
      ...form,
      entries: [
        ...form.entries,
        {
          name: "",
          label: "",
          model: model.id,
          maskedApiKey: "",
          apiKey: fetchKey.trim(),
        },
      ],
    });
  };

  const updateEntry = (index: number, patch: Partial<EntryForm>) => {
    const next = { ...form, entries: [...form.entries] };
    next.entries[index] = { ...next.entries[index], ...patch };
    onFormChange(next);
  };

  const removeEntry = (index: number) => {
    onFormChange({
      ...form,
      entries: form.entries.filter((_, j) => j !== index),
    });
  };

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <SheetBody className="flex flex-col gap-4">
          <FieldRow label={t("settings.api-providers.field-title")} required>
            <Input
              value={form.title}
              onChange={(e) => onFormChange({ ...form, title: e.target.value })}
              placeholder={t("settings.api-providers.field-title-placeholder")}
            />
          </FieldRow>
          <FieldRow label={t("settings.api-providers.field-type")} required>
            <Select
              value={form.providerType}
              onValueChange={(v) =>
                onFormChange({ ...form, providerType: v ?? "deepseek" })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PROVIDER_TYPE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>
          <FieldRow
            label={t("settings.api-providers.field-base-url")}
            hint={t("settings.api-providers.field-base-url-hint")}
          >
            <Input
              value={form.baseUrl}
              onChange={(e) =>
                onFormChange({ ...form, baseUrl: e.target.value })
              }
              placeholder={t(
                "settings.api-providers.field-base-url-placeholder"
              )}
              spellCheck={false}
            />
          </FieldRow>
          <FieldRow label={t("settings.api-providers.field-description")}>
            <Input
              value={form.description}
              onChange={(e) =>
                onFormChange({ ...form, description: e.target.value })
              }
              placeholder={t(
                "settings.api-providers.field-description-placeholder"
              )}
            />
          </FieldRow>

          {/* Entries */}
          <div className="flex flex-col gap-2">
            <FieldRow
              label={t("settings.api-providers.field-entries")}
              hint={t("settings.api-providers.field-entries-hint")}
            >
              <div className="flex flex-col gap-2">
                <div className="flex gap-2 items-end">
                  <div className="flex-1">
                    <Input
                      type="password"
                      autoComplete="off"
                      data-1p-ignore
                      value={fetchKey}
                      onChange={(e) => setFetchKey(e.target.value)}
                      placeholder={t(
                        "settings.api-providers.field-fetch-key-placeholder"
                      )}
                    />
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={fetching}
                    onClick={fetchModels}
                  >
                    <RefreshCw className="w-4 h-4" />
                    {fetching
                      ? t("settings.api-providers.fetching")
                      : t("settings.api-providers.fetch-models")}
                  </Button>
                </div>
                {fetchError && (
                  <p className="text-xs text-danger">{fetchError}</p>
                )}
                {models.length > 0 && (
                  <div className="max-h-40 overflow-y-auto flex flex-col gap-1">
                    {models.map((m) => {
                      const enabled = addedModels.has(m.id);
                      return (
                        <label
                          key={m.id}
                          className="flex items-center justify-between gap-2 py-1 px-2 rounded-xs hover:bg-control-bg cursor-pointer"
                        >
                          <span className="text-sm truncate">
                            {m.name || m.id}
                          </span>
                          <input
                            type="checkbox"
                            checked={enabled}
                            onChange={() => toggleModel(m)}
                            className="accent-accent"
                          />
                        </label>
                      );
                    })}
                  </div>
                )}
                {form.entries.length > 0 && (
                  <div className="flex flex-col gap-2 border-t border-control-border pt-2">
                    {form.entries.map((e, i) => (
                      <div
                        key={e.name || `${e.model}-${i}`}
                        className="flex flex-col gap-1.5 border border-control-border rounded-xs p-2"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-sm font-medium truncate">
                            {e.model}
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-danger"
                            onClick={() => removeEntry(i)}
                            aria-label={t("common.delete")}
                          >
                            <Trash2 className="w-4 h-4" />
                          </Button>
                        </div>
                        <Input
                          value={e.label}
                          onChange={(ev) =>
                            updateEntry(i, { label: ev.target.value })
                          }
                          placeholder={t(
                            "settings.api-providers.entry-label-placeholder"
                          )}
                          className="h-8"
                        />
                        {e.name ? (
                          <div className="flex flex-col gap-1.5">
                            <span className="text-xs text-control-placeholder">
                              {e.maskedApiKey ||
                                t("settings.api-providers.entry-key-kept")}
                            </span>
                            <Input
                              type="password"
                              autoComplete="off"
                              data-1p-ignore
                              value={e.apiKey}
                              onChange={(ev) =>
                                updateEntry(i, { apiKey: ev.target.value })
                              }
                              placeholder={t(
                                "settings.api-providers.entry-key-replace-placeholder"
                              )}
                              className="h-8"
                            />
                          </div>
                        ) : (
                          <span className="text-xs text-control-placeholder">
                            {e.apiKey
                              ? t("settings.api-providers.entry-key-set")
                              : t("settings.api-providers.entry-key-required")}
                          </span>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </FieldRow>
          </div>

          {/* Members */}
          <div className="flex flex-col gap-2">
            <FieldRow
              label={t("settings.api-providers.field-members")}
              hint={t("settings.api-providers.field-members-hint")}
            >
              <div className="flex flex-col gap-2">
                {form.members.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {form.members.map((m) => (
                      <Badge key={m} variant="secondary" className="gap-1.5">
                        {memberLabel(m, users, groups)}
                        <button
                          type="button"
                          className="text-control-placeholder hover:text-danger"
                          onClick={() =>
                            onFormChange({
                              ...form,
                              members: form.members.filter((x) => x !== m),
                            })
                          }
                          aria-label={t("common.remove")}
                        >
                          ×
                        </button>
                      </Badge>
                    ))}
                  </div>
                )}
                <MemberPicker
                  users={users}
                  groups={groups}
                  value=""
                  allowAllUsers
                  onSelect={(member) => {
                    if (!member || usedMembers.has(member)) return;
                    onFormChange({
                      ...form,
                      members: [...form.members, member],
                    });
                  }}
                />
              </div>
            </FieldRow>
          </div>
        </SheetBody>
        <SheetFooter>
          <Button onClick={onSubmit} disabled={submitting}>
            {submitting ? t("common.saving") : t("common.save")}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
