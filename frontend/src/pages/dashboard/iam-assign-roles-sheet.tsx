import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { Loader2, Shield, User as UserIcon } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
import { Alert } from "@/components/ui/alert";
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
import { describeError } from "@/lib/connect-errors";
import { roleIDFromName } from "@/lib/resource";
import { toastManager } from "@/lib/toast";
import type { IamPolicyState } from "@/pages/dashboard/use-iam-policy";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// NON_GRANTABLE_WORKSPACE_ROLE_IDS lists roles that must not be offered on the
// workspace policy. workspaceMember is the auto-granted authenticated-principal
// baseline, so assigning it is a no-op. (The chat-membership markers and the
// removed agentEditor/reviewer roles are no longer returned by listRoles at
// all, so they do not need listing here.)
export const NON_GRANTABLE_WORKSPACE_ROLE_IDS = new Set(["workspaceMember"]);

export function isGrantableWorkspaceRole(role: Role): boolean {
  return !NON_GRANTABLE_WORKSPACE_ROLE_IDS.has(roleIDFromName(role.name));
}

// ---------------------------------------------------------------------------
// AssignRolesSheet — the "assign roles to a member" drawer of the IAM page.
//
// Owns selectedMember / selectedRoleIds / assignError / saving. The outer
// component keys the inner form on the open sequence (fresh-mount pattern,
// see ResourceSheet): every open re-seeds the member + checked-roles state,
// replacing the old openAssign() manual reset.
//
// The save runner stays the page's savePolicy (throws on failure); the
// etag-mismatch ConnectError (Code.Aborted) catch — its internal Alert plus
// an awaited policy reload — is preserved verbatim below.
// ---------------------------------------------------------------------------

interface AssignRolesSheetProps {
  open: boolean;
  onClose: () => void;
  users: User[];
  groups: Group[];
  grantableRoles: Role[];
  // Group resolution for the member count of a selected group member.
  groupByMember: Map<string, Group>;
  // Page-side member label resolver (allUsers / users/{uid} email / group).
  labelMember: (member: string) => string;
  policyState: IamPolicyState | null;
  reload: () => Promise<void>;
  // Wired to the hook's savePolicy (its resolved response view is unused
  // here): it throws so the catch below runs.
  onSave: (policy: IamPolicy, etag: string) => Promise<unknown>;
}

export function AssignRolesSheet(props: AssignRolesSheetProps) {
  const { open } = props;

  // One remount per open — inner state seeds fresh every time.
  const wasOpenRef = useRef(false);
  const openSeqRef = useRef(0);
  if (open !== wasOpenRef.current) {
    wasOpenRef.current = open;
    if (open) openSeqRef.current += 1;
  }

  return <AssignRolesForm key={openSeqRef.current} {...props} />;
}

function AssignRolesForm({
  open,
  onClose,
  users,
  groups,
  grantableRoles,
  groupByMember,
  labelMember,
  policyState,
  reload,
  onSave,
}: AssignRolesSheetProps) {
  const { t } = useTranslation();
  const [selectedMember, setSelectedMember] = useState("");
  const [selectedRoleIds, setSelectedRoleIds] = useState<Set<string>>(
    new Set()
  );
  const [saving, setSaving] = useState(false);
  const [assignError, setAssignError] = useState("");

  const selectedUser = useMemo(() => {
    return users.find((u) => u.name === selectedMember) ?? null;
  }, [users, selectedMember]);

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

  async function handleSaveAssign() {
    if (!policyState || !selectedMember) return;
    setAssignError("");
    setSaving(true);
    try {
      const policy = buildEditedPolicy();
      await onSave(policy, policyState.etag);
      toastManager.add({ type: "success", title: t("settings.iam.saved") });
      onClose();
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        // Etag mismatch: another writer changed the policy. Re-fetch so the
        // sheet reflects the latest state and let the admin retry.
        setAssignError(t("settings.iam.etag-mismatch"));
        await reload();
      } else {
        setAssignError(describeError(err));
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
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
            <Alert variant="error" description={assignError} className="mb-2" />
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
                    {labelMember(selectedMember)}
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
          <Button variant="outline" onClick={onClose} disabled={saving}>
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
  );
}
