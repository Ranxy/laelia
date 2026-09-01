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
import { useMemo, useRef, useState } from "react";
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
import { cn } from "@/lib/utils";
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

// ---------------------------------------------------------------------------
// RoleMembersSheet — the "manage role members" drawer of the IAM page, opened
// by clicking a binding row. Owns the member set + original snapshot (via
// hasChanges), the group-expansion set, the busy/error state, and the
// "discard unsaved changes" AlertDialog (an AlertDialog, not the delete
// ConfirmActionDialog — the action is destructive-ish but discards edits).
//
// The outer component freezes the open target (outer shell + inner form +
// stable-target-ref + key, see ResourceSheet): every open remounts the form
// with the clicked binding's members, replacing the old openRoleSheet manual
// state seeding. Closing is guarded: hasChanges routes through the confirm.
//
// The save runner stays the page's savePolicy (throws on failure); the
// etag-mismatch ConnectError (Code.Aborted) catch — its internal Alert plus
// an awaited policy reload — is preserved verbatim below.
// ---------------------------------------------------------------------------

export interface RoleMembersTarget {
  // Resolved role for display (null when listRoles has no matching role).
  role: Role | null;
  bindingRole: string;
  members: string[];
}

interface RoleMembersSheetProps {
  open: boolean;
  target: RoleMembersTarget | null;
  onClose: () => void;
  users: User[];
  groups: Group[];
  // Group resolution for the member rows' expansion / member counts.
  groupByMember: Map<string, Group>;
  // Page-side member label resolver (allUsers / users/{uid} email / group).
  labelMember: (member: string) => string;
  policyState: IamPolicyState | null;
  reload: () => Promise<void>;
  // Wired to the hook's savePolicy (its resolved response view is unused
  // here): it throws so the catch below runs.
  onSave: (policy: IamPolicy, etag: string) => Promise<unknown>;
}

export function RoleMembersSheet(props: RoleMembersSheetProps) {
  const { open, target } = props;

  // Freeze the target while open so the sheet keeps the clicked role / member
  // seed through the close animation, and the next open re-seeds from it.
  const openTargetRef = useRef<RoleMembersTarget | null>(null);
  if (open) {
    openTargetRef.current = target;
  }

  // One remount per open — inner state seeds fresh every time.
  const wasOpenRef = useRef(false);
  const openSeqRef = useRef(0);
  if (open !== wasOpenRef.current) {
    wasOpenRef.current = open;
    if (open) openSeqRef.current += 1;
  }

  return (
    <RoleMembersForm
      key={openSeqRef.current}
      open={open}
      target={openTargetRef.current}
      onClose={props.onClose}
      users={props.users}
      groups={props.groups}
      groupByMember={props.groupByMember}
      labelMember={props.labelMember}
      policyState={props.policyState}
      reload={props.reload}
      onSave={props.onSave}
    />
  );
}

function RoleMembersForm({
  open,
  target,
  onClose,
  users,
  groups,
  groupByMember,
  labelMember,
  policyState,
  reload,
  onSave,
}: RoleMembersSheetProps) {
  const { t } = useTranslation();
  const role = target?.role ?? null;
  const bindingRole = target?.bindingRole ?? "";

  const [members, setMembers] = useState<Set<string>>(
    () => new Set(target?.members ?? [])
  );
  // The snapshot is fixed at mount (per-open remount re-seeds it), so only
  // the mutable set needs a setter.
  const [originalMembers] = useState<Set<string>>(
    () => new Set(target?.members ?? [])
  );
  const [confirmClose, setConfirmClose] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  // Which members are newly added (pending, not yet saved) in the role sheet.
  const newMembers = useMemo(() => {
    const added = new Set<string>();
    for (const m of members) {
      if (!originalMembers.has(m)) added.add(m);
    }
    return added;
  }, [members, originalMembers]);

  const hasChanges = useMemo(() => {
    return (
      members.size !== originalMembers.size ||
      [...members].some((m) => !originalMembers.has(m))
    );
  }, [members, originalMembers]);

  function tryCloseRoleSheet() {
    if (hasChanges) {
      setConfirmClose(true);
    } else {
      onClose();
    }
  }

  function forceCloseRoleSheet() {
    setConfirmClose(false);
    onClose();
  }

  function handleAddMember(name: string) {
    if (!name || members.has(name)) return;
    setMembers((prev) => new Set(prev).add(name));
  }

  function handleRemoveMember(name: string) {
    setMembers((prev) => {
      const next = new Set(prev);
      next.delete(name);
      return next;
    });
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

  async function handleSaveRoleSheet() {
    if (!policyState || !bindingRole) return;
    setError("");
    setSaving(true);
    try {
      const policy = buildEditedPolicyForRole(bindingRole, members);
      await onSave(policy, policyState.etag);
      toastManager.add({ type: "success", title: t("settings.iam.saved") });
      onClose();
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        setError(t("settings.iam.etag-mismatch"));
        await reload();
      } else {
        setError(describeError(err));
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Sheet
        open={open}
        onOpenChange={(next) => {
          if (!next) {
            tryCloseRoleSheet();
            return;
          }
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("settings.iam.role-sheet-title", {
                title: role?.title ?? roleIDFromName(bindingRole),
              })}
            </SheetTitle>
            <SheetDescription>
              {t("settings.iam.role-sheet-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {error && (
              <Alert variant="error" description={error} className="mb-2" />
            )}

            <div className="flex flex-col gap-5">
              {/* Role info header */}
              {role && (
                <div className="flex flex-col gap-2">
                  {role.description && (
                    <p className="text-sm text-control-light leading-relaxed">
                      {role.description}
                    </p>
                  )}
                  <div className="flex gap-2">
                    {role.predefined ? (
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
                  {members.size === 0 ? (
                    <p className="text-sm text-control-light py-4 text-center border border-dashed border-control-border rounded-xs">
                      {t("settings.iam.role-sheet-no-members")}
                    </p>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {[...members]
                        .sort((a, b) =>
                          labelMember(a).localeCompare(labelMember(b))
                        )
                        .map((member) => {
                          const user = users.find((u) => u.name === member);
                          const group = member.startsWith("groups/")
                            ? groupByMember.get(member)
                            : undefined;
                          const isNew = newMembers.has(member);
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
                                      {labelMember(member)}
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
                                        {
                                          count: group.members?.length ?? 0,
                                        }
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
                                      email: labelMember(member),
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
                  users={users.filter((u) => !members.has(u.name))}
                  groups={groups.filter((g) => !members.has(g.name))}
                  value=""
                  onSelect={handleAddMember}
                  allowAllUsers={!members.has("allUsers")}
                />
              </div>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={tryCloseRoleSheet}
              disabled={saving}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} onClick={handleSaveRoleSheet}>
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Confirm discard unsaved changes — a discard variant, deliberately an
          AlertDialog (not the delete ConfirmActionDialog). */}
      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
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
    </>
  );
}
