import { Check, ChevronsUpDown, Loader2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { SearchInput } from "@/components/ui/search-input";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// member_type values mirror ChannelMember.member_type: 1 = user, 2 = agent.
export type MemberPickerType = 1 | 2;

type Option = {
  memberId: string;
  label: string;
  sublabel?: string;
};

export interface MemberPickerProps {
  memberType: MemberPickerType;
  /** memberIds already in the channel for this memberType — disabled + badged. */
  existingMemberIds: Set<string>;
  /** Currently selected memberId ("" = nothing selected). */
  value: string;
  onPick: (memberId: string) => void;
  disabled?: boolean;
  placeholder: string;
}

// memberId for a user is the numeric id in users/{id}; for an agent it's the
// resource id in agents/{resourceId}.
function memberIdOf(name: string): string {
  return name.split("/").pop() || name;
}

function userOption(u: User): Option {
  return {
    memberId: memberIdOf(u.name),
    label: u.title || u.email || memberIdOf(u.name),
    // Prefer the self-description so an admin picking members sees each
    // person's role; fall back to email when none is set so the entry still
    // disambiguates users with the same title.
    sublabel: u.description || u.email || undefined,
  };
}

function agentOption(a: Agent): Option {
  const id = memberIdOf(a.name);
  return { memberId: id, label: a.title || id, sublabel: undefined };
}

// The backend turns `name.matches(q)` / `email.matches(q)` into
// `LOWER(principal.<col>) LIKE %q%`. Strip CEL string delimiters and LIKE
// wildcards so the user's typed query can't break the parse or over-match.
function escapeFilterQuery(raw: string): string {
  return raw.replace(/[\\"]|%|_/g, "").trim();
}

function buildUserFilter(query: string): string {
  const q = escapeFilterQuery(query);
  if (q === "") return "";
  return `name.matches("${q}") || email.matches("${q}")`;
}

export function MemberPicker({
  memberType,
  existingMemberIds,
  value,
  onPick,
  disabled,
  placeholder,
}: MemberPickerProps) {
  const { t } = useTranslation();
  const users = useAppStore((s) => s.users);
  const usersLoading = useAppStore((s) => s.usersLoading);
  const fetchUsers = useAppStore((s) => s.fetchUsers);
  const agents = useAppStore((s) => s.agents);

  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const isUser = memberType === 1;

  // Debounced backend search for users; agents are filtered client-side.
  useEffect(() => {
    if (!open || !isUser) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      fetchUsers({ pageSize: 50, filter: buildUserFilter(query) });
    }, 250);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [open, isUser, query, fetchUsers]);

  // Reset the search box whenever the picker closes or switches type.
  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const options = useMemo<Option[]>(() => {
    if (isUser) return users.map(userOption);
    const q = query.trim().toLowerCase();
    return agents
      .map(agentOption)
      .filter(
        (o) =>
          q === "" ||
          o.label.toLowerCase().includes(q) ||
          o.memberId.toLowerCase().includes(q)
      );
  }, [isUser, users, agents, query]);

  const selectedLabel = useMemo(() => {
    if (!value) return "";
    const found = options.find((o) => o.memberId === value);
    return found?.label ?? value;
  }, [options, value]);

  function handlePick(o: Option) {
    if (existingMemberIds.has(o.memberId)) return;
    onPick(o.memberId);
    setOpen(false);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            disabled={disabled}
            className="flex-1 h-auto justify-between rounded-md border border-control-border bg-background px-2.5 py-1.5 text-xs text-main font-normal"
          >
            <span
              className={cn("truncate", !value && "text-control-placeholder")}
            >
              {value ? selectedLabel : placeholder}
            </span>
            <ChevronsUpDown className="size-3.5 shrink-0 text-control-placeholder" />
          </Button>
        }
      />
      <PopoverContent
        align="start"
        className="w-(--anchor-width) min-w-56 max-h-72 flex flex-col gap-2 p-2"
      >
        <SearchInput
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={
            isUser
              ? t("channel.add-member-search")
              : t("channel.add-member-search-agent")
          }
          className="text-xs"
        />
        <div className="flex-1 overflow-y-auto">
          {(isUser ? usersLoading : false) && (
            <div className="flex items-center justify-center gap-2 py-6 text-xs text-control-light">
              <Loader2 className="size-3.5 animate-spin" />
              {t("common.loading")}
            </div>
          )}
          {!(isUser ? usersLoading : false) && options.length === 0 && (
            <p className="py-6 text-center text-xs text-control-light">
              {t("channel.no-results")}
            </p>
          )}
          {!(isUser ? usersLoading : false) &&
            options.map((o) => {
              const joined = existingMemberIds.has(o.memberId);
              const selected = o.memberId === value;
              return (
                <button
                  key={o.memberId}
                  type="button"
                  disabled={joined}
                  onClick={() => handlePick(o)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
                    joined
                      ? "cursor-not-allowed text-control-light"
                      : "hover:bg-accent/10 text-main"
                  )}
                >
                  <div className="flex size-6 shrink-0 items-center justify-center rounded-full bg-control-bg text-[10px] font-medium">
                    {o.label.charAt(0).toUpperCase()}
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="truncate">{o.label}</p>
                    {o.sublabel && (
                      <p className="truncate text-[10px] text-control-placeholder">
                        {o.sublabel}
                      </p>
                    )}
                  </div>
                  {joined && (
                    <span className="rounded bg-control-bg px-1.5 py-0 text-[10px] font-medium text-control">
                      {t("channel.member-joined")}
                    </span>
                  )}
                  {selected && !joined && (
                    <Check className="size-3.5 shrink-0 text-accent" />
                  )}
                </button>
              );
            })}
        </div>
      </PopoverContent>
    </Popover>
  );
}
