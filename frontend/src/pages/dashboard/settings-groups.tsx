import { Pencil, Plus, Trash2 } from "lucide-react";
import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
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
import { groupServiceClient, userServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import { State } from "@/types/proto-es/v1/common_pb";
import {
  type Group,
  GroupMemberRole,
  type GroupReference,
} from "@/types/proto-es/v1/group_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

interface MemberRow {
  member: string;
  role: GroupMemberRole;
}

interface GroupForm {
  email: string;
  title: string;
  description: string;
  members: MemberRow[];
}

function emptyForm(): GroupForm {
  return { email: "", title: "", description: "", members: [] };
}

function groupToForm(group: Group): GroupForm {
  return {
    email: group.email,
    title: group.title,
    description: group.description,
    members: (group.members ?? []).map((m) => ({
      member: m.member,
      role: m.role,
    })),
  };
}

function displayName(user: User): string {
  return user.title || user.email || user.name || "";
}

export function SettingsGroupsPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.groups.list");
  const canCreate = useHasPermission("laelia.groups.create");

  const [groups, setGroups] = useState<Group[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<GroupForm>(emptyForm());
  const [creating, setCreating] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Group | null>(null);
  const [editForm, setEditForm] = useState<GroupForm>(emptyForm());
  const [saving, setSaving] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Group | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [refsByGroup, setRefsByGroup] = useState<Map<string, GroupReference[]>>(
    new Map()
  );
  const [expandedRefs, setExpandedRefs] = useState<Set<string>>(new Set());

  const activeUsers = useMemo(
    () => users.filter((u) => u.state === State.ACTIVE),
    [users]
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [groupRes, userRes] = await Promise.all([
        groupServiceClient.listGroups({ pageSize: 1000 }),
        userServiceClient.listUsers({ pageSize: 1000 }),
      ]);
      setGroups(groupRes.groups ?? []);
      setUsers(userRes.users ?? []);
      // References are fetched lazily per group when its references row is
      // expanded (see toggleRefs) — fetching them for every group on load was
      // an N+1 burst that most pages never displayed.
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  // Fetch a group's policy references lazily the first time its references row
  // is expanded, then toggle the expansion. Without this the page load fired a
  // getGroupReferences RPC for every group, most of which are never displayed.
  // Group names whose references are currently being fetched, so a double-click
  // can't fire duplicate RPCs and net-cancel the expansion (the toggle happens
  // once the fetch completes, not once per click).
  const pendingRefsRef = useRef<Set<string>>(new Set());

  const loadRefs = useCallback(
    async (groupName: string) => {
      if (refsByGroup.has(groupName) || pendingRefsRef.current.has(groupName)) {
        return;
      }
      pendingRefsRef.current.add(groupName);
      try {
        const res = await groupServiceClient.getGroupReferences({
          name: groupName,
        });
        setRefsByGroup((prev) =>
          new Map(prev).set(groupName, res.references ?? [])
        );
      } catch {
        setRefsByGroup((prev) => new Map(prev).set(groupName, []));
      } finally {
        pendingRefsRef.current.delete(groupName);
      }
      // Reveal the row once the references arrive — the click that started the
      // load intended to expand it.
      setExpandedRefs((prev) => new Set(prev).add(groupName));
    },
    [refsByGroup]
  );

  const toggleRefs = useCallback(
    (groupName: string) => {
      if (refsByGroup.has(groupName)) {
        // Already loaded — plain expand/collapse toggle.
        setExpandedRefs((prev) => {
          const next = new Set(prev);
          if (next.has(groupName)) next.delete(groupName);
          else next.add(groupName);
          return next;
        });
        return;
      }
      // Not loaded — fetch once (idempotent while in flight) and expand on
      // completion; a second click while loading is ignored.
      void loadRefs(groupName);
    },
    [refsByGroup, loadRefs]
  );

  useEffect(() => {
    if (canList) load();
    else setLoading(false);
  }, [canList, load]);

  const hasOwner = (form: GroupForm) =>
    form.members.some((m) => m.role === GroupMemberRole.OWNER);

  const updateMember = (
    form: GroupForm,
    setForm: (f: GroupForm) => void,
    index: number,
    patch: Partial<MemberRow>
  ) => {
    const next = { ...form, members: [...form.members] };
    next.members[index] = { ...next.members[index], ...patch };
    setForm(next);
  };

  const create = async () => {
    if (!createForm.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.title-required"),
      });
      return;
    }
    if (!hasOwner(createForm)) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.at-least-one-owner"),
      });
      return;
    }
    setCreating(true);
    try {
      await groupServiceClient.createGroup({
        groupEmail: createForm.email.trim().toLowerCase(),
        group: {
          title: createForm.title,
          description: createForm.description,
          members: createForm.members,
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.groups.created"),
      });
      setCreateOpen(false);
      setCreateForm(emptyForm());
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.create-title"),
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
        title: t("settings.groups.title-required"),
      });
      return;
    }
    if (!hasOwner(editForm)) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.at-least-one-owner"),
      });
      return;
    }
    setSaving(true);
    try {
      const paths = ["title", "description"];
      if (
        JSON.stringify(editForm.members) !==
        JSON.stringify(groupToForm(editTarget).members)
      ) {
        paths.push("members");
      }
      await groupServiceClient.updateGroup({
        group: {
          name: editTarget.name,
          title: editForm.title,
          description: editForm.description,
          members: editForm.members,
        },
        updateMask: { paths },
      });
      toastManager.add({
        type: "success",
        title: t("settings.groups.updated"),
      });
      setEditOpen(false);
      setEditTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.edit-title", { title: editTarget.title }),
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
      await groupServiceClient.deleteGroup({ name: deleteTarget.name });
      toastManager.add({
        type: "success",
        title: t("settings.groups.deleted"),
      });
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.delete-failed"),
        description: describeError(err),
      });
    } finally {
      setDeleting(false);
    }
  };

  if (!canList) {
    return <PermissionNotice message={t("settings.groups.not-allowed")} />;
  }

  return (
    <SettingsPage
      title={t("settings.groups.title")}
      description={t("settings.groups.description")}
      actions={
        canCreate && (
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="w-4 h-4" />
            {t("settings.groups.create")}
          </Button>
        )
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("settings.groups.header-title")}</TableHead>
            <TableHead>{t("settings.groups.header-email")}</TableHead>
            <TableHead>{t("settings.groups.header-members")}</TableHead>
            <TableHead>{t("settings.groups.header-source")}</TableHead>
            <TableHead>{t("settings.groups.header-references")}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {groups.map((group) => {
            const refs = refsByGroup.get(group.name ?? "") ?? [];
            const expanded = expandedRefs.has(group.name ?? "");
            return (
              <Fragment key={group.name}>
                <TableRow>
                  <TableCell className="font-medium text-main">
                    {group.title}
                  </TableCell>
                  <TableCell className="text-control-light">
                    {group.email}
                  </TableCell>
                  <TableCell>{group.members?.length ?? 0}</TableCell>
                  <TableCell>
                    {group.source ? (
                      <Badge variant="secondary">
                        {t("settings.groups.source-external")}
                      </Badge>
                    ) : (
                      <Badge variant="secondary">
                        {t("settings.groups.source-manual")}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {refs.length > 0 ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => toggleRefs(group.name ?? "")}
                        aria-expanded={expanded}
                      >
                        {t("settings.groups.references-count", {
                          count: refs.length,
                        })}
                      </Button>
                    ) : refsByGroup.has(group.name ?? "") ? (
                      // Loaded and genuinely reference-free.
                      <span className="text-control-placeholder">—</span>
                    ) : (
                      // Not yet loaded — fetch on first click.
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => toggleRefs(group.name ?? "")}
                        aria-expanded={expanded}
                      >
                        {t("settings.groups.header-references")}
                      </Button>
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      {group.canManage && !group.source && (
                        <>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setEditTarget(group);
                              setEditForm(groupToForm(group));
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
                              setDeleteTarget(group);
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
                {expanded && refs.length > 0 && (
                  <TableRow>
                    <TableCell colSpan={6}>
                      <div className="flex flex-col gap-1 py-1">
                        {refs.map((r) => (
                          <span
                            key={`${r.resourceType}-${r.resource}`}
                            className="font-mono text-xs text-control-light"
                          >
                            {r.resource} ({r.resourceType})
                          </span>
                        ))}
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            );
          })}
          {groups.length === 0 && !loading && (
            <TableRow>
              <TableCell
                colSpan={6}
                className="text-center text-control-light py-8"
              >
                {t("settings.groups.no-groups")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <GroupSheet
        open={createOpen}
        title={t("settings.groups.create-title")}
        description={t("settings.groups.create-description")}
        form={createForm}
        users={activeUsers}
        onSubmit={create}
        submitting={creating}
        emailDisabled={false}
        onClose={() => setCreateOpen(false)}
        onFormChange={setCreateForm}
        onUpdateMember={updateMember}
      />

      <GroupSheet
        open={editOpen}
        title={t("settings.groups.edit-title", {
          title: editTarget?.title ?? "",
        })}
        description={t("settings.groups.edit-description")}
        form={editForm}
        users={activeUsers}
        onSubmit={save}
        submitting={saving}
        emailDisabled={true}
        onClose={() => setEditOpen(false)}
        onFormChange={setEditForm}
        onUpdateMember={updateMember}
      />

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.groups.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.groups.delete-confirm-description", {
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

interface GroupSheetProps {
  open: boolean;
  title: string;
  description: string;
  form: GroupForm;
  users: User[];
  submitting: boolean;
  emailDisabled: boolean;
  onSubmit: () => void;
  onClose: () => void;
  onFormChange: (f: GroupForm) => void;
  onUpdateMember: (
    form: GroupForm,
    setForm: (f: GroupForm) => void,
    index: number,
    patch: Partial<MemberRow>
  ) => void;
}

function GroupSheet({
  open,
  title,
  description,
  form,
  users,
  submitting,
  emailDisabled,
  onSubmit,
  onClose,
  onFormChange,
  onUpdateMember,
}: GroupSheetProps) {
  const { t } = useTranslation();
  const usedMembers = new Set(
    form.members.map((m) => m.member).filter(Boolean)
  );
  const nextMember = users.find((u) => !usedMembers.has(u.name));

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <SheetBody className="flex flex-col gap-4">
          <FieldRow label={t("settings.groups.field-title")} required>
            <Input
              value={form.title}
              onChange={(e) => onFormChange({ ...form, title: e.target.value })}
              placeholder={t("settings.groups.field-title-placeholder")}
            />
          </FieldRow>
          <FieldRow label={t("settings.groups.field-description")}>
            <Input
              value={form.description}
              onChange={(e) =>
                onFormChange({ ...form, description: e.target.value })
              }
              placeholder={t("settings.groups.field-description-placeholder")}
            />
          </FieldRow>
          <FieldRow
            label={t("settings.groups.field-email")}
            hint={t("settings.groups.field-email-hint")}
          >
            <Input
              value={form.email}
              disabled={emailDisabled}
              onChange={(e) => onFormChange({ ...form, email: e.target.value })}
              placeholder={t("settings.groups.field-email-placeholder")}
            />
          </FieldRow>
          <div className="flex flex-col gap-2">
            <FieldRow
              label={t("settings.groups.field-members")}
              required
              hint={t("settings.groups.field-members-hint")}
            >
              <div className="flex flex-col gap-2">
                {form.members.map((row, i) => (
                  <div key={i} className="flex gap-2 items-center">
                    <Select
                      value={row.member}
                      onValueChange={(member) => {
                        // Never allow the same user in two rows: options below
                        // already exclude used members; this guards programmatic
                        // changes as well.
                        if (
                          member &&
                          form.members.some(
                            (m, j) => j !== i && m.member === member
                          )
                        ) {
                          return;
                        }
                        onUpdateMember(form, onFormChange, i, {
                          member: member ?? "",
                        });
                      }}
                    >
                      <SelectTrigger className="flex-1">
                        <SelectValue>
                          {(value) => {
                            const user = users.find((u) => u.name === value);
                            return user
                              ? displayName(user)
                              : t("settings.groups.member-user-placeholder");
                          }}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {users
                          .filter(
                            (u) =>
                              u.name === row.member || !usedMembers.has(u.name)
                          )
                          .map((u) => (
                            <SelectItem key={u.name} value={u.name ?? ""}>
                              {displayName(u)}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                    <Select
                      value={String(row.role)}
                      onValueChange={(role) =>
                        onUpdateMember(form, onFormChange, i, {
                          role: Number(role) as GroupMemberRole,
                        })
                      }
                    >
                      <SelectTrigger className="w-32">
                        <SelectValue>
                          {(value) =>
                            String(value) === String(GroupMemberRole.OWNER)
                              ? t("settings.groups.member-role-owner")
                              : t("settings.groups.member-role-member")
                          }
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={String(GroupMemberRole.OWNER)}>
                          {t("settings.groups.member-role-owner")}
                        </SelectItem>
                        <SelectItem value={String(GroupMemberRole.MEMBER)}>
                          {t("settings.groups.member-role-member")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        onFormChange({
                          ...form,
                          members: form.members.filter((_, j) => j !== i),
                        })
                      }
                    >
                      {t("settings.groups.member-remove")}
                    </Button>
                  </div>
                ))}
                {users.length === 0 ? (
                  <span className="text-xs text-control-placeholder">
                    {t("settings.groups.member-no-users")}
                  </span>
                ) : (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!nextMember}
                    onClick={() =>
                      nextMember &&
                      onFormChange({
                        ...form,
                        members: [
                          ...form.members,
                          {
                            member: nextMember.name ?? "",
                            role: GroupMemberRole.MEMBER,
                          },
                        ],
                      })
                    }
                  >
                    {t("settings.groups.member-add")}
                  </Button>
                )}
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
