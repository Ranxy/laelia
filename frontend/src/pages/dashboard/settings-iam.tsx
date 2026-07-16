import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { Loader2, Shield, User as UserIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { iamServiceClient, roleServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/auth";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import { type Role } from "@/types/proto-es/v1/role_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

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
  const [loading, setLoading] = useState(true);

  const [assignOpen, setAssignOpen] = useState(false);
  const [selectedUserName, setSelectedUserName] = useState("");
  const [selectedRoleIds, setSelectedRoleIds] = useState<Set<string>>(
    new Set()
  );
  const [saving, setSaving] = useState(false);
  const [assignError, setAssignError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [policyRes, rolesRes] = await Promise.all([
        iamServiceClient.getWorkspaceIamPolicy({}),
        roleServiceClient.listRoles({}),
      ]);
      setPolicyState({
        policy: policyRes.policy ?? create(IamPolicySchema, {}),
        etag: policyRes.etag,
      });
      setRoles(rolesRes.roles ?? []);
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
      fetchUsers({ pageSize: 100 });
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
    return users.find((u) => u.name === selectedUserName) ?? null;
  }, [users, selectedUserName]);

  // userEmailByMember resolves a `users/{uid}` member string to an email for the
  // binding table, falling back to the raw member.
  const userEmailByMember = useMemo(() => {
    const m = new Map<string, string>();
    for (const u of users) m.set(u.name, u.email);
    return m;
  }, [users]);

  function memberLabel(member: string): string {
    if (member === "allUsers") return t("settings.iam.member-all-users");
    if (member.startsWith("users/")) {
      return userEmailByMember.get(member) ?? member;
    }
    if (member.startsWith("groups/")) return member.slice("groups/".length);
    if (member.startsWith("agents/")) return member;
    return member;
  }

  function openAssign() {
    setSelectedUserName("");
    setSelectedRoleIds(new Set());
    setAssignError("");
    setAssignOpen(true);
  }

  // When the selected user changes, pre-check the roles they currently hold on
  // the workspace policy (only grantable ones).
  function onUserChange(name: string) {
    setSelectedUserName(name);
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
      const members = binding.members.filter((m) => m !== selectedUserName);
      if (selectedRoleIds.has(role.name)) members.push(selectedUserName);
      binding.members = members;
      byRole.set(role.name, binding);
    }

    const bindings: Binding[] = [];
    for (const b of byRole.values()) {
      if (b.members.length > 0) bindings.push(b);
    }
    return create(IamPolicySchema, { bindings });
  }

  async function handleSaveAssign() {
    if (!policyState || !selectedUserName) return;
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
                    <TableRow key={binding.role}>
                      <TableCell className="font-medium align-top">
                        <div className="flex flex-col gap-1">
                          <span className="text-main">
                            {role?.title ?? roleIDFromName(binding.role)}
                          </span>
                          {role?.predefined && (
                            <Badge variant="success" className="w-fit">
                              {t("settings.roles.type-predefined")}
                            </Badge>
                          )}
                          {!role?.predefined && role && (
                            <Badge variant="warning" className="w-fit">
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
          if (!next) setSelectedUserName("");
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
                <label
                  htmlFor="assign-user"
                  className="text-xs font-semibold uppercase tracking-wide text-control"
                >
                  {t("settings.iam.field-user")}
                </label>
                <Select
                  value={selectedUserName}
                  onValueChange={(v) => onUserChange(String(v ?? ""))}
                >
                  <SelectTrigger id="assign-user" className="w-full">
                    <SelectValue
                      placeholder={t("settings.iam.field-user-placeholder")}
                    >
                      {(v: string | null) => {
                        const u = users.find((x) => x.name === v);
                        return u?.email ?? v ?? "";
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {users.length === 0 ? (
                      <SelectItem value="" disabled>
                        {t("settings.iam.no-users")}
                      </SelectItem>
                    ) : (
                      users.map((u: User) => (
                        <SelectItem key={u.name} value={u.name}>
                          {u.email}
                        </SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
              </div>

              {selectedUser && (
                <div className="flex items-center gap-3 rounded-xs border border-control-border bg-control-bg/50 p-3">
                  <div className="flex size-9 items-center justify-center rounded-full bg-accent/10 text-accent">
                    <UserIcon className="size-4.5" />
                  </div>
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="text-sm font-medium text-main truncate">
                      {selectedUser.email}
                    </span>
                    {selectedUser.title && (
                      <span className="text-xs text-control-light truncate">
                        {selectedUser.title}
                      </span>
                    )}
                  </div>
                </div>
              )}

              {selectedUserName && (
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
              disabled={saving || !selectedUserName}
              onClick={handleSaveAssign}
            >
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  );
}
