import { Search, Users } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { type Group } from "@/types/proto-es/v1/group_service_pb";
import { type User } from "@/types/proto-es/v1/user_service_pb";

function displayName(user: User): string {
  return user.title || user.email || user.name || "";
}

function groupDisplayName(group: Group): string {
  return group.title || group.email || group.name || "";
}

interface MemberPickerProps {
  users: User[];
  groups: Group[];
  value: string;
  onSelect: (member: string) => void;
  allowAllUsers?: boolean;
}

/**
 * MemberPicker selects an IAM binding member: a user, a group, or (for
 * workspace policies) the allUsers pseudo-member. Users and groups are listed
 * in separate sections with search; group entries show their member count.
 */
export function MemberPicker({
  users,
  groups,
  value,
  onSelect,
  allowAllUsers,
}: MemberPickerProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");

  const q = query.trim().toLowerCase();
  const filteredUsers = useMemo(() => {
    if (!q) return users;
    return users.filter((u) => displayName(u).toLowerCase().includes(q));
  }, [users, q]);
  const filteredGroups = useMemo(() => {
    if (!q) return groups;
    return groups.filter(
      (g) =>
        groupDisplayName(g).toLowerCase().includes(q) ||
        (g.email ?? "").toLowerCase().includes(q)
    );
  }, [groups, q]);

  const showAllUsers =
    allowAllUsers &&
    (q === "" || t("settings.iam.member-all-users").toLowerCase().includes(q));

  return (
    <div className="flex flex-col gap-2">
      <div className="relative">
        <Search className="size-4 absolute left-2.5 top-1/2 -translate-y-1/2 text-control-placeholder" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("settings.iam.member-picker-search")}
          className="w-full h-9 pl-8 pr-3 text-sm rounded-xs border border-control-border bg-background text-control focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent"
        />
      </div>

      <div className="max-h-64 overflow-y-auto flex flex-col gap-1">
        {showAllUsers && (
          <MemberRow
            selected={value === "allUsers"}
            title={t("settings.iam.member-all-users")}
            subtitle={t("settings.iam.member-all-users-hint")}
            icon={<Users className="size-4" />}
            onClick={() => onSelect("allUsers")}
          />
        )}

        {filteredUsers.length > 0 && (
          <SectionLabel>{t("settings.iam.member-picker-users")}</SectionLabel>
        )}
        {filteredUsers.map((u) => (
          <MemberRow
            key={u.name}
            selected={value === u.name}
            title={displayName(u)}
            subtitle={u.email ?? ""}
            onClick={() => onSelect(u.name ?? "")}
          />
        ))}

        {filteredGroups.length > 0 && (
          <SectionLabel>{t("settings.iam.member-picker-groups")}</SectionLabel>
        )}
        {filteredGroups.map((g) => (
          <MemberRow
            key={g.name}
            selected={value === g.name}
            title={groupDisplayName(g)}
            subtitle={t("settings.iam.member-picker-members-count", {
              count: g.members?.length ?? 0,
            })}
            icon={<Users className="size-4" />}
            onClick={() => onSelect(g.name ?? "")}
          />
        ))}

        {!showAllUsers &&
          filteredUsers.length === 0 &&
          filteredGroups.length === 0 && (
            <p className="text-sm text-control-placeholder py-4 text-center">
              {t("settings.iam.member-picker-no-results")}
            </p>
          )}
      </div>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-2 pt-2 pb-1 text-xs font-medium text-control-light uppercase tracking-wide">
      {children}
    </div>
  );
}

function MemberRow({
  selected,
  title,
  subtitle,
  icon,
  onClick,
}: {
  selected: boolean;
  title: string;
  subtitle?: string;
  icon?: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex items-center gap-2.5 px-2 py-1.5 rounded-xs text-left cursor-pointer hover:bg-control-bg",
        selected && "bg-accent/10"
      )}
    >
      <span className="size-7 rounded-full bg-control-bg-hover flex items-center justify-center shrink-0">
        {icon ?? <span className="text-xs font-medium">{initials(title)}</span>}
      </span>
      <span className="flex flex-col min-w-0">
        <span className="text-sm font-medium text-main truncate">{title}</span>
        {subtitle && (
          <span className="text-xs text-control-light truncate">
            {subtitle}
          </span>
        )}
      </span>
    </button>
  );
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}
