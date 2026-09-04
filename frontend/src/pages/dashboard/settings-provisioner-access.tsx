import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { Loader2, Shield, User as UserIcon, X } from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MemberPicker } from "@/components/member-picker";
import { Alert } from "@/components/ui/alert";
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
import { groupServiceClient, iamServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { useAppStore } from "@/stores";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// MACHINE_CREATOR_ROLE is the provisioner-scope IAM role bound on a
// provisioner's IAM policy to grant creating machines on that provisioner. Only
// this role's bindings are surfaced on the provisioner detail's access card.
const MACHINE_CREATOR_ROLE = "roles/provisionerMachineCreator";

interface ProvisionerAccessCardProps {
  name: string;
  title: string;
  canManage: boolean;
}

// ProvisionerAccessCard manages the per-provisioner IAM policy that controls
// "who can create machines on this provisioner". It surfaces the principals
// bound to roles/provisionerMachineCreator and lets the provisioner's creator
// or a workspace admin edit them via a manage sheet. Self-contained: it loads
// the policy, members and groups, and writes back etag-guarded.
export const ProvisionerAccessCard = memo(function ProvisionerAccessCard({
  name,
  title,
  canManage,
}: ProvisionerAccessCardProps) {
  const { t } = useTranslation();
  const [policyState, setPolicyState] = useState<{
    policy: IamPolicy;
    etag: string;
  } | null>(null);
  const [loadError, setLoadError] = useState("");
  const [open, setOpen] = useState(false);
  const [members, setMembers] = useState<Set<string>>(new Set());
  const initializedRef = useRef(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);

  const loadPolicy = useCallback(async () => {
    try {
      const res = await iamServiceClient.getProvisionerIamPolicy({
        name,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      setLoadError("");
    } catch (err) {
      setLoadError(describeError(err));
    }
  }, [name]);

  useEffect(() => {
    if (!canManage) return;
    void fetchUsers({ pageSize: 1000 });
    void loadPolicy();
    void groupServiceClient
      .listGroups({ pageSize: 1000 })
      .then((res) => setGroups(res.groups ?? []));
  }, [canManage, fetchUsers, loadPolicy]);

  // members are the principals bound to the provisionerMachineCreator role.
  const creatorMembers = useMemo(() => {
    const binding = policyState?.policy.bindings.find(
      (b) => b.role === MACHINE_CREATOR_ROLE
    );
    return binding?.members ?? [];
  }, [policyState]);

  // Populate the manage sheet's member selection as soon as the policy loads.
  useEffect(() => {
    if (!open) {
      initializedRef.current = false;
      return;
    }
    if (!policyState || initializedRef.current) return;
    initializedRef.current = true;
    setMembers(new Set(creatorMembers));
  }, [open, policyState, creatorMembers]);

  const memberLabel = useMemberLabel(users, groups);

  // handleSave replaces only the provisionerMachineCreator binding (members set
  // to the sheet's selection), leaving any other bindings untouched, and writes
  // it back etag-guarded.
  async function handleSave() {
    if (!policyState) return;
    setSaving(true);
    setSaveError("");
    try {
      const bindings: Binding[] = [];
      let found = false;
      for (const b of policyState.policy.bindings) {
        if (b.role === MACHINE_CREATOR_ROLE) {
          found = true;
          if (members.size > 0) {
            bindings.push(
              create(BindingSchema, {
                role: MACHINE_CREATOR_ROLE,
                members: [...members],
              })
            );
          }
        } else {
          bindings.push(
            create(BindingSchema, { role: b.role, members: [...b.members] })
          );
        }
      }
      if (!found && members.size > 0) {
        bindings.push(
          create(BindingSchema, {
            role: MACHINE_CREATOR_ROLE,
            members: [...members],
          })
        );
      }
      const policy = create(IamPolicySchema, { bindings });
      const res = await iamServiceClient.setProvisionerIamPolicy({
        name,
        policy,
        etag: policyState.etag,
      });
      setPolicyState({
        policy: res.policy ?? create(IamPolicySchema, {}),
        etag: res.etag,
      });
      setOpen(false);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Aborted) {
        setSaveError(t("settings.provisioner-detail.access-etag-mismatch"));
        await loadPolicy();
      } else {
        setSaveError(describeError(err));
      }
    } finally {
      setSaving(false);
    }
  }

  if (!canManage) {
    return null;
  }

  return (
    <>
      <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-main">
              {t("settings.provisioner-detail.access-title")}
            </h3>
            <p className="mt-0.5 text-xs text-control-light">
              {t("settings.provisioner-detail.access-hint")}
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setOpen(true)}
            disabled={!policyState}
          >
            {t("settings.provisioner-detail.access-manage")}
          </Button>
        </div>
        <div className="mt-4">
          {loadError && <Alert variant="error" description={loadError} />}
          {creatorMembers.length === 0 ? (
            <p className="text-xs text-control-light">
              {t("settings.provisioner-detail.access-no-members")}
            </p>
          ) : (
            <ul className="flex flex-wrap gap-1.5">
              {creatorMembers.map((m) => (
                <li key={m}>
                  <Badge variant="secondary">{memberLabel(m)}</Badge>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      <ProvisionerAccessManageSheet
        open={open}
        provisionerTitle={title}
        accessError={saveError}
        saving={saving}
        members={members}
        users={users}
        groups={groups}
        onClose={() => {
          setOpen(false);
          setSaveError("");
        }}
        onAdd={(member) => {
          setMembers((prev) => new Set(prev).add(member));
        }}
        onRemove={(member) => {
          setMembers((prev) => {
            const next = new Set(prev);
            next.delete(member);
            return next;
          });
        }}
        onSave={handleSave}
      />
    </>
  );
});

// useMemberLabel resolves an IAM member reference to its display label, matching
// the semantics used by the machine access card (users → title/email, groups →
// title/email, allUsers → localized label).
function useMemberLabel(users: User[], groups: Group[]) {
  const { t } = useTranslation();
  return useCallback(
    (member: string): string => {
      if (member === "allUsers")
        return t("settings.provisioner-detail.access-member-all-users");
      if (member.startsWith("users/")) {
        const u = users.find((u) => u.name === member);
        return u ? u.title || u.email || member : member;
      }
      if (member.startsWith("groups/")) {
        const g = groups.find(
          (grp) =>
            grp.name === member ||
            (grp.email ? `groups/${grp.email}` === member : false)
        );
        return g
          ? g.title || g.email || member
          : member.slice("groups/".length);
      }
      return member;
    },
    [t, users, groups]
  );
}

interface ProvisionerAccessManageSheetProps {
  open: boolean;
  provisionerTitle: string;
  accessError: string;
  saving: boolean;
  members: Set<string>;
  users: User[];
  groups: Group[];
  onClose: () => void;
  onAdd: (member: string) => void;
  onRemove: (member: string) => void;
  onSave: () => void;
}

const ProvisionerAccessManageSheet = memo(
  function ProvisionerAccessManageSheet({
    open,
    provisionerTitle,
    accessError,
    saving,
    members,
    users,
    groups,
    onClose,
    onAdd,
    onRemove,
    onSave,
  }: ProvisionerAccessManageSheetProps) {
    const { t } = useTranslation();
    const memberLabel = useMemberLabel(users, groups);

    return (
      <Sheet
        open={open}
        onOpenChange={(next) => {
          if (!next) onClose();
        }}
      >
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("settings.provisioner-detail.access-manage-title")}
            </SheetTitle>
            <SheetDescription>
              {t("settings.provisioner-detail.access-manage-description", {
                title: provisionerTitle,
              })}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>
            {accessError && (
              <Alert
                variant="error"
                description={accessError}
                className="mb-2"
              />
            )}
            <div className="flex flex-col gap-5">
              {/* Current members */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("settings.provisioner-detail.access-current-members")}
                </label>
                <div className="max-h-64 overflow-y-auto pr-1">
                  {members.size === 0 ? (
                    <p className="border border-dashed border-control-border rounded-xs py-4 text-center text-sm text-control-light">
                      {t("settings.provisioner-detail.access-no-members")}
                    </p>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {[...members]
                        .sort((a, b) =>
                          (memberLabel(a) ?? a).localeCompare(
                            memberLabel(b) ?? b
                          )
                        )
                        .map((member) => {
                          const user = users.find((u) => u.name === member);
                          const group = member.startsWith("groups/")
                            ? groups.find(
                                (g) =>
                                  g.name === member ||
                                  (g.email
                                    ? `groups/${g.email}` === member
                                    : false)
                              )
                            : undefined;
                          return (
                            <div
                              key={member}
                              className="flex items-center gap-3 rounded-xs border border-control-border bg-background p-3"
                            >
                              <div className="flex size-9 items-center justify-center rounded-full bg-accent/10 text-accent shrink-0">
                                {group ? (
                                  <Shield className="size-4.5" />
                                ) : (
                                  <UserIcon className="size-4.5" />
                                )}
                              </div>
                              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                                <span className="truncate text-sm font-medium text-main">
                                  {memberLabel(member)}
                                </span>
                                {user?.title && (
                                  <span className="truncate text-xs text-control-light">
                                    {user.title}
                                  </span>
                                )}
                                {group && (
                                  <span className="truncate text-xs text-control-light">
                                    {t(
                                      "settings.provisioner-detail.access-member-group-count",
                                      { count: group.members?.length ?? 0 }
                                    )}
                                  </span>
                                )}
                              </div>
                              <Button
                                variant="ghost"
                                size="xs"
                                onClick={() => onRemove(member)}
                                aria-label={t(
                                  "settings.provisioner-detail.access-remove-member",
                                  { email: memberLabel(member) }
                                )}
                                className="shrink-0 text-control-light hover:text-error"
                              >
                                <X className="size-4" />
                              </Button>
                            </div>
                          );
                        })}
                    </div>
                  )}
                </div>
              </div>

              {/* Add member */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("settings.provisioner-detail.access-add-member")}
                </label>
                <p className="text-xs text-control-placeholder">
                  {t("settings.provisioner-detail.access-add-member-hint")}
                </p>
                <MemberPicker
                  users={users.filter((u) => !members.has(u.name ?? ""))}
                  groups={groups.filter(
                    (g) =>
                      !members.has(g.name ?? "") &&
                      !(g.email && members.has(`groups/${g.email}`))
                  )}
                  value=""
                  onSelect={onAdd}
                />
              </div>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button variant="outline" onClick={onClose} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} onClick={onSave}>
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              {t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    );
  }
);
