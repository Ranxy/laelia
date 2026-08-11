import { Plus, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/chat/avatar";
import { MemberPicker } from "@/components/chat/member-picker";
import { LoadingState } from "@/components/chat/states";
import { MemberPicker as IamMemberPicker } from "@/components/member-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { groupServiceClient, userServiceClient } from "@/connect";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { ChannelMember } from "@/types/proto-es/v1/command_pb";
import { State } from "@/types/proto-es/v1/common_pb";
import type { Group } from "@/types/proto-es/v1/group_service_pb";
import type { User as UserMessage } from "@/types/proto-es/v1/user_service_pb";

// Add-member mode: 1 = user, 2 = agent, 3 = group snapshot.
type AddMemberType = 1 | 2 | 3;

// Stable empty fallback so a per-key selector returning undefined for an
// unloaded conversation doesn't mint a new array each run (which would defeat
// zustand's Object.is equality and re-render on every store change).
const EMPTY_MEMBERS: ChannelMember[] = [];

function memberTypeLabel(
  t: (key: string) => string,
  memberType: number
): string {
  return memberType === 2
    ? t("channel.member-type-agent")
    : t("channel.member-type-user");
}

// roleLabel maps the IAM-derived chat role ints (1=Owner, 2=Member, 3=Admin)
// to the UI badges shared by the chat members sheet and the channel detail page.
function roleLabel(t: (key: string) => string, role: number): string {
  switch (role) {
    case 1:
      return t("channel.role-owner");
    case 3:
      return t("channel.role-admin");
    default:
      return t("channel.role-member");
  }
}

// roleBadgeVariant keeps the visual hierarchy stable across surfaces: Owner is
// the only highlighted role; Admin and Member are quieter secondary badges.
function roleBadgeVariant(role: number): "success" | "secondary" | "default" {
  switch (role) {
    case 1:
      return "success";
    case 3:
      return "default";
    default:
      return "secondary";
  }
}

export interface ChannelMembersPanelProps {
  conversationId: string;
  // canManage is true for the channel owner (or the sole writable member in a
  // DM) and gates the remove buttons and the add-member section.
  canManage: boolean;
  // membershipFixed is true for DMs, whose roster is fixed at creation (1:1
  // user+agent or agent+agent); it hides add/remove entirely.
  membershipFixed: boolean;
}

// ChannelMembersPanel renders a conversation's member roster with role badges
// and (for channel owners) the add/remove controls. It is shared by the chat
// page's members Sheet and the channel detail page; the surrounding shell
// (Sheet vs inline section) is the caller's choice. It owns the add-member
// flow (user/agent picker and group snapshot) so both surfaces stay in sync.
export function ChannelMembersPanel({
  conversationId,
  canManage,
  membershipFixed,
}: ChannelMembersPanelProps) {
  const { t } = useTranslation();
  const conversationName = `conversations/${conversationId}`;
  const members =
    useAppStore((s) => s.channelMembersByConv[conversationName]) ??
    EMPTY_MEMBERS;
  const membersLoading = useAppStore(
    (s) => s.channelMembersLoading[conversationName] ?? false
  );
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const addChannelMember = useAppStore((s) => s.addChannelMember);
  const addChannelGroup = useAppStore((s) => s.addChannelGroup);
  const removeChannelMember = useAppStore((s) => s.removeChannelMember);

  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [addMemberType, setAddMemberType] = useState<AddMemberType>(2); // default AGENT
  const [addMemberIds, setAddMemberIds] = useState<string[]>([]);
  const [selectedGroupName, setSelectedGroupName] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const [groupUsers, setGroupUsers] = useState<Map<string, UserMessage>>(
    new Map()
  );
  const [addingMember, setAddingMember] = useState(false);

  // Fetch the roster whenever the panel mounts (sheet open / detail page
  // visited) so both surfaces always render the current membership.
  useEffect(() => {
    void listChannelMembers(conversationId);
  }, [conversationId, listChannelMembers]);

  // memberIds already in the channel for the currently-selected add-member
  // type, used to disable + badge them in the picker so they can't be re-added.
  const existingMemberIds = useMemo(
    () =>
      new Set(
        members
          .filter((m) => m.memberType === addMemberType)
          .map((m) => m.memberId)
      ),
    [members, addMemberType]
  );

  // toggleAddMemberId adds/removes a memberId from the pending batch selection.
  // The picker stays open between toggles so several members can be chosen
  // before the single batch add is submitted.
  const toggleAddMemberId = useCallback((memberId: string) => {
    setAddMemberIds((prev) =>
      prev.includes(memberId)
        ? prev.filter((id) => id !== memberId)
        : [...prev, memberId]
    );
  }, []);

  // Group snapshot mode loads the group list once the picker is open.
  useEffect(() => {
    if (addMemberType !== 3 || !addMemberOpen) return;
    let cancelled = false;
    groupServiceClient
      .listGroups({ pageSize: 1000 })
      .then((res) => {
        if (!cancelled) setGroups(res.groups ?? []);
      })
      .catch(() => {
        if (!cancelled) setGroups([]);
      });
    return () => {
      cancelled = true;
    };
  }, [addMemberType, addMemberOpen]);

  // When a group is selected for a snapshot add, resolve its members' user
  // records so the preview can show names and skip-deleted status.
  const selectedGroup =
    groups.find((g) => g.name === selectedGroupName) ?? null;
  useEffect(() => {
    if (addMemberType !== 3 || !selectedGroup) {
      setGroupUsers(new Map());
      return;
    }
    const names = (selectedGroup.members ?? [])
      .map((m) => m.member)
      .filter(Boolean);
    if (names.length === 0) {
      setGroupUsers(new Map());
      return;
    }
    let cancelled = false;
    userServiceClient
      .batchGetUsers({ names })
      .then((res) => {
        if (cancelled) return;
        const byName = new Map<string, UserMessage>();
        for (const u of res.users ?? []) byName.set(u.name ?? "", u);
        setGroupUsers(byName);
      })
      .catch(() => {
        if (!cancelled) setGroupUsers(new Map());
      });
    return () => {
      cancelled = true;
    };
  }, [addMemberType, selectedGroup]);

  // user ids already in the channel, for the group preview status badges.
  const existingChannelUserIds = useMemo(
    () =>
      new Set(members.filter((m) => m.memberType === 1).map((m) => m.memberId)),
    [members]
  );

  const groupPreview = useMemo(() => {
    if (!selectedGroup) return null;
    const rows = (selectedGroup.members ?? []).map((gm) => {
      const uid = gm.member?.split("/").pop() ?? "";
      const user = groupUsers.get(gm.member ?? "");
      const inChannel = uid ? existingChannelUserIds.has(uid) : false;
      const skipped = user?.state === State.DELETED;
      return { member: gm.member ?? "", uid, user, inChannel, skipped };
    });
    return {
      rows,
      total: rows.length,
      inChannel: rows.filter((r) => r.inChannel).length,
      toAdd: rows.filter((r) => !r.inChannel && !r.skipped).length,
    };
  }, [selectedGroup, groupUsers, existingChannelUserIds]);

  const handleAddMember = useCallback(async () => {
    if (addingMember) return;
    if (addMemberType === 3) {
      if (!selectedGroupName) return;
      setAddingMember(true);
      try {
        await addChannelGroup(conversationId, selectedGroupName);
        setSelectedGroupName("");
        setAddMemberIds([]);
        setAddMemberOpen(false);
        listChannelMembers(conversationId);
      } catch {
        // add failed — keep the selection so the user can retry
      } finally {
        setAddingMember(false);
      }
      return;
    }
    if (addMemberIds.length === 0) return;
    setAddingMember(true);
    try {
      await addChannelMember(conversationId, addMemberType, addMemberIds);
      setAddMemberIds([]);
      setAddMemberOpen(false);
      listChannelMembers(conversationId);
    } catch {
      // add failed — keep the selection so the user can retry
    } finally {
      setAddingMember(false);
    }
  }, [
    addMemberIds,
    addMemberType,
    selectedGroupName,
    addingMember,
    conversationId,
    addChannelMember,
    addChannelGroup,
    listChannelMembers,
  ]);

  const handleRemoveMember = useCallback(
    async (memberType: number, memberId: string) => {
      try {
        await removeChannelMember(conversationId, memberType, memberId);
        listChannelMembers(conversationId);
      } catch {
        // remove failed
      }
    },
    [conversationId, removeChannelMember, listChannelMembers]
  );

  return (
    <div className="flex flex-col gap-0">
      {membersLoading && <LoadingState />}
      {!membersLoading && (
        <div className="flex flex-col gap-2">
          {members.map((m) => (
            <ChannelMemberRow
              key={`${m.memberType}-${m.memberId}`}
              member={m}
              removable={!membershipFixed && canManage && m.memberRole !== 1}
              onRemove={() => handleRemoveMember(m.memberType, m.memberId)}
            />
          ))}
        </div>
      )}

      {/* Add member section — channels only (both DM shapes are fixed
          1:1 rosters: user+agent and agent+agent). */}
      {!membershipFixed && canManage && (
        <div className="mt-4 border-t border-control-border pt-5">
          {addMemberOpen ? (
            <div className="flex flex-col gap-4">
              <div className="flex flex-col gap-1.5">
                <span className="text-xs font-semibold uppercase tracking-wide text-control">
                  {t("channel.member-type-label")}
                </span>
                <div className="flex gap-2">
                  <Button
                    variant={addMemberType === 1 ? "default" : "outline"}
                    size="sm"
                    onClick={() => {
                      setAddMemberType(1);
                      setAddMemberIds([]);
                      setSelectedGroupName("");
                    }}
                    className="flex-1"
                  >
                    {t("channel.member-type-user")}
                  </Button>
                  <Button
                    variant={addMemberType === 2 ? "default" : "outline"}
                    size="sm"
                    onClick={() => {
                      setAddMemberType(2);
                      setAddMemberIds([]);
                      setSelectedGroupName("");
                    }}
                    className="flex-1"
                  >
                    {t("channel.member-type-agent")}
                  </Button>
                  <Button
                    variant={addMemberType === 3 ? "default" : "outline"}
                    size="sm"
                    onClick={() => {
                      setAddMemberType(3);
                      setAddMemberIds([]);
                      setSelectedGroupName("");
                    }}
                    className="flex-1"
                  >
                    {t("channel.member-type-group")}
                  </Button>
                </div>
              </div>
              {addMemberType === 3 ? (
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold uppercase tracking-wide text-control">
                    {t("channel.group-label")}
                  </span>
                  <p className="text-xs text-control-placeholder">
                    {t("channel.add-group-hint")}
                  </p>
                  <IamMemberPicker
                    users={[]}
                    groups={groups}
                    value={selectedGroupName}
                    onSelect={setSelectedGroupName}
                  />
                  {selectedGroup && groupPreview && (
                    <div className="flex flex-col gap-2 rounded-xs border border-control-border p-3">
                      <div className="flex items-center justify-between gap-2">
                        <span className="min-w-0 truncate text-sm font-medium text-main">
                          {selectedGroup.title || selectedGroup.name}
                        </span>
                        <span className="shrink-0 text-xs text-control-light">
                          {t("channel.group-members-count", {
                            count: groupPreview.total,
                          })}
                        </span>
                      </div>
                      <div className="flex max-h-48 flex-col gap-1.5 overflow-y-auto pr-1">
                        {groupPreview.rows.map((r) => {
                          const label = r.user
                            ? r.user.title || r.user.email || r.member
                            : r.member;
                          return (
                            <div
                              key={r.member}
                              className="flex items-center gap-2"
                            >
                              <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent/10 text-xs font-medium text-accent">
                                {label.charAt(0).toUpperCase()}
                              </span>
                              <span className="min-w-0 flex-1 truncate text-sm text-main">
                                {label}
                              </span>
                              {r.inChannel ? (
                                <Badge
                                  variant="secondary"
                                  className="text-xs shrink-0"
                                >
                                  {t("channel.group-status-in-channel")}
                                </Badge>
                              ) : r.skipped ? (
                                <Badge
                                  variant="warning"
                                  className="text-xs shrink-0"
                                >
                                  {t("channel.group-status-skipped")}
                                </Badge>
                              ) : (
                                <Badge
                                  variant="default"
                                  className="text-xs shrink-0"
                                >
                                  {t("channel.group-status-add")}
                                </Badge>
                              )}
                            </div>
                          );
                        })}
                      </div>
                      {groupPreview.toAdd === 0 ? (
                        <p className="text-xs text-control-placeholder">
                          {t("channel.add-group-all-in-channel")}
                        </p>
                      ) : (
                        <p className="text-xs text-control-light">
                          {t("channel.add-group-summary", {
                            count: groupPreview.toAdd,
                            total: groupPreview.total,
                          })}
                        </p>
                      )}
                    </div>
                  )}
                </div>
              ) : (
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold uppercase tracking-wide text-control">
                    {t("channel.member-id-label")}
                  </span>
                  <div className="flex gap-2">
                    <MemberPicker
                      key={addMemberType}
                      memberType={addMemberType}
                      existingMemberIds={existingMemberIds}
                      value={addMemberIds}
                      onToggle={toggleAddMemberId}
                      placeholder={t("channel.member-id-placeholder")}
                    />
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setAddMemberOpen(false);
                        setAddMemberIds([]);
                      }}
                      className="size-7 p-0"
                    >
                      <X className="size-3.5" />
                    </Button>
                  </div>
                </div>
              )}
              <Button
                onClick={handleAddMember}
                disabled={
                  (addMemberType === 3
                    ? !selectedGroup || (groupPreview?.toAdd ?? 0) === 0
                    : addMemberIds.length === 0) || addingMember
                }
                className="w-full"
              >
                {addingMember
                  ? t("common.creating")
                  : addMemberType === 3
                    ? selectedGroup
                      ? t("channel.add-group-count", {
                          count: groupPreview?.toAdd ?? 0,
                        })
                      : t("channel.add-group")
                    : addMemberIds.length > 0
                      ? t("channel.add-member-batch", {
                          count: addMemberIds.length,
                        })
                      : t("channel.add-member")}
              </Button>
            </div>
          ) : (
            <Button
              variant="ghost"
              onClick={() => setAddMemberOpen(true)}
              className="flex w-full items-center gap-1.5 rounded-md px-2.5 py-2 text-sm"
            >
              <Plus className="size-4" />
              {t("channel.add-member")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

// ChannelMemberRow is one roster row: real avatar (uploaded image or pixel
// identicon, same as the members directory), name, and a fixed-width type
// label so the role badge and join time line up across user/agent rows.
function ChannelMemberRow({
  member,
  removable,
  onRemove,
}: {
  member: ChannelMember;
  removable: boolean;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  const isAgent = member.memberType === 2;
  const avatarName = isAgent
    ? avatarNameForAgentId(member.memberId)
    : avatarNameForUserId(member.memberId);
  const avatarSrc = useAvatar(avatarName);

  return (
    <div className="flex items-center gap-3 rounded-xs border border-control-border bg-background p-3 transition-colors hover:bg-control-bg/60">
      <Avatar seed={member.memberId || member.displayName} src={avatarSrc} />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-main truncate">
          {member.displayName || member.memberId}
        </p>
        <div className="flex items-center gap-1.5 mt-0.5">
          <span className="w-10 shrink-0 text-xs text-control-light">
            {memberTypeLabel(t, member.memberType)}
          </span>
          <Badge
            variant={roleBadgeVariant(member.memberRole)}
            className="w-16 shrink-0 justify-center text-xs"
          >
            {roleLabel(t, member.memberRole)}
          </Badge>
          {member.joinedAt && (
            <span className="text-xs text-control-placeholder">
              {t("channel.joined-at", {
                date: new Date(
                  Number(member.joinedAt.seconds) * 1000
                ).toLocaleDateString(),
              })}
            </span>
          )}
        </div>
      </div>
      {/* DMs have fixed membership (user + agent); only channel owners can
          remove members. */}
      {removable && (
        <Button
          variant="ghost"
          size="sm"
          onClick={onRemove}
          aria-label={t("common.delete")}
          className={cn(
            "size-7 p-0 text-control-placeholder hover:text-error hover:bg-error/10"
          )}
        >
          <Trash2 className="size-3.5" />
        </Button>
      )}
    </div>
  );
}
