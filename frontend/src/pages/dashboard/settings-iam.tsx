import { Code, ConnectError } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { Loader2 } from "lucide-react";
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
  BindingSchema,
  type Binding,
  IamPolicySchema,
  type IamPolicy,
} from "@/types/proto-es/store/policy_pb";
import { type Role } from "@/types/proto-es/v1/role_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

// roleIDFromName extracts the bare id from `roles/{id}`.
function roleIDFromName(name: string | undefined): string {
  if (!name) return "";
  return name.startsWith("roles/") ? name.slice("roles/".length) : name;
}

// NON_GRANTABLE_WORKSPACE_ROLE_IDS lists predefined roles that must not be
// offered on the workspace policy: workspaceMember is auto-granted to everyone,
// the conversation* roles are chat-membership markers managed on the channel,
// and agentEditor is agent-scoped (set per-agent, not on the workspace). The
// backend handler rejects these on SetWorkspaceIamPolicy, so the UI must not
// offer them.
const NON_GRANTABLE_WORKSPACE_ROLE_IDS = new Set([
  "workspaceMember",
  "conversationMember",
  "conversationAdmin",
  "conversationOwner",
  "agentEditor",
]);

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
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">
          {t("settings.iam.title")}
        </h1>
        {canSet && (
          <Button onClick={openAssign}>{t("settings.iam.assign")}</Button>
        )}
      </div>

      <p className="text-sm text-control-light">
        {t("settings.iam.description")}
      </p>

      {loading ? (
        <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
          <Loader2 className="size-4 animate-spin" />
          {t("common.loading")}
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("settings.iam.header-role")}</TableHead>
              <TableHead>{t("settings.iam.header-members")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visibleBindings.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={2}
                  className="text-center text-control-light"
                >
                  {t("common.no-data")}
                </TableCell>
              </TableRow>
            ) : (
              visibleBindings.map((binding) => {
                const role = roles.find((r) => r.name === binding.role);
                return (
                  <TableRow key={binding.role}>
                    <TableCell className="font-medium">
                      <div className="flex flex-col gap-0.5">
                        <span>
                          {role?.title ?? roleIDFromName(binding.role)}
                        </span>
                        {role?.predefined && (
                          <Badge variant="success" className="w-fit">
                            {t("settings.roles.type-predefined")}
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
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
      )}

      {/* Assign roles to user */}
      <Sheet
        open={assignOpen}
        onOpenChange={(next) => {
          setAssignOpen(next);
          if (!next) setSelectedUserName("");
        }}
      >
        <SheetContent width="standard">
          <SheetTitle>{t("settings.iam.assign-title")}</SheetTitle>
          <SheetDescription>
            {t("settings.iam.assign-description")}
          </SheetDescription>
          <SheetBody>
            {assignError && (
              <Alert
                variant="error"
                description={assignError}
                className="mb-4"
              />
            )}
            <div className="flex flex-col gap-4">
              <label className="block">
                <span className="mb-1.5 block text-xs font-medium text-control">
                  {t("settings.iam.field-user")}
                </span>
                <Select
                  value={selectedUserName}
                  onValueChange={(v) => onUserChange(String(v ?? ""))}
                >
                  <SelectTrigger>
                    <SelectValue
                      placeholder={t("settings.iam.field-user-placeholder")}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {users.map((u: User) => (
                      <SelectItem key={u.name} value={u.name}>
                        {u.email}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </label>

              {selectedUserName && (
                <div className="flex flex-col gap-2">
                  <span className="text-xs font-medium text-control">
                    {t("settings.iam.field-roles")}
                  </span>
                  <div className="flex flex-col gap-1.5">
                    {grantableRoles.map((role) => (
                      <label
                        key={role.name}
                        className="flex items-center gap-2 text-sm text-main"
                        title={role.description}
                      >
                        <Checkbox
                          checked={selectedRoleIds.has(role.name)}
                          onCheckedChange={() => toggleRole(role.name)}
                          size="sm"
                        />
                        <span className="flex-1">{role.title}</span>
                        {role.predefined && (
                          <Badge variant="success">
                            {t("settings.roles.type-predefined")}
                          </Badge>
                        )}
                      </label>
                    ))}
                  </div>
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
