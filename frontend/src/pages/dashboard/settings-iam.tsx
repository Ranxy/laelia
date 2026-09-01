import { Shield } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
  roleServiceClient,
  userServiceClient,
} from "@/connect";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { roleIDFromName } from "@/lib/resource";
import { cn } from "@/lib/utils";
import {
  AssignRolesSheet,
  isGrantableWorkspaceRole,
} from "@/pages/dashboard/iam-assign-roles-sheet";
import {
  RoleMembersSheet,
  type RoleMembersTarget,
} from "@/pages/dashboard/iam-role-members-sheet";
import { useIamPolicy } from "@/pages/dashboard/use-iam-policy";
import { useHasPermission } from "@/stores/permissions";
import { type Binding } from "@/types/proto-es/store/policy_pb";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type Role } from "@/types/proto-es/v1/role_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

export function SettingsIamPage() {
  const { t } = useTranslation();
  const canGet = useHasPermission("laelia.iam.getPolicy");
  const canSet = useHasPermission("laelia.iam.setPolicy");

  // The policy resource + etag: page-scoped query whose load failure toasts
  // once per failure episode, and whose savePolicy throws so the sheets keep
  // their own etag-mismatch handling.
  const {
    policyState,
    savePolicy,
    reload,
    initialLoading: policyLoading,
  } = useIamPolicy({
    enabled: canGet,
    failureTitle: t("settings.iam.load-failed"),
  });

  const rolesQuery = useResourceQuery<Role>({
    enabled: canGet,
    queryKey: ["settings", "roles"],
    // Same call shape as the old load(): listRoles({}) with no pageSize.
    queryFn: async (signal) =>
      (await roleServiceClient.listRoles({}, { signal })).roles ?? [],
    failureTitle: t("settings.iam.load-failed"),
  });

  // Workspace users/groups directories: shared ["directory",…] entries
  // (60s TTL). The users query replaces the old fetchUsers call on the global
  // store slice, whose catch silently wiped the shared users cache (01-B8) —
  // the page now only reads the directory, never writes the store.
  const groupsQuery = useResourceQuery<Group>({
    enabled: canGet,
    queryKey: ["directory", "groups"],
    queryFn: async (signal) =>
      (await groupServiceClient.listGroups({ pageSize: 1000 }, { signal }))
        .groups ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });
  const usersQuery = useResourceQuery<User>({
    enabled: canGet,
    queryKey: ["directory", "users"],
    queryFn: async (signal) =>
      (await userServiceClient.listUsers({ pageSize: 1000 }, { signal }))
        .users ?? [],
    failureTitle: t("settings.directory.load-failed"),
    staleTime: 60_000,
  });

  const roles = rolesQuery.items;
  const groups = groupsQuery.items;
  const users = usersQuery.items;

  const [assignOpen, setAssignOpen] = useState(false);
  const [roleSheetOpen, setRoleSheetOpen] = useState(false);
  const [roleSheetTarget, setRoleSheetTarget] =
    useState<RoleMembersTarget | null>(null);

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

  function openRoleSheet(binding: Binding) {
    const role = roles.find((r) => r.name === binding.role) ?? null;
    setRoleSheetTarget({
      role,
      bindingRole: binding.role,
      members: [...binding.members],
    });
    setRoleSheetOpen(true);
  }

  if (!canGet) {
    return <PermissionNotice message={t("settings.iam.not-allowed")} />;
  }

  const visibleBindings = policyState?.policy.bindings ?? [];

  return (
    <SettingsPage
      title={
        <span className="flex items-center gap-2">
          <Shield className="size-5 text-accent" />
          {t("settings.iam.title")}
        </span>
      }
      description={t("settings.iam.description")}
      actions={
        canSet && (
          <Button onClick={() => setAssignOpen(true)}>
            {t("settings.iam.assign")}
          </Button>
        )
      }
    >
      {/* The skeleton binds to the policy query only — the directory queries
          load under it (labels resolve when they land), matching the old
          policy-load-driven spinner. A refetch keeps the table mounted. */}
      {policyLoading ? (
        <PageLoading />
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

      <AssignRolesSheet
        open={assignOpen}
        onClose={() => setAssignOpen(false)}
        users={users}
        groups={groups}
        grantableRoles={grantableRoles}
        groupByMember={groupByMember}
        labelMember={memberLabel}
        policyState={policyState}
        reload={reload}
        onSave={savePolicy}
      />

      <RoleMembersSheet
        open={roleSheetOpen}
        target={roleSheetTarget}
        onClose={() => setRoleSheetOpen(false)}
        users={users}
        groups={groups}
        groupByMember={groupByMember}
        labelMember={memberLabel}
        policyState={policyState}
        reload={reload}
        onSave={savePolicy}
      />
    </SettingsPage>
  );
}
