import { create } from "@bufbuild/protobuf";
import { Pencil, Plus, Trash2 } from "lucide-react";
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
  groupServiceClient,
  mcpServerServiceClient,
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
}

function emptyForm(): McpServerForm {
  return {
    title: "",
    description: "",
    transportType: "http",
    url: "",
    headers: [],
    members: [],
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

export function SettingsMcpServersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.mcpServers.list");
  const canCreate = useHasPermission("laelia.mcpServers.create");
  const canUpdate = useHasPermission("laelia.mcpServers.update");

  const [servers, setServers] = useState<McpServer[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<McpServerForm>(emptyForm());
  const [creating, setCreating] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<McpServer | null>(null);
  const [editForm, setEditForm] = useState<McpServerForm>(emptyForm());
  const [saving, setSaving] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<McpServer | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [serverRes, userRes, groupRes] = await Promise.all([
        mcpServerServiceClient.listMcpServers({ pageSize: 1000 }),
        userServiceClient.listUsers({ pageSize: 1000 }),
        groupServiceClient.listGroups({ pageSize: 1000 }),
      ]);
      setServers(serverRes.mcpServers ?? []);
      setUsers(userRes.users ?? []);
      setGroups(groupRes.groups ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.mcp-servers.load-failed"),
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
          members: createForm.members,
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.mcp-servers.created"),
      });
      setCreateOpen(false);
      setCreateForm(emptyForm());
      load();
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
      load();
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
      load();
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

  if (!canList) {
    return <PermissionNotice message={t("settings.mcp-servers.not-allowed")} />;
  }

  return (
    <SettingsPage
      title={t("settings.mcp-servers.title")}
      description={t("settings.mcp-servers.description")}
      actions={
        canCreate && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="w-4 h-4" />
            {t("settings.mcp-servers.create")}
          </Button>
        )
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("settings.mcp-servers.header-title")}</TableHead>
            <TableHead>{t("settings.mcp-servers.header-type")}</TableHead>
            <TableHead>{t("settings.mcp-servers.header-url")}</TableHead>
            <TableHead>{t("settings.mcp-servers.header-members")}</TableHead>
            <TableHead>{t("settings.mcp-servers.header-actions")}</TableHead>
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
              <TableCell>{server.members.length}</TableCell>
              <TableCell>
                <div className="flex gap-1">
                  {canUpdate && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setEditTarget(server);
                        setEditForm(serverToForm(server));
                        setEditOpen(true);
                      }}
                      aria-label={t("common.edit")}
                    >
                      <Pencil className="w-4 h-4" />
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-danger"
                    onClick={() => {
                      setDeleteTarget(server);
                      setDeleteOpen(true);
                    }}
                    aria-label={t("common.delete")}
                  >
                    <Trash2 className="w-4 h-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
          {servers.length === 0 && !loading && (
            <TableRow>
              <TableCell
                colSpan={5}
                className="text-center text-control-placeholder py-8"
              >
                {t("settings.mcp-servers.no-servers")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <McpServerSheet
        open={createOpen}
        title={t("settings.mcp-servers.create-title")}
        description={t("settings.mcp-servers.create-description")}
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
        description={t("settings.mcp-servers.edit-description")}
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

interface McpServerSheetProps {
  open: boolean;
  title: string;
  description: string;
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
                      <Input
                        type="password"
                        autoComplete="off"
                        data-1p-ignore
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
                    <Input
                      type="password"
                      autoComplete="off"
                      data-1p-ignore
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
