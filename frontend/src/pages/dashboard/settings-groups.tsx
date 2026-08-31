import { Pencil, Plus, Trash2 } from "lucide-react";
import { Fragment, useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmActionDialog } from "@/components/settings/confirm-action-dialog";
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
import { useCrudDialog } from "@/composables/use-crud-dialog";
import { useResourceQuery } from "@/composables/use-resource-query";
import { groupServiceClient, userServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { displayName } from "@/lib/members";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
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

export function SettingsGroupsPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.groups.list");
  const canCreate = useHasPermission("laelia.groups.create");

  const groupsQuery = useResourceQuery<Group>({
    enabled: canList,
    queryKey: ["settings", "groups"],
    queryFn: async (signal) =>
      (await groupServiceClient.listGroups({ pageSize: 1000 }, { signal }))
        .groups ?? [],
    failureTitle: t("settings.groups.load-failed"),
  });
  // Workspace users directory: shared with the other member-editor pages via
  // the ["directory","users"] entry (60s TTL), not refetched per page visit.
  const usersQuery = useResourceQuery<User>({
    enabled: canList,
    queryKey: ["directory", "users"],
    queryFn: async (signal) =>
      (await userServiceClient.listUsers({ pageSize: 1000 }, { signal }))
        .users ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });

  const groups = groupsQuery.items;
  const activeUsers = useMemo(
    () => usersQuery.items.filter((u) => u.state === State.ACTIVE),
    [usersQuery.items]
  );

  const crud = useCrudDialog<Group>({
    // Post-mutation refresh of the resource list (only the resource list —
    // group CRUD does not change the user directory).
    onChanged: () => void groupsQuery.reload(),
  });

  // References are fetched lazily per group when its references row is
  // expanded (see toggleRefs) — fetching them for every group on load was
  // an N+1 burst that most pages never displayed.
  const [refsByGroup, setRefsByGroup] = useState<Map<string, GroupReference[]>>(
    new Map()
  );
  const [expandedRefs, setExpandedRefs] = useState<Set<string>>(new Set());
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

  const hasOwner = (form: GroupForm) =>
    form.members.some((m) => m.role === GroupMemberRole.OWNER);

  const handleCreateForm = async (form: GroupForm) => {
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.title-required"),
      });
      return;
    }
    if (!hasOwner(form)) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.at-least-one-owner"),
      });
      return;
    }
    await crud.runCreate(
      async () => {
        await groupServiceClient.createGroup({
          groupEmail: form.email.trim().toLowerCase(),
          group: {
            title: form.title,
            description: form.description,
            members: form.members,
          },
        });
      },
      {
        successTitle: t("settings.groups.created"),
        onError: (err) => {
          void showErrorToast(err, t("settings.groups.create-title"));
        },
      }
    );
  };

  const handleSaveForm = async (form: GroupForm) => {
    const target = crud.editTarget;
    if (!target) return;
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.title-required"),
      });
      return;
    }
    if (!hasOwner(form)) {
      toastManager.add({
        type: "error",
        title: t("settings.groups.at-least-one-owner"),
      });
      return;
    }
    await crud.runSave(
      async () => {
        const paths = ["title", "description"];
        if (
          JSON.stringify(form.members) !==
          JSON.stringify(groupToForm(target).members)
        ) {
          paths.push("members");
        }
        await groupServiceClient.updateGroup({
          group: {
            name: target.name,
            title: form.title,
            description: form.description,
            members: form.members,
          },
          updateMask: { paths },
        });
      },
      {
        successTitle: t("settings.groups.updated"),
        onError: (err) => {
          toastManager.add({
            type: "error",
            title: t("settings.groups.edit-title", { title: target.title }),
            description: describeError(err),
          });
        },
      }
    );
  };

  const handleDelete = async () => {
    const target = crud.deleteTarget;
    if (!target) return;
    await crud.runDelete(
      async () => {
        await groupServiceClient.deleteGroup({ name: target.name });
      },
      {
        successTitle: t("settings.groups.deleted"),
        onError: (err) => {
          void showErrorToast(err, t("settings.groups.delete-failed"));
        },
      }
    );
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
          <Button onClick={crud.openCreate}>
            <Plus className="w-4 h-4" />
            {t("settings.groups.create")}
          </Button>
        )
      }
    >
      {groupsQuery.initialLoading ? (
        <PageLoading />
      ) : (
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
                              onClick={() => crud.openEdit(group)}
                              aria-label={t("common.edit")}
                              title={t("common.edit")}
                            >
                              <Pencil className="w-4 h-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-error"
                              onClick={() => crud.openDelete(group)}
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
            {groups.length === 0 && !groupsQuery.refreshing && (
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
      )}

      <ResourceSheet
        open={crud.createOpen}
        entity={null}
        title={t("settings.groups.create-title")}
        description={t("settings.groups.create-description")}
        submitting={crud.creating}
        onClose={crud.closeCreate}
        renderForm={({ formId }) => (
          <GroupFormFields
            entity={null}
            formId={formId}
            users={activeUsers}
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
          t("settings.groups.edit-title", { title: target?.title ?? "" })
        }
        description={t("settings.groups.edit-description")}
        submitting={crud.saving}
        onClose={crud.closeEdit}
        renderForm={({ entity, formId }) =>
          entity ? (
            <GroupFormFields
              entity={entity}
              formId={formId}
              users={activeUsers}
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
        title={t("settings.groups.delete-confirm-title")}
        description={t("settings.groups.delete-confirm-description", {
          title: crud.deleteTarget?.title ?? "",
        })}
        onConfirm={() => {
          void handleDelete();
        }}
      />
    </SettingsPage>
  );
}

interface GroupFormFieldsProps {
  // null seeds an empty create form; a group seeds its edit form.
  entity: Group | null;
  formId: string;
  users: User[];
  onSubmit: (form: GroupForm) => void;
}

// Inner form of the group drawer. Mounts fresh per open (ResourceSheet keys
// on the open sequence), so useState seeds read the current entity.
function GroupFormFields({
  entity,
  formId,
  users,
  onSubmit,
}: GroupFormFieldsProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<GroupForm>(() =>
    entity ? groupToForm(entity) : emptyForm()
  );

  const usedMembers = new Set(
    form.members.map((m) => m.member).filter(Boolean)
  );
  const nextMember = users.find((u) => !usedMembers.has(u.name));

  const setMember = (index: number, patch: Partial<MemberRow>) => {
    setForm((f) => {
      const next = { ...f, members: [...f.members] };
      next.members[index] = { ...next.members[index], ...patch };
      return next;
    });
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
      <FieldRow label={t("settings.groups.field-title")} required>
        <Input
          value={form.title}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          placeholder={t("settings.groups.field-title-placeholder")}
        />
      </FieldRow>
      <FieldRow label={t("settings.groups.field-description")}>
        <Input
          value={form.description}
          onChange={(e) =>
            setForm((f) => ({ ...f, description: e.target.value }))
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
          disabled={entity !== null}
          onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
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
                    setMember(i, { member: member ?? "" });
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
                        (u) => u.name === row.member || !usedMembers.has(u.name)
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
                    setMember(i, { role: Number(role) as GroupMemberRole })
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
                    setForm((f) => ({
                      ...f,
                      members: f.members.filter((_, j) => j !== i),
                    }))
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
                onClick={() => {
                  if (!nextMember) return;
                  setForm((f) => ({
                    ...f,
                    members: [
                      ...f.members,
                      {
                        member: nextMember.name ?? "",
                        role: GroupMemberRole.MEMBER,
                      },
                    ],
                  }));
                }}
              >
                {t("settings.groups.member-add")}
              </Button>
            )}
          </div>
        </FieldRow>
      </div>
    </form>
  );
}
