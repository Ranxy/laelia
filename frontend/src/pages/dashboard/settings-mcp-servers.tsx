import { create } from "@bufbuild/protobuf";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
import { SettingsPage } from "@/components/settings-page";
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
import { SecretInput } from "@/components/ui/secret-input";
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
import { Tabs, TabsList, TabsPanel, TabsTrigger } from "@/components/ui/tabs";
import {
  groupServiceClient,
  mcpServerServiceClient,
  settingServiceClient,
  userServiceClient,
} from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
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

  const [workspaceServers, setWorkspaceServers] = useState<McpServer[]>([]);
  const [myServers, setMyServers] = useState<McpServer[]>([]);
  const [userServers, setUserServers] = useState<McpServer[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [creatorQuery, setCreatorQuery] = useState("");
  const [allowUserMcp, setAllowUserMcp] = useState(true);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<McpTab>(
    isAdmin ? "workspace" : "my"
  );

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<McpServerForm>(() =>
    emptyForm("workspace")
  );
  const [creating, setCreating] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<McpServer | null>(null);
  const [editForm, setEditForm] = useState<McpServerForm>(() =>
    emptyForm("workspace")
  );
  const [saving, setSaving] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<McpServer | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [cfgRes, myRes] = await Promise.all([
        settingServiceClient.getUserMcpConfig({}),
        mcpServerServiceClient.listMyMcpServers({ pageSize: 1000 }),
      ]);
      setAllowUserMcp(cfgRes.config?.allowUserMcpServers ?? true);
      setMyServers(myRes.mcpServers ?? []);
      if (isAdmin) {
        const [wsRes, allUserRes, userRes, groupRes] = await Promise.all([
          mcpServerServiceClient.listMcpServers({ pageSize: 1000 }),
          mcpServerServiceClient.listUserMcpServers({ pageSize: 1000 }),
          userServiceClient.listUsers({ pageSize: 1000 }),
          groupServiceClient.listGroups({ pageSize: 1000 }),
        ]);
        setWorkspaceServers(wsRes.mcpServers ?? []);
        setUserServers(allUserRes.mcpServers ?? []);
        setUsers(userRes.users ?? []);
        setGroups(groupRes.groups ?? []);
      } else {
        setWorkspaceServers([]);
        setUserServers([]);
        setUsers([]);
        setGroups([]);
      }
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [isAdmin, t]);

  useEffect(() => {
    void load();
  }, [load]);

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

  const create = async () => {
    if (!validateForm(createForm)) return;
    setCreating(true);
    try {
      const transport = toProtoTransport(createForm);
      await mcpServerServiceClient.createMcpServer({
        mcpServer: {
          title: createForm.title.trim(),
          description: createForm.description.trim(),
          transport,
          members: createForm.scope === "user" ? [] : createForm.members,
          scope:
            createForm.scope === "user"
              ? McpServerScope.USER
              : McpServerScope.WORKSPACE,
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.mcp-servers.created"),
      });
      setCreateOpen(false);
      void load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.create-failed"),
        description: describeError(err),
      });
    } finally {
      setCreating(false);
    }
  };

  const save = async () => {
    if (!editTarget) return;
    if (!validateForm(editForm)) return;
    setSaving(true);
    try {
      const transport = toProtoTransport(editForm);
      await mcpServerServiceClient.updateMcpServer({
        mcpServer: {
          name: editTarget.name,
          title: editForm.title.trim(),
          description: editForm.description.trim(),
          transport,
          members: editForm.members,
          scope: editTarget.scope,
        },
        updateMask: {
          paths: ["title", "description", editForm.transportType, "members"],
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.mcp-servers.updated"),
      });
      setEditOpen(false);
      setEditTarget(null);
      void load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.update-failed"),
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
      await mcpServerServiceClient.deleteMcpServer({
        name: deleteTarget.name,
      });
      toastManager.add({
        type: "success",
        title: t("settings.mcp-servers.deleted"),
      });
      setDeleteOpen(false);
      setDeleteTarget(null);
      void load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.delete-failed"),
        description: describeError(err),
      });
    } finally {
      setDeleting(false);
    }
  };

  const openEdit = (server: McpServer) => {
    setEditTarget(server);
    setEditForm(serverToForm(server));
    setEditOpen(true);
  };

  const openDelete = (server: McpServer) => {
    setDeleteTarget(server);
    setDeleteOpen(true);
  };

  const filteredUserServers = useMemo(() => {
    const query = creatorQuery.trim().toLowerCase();
    if (!query) return userServers;
    return userServers.filter((server) => {
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
  }, [creatorQuery, userServers, users, groups]);

  return (
    <SettingsPage
      title={t("settings.mcp-servers.title")}
      description={t("settings.mcp-servers.description")}
      actions={
        activeTab === "workspace" && canCreateWorkspace ? (
          <Button
            onClick={() => {
              setCreateForm(emptyForm("workspace"));
              setCreateOpen(true);
            }}
          >
            <Plus className="w-4 h-4" />
            {t("settings.mcp-servers.create")}
          </Button>
        ) : activeTab === "my" && allowUserMcp ? (
          <Button
            onClick={() => {
              setCreateForm(emptyForm("user"));
              setCreateOpen(true);
            }}
          >
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
            <McpServerTable
              servers={workspaceServers}
              loading={loading}
              emptyText={t("settings.mcp-servers.no-servers")}
              showMembers
              onEdit={canUpdateWorkspace ? openEdit : undefined}
              onDelete={canUpdateWorkspace ? openDelete : undefined}
            />
          </TabsPanel>
        )}

        <TabsPanel value="my">
          {!allowUserMcp && (
            <div className="mb-3 rounded-md border border-control-border bg-control-bg px-3 py-2 text-xs text-control-light">
              {t("settings.mcp-servers.feature-disabled")}
            </div>
          )}
          <McpServerTable
            servers={myServers}
            loading={loading}
            emptyText={t("settings.mcp-servers.no-my-servers")}
            onEdit={allowUserMcp ? openEdit : undefined}
            onDelete={openDelete}
          />
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
            <McpServerTable
              servers={filteredUserServers}
              loading={loading}
              emptyText={t("settings.mcp-servers.no-user-servers")}
              showCreator
              creatorLabel={(name) => memberLabel(name, users, groups)}
            />
          </TabsPanel>
        )}
      </Tabs>

      <McpServerSheet
        open={createOpen}
        title={t("settings.mcp-servers.create-title")}
        description={
          createForm.scope === "user"
            ? t("settings.mcp-servers.create-my-description")
            : t("settings.mcp-servers.create-description")
        }
        personal={createForm.scope === "user"}
        form={createForm}
        users={users}
        groups={groups}
        submitting={creating}
        onClose={() => setCreateOpen(false)}
        onFormChange={setCreateForm}
        onSubmit={create}
      />
      <McpServerSheet
        open={editOpen}
        title={t("settings.mcp-servers.edit-title", {
          title: editTarget?.title ?? "",
        })}
        description={
          editForm.scope === "user"
            ? t("settings.mcp-servers.edit-my-description")
            : t("settings.mcp-servers.edit-description")
        }
        personal={editForm.scope === "user"}
        form={editForm}
        users={users}
        groups={groups}
        submitting={saving}
        onClose={() => {
          setEditOpen(false);
          setEditTarget(null);
        }}
        onFormChange={setEditForm}
        onSubmit={save}
      />

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.mcp-servers.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.mcp-servers.delete-confirm-description", {
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

interface McpServerTableProps {
  servers: McpServer[];
  loading: boolean;
  emptyText: string;
  showMembers?: boolean;
  showCreator?: boolean;
  creatorLabel?: (name: string) => string;
  onEdit?: (server: McpServer) => void;
  onDelete?: (server: McpServer) => void;
}

function McpServerTable({
  servers,
  loading,
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
                      className="text-danger"
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
        {servers.length === 0 && !loading && (
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

interface McpServerSheetProps {
  open: boolean;
  title: string;
  description: string;
  personal?: boolean;
  form: McpServerForm;
  users: User[];
  groups: Group[];
  submitting: boolean;
  onClose: () => void;
  onFormChange: (f: McpServerForm) => void;
  onSubmit: () => void;
}

function McpServerSheet({
  open,
  title,
  description,
  personal = false,
  form,
  users,
  groups,
  submitting,
  onClose,
  onFormChange,
  onSubmit,
}: McpServerSheetProps) {
  const { t } = useTranslation();
  const usedMembers = useMemo(() => new Set(form.members), [form.members]);

  const updateHeader = (index: number, patch: Partial<HeaderForm>) => {
    const next = { ...form, headers: [...form.headers] };
    next.headers[index] = { ...next.headers[index], ...patch };
    onFormChange(next);
  };

  const removeHeader = (index: number) => {
    onFormChange({
      ...form,
      headers: form.headers.filter((_, j) => j !== index),
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
          <FieldRow label={t("settings.mcp-servers.field-title")} required>
            <Input
              value={form.title}
              onChange={(e) => onFormChange({ ...form, title: e.target.value })}
              placeholder={t("settings.mcp-servers.field-title-placeholder")}
            />
          </FieldRow>
          <FieldRow label={t("settings.mcp-servers.field-type")} required>
            <Select
              value={form.transportType}
              onValueChange={(v) =>
                onFormChange({
                  ...form,
                  transportType: v === "sse" ? "sse" : "http",
                })
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
              onChange={(e) => onFormChange({ ...form, url: e.target.value })}
              placeholder={t("settings.mcp-servers.field-url-placeholder")}
              spellCheck={false}
            />
          </FieldRow>
          <FieldRow label={t("settings.mcp-servers.field-description")}>
            <Input
              value={form.description}
              onChange={(e) =>
                onFormChange({ ...form, description: e.target.value })
              }
              placeholder={t(
                "settings.mcp-servers.field-description-placeholder"
              )}
            />
          </FieldRow>

          <FieldRow
            label={t("settings.mcp-servers.field-headers")}
            hint={t("settings.mcp-servers.field-headers-hint")}
          >
            <div className="flex flex-col gap-2">
              {form.headers.map((h, i) => (
                <div
                  key={`${h.name}-${i}`}
                  className="flex flex-col gap-1.5 border border-control-border rounded-xs p-2"
                >
                  <div className="flex items-center justify-between gap-2">
                    <Input
                      value={h.name}
                      onChange={(e) =>
                        updateHeader(i, { name: e.target.value })
                      }
                      placeholder={t(
                        "settings.mcp-servers.header-name-placeholder"
                      )}
                      className="h-8"
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-danger"
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
                        onChange={(e) =>
                          updateHeader(i, { value: e.target.value })
                        }
                        placeholder={t(
                          "settings.mcp-servers.header-value-keep-placeholder"
                        )}
                        className="h-8"
                      />
                    </div>
                  ) : (
                    <SecretInput
                      value={h.value}
                      onChange={(e) =>
                        updateHeader(i, { value: e.target.value })
                      }
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
                  onFormChange({
                    ...form,
                    headers: [
                      ...form.headers,
                      { name: "", value: "", maskedValue: "" },
                    ],
                  })
                }
              >
                <Plus className="w-4 h-4" />
                {t("settings.mcp-servers.add-header")}
              </Button>
            </div>
          </FieldRow>

          {!personal && (
            <div className="flex flex-col gap-2">
              <FieldRow
                label={t("settings.mcp-servers.field-members")}
                hint={t("settings.mcp-servers.field-members-hint")}
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
          )}
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
