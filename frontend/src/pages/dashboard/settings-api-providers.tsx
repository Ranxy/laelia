import { Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmActionDialog } from "@/components/settings/confirm-action-dialog";
import { MemberEditor } from "@/components/settings/member-editor";
import { ResourceSheet } from "@/components/settings/resource-sheet";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import {
  apiProviderServiceClient,
  groupServiceClient,
  userServiceClient,
} from "@/connect";
import { useCrudDialog } from "@/hooks/use-crud-dialog";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { invalidateApiProvidersCache } from "@/stores/api-provider";
import { useHasPermission } from "@/stores/permissions";
import type { PiModel } from "@/types/proto-es/v1/agent_pb";
import type { ApiProvider } from "@/types/proto-es/v1/api_provider_service_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

// providerTypeOptions is the phase-1 pi runtime support set.
const PROVIDER_TYPE_OPTIONS = [
  { value: "deepseek", labelKey: "settings.api-providers.type-deepseek" },
  { value: "openrouter", labelKey: "settings.api-providers.type-openrouter" },
  { value: "custom", labelKey: "settings.api-providers.type-custom" },
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

export function SettingsApiProvidersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.apiProviders.list");
  const canCreate = useHasPermission("laelia.apiProviders.create");
  const canUpdate = useHasPermission("laelia.apiProviders.update");

  const providersQuery = useResourceQuery<ApiProvider>({
    enabled: canList,
    queryKey: ["settings", "apiProviders"],
    queryFn: async (signal) =>
      (
        await apiProviderServiceClient.listAPIProviders(
          { pageSize: 1000 },
          { signal }
        )
      ).apiProviders ?? [],
    failureTitle: t("settings.api-providers.load-failed"),
  });
  // Workspace users/groups directories: shared with the other member-editor
  // pages via the ["directory",…] entries (60s TTL), not refetched per visit.
  const usersQuery = useResourceQuery<User>({
    enabled: canList,
    queryKey: ["directory", "users"],
    queryFn: async (signal) =>
      (await userServiceClient.listUsers({ pageSize: 1000 }, { signal }))
        .users ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });
  const groupsQuery = useResourceQuery<Group>({
    enabled: canList,
    queryKey: ["directory", "groups"],
    queryFn: async (signal) =>
      (await groupServiceClient.listGroups({ pageSize: 1000 }, { signal }))
        .groups ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });

  const users = usersQuery.items;

  const crud = useCrudDialog<ApiProvider>({
    // Post-mutation refresh of the resource list plus an intentional cache
    // invalidation (01-B9): settings CRUD must invalidate the apiProviders
    // store slice that agent/machine forms read, so their dropdowns stop
    // serving stale lists.
    onChanged: () => {
      void providersQuery.reload();
      invalidateApiProvidersCache();
    },
  });

  const handleCreateForm = async (form: ProviderForm) => {
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.title-required"),
      });
      return;
    }
    if (form.providerType === "custom" && !form.baseUrl.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.base-url-required"),
      });
      return;
    }
    for (const e of form.entries) {
      if (!e.model.trim() || !e.apiKey.trim()) {
        toastManager.add({
          type: "error",
          title: t("settings.api-providers.entry-incomplete"),
        });
        return;
      }
    }
    await crud.runCreate(
      async () => {
        await apiProviderServiceClient.createAPIProvider({
          apiProvider: {
            title: form.title.trim(),
            providerType: form.providerType,
            baseUrl: form.providerType === "custom" ? form.baseUrl.trim() : "",
            description: form.description.trim(),
            members: form.members,
            entries: form.entries.map((e) => ({
              label: e.label.trim(),
              model: e.model.trim(),
              apiKey: e.apiKey.trim(),
            })),
          },
        });
      },
      {
        successTitle: t("settings.api-providers.created"),
        onError: (err) => {
          void showErrorToast(err, t("settings.api-providers.create-failed"));
        },
      }
    );
  };

  const handleSaveForm = async (form: ProviderForm) => {
    const target = crud.editTarget;
    if (!target) return;
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.title-required"),
      });
      return;
    }
    if (form.providerType === "custom" && !form.baseUrl.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.api-providers.base-url-required"),
      });
      return;
    }
    for (const e of form.entries) {
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
    await crud.runSave(
      async () => {
        await apiProviderServiceClient.updateAPIProvider({
          apiProvider: {
            name: target.name,
            title: form.title.trim(),
            providerType: form.providerType,
            baseUrl: form.providerType === "custom" ? form.baseUrl.trim() : "",
            description: form.description.trim(),
            members: form.members,
            entries: form.entries.map((e) => ({
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
      },
      {
        successTitle: t("settings.api-providers.updated"),
        onError: (err) => {
          void showErrorToast(err, t("settings.api-providers.update-failed"));
        },
      }
    );
  };

  const handleDelete = async () => {
    const target = crud.deleteTarget;
    if (!target) return;
    await crud.runDelete(
      async () => {
        await apiProviderServiceClient.deleteAPIProvider({
          name: target.name,
        });
      },
      {
        successTitle: t("settings.api-providers.deleted"),
        onError: (err) => {
          void showErrorToast(err, t("settings.api-providers.delete-failed"));
        },
      }
    );
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
          <Button onClick={crud.openCreate}>
            <Plus className="w-4 h-4" />
            {t("settings.api-providers.create")}
          </Button>
        )
      }
    >
      {providersQuery.initialLoading ? (
        <PageLoading />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("settings.api-providers.header-title")}</TableHead>
              <TableHead>{t("settings.api-providers.header-type")}</TableHead>
              <TableHead>
                {t("settings.api-providers.header-entries")}
              </TableHead>
              <TableHead>
                {t("settings.api-providers.header-members")}
              </TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {providersQuery.items.map((p) => (
              <TableRow key={p.name}>
                <TableCell className="font-medium text-main">
                  {p.title}
                </TableCell>
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
                          onClick={() => crud.openEdit(p)}
                          aria-label={t("common.edit")}
                          title={t("common.edit")}
                        >
                          <Pencil className="w-4 h-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-error"
                          onClick={() => crud.openDelete(p)}
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
            {providersQuery.items.length === 0 &&
              !providersQuery.refreshing && (
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
      )}

      <ResourceSheet
        open={crud.createOpen}
        entity={null}
        title={t("settings.api-providers.create-title")}
        description={t("settings.api-providers.create-description")}
        submitting={crud.creating}
        onClose={crud.closeCreate}
        renderForm={({ formId }) => (
          <ProviderFormFields
            entity={null}
            formId={formId}
            users={users}
            groups={groupsQuery.items}
            onSubmit={(form) => {
              void handleCreateForm(form);
            }}
          />
        )}
      />

      <ResourceSheet
        open={crud.editOpen}
        entity={crud.editTarget}
        title={(target) =>
          t("settings.api-providers.edit-title", { title: target?.title ?? "" })
        }
        description={t("settings.api-providers.edit-description")}
        submitting={crud.saving}
        onClose={crud.closeEdit}
        renderForm={({ entity, formId }) =>
          entity ? (
            <ProviderFormFields
              entity={entity}
              formId={formId}
              users={users}
              groups={groupsQuery.items}
              onSubmit={(form) => {
                void handleSaveForm(form);
              }}
            />
          ) : null
        }
      />

      <ConfirmActionDialog
        open={crud.deleteOpen}
        onClose={crud.closeDelete}
        busy={crud.deleting}
        title={t("settings.api-providers.delete-confirm-title")}
        description={t("settings.api-providers.delete-confirm-description", {
          title: crud.deleteTarget?.title ?? "",
        })}
        onConfirm={() => {
          void handleDelete();
        }}
      />
    </SettingsPage>
  );
}

interface ProviderFormFieldsProps {
  // null seeds an empty create form; a provider seeds its edit form.
  entity: ApiProvider | null;
  formId: string;
  users: User[];
  groups: Group[];
  onSubmit: (form: ProviderForm) => void;
}

// Inner form of the provider drawer. Mounts fresh per open (ResourceSheet keys
// on the open sequence), so useState seeds read the current entity and the
// form-local fetch state (fetch key / model list) resets per open without a
// manual effect.
function ProviderFormFields({
  entity,
  formId,
  users,
  groups,
  onSubmit,
}: ProviderFormFieldsProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<ProviderForm>(() =>
    entity ? providerToForm(entity) : emptyForm()
  );

  const [fetchKey, setFetchKey] = useState("");
  const [models, setModels] = useState<PiModel[]>([]);
  const [fetching, setFetching] = useState(false);
  const [fetchError, setFetchError] = useState("");

  const addedModels = useMemo(
    () => new Set(form.entries.map((e) => e.model)),
    [form.entries]
  );

  const fetchModels = async () => {
    if (!fetchKey.trim()) {
      setFetchError(t("settings.api-providers.fetch-key-required"));
      return;
    }
    if (form.providerType === "custom" && !form.baseUrl.trim()) {
      setFetchError(t("settings.api-providers.base-url-required"));
      return;
    }
    setFetching(true);
    setFetchError("");
    try {
      const res = await apiProviderServiceClient.listAPIProviderModels({
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
      setForm((f) => ({
        ...f,
        entries: f.entries.filter((e) => e.model !== model.id),
      }));
      return;
    }
    setForm((f) => ({
      ...f,
      entries: [
        ...f.entries,
        {
          name: "",
          label: "",
          model: model.id,
          maskedApiKey: "",
          apiKey: fetchKey.trim(),
        },
      ],
    }));
  };

  const updateEntry = (index: number, patch: Partial<EntryForm>) => {
    setForm((f) => {
      const next = { ...f, entries: [...f.entries] };
      next.entries[index] = { ...next.entries[index], ...patch };
      return next;
    });
  };

  const removeEntry = (index: number) => {
    setForm((f) => ({
      ...f,
      entries: f.entries.filter((_, j) => j !== index),
    }));
  };

  return (
    <form
      id={formId}
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(form);
      }}
    >
      <FieldRow label={t("settings.api-providers.field-title")} required>
        <Input
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          placeholder={t("settings.api-providers.field-title-placeholder")}
        />
      </FieldRow>
      <FieldRow label={t("settings.api-providers.field-type")} required>
        <Select
          value={form.providerType}
          onValueChange={(v) =>
            setForm((f) => ({ ...f, providerType: v ?? "deepseek" }))
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
      {form.providerType === "custom" && (
        <FieldRow
          label={t("settings.api-providers.field-base-url")}
          hint={t("settings.api-providers.field-base-url-hint")}
          required
        >
          <Input
            value={form.baseUrl}
            onChange={(e) =>
              setForm((f) => ({ ...f, baseUrl: e.target.value }))
            }
            placeholder={t("settings.api-providers.field-base-url-placeholder")}
            spellCheck={false}
          />
        </FieldRow>
      )}
      <FieldRow label={t("settings.api-providers.field-description")}>
        <Input
          value={form.description}
          onChange={(e) =>
            setForm((f) => ({ ...f, description: e.target.value }))
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
                <SecretInput
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
            {fetchError && <p className="text-xs text-error">{fetchError}</p>}
            {models.length > 0 && (
              <div className="max-h-40 overflow-y-auto flex flex-col gap-1">
                {models.map((m) => {
                  const enabled = addedModels.has(m.id);
                  return (
                    <label
                      key={m.id}
                      className="flex items-center justify-between gap-2 py-1 px-2 rounded-xs hover:bg-control-bg cursor-pointer"
                    >
                      <span className="text-sm truncate">{m.name || m.id}</span>
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
                        className="text-error"
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
                        <SecretInput
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
      <MemberEditor
        members={form.members}
        users={users}
        groups={groups}
        onChange={(members) => setForm((f) => ({ ...f, members }))}
        label={t("settings.api-providers.field-members")}
        hint={t("settings.api-providers.field-members-hint")}
      />
    </form>
  );
}
