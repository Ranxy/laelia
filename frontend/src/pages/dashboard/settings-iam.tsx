import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  ChevronDown,
  ChevronRight,
  Loader2,
  Plus,
  Shield,
  User as UserIcon,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
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
  iamServiceClient,
  roleServiceClient,
} from "@/connect";
import { toastManager } from "@/lib/toast";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/auth";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type Role } from "@/types/proto-es/v1/role_service_pb";

// roleIDFromName extracts the bare id from `roles/{id}`.
function roleIDFromName(name: string | undefined): string {
  if (!name) return "";
  return name.startsWith("roles/") ? name.slice("roles/".length) : name;
}

// NON_GRANTABLE_WORKSPACE_ROLE_IDS lists roles that must not be offered on the
// workspace policy. workspaceMember is the auto-granted authenticated-principal
// baseline, so assigning it is a no-op. (The chat-membership markers and the
// removed agentEditor/reviewer roles are no longer returned by listRoles at
// all, so they do not need listing here.)
const NON_GRANTABLE_WORKSPACE_ROLE_IDS = new Set(["workspaceMember"]);

function isGrantableWorkspaceRole(role: Role): boolean {
  return !NON_GRANTABLE_WORKSPACE_ROLE_IDS.has(roleIDFromName(role.name));
}

interface PolicyState {
  policy: IamPolicy;
  etag: string;
}

export function SettingsIamPage() {
  const { t } = useTranslation();
  const canGet = useHasPermission("laelia.iam.getPolicy");
  const canSet = useHasPermission("laelia.iam.setPolicy");

  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);

  const [policyState, setPolicyState] = useState<PolicyState | null>(null);
  const [roles, setRoles] = useState<Role[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);

  const [assignOpen, setAssignOpen] = useState(false);
  const [selectedMember, setSelectedMember] = useState("");
  const [selectedRoleIds, setSelectedRoleIds] = useState<Set<string>>(
    new Set()
  );
  const [saving, setSaving] = useState(false);
  const [assignError, setAssignError] = useState("");

  // Role-centric sheet state
  const [roleSheetOpen, setRoleSheetOpen] = useState(false);
  const [roleSheetRole, setRoleSheetRole] = useState<Role | null>(null);
  const [roleSheetBindingRole, setRoleSheetBindingRole] = useState("");
  const [roleSheetMembers, setRoleSheetMembers] = useState<Set<string>>(
    new Set()
  );
  const [roleSheetOriginalMembers, setRoleSheetOriginalMembers] = useState<
    Set<string>
  >(new Set());
  const [roleSheetConfirmClose, setRoleSheetConfirmClose] = useState(false);
  const [roleSheetSaving, setRoleSheetSaving] = useState(false);
  const [roleSheetError, setRoleSheetError] = useState("");
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [policyRes, rolesRes, groupsRes] = await Promise.all([
        iamServiceClient.getWorkspaceIamPolicy({}),
        roleServiceClient.listRoles({}),
        groupServiceClient.listGroups({ pageSize: 1000 }),
      ]);
      setPolicyState({
        policy: policyRes.policy ?? create(IamPolicySchema, {}),
        etag: policyRes.etag,
      });
      setRoles(rolesRes.roles ?? []);
      setGroups(groupsRes.groups ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.iam.load-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (canGet) {
      load();
      fetchUsers({ pageSize: 1000 });
    } else {
      setLoading(false);
    }
  }, [canGet, load, fetchUsers]);

  // Grantable roles for the workspace policy, sorted predefined-first then
  // custom by title, so the assign sheet lists the common built-ins at the top.
  const grantableRoles = useMemo(() => {
    return roles.filter(isGrantableWorkspaceRole).sort((a, b) => {
      if (a.predefined !== b.predefined) return a.predefined ? -1 : 1;
      return a.title.localeCompare(b.title);
    });
  }, [roles]);

  const selectedUser = useMemo(() => {
    return users.find((u) => u.name === selectedMember) ?? null;
  }, [users, selectedMember]);

  // userEmailByMember resolves a `users/{uid}` member string to an email for the
  // binding table, falling back to the raw member.
  const userEmailByMember = useMemo(() => {
    const m = new Map<string, string>();
    for (const u of users) m.set(u.name, u.email);
    return m;
  }, [users]);

  // groupByMember resolves a `groups/{id}` or `groups/{email}` member string to
  // its group for display and expansion.
  const groupByMember = useMemo(() => {
    const m = new Map<string, Group>();
    for (const g of groups) {
      m.set(g.name ?? "", g);
      if (g.email) m.set(`groups/${g.email}`, g);
    }
    return m;
  }, [groups]);

  // Which members are newly added (pending, not yet saved) in the role sheet.
  const roleSheetNewMembers = useMemo(() => {
    const added = new Set<string>();
    for (const m of roleSheetMembers) {
      if (!roleSheetOriginalMembers.has(m)) added.add(m);
    }
    return added;
  }, [roleSheetMembers, roleSheetOriginalMembers]);

  const roleSheetHasChanges = useMemo(() => {
    return (
      roleSheetMembers.size !== roleSheetOriginalMembers.size ||
      [...roleSheetMembers].some((m) => !roleSheetOriginalMembers.has(m))
    );
  }, [roleSheetMembers, roleSheetOriginalMembers]);

  function memberLabel(member: string): string {
    if (member === "allUsers") return t("settings.iam.member-all-users");
    if (member.startsWith("users/")) {
      return userEmailByMember.get(member) ?? member;
    }
    if (member.startsWith("groups/")) {
      return groupByMember.get(member)?.title ?? member.slice("groups/".length);
    }
    if (member.startsWith("agents/")) return member;
    return member;
  }

  function tryCloseRoleSheet() {
    if (roleSheetHasChanges) {
      setRoleSheetConfirmClose(true);
    } else {
      setRoleSheetOpen(false);
    }
  }

  function forceCloseRoleSheet() {
    setRoleSheetConfirmClose(false);
    setRoleSheetOpen(false);
    setRoleSheetRole(null);
    setRoleSheetBindingRole("");
    setRoleSheetMembers(new Set());
    setRoleSheetOriginalMembers(new Set());
  }

  function openAssign() {
    setSelectedMember("");
    setSelectedRoleIds(new Set());
    setAssignError("");
    setAssignOpen(true);
  }

  // When the selected member (user, group, or allUsers) changes, pre-check the
  // roles they currently hold on the workspace policy (only grantable ones).
  function onMemberChange(name: string) {
    setSelectedMember(name);
    const held = new Set<string>();
    const policy = policyState?.policy;
    if (policy) {
      for (const binding of policy.bindings) {
        if (binding.members.includes(name)) {
          const id = roleIDFromName(binding.role);
          if (!NON_GRANTABLE_WORKSPACE_ROLE_IDS.has(id)) {
            held.add(binding.role);
          }
        }
      }
    }
    setSelectedRoleIds(held);
  }

  function toggleRole(roleName: string) {
    setSelectedRoleIds((prev) => {
      const next = new Set(prev);
      if (next.has(roleName)) next.delete(roleName);
      else next.add(roleName);
      return next;
    });
  }

  // buildEditedPolicy produces the new workspace policy: for every grantable
  // role, set the selected user's membership to exactly the checked state,
  // leaving other members and non-grantable bindings untouched. Empty bindings
  // (no members left) are dropped.
  function buildEditedPolicy(): IamPolicy {
    const policy = policyState?.policy ?? create(IamPolicySchema, {});
    const byRole = new Map<string, Binding>();
    for (const b of policy.bindings)
      byRole.set(
        b.role,
        create(BindingSchema, { role: b.role, members: [...b.members] })
      );

    for (const role of grantableRoles) {
      const binding =
        byRole.get(role.name) ??
        create(BindingSchema, { role: role.name, members: [] });
      const members = binding.members.filter((m) => m !== selectedMember);
      if (selectedRoleIds.has(role.name)) members.push(selectedMember);
      binding.members = members;
      byRole.set(role.name, binding);
    }

    const bindings: Binding[] = [];
    for (const b of byRole.values()) {
      if (b.members.length > 0) bindings.push(b);
    }
    return create(IamPolicySchema, { bindings });
  }

  // buildEditedPolicyForRole replaces or drops only the single targeted role
  // binding, leaving all other bindings untouched. New members replace the old
  // ones in full. An empty set drops the binding.
  function buildEditedPolicyForRole(
    roleName: string,
    newMembers: Set<string>
  ): IamPolicy {
    const policy = policyState?.policy ?? create(IamPolicySchema, {});
    const bindings: Binding[] = [];
    let found = false;

    for (const b of policy.bindings) {
      if (b.role === roleName) {
        found = true;
        if (newMembers.size > 0) {
          bindings.push(
            create(BindingSchema, {
              role: roleName,
              members: [...newMembers],
            })
          );
        }
      } else {
        bindings.push(
          create(BindingSchema, { role: b.role, members: [...b.members] })
        );
      }
    }

    if (!found && newMembers.size > 0) {
      bindings.push(
        create(BindingSchema, { role: roleName, members: [...newMembers] })
      );
    }

    return create(IamPolicySchema, { bindings });
  }

  function openRoleSheet(binding: Binding) {
    const role = roles.find((r) => r.name === binding.role) ?? null;
    const members = new Set(binding.members);
    setRoleSheetRole(role);
    setRoleSheetBindingRole(binding.role);
    setRoleSheetMembers(new Set(members));
    setRoleSheetOriginalMembers(new Set(members));
    setRoleSheetError("");
    setRoleSheetOpen(true);
  }

  function handleAddMember(name: string) {
    if (!name || roleSheetMembers.has(name)) return;
    setRoleSheetMembers((prev) => new Set(prev).add(name));
  }

  function handleRemoveMember(name: string) {
    setRoleSheetMembers((prev) => {
      const next = new Set(prev);
      next.delete(name);
      return next;
    });
  }

  async function handleSaveRoleSheet() {
    if (!policyState || !roleSheetBindingRole) return;
    setRoleSheetError("");
    setRoleSheetSaving(true);
    try {
      const policy = buildEditedPolicyForRole(
        roleSheetBindingRole,
        roleSheetMembers
      );
      const res = await iamServiceClient.setWorkspaceIamPolicy({
        policy,
        etag: policyState.etag,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      toastManager.add({ type: "success", title: t("settings.iam.saved") });
      setRoleSheetOpen(false);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        setRoleSheetError(t("settings.iam.etag-mismatch"));
        await load();
      } else {
        setRoleSheetError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setRoleSheetSaving(false);
    }
  }

  async function handleSaveAssign() {
    if (!policyState || !selectedMember) return;
    setAssignError("");
    setSaving(true);
    try {
      const policy = buildEditedPolicy();
      const res = await iamServiceClient.setWorkspaceIamPolicy({
        policy,
        etag: policyState.etag,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      toastManager.add({ type: "success", title: t("settings.iam.saved") });
      setAssignOpen(false);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        // Etag mismatch: another writer changed the policy. Re-fetch so the
        // sheet reflects the latest state and let the admin retry.
        setAssignError(t("settings.iam.etag-mismatch"));
        await load();
      } else {
        setAssignError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  }

  if (!canGet) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">
          {t("settings.iam.not-allowed")}
        </p>
      </div>
    );
  }

  const visibleBindings = policyState?.policy.bindings ?? [];

  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-5 w-full">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold text-main flex items-center gap-2">
            <Shield className="size-5 text-accent" />
            {t("settings.iam.title")}
          </h1>
          <p className="text-sm text-control-light max-w-2xl">
            {t("settings.iam.description")}
          </p>
        </div>
        {canSet && (
          <Button onClick={openAssign}>{t("settings.iam.assign")}</Button>
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
                <TableHead className="w-[40%]">
                  {t("settings.iam.header-role")}
                </TableHead>
                <TableHead>{t("settings.iam.header-members")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleBindings.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={2}
                    className="text-center text-control-light py-12"
                  >
                    {t("common.no-data")}
                  </TableCell>
                </TableRow>
              ) : (
                visibleBindings.map((binding) => {
                  const role = roles.find((r) => r.name === binding.role);
                  return (
                    <TableRow
                      key={binding.role}
                      onClick={
                        canSet ? () => openRoleSheet(binding) : undefined
                      }
                      className={cn(canSet && "cursor-pointer")}
                    >
                      <TableCell className="font-medium align-top">
                        <div className="flex items-center gap-2">
                          <span className="text-main">
                            {role?.title ?? roleIDFromName(binding.role)}
                          </span>
                          {role?.predefined && (
                            <Badge variant="success" className="w-fit text-xs">
                              {t("settings.roles.type-predefined")}
                            </Badge>
                          )}
                          {!role?.predefined && role && (
                            <Badge variant="warning" className="w-fit text-xs">
                              {t("settings.roles.type-custom")}
                            </Badge>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="align-top">
                        <div className="flex flex-wrap gap-1.5">
                          {binding.members.map((m) => (
                            <Badge key={m} variant="secondary">
                              {memberLabel(m)}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Assign roles to user */}
      <Sheet
        open={assignOpen}
        onOpenChange={(next) => {
          setAssignOpen(next);
          if (!next) setSelectedMember("");
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{t("settings.iam.assign-title")}</SheetTitle>
            <SheetDescription>
              {t("settings.iam.assign-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {assignError && (
              <Alert
                variant="error"
                description={assignError}
                className="mb-2"
              />
            )}
            <div className="flex flex-col gap-5">
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("settings.iam.field-member")}
                </label>
                <MemberPicker
                  users={users}
                  groups={groups}
                  value={selectedMember}
                  onSelect={onMemberChange}
                  allowAllUsers
                />
              </div>

              {selectedMember && (
                <div className="flex items-center gap-3 rounded-xs border border-control-border bg-control-bg/50 p-3">
                  <div className="flex size-9 items-center justify-center rounded-full bg-accent/10 text-accent">
                    {selectedMember.startsWith("groups/") ? (
                      <Shield className="size-4.5" />
                    ) : (
                      <UserIcon className="size-4.5" />
                    )}
                  </div>
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="text-sm font-medium text-main truncate">
                      {memberLabel(selectedMember)}
                    </span>
                    {selectedUser?.title && (
                      <span className="text-xs text-control-light truncate">
                        {selectedUser.title}
                      </span>
                    )}
                    {selectedMember === "allUsers" && (
                      <span className="text-xs text-control-light truncate">
                        {t("settings.iam.member-all-users-hint")}
                      </span>
                    )}
                    {selectedMember.startsWith("groups/") &&
                      groupByMember.get(selectedMember) && (
                        <span className="text-xs text-control-light truncate">
                          {t("settings.iam.member-picker-members-count", {
                            count:
                              groupByMember.get(selectedMember)?.members
                                ?.length ?? 0,
                          })}
                        </span>
                      )}
                  </div>
                </div>
              )}

              {selectedMember && (
                <div className="flex flex-col gap-2">
                  <span className="text-xs font-semibold uppercase tracking-wide text-control">
                    {t("settings.iam.field-roles")}
                  </span>
                  {grantableRoles.length === 0 ? (
                    <p className="text-sm text-control-light">
                      {t("settings.iam.no-grantable-roles")}
                    </p>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {grantableRoles.map((role) => (
                        <label
                          key={role.name}
                          className="flex cursor-pointer items-start gap-3 rounded-xs border border-control-border bg-background p-3 transition-colors hover:bg-control-bg/60 hover:border-accent/30 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-accent"
                          title={role.description}
                        >
                          <Checkbox
                            checked={selectedRoleIds.has(role.name)}
                            onCheckedChange={() => toggleRole(role.name)}
                            size="md"
                            className="mt-0.5"
                          />
                          <div className="flex flex-1 flex-col gap-1 min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-medium text-main">
                                {role.title}
                              </span>
                              {role.predefined ? (
                                <Badge variant="success" className="text-xs">
                                  {t("settings.roles.type-predefined")}
                                </Badge>
                              ) : (
                                <Badge variant="warning" className="text-xs">
                                  {t("settings.roles.type-custom")}
                                </Badge>
                              )}
                            </div>
                            {role.description && (
                              <p className="text-xs text-control-light leading-relaxed">
                                {role.description}
                              </p>
                            )}
                          </div>
                        </label>
                      ))}
                    </div>
                  )}
                  <p className="text-xs text-control-placeholder">
                    {t("settings.iam.field-roles-hint")}
                  </p>
                </div>
              )}
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setAssignOpen(false)}
              disabled={saving}
            >
              {t("common.cancel")}
            </Button>
            <Button
              disabled={saving || !selectedMember}
              onClick={handleSaveAssign}
            >
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Manage role members */}
      <Sheet
        open={roleSheetOpen}
        onOpenChange={(next) => {
          if (!next) {
            tryCloseRoleSheet();
            return;
          }
          setRoleSheetOpen(next);
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("settings.iam.role-sheet-title", {
                title:
                  roleSheetRole?.title ?? roleIDFromName(roleSheetBindingRole),
              })}
            </SheetTitle>
            <SheetDescription>
              {t("settings.iam.role-sheet-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {roleSheetError && (
              <Alert
                variant="error"
                description={roleSheetError}
                className="mb-2"
              />
            )}

            <div className="flex flex-col gap-5">
              {/* Role info header */}
              {roleSheetRole && (
                <div className="flex flex-col gap-2">
                  {roleSheetRole.description && (
                    <p className="text-sm text-control-light leading-relaxed">
                      {roleSheetRole.description}
                    </p>
                  )}
                  <div className="flex gap-2">
                    {roleSheetRole.predefined ? (
                      <Badge variant="success" className="w-fit">
                        {t("settings.roles.type-predefined")}
                      </Badge>
                    ) : (
                      <Badge variant="warning" className="w-fit">
                        {t("settings.roles.type-custom")}
                      </Badge>
                    )}
                  </div>
                </div>
              )}

              {/* Current members */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("settings.iam.role-sheet-current-members")}
                </label>
                <div className="max-h-72 overflow-y-auto pr-1">
                  {roleSheetMembers.size === 0 ? (
                    <p className="text-sm text-control-light py-4 text-center border border-dashed border-control-border rounded-xs">
                      {t("settings.iam.role-sheet-no-members")}
                    </p>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {[...roleSheetMembers]
                        .sort((a, b) =>
                          (memberLabel(a) ?? a).localeCompare(
                            memberLabel(b) ?? b
                          )
                        )
                        .map((member) => {
                          const user = users.find((u) => u.name === member);
                          const group = member.startsWith("groups/")
                            ? groupByMember.get(member)
                            : undefined;
                          const isNew = roleSheetNewMembers.has(member);
                          const isGroupExpanded = expandedGroups.has(member);
                          return (
                            <div key={member} className="flex flex-col gap-2">
                              <div
                                className={cn(
                                  "flex items-center gap-3 rounded-xs border p-3 transition-colors",
                                  isNew
                                    ? "border-dashed border-accent/40 bg-accent/[0.03]"
                                    : "border-control-border bg-background hover:bg-control-bg/60"
                                )}
                              >
                                <div
                                  className={cn(
                                    "flex size-9 items-center justify-center rounded-full shrink-0 bg-accent/10 text-accent"
                                  )}
                                >
                                  {isNew ? (
                                    <Plus className="size-4.5" />
                                  ) : group ? (
                                    <Shield className="size-4.5" />
                                  ) : (
                                    <UserIcon className="size-4.5" />
                                  )}
                                </div>
                                <div className="flex flex-col gap-0.5 min-w-0 flex-1">
                                  <div className="flex items-center gap-2">
                                    <span className="text-sm font-medium text-main truncate">
                                      {memberLabel(member)}
                                    </span>
                                    {isNew && (
                                      <Badge
                                        variant="warning"
                                        className="text-xs shrink-0"
                                      >
                                        {t("settings.iam.role-sheet-pending")}
                                      </Badge>
                                    )}
                                  </div>
                                  {user?.title && (
                                    <span className="text-xs text-control-light truncate">
                                      {user.title}
                                    </span>
                                  )}
                                  {group && (
                                    <span className="text-xs text-control-light truncate">
                                      {t(
                                        "settings.iam.member-picker-members-count",
                                        { count: group.members?.length ?? 0 }
                                      )}
                                    </span>
                                  )}
                                </div>
                                {group && (
                                  <Button
                                    variant="ghost"
                                    size="xs"
                                    aria-label={t(
                                      isGroupExpanded
                                        ? "settings.iam.role-sheet-collapse-group"
                                        : "settings.iam.role-sheet-expand-group",
                                      { title: group.title }
                                    )}
                                    onClick={() =>
                                      setExpandedGroups((prev) => {
                                        const next = new Set(prev);
                                        if (next.has(member))
                                          next.delete(member);
                                        else next.add(member);
                                        return next;
                                      })
                                    }
                                  >
                                    {isGroupExpanded ? (
                                      <ChevronDown className="size-4" />
                                    ) : (
                                      <ChevronRight className="size-4" />
                                    )}
                                  </Button>
                                )}
                                <Button
                                  variant="ghost"
                                  size="xs"
                                  onClick={() => handleRemoveMember(member)}
                                  aria-label={t(
                                    "settings.iam.role-sheet-remove-member",
                                    {
                                      email: memberLabel(member),
                                    }
                                  )}
                                  className="shrink-0 text-control-light hover:text-error"
                                >
                                  <X className="size-4" />
                                </Button>
                              </div>
                              {isGroupExpanded && group && (
                                <div className="flex flex-col gap-1 pl-12">
                                  {group.members?.map((gm) => {
                                    const gu = users.find(
                                      (u) => u.name === gm.member
                                    );
                                    return (
                                      <span
                                        key={gm.member}
                                        className="text-xs text-control-light truncate"
                                      >
                                        {gu
                                          ? gu.title || gu.email || gm.member
                                          : gm.member}
                                      </span>
                                    );
                                  })}
                                </div>
                              )}
                            </div>
                          );
                        })}
                    </div>
                  )}
                </div>
              </div>

              {/* Add member section */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("settings.iam.role-sheet-field-add-member")}
                </label>
                <p className="text-xs text-control-placeholder">
                  {t("settings.iam.role-sheet-field-add-member-hint")}
                </p>
                <MemberPicker
                  users={users.filter((u) => !roleSheetMembers.has(u.name))}
                  groups={groups.filter((g) => !roleSheetMembers.has(g.name))}
                  value=""
                  onSelect={handleAddMember}
                  allowAllUsers={!roleSheetMembers.has("allUsers")}
                />
              </div>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={tryCloseRoleSheet}
              disabled={roleSheetSaving}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={roleSheetSaving} onClick={handleSaveRoleSheet}>
              {roleSheetSaving ? (
                <Loader2 className="size-4 animate-spin" />
              ) : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Confirm discard unsaved changes */}
      <AlertDialog
        open={roleSheetConfirmClose}
        onOpenChange={setRoleSheetConfirmClose}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.iam.role-sheet-discard-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.iam.role-sheet-discard-description")}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline">{t("common.cancel")}</Button>
            </AlertDialogClose>
            <Button variant="destructive" onClick={forceCloseRoleSheet}>
              {t("common.discard")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
