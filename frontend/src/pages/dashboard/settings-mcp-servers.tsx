import { create } from "@bufbuild/protobuf";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmActionDialog } from "@/components/settings/confirm-action-dialog";
import { MemberEditor } from "@/components/settings/member-editor";
import { ResourceSheet } from "@/components/settings/resource-sheet";
import { PageLoading, SettingsPage } from "@/components/settings-page";
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
import { Tabs, TabsList, TabsPanel, TabsTrigger } from "@/components/ui/tabs";
import {
  groupServiceClient,
  mcpServerServiceClient,
  settingServiceClient,
  userServiceClient,
} from "@/connect";
import { useCrudDialog } from "@/hooks/use-crud-dialog";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { memberLabel } from "@/lib/members";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { invalidateMcpServersCache } from "@/stores/mcp";
import { useHasPermission } from "@/stores/permissions";
import type { UserMcpConfigSetting } from "@/types/proto-es/store/setting_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import {
  type McpHeader,
  McpHeaderSchema,
  McpHttpTransportSchema,
  type McpServer,
  McpServerScope,
  McpSseTransportSchema,
} from "@/types/proto-es/v1/mcp_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

interface HeaderForm {
  name: string;
  value: string;
  maskedValue: string;
}

interface McpServerForm {
  title: string;
  description: string;
  transportType: "http" | "sse";
  url: string;
  headers: HeaderForm[];
  members: string[];
  scope: "workspace" | "user";
}

function emptyForm(scope: "workspace" | "user"): McpServerForm {
  return {
    title: "",
    description: "",
    transportType: "http",
    url: "",
    headers: [],
    members: [],
    scope,
  };
}

function serverToForm(server: McpServer): McpServerForm {
  const transport = server.transport.value;
  const isSse = server.transport.case === "sse";
  const headers: HeaderForm[] = (transport?.headers ?? []).map((h) => ({
    name: h.name,
    value: "",
    maskedValue: h.maskedValue,
  }));
  return {
    title: server.title,
    description: server.description,
    transportType: isSse ? "sse" : "http",
    url: transport?.url ?? "",
    headers,
    members: [...server.members],
    scope: server.scope === McpServerScope.USER ? "user" : "workspace",
  };
}

function toProtoHeaders(headers: HeaderForm[]): McpHeader[] {
  return headers
    .filter((h) => h.name.trim() !== "")
    .map((h) =>
      create(McpHeaderSchema, { name: h.name.trim(), value: h.value })
    );
}

function toProtoTransport(form: McpServerForm) {
  const headers = toProtoHeaders(form.headers);
  if (form.transportType === "sse") {
    return {
      case: "sse" as const,
      value: create(McpSseTransportSchema, { url: form.url.trim(), headers }),
    };
  }
  return {
    case: "http" as const,
    value: create(McpHttpTransportSchema, { url: form.url.trim(), headers }),
  };
}

type McpTab = "workspace" | "my" | "users";

export function SettingsMcpServersPage() {
  const { t } = useTranslation();
  const isAdmin = useHasPermission("laelia.mcpServers.list");
  const canCreateWorkspace = useHasPermission("laelia.mcpServers.create");
  const canUpdateWorkspace = useHasPermission("laelia.mcpServers.update");

  const [activeTab, setActiveTab] = useState<McpTab>(
    isAdmin ? "workspace" : "my"
  );
  const [creatorQuery, setCreatorQuery] = useState("");

  // One tab-scoped server list: the key carries every value that selects a
  // different server view (active tab + admin), so a permission/tab flip
  // re-queries under a different key instead of racing two loads (01-B7),
  // and the queryFn performs the same list RPC the old load did for the
  // active tab.
  const serversQuery = useResourceQuery<McpServer>({
    enabled: activeTab === "my" ? true : isAdmin,
    queryKey: ["settings", "mcpServers", activeTab, isAdmin],
    queryFn: async (signal) => {
      if (activeTab === "workspace") {
        return (
          (
            await mcpServerServiceClient.listMcpServers(
              { pageSize: 1000 },
              { signal }
            )
          ).mcpServers ?? []
        );
      }
      if (activeTab === "users") {
        return (
          (
            await mcpServerServiceClient.listUserMcpServers(
              { pageSize: 1000 },
              { signal }
            )
          ).mcpServers ?? []
        );
      }
      return (
        (
          await mcpServerServiceClient.listMyMcpServers(
            { pageSize: 1000 },
            { signal }
          )
        ).mcpServers ?? []
      );
    },
    failureTitle: t("settings.mcp-servers.load-failed"),
  });

  // Personal-MCP config (same getSetting RPC the old load always performed):
  // gates the my-tab create/edit actions and the sheet's IP-policy hint.
  const configQuery = useResourceQuery<UserMcpConfigSetting>({
    queryKey: ["settings", "mcpServers", "config"],
    queryFn: async (signal) => {
      const res = await settingServiceClient.getSetting(
        { name: "settings/user_mcp_config" },
        { signal }
      );
      const v = res.value?.value;
      return v?.case === "userMcpConfig" ? [v.value] : [];
    },
    failureTitle: t("settings.mcp-servers.load-failed"),
  });
  const cfg = configQuery.items[0];
  const allowUserMcp = cfg?.allowUserMcpServers ?? true;
  const mcpIpPolicyEnabled = cfg?.mcpIpPolicy?.enabled ?? false;

  // Workspace users/groups directories for the member editor and creator
  // labels: shared ["directory",…] entries (60s TTL), only fetched for
  // admins — the same gate the old load applied.
  const usersQuery = useResourceQuery<User>({
    enabled: isAdmin,
    queryKey: ["directory", "users"],
    queryFn: async (signal) =>
      (await userServiceClient.listUsers({ pageSize: 1000 }, { signal }))
        .users ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });
  const groupsQuery = useResourceQuery<Group>({
    enabled: isAdmin,
    queryKey: ["directory", "groups"],
    queryFn: async (signal) =>
      (await groupServiceClient.listGroups({ pageSize: 1000 }, { signal }))
        .groups ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });

  const users = usersQuery.items;
  const groups = groupsQuery.items;

  const crud = useCrudDialog<McpServer>({
    // Post-mutation refresh of the active tab's list (the other tabs refresh
    // under their own stale keys when visited), plus the intentional store
    // invalidation (01-B9): settings CRUD must refresh the mcpServers store
    // slice that agent/machine forms read.
    onChanged: () => {
      void serversQuery.reload();
      invalidateMcpServersCache();
    },
  });

  const validateForm = (form: McpServerForm) => {
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.title-required"),
      });
      return false;
    }
    if (!form.url.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.url-required"),
      });
      return false;
    }
    return true;
  };

  const create = async (form: McpServerForm) => {
    if (!validateForm(form)) return;
    await crud.runCreate(
      async () => {
        const transport = toProtoTransport(form);
        await mcpServerServiceClient.createMcpServer({
          mcpServer: {
            title: form.title.trim(),
            description: form.description.trim(),
            transport,
            members: form.scope === "user" ? [] : form.members,
            scope:
              form.scope === "user"
                ? McpServerScope.USER
                : McpServerScope.WORKSPACE,
          },
        });
      },
      {
        successTitle: t("settings.mcp-servers.created"),
        onError: (err) => {
          void showErrorToast(err, t("settings.mcp-servers.create-failed"));
        },
      }
    );
  };

  const save = async (form: McpServerForm) => {
    const target = crud.editTarget;
    if (!target) return;
    if (!validateForm(form)) return;
    await crud.runSave(
      async () => {
        const transport = toProtoTransport(form);
        await mcpServerServiceClient.updateMcpServer({
          mcpServer: {
            name: target.name,
            title: form.title.trim(),
            description: form.description.trim(),
            transport,
            members: form.members,
            scope: target.scope,
          },
          updateMask: {
            paths: ["title", "description", form.transportType, "members"],
          },
        });
      },
      {
        successTitle: t("settings.mcp-servers.updated"),
        onError: (err) => {
          void showErrorToast(err, t("settings.mcp-servers.update-failed"));
        },
      }
    );
  };

  const remove = async () => {
    const target = crud.deleteTarget;
    if (!target) return;
    await crud.runDelete(
      async () => {
        await mcpServerServiceClient.deleteMcpServer({
          name: target.name,
        });
      },
      {
        successTitle: t("settings.mcp-servers.deleted"),
        onError: (err) => {
          void showErrorToast(err, t("settings.mcp-servers.delete-failed"));
        },
      }
    );
  };

  const filteredUserServers = useMemo(() => {
    const query = creatorQuery.trim().toLowerCase();
    if (!query) return serversQuery.items;
    return serversQuery.items.filter((server) => {
      const creator = memberLabel(
        server.createdBy,
        users,
        groups
      ).toLowerCase();
      return (
        creator.includes(query) ||
        server.createdBy.toLowerCase().includes(query)
      );
    });
  }, [creatorQuery, serversQuery.items, users, groups]);

  // The create drawer is modal, so seeding its scope from the tab it was
  // opened from (the old action buttons seeded it the same way) is stable
  // for the whole open.
  const createScope: "workspace" | "user" =
    activeTab === "my" ? "user" : "workspace";

  return (
    <SettingsPage
      title={t("settings.mcp-servers.title")}
      description={t("settings.mcp-servers.description")}
      actions={
        activeTab === "workspace" && canCreateWorkspace ? (
          <Button onClick={crud.openCreate}>
            <Plus className="w-4 h-4" />
            {t("settings.mcp-servers.create")}
          </Button>
        ) : activeTab === "my" && allowUserMcp ? (
          <Button onClick={crud.openCreate}>
            <Plus className="w-4 h-4" />
            {t("settings.mcp-servers.create-my")}
          </Button>
        ) : undefined
      }
    >
      <Tabs
        value={activeTab}
        onValueChange={(v) => {
          if (v) setActiveTab(v as McpTab);
        }}
      >
        <TabsList>
          {isAdmin && (
            <TabsTrigger value="workspace">
              {t("settings.mcp-servers.tab-workspace")}
            </TabsTrigger>
          )}
          <TabsTrigger value="my">
            {t("settings.mcp-servers.tab-my")}
          </TabsTrigger>
          {isAdmin && (
            <TabsTrigger value="users">
              {t("settings.mcp-servers.tab-users")}
            </TabsTrigger>
          )}
        </TabsList>

        {isAdmin && (
          <TabsPanel value="workspace">
            {serversQuery.initialLoading ? (
              <PageLoading />
            ) : (
              <McpServerTable
                servers={serversQuery.items}
                refreshing={serversQuery.refreshing}
                emptyText={t("settings.mcp-servers.no-servers")}
                showMembers
                onEdit={canUpdateWorkspace ? crud.openEdit : undefined}
                onDelete={canUpdateWorkspace ? crud.openDelete : undefined}
              />
            )}
          </TabsPanel>
        )}

        <TabsPanel value="my">
          {!allowUserMcp && (
            <div className="mb-3 rounded-md border border-control-border bg-control-bg px-3 py-2 text-xs text-control-light">
              {t("settings.mcp-servers.feature-disabled")}
            </div>
          )}
          {serversQuery.initialLoading ? (
            <PageLoading />
          ) : (
            <McpServerTable
              servers={serversQuery.items}
              refreshing={serversQuery.refreshing}
              emptyText={t("settings.mcp-servers.no-my-servers")}
              onEdit={allowUserMcp ? crud.openEdit : undefined}
              onDelete={crud.openDelete}
            />
          )}
        </TabsPanel>

        {isAdmin && (
          <TabsPanel value="users">
            <p className="mb-3 text-xs text-control-light">
              {t("settings.mcp-servers.users-hint")}
            </p>
            <Input
              value={creatorQuery}
              onChange={(e) => setCreatorQuery(e.target.value)}
              placeholder={t("settings.mcp-servers.search-creator-placeholder")}
              className="mb-3 max-w-xs"
            />
            {serversQuery.initialLoading ? (
              <PageLoading />
            ) : (
              <McpServerTable
                servers={filteredUserServers}
                refreshing={serversQuery.refreshing}
                emptyText={t("settings.mcp-servers.no-user-servers")}
                showCreator
                creatorLabel={(name) => memberLabel(name, users, groups)}
              />
            )}
          </TabsPanel>
        )}
      </Tabs>

      <ResourceSheet
        open={crud.createOpen}
        entity={null}
        title={t("settings.mcp-servers.create-title")}
        description={
          createScope === "user"
            ? t("settings.mcp-servers.create-my-description")
            : t("settings.mcp-servers.create-description")
        }
        submitting={crud.creating}
        onClose={crud.closeCreate}
        renderForm={({ formId }) => (
          <McpServerFormFields
            entity={null}
            formId={formId}
            seedScope={createScope}
            ipPolicyActive={mcpIpPolicyEnabled}
            users={users}
            groups={groups}
            onSubmit={(form) => {
              void create(form);
            }}
          />
        )}
      />

      <ResourceSheet
        open={crud.editOpen}
        entity={crud.editTarget}
        title={(target) =>
          t("settings.mcp-servers.edit-title", { title: target?.title ?? "" })
        }
        description={(target) =>
          target?.scope === McpServerScope.USER
            ? t("settings.mcp-servers.edit-my-description")
            : t("settings.mcp-servers.edit-description")
        }
        submitting={crud.saving}
        onClose={crud.closeEdit}
        renderForm={({ entity, formId }) =>
          entity ? (
            <McpServerFormFields
              entity={entity}
              formId={formId}
              seedScope="workspace"
              ipPolicyActive={mcpIpPolicyEnabled}
              users={users}
              groups={groups}
              onSubmit={(form) => {
                void save(form);
              }}
            />
          ) : null
        }
      />

      <ConfirmActionDialog
        open={crud.deleteOpen}
        onClose={crud.closeDelete}
        busy={crud.deleting}
        title={t("settings.mcp-servers.delete-confirm-title")}
        description={t("settings.mcp-servers.delete-confirm-description", {
          title: crud.deleteTarget?.title ?? "",
        })}
        onConfirm={() => {
          void remove();
        }}
      />
    </SettingsPage>
  );
}

interface McpServerTableProps {
  servers: McpServer[];
  refreshing: boolean;
  emptyText: string;
  showMembers?: boolean;
  showCreator?: boolean;
  creatorLabel?: (name: string) => string;
  onEdit?: (server: McpServer) => void;
  onDelete?: (server: McpServer) => void;
}

function McpServerTable({
  servers,
  refreshing,
  emptyText,
  showMembers,
  showCreator,
  creatorLabel,
  onEdit,
  onDelete,
}: McpServerTableProps) {
  const { t } = useTranslation();
  const showActions = Boolean(onEdit || onDelete);
  const colSpan =
    3 + (showMembers ? 1 : 0) + (showCreator ? 1 : 0) + (showActions ? 1 : 0);
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("settings.mcp-servers.header-title")}</TableHead>
          <TableHead>{t("settings.mcp-servers.header-type")}</TableHead>
          <TableHead>{t("settings.mcp-servers.header-url")}</TableHead>
          {showMembers && (
            <TableHead>{t("settings.mcp-servers.header-members")}</TableHead>
          )}
          {showCreator && (
            <TableHead>{t("settings.mcp-servers.header-owner")}</TableHead>
          )}
          {showActions && (
            <TableHead>{t("settings.mcp-servers.header-actions")}</TableHead>
          )}
        </TableRow>
      </TableHeader>
      <TableBody>
        {servers.map((server) => (
          <TableRow key={server.name}>
            <TableCell className="font-medium">{server.title}</TableCell>
            <TableCell>
              <Badge variant="secondary">
                {server.transport.case === "sse" ? "SSE" : "HTTP"}
              </Badge>
            </TableCell>
            <TableCell className="text-control-placeholder max-w-64 truncate">
              {server.transport.value?.url ?? ""}
            </TableCell>
            {showMembers && <TableCell>{server.members.length}</TableCell>}
            {showCreator && (
              <TableCell className="text-control-placeholder">
                {creatorLabel
                  ? creatorLabel(server.createdBy)
                  : server.createdBy}
              </TableCell>
            )}
            {showActions && (
              <TableCell>
                <div className="flex gap-1">
                  {onEdit && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onEdit(server)}
                      aria-label={t("common.edit")}
                    >
                      <Pencil className="w-4 h-4" />
                    </Button>
                  )}
                  {onDelete && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-error"
                      onClick={() => onDelete(server)}
                      aria-label={t("common.delete")}
                    >
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  )}
                </div>
              </TableCell>
            )}
          </TableRow>
        ))}
        {servers.length === 0 && !refreshing && (
          <TableRow>
            <TableCell
              colSpan={colSpan}
              className="text-center text-control-placeholder py-8"
            >
              {emptyText}
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  );
}

interface McpServerFormFieldsProps {
  // null seeds an empty create form (scoped by seedScope); a server seeds
  // its edit form.
  entity: McpServer | null;
  formId: string;
  // Create scope: "user" when the sheet was opened from the my tab.
  seedScope: "workspace" | "user";
  ipPolicyActive: boolean;
  users: User[];
  groups: Group[];
  onSubmit: (form: McpServerForm) => void;
}

// Inner form of the mcp-server drawer. Mounts fresh per open (ResourceSheet
// keys on the open sequence), so useState seeds read the current entity and
// every open resets the form without a manual effect.
function McpServerFormFields({
  entity,
  formId,
  seedScope,
  ipPolicyActive,
  users,
  groups,
  onSubmit,
}: McpServerFormFieldsProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<McpServerForm>(() =>
    entity ? serverToForm(entity) : emptyForm(seedScope)
  );

  const personal = form.scope === "user";

  const updateHeader = (index: number, patch: Partial<HeaderForm>) => {
    setForm((f) => {
      const next = { ...f, headers: [...f.headers] };
      next.headers[index] = { ...next.headers[index], ...patch };
      return next;
    });
  };

  const removeHeader = (index: number) => {
    setForm((f) => ({
      ...f,
      headers: f.headers.filter((_, j) => j !== index),
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
      <FieldRow label={t("settings.mcp-servers.field-title")} required>
        <Input
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          placeholder={t("settings.mcp-servers.field-title-placeholder")}
        />
      </FieldRow>
      <FieldRow label={t("settings.mcp-servers.field-type")} required>
        <Select
          value={form.transportType}
          onValueChange={(v) =>
            setForm((f) => ({
              ...f,
              transportType: v === "sse" ? "sse" : "http",
            }))
          }
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="http">HTTP (Streamable)</SelectItem>
            <SelectItem value="sse">SSE</SelectItem>
          </SelectContent>
        </Select>
      </FieldRow>
      <FieldRow label={t("settings.mcp-servers.field-url")} required>
        <Input
          value={form.url}
          onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
          placeholder={t("settings.mcp-servers.field-url-placeholder")}
          spellCheck={false}
        />
        {personal && ipPolicyActive && (
          <p className="mt-1 text-xs text-control-light">
            {t("settings.mcp-servers.ip-policy-active-hint")}
          </p>
        )}
      </FieldRow>
      <FieldRow label={t("settings.mcp-servers.field-description")}>
        <Input
          value={form.description}
          onChange={(e) =>
            setForm((f) => ({ ...f, description: e.target.value }))
          }
          placeholder={t("settings.mcp-servers.field-description-placeholder")}
        />
      </FieldRow>

      <FieldRow
        label={t("settings.mcp-servers.field-headers")}
        hint={t("settings.mcp-servers.field-headers-hint")}
      >
        <div className="flex flex-col gap-2">
          {form.headers.map((h, i) => (
            // Phase-0 fix: rows key on the row index, not on header content —
            // content keys would remount a row (losing focus and its typed
            // value) whenever the name or masked value changes.
            <div
              key={i}
              className="flex flex-col gap-1.5 border border-control-border rounded-xs p-2"
            >
              <div className="flex items-center justify-between gap-2">
                <Input
                  value={h.name}
                  onChange={(e) => updateHeader(i, { name: e.target.value })}
                  placeholder={t(
                    "settings.mcp-servers.header-name-placeholder"
                  )}
                  className="h-8"
                />
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-error"
                  onClick={() => removeHeader(i)}
                  aria-label={t("common.delete")}
                >
                  <Trash2 className="w-4 h-4" />
                </Button>
              </div>
              {h.maskedValue ? (
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs text-control-placeholder">
                    {h.maskedValue}
                  </span>
                  <SecretInput
                    value={h.value}
                    onChange={(e) => updateHeader(i, { value: e.target.value })}
                    placeholder={t(
                      "settings.mcp-servers.header-value-keep-placeholder"
                    )}
                    className="h-8"
                  />
                </div>
              ) : (
                <SecretInput
                  value={h.value}
                  onChange={(e) => updateHeader(i, { value: e.target.value })}
                  placeholder={t(
                    "settings.mcp-servers.header-value-placeholder"
                  )}
                  className="h-8"
                />
              )}
            </div>
          ))}
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              setForm((f) => ({
                ...f,
                headers: [
                  ...f.headers,
                  { name: "", value: "", maskedValue: "" },
                ],
              }))
            }
          >
            <Plus className="w-4 h-4" />
            {t("settings.mcp-servers.add-header")}
          </Button>
        </div>
      </FieldRow>

      {!personal && (
        <MemberEditor
          members={form.members}
          users={users}
          groups={groups}
          onChange={(members) => setForm((f) => ({ ...f, members }))}
          label={t("settings.mcp-servers.field-members")}
          hint={t("settings.mcp-servers.field-members-hint")}
        />
      )}
    </form>
  );
}
