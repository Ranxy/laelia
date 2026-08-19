import { ChevronsUpDown, Loader2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { SearchInput } from "@/components/ui/search-input";
import { userServiceClient } from "@/connect";
import { buildUserFilter } from "@/lib/user-filter";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
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
  /** Currently selected memberIds ([] = nothing selected). */
  value: string[];
  /** Toggle a memberId in the parent's selection; the popover stays open so
   *  several members can be picked before the add is submitted. */
  onToggle: (memberId: string) => void;
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

function agentOption(a: AgentSummary): Option {
  const id = memberIdOf(a.name);
  return {
    memberId: id,
    label: a.title || id,
    // Show the public agent intro so pickers can tell what each agent is for.
    sublabel: a.description || undefined,
  };
}

export function MemberPicker({
  memberType,
  existingMemberIds,
  value,
  onToggle,
  disabled,
  placeholder,
}: MemberPickerProps) {
  const { t } = useTranslation();
  const agents = useAppStore((s) => s.agents);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const currentUser = useAppStore((s) => s.currentUser);

  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // User search results live in local state (not the shared users roster) so a
  // filtered picker fetch can never clobber the roster other pages depend on.
  const [userResults, setUserResults] = useState<User[]>([]);
  const [userResultsLoading, setUserResultsLoading] = useState(false);

  const isUser = memberType === 1;
  // Workspace admins hold laelia.agents.edit and may add any agent; everyone
  // else only sees public agents (allowAddToChannel) or their own creations.
  const isAdmin =
    currentUser?.permissions?.includes("laelia.agents.edit") ?? false;

  // Debounced backend search for users (local state); agents are filtered
  // client-side from the shared roster below.
  useEffect(() => {
    if (!open || !isUser) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(async () => {
      setUserResultsLoading(true);
      try {
        const res = await userServiceClient.listUsers({
          pageSize: 50,
          filter: buildUserFilter(query),
        });
        setUserResults(res.users ?? []);
      } catch {
        setUserResults([]);
      } finally {
        setUserResultsLoading(false);
      }
    }, 250);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [open, isUser, query]);

  // The agent list filters the shared roster client-side; ensure the roster is
  // loaded when the picker opens so it's never empty just because the user
  // hasn't visited the members/agents pages yet.
  useEffect(() => {
    if (!open || isUser) return;
    if (useAppStore.getState().agents.length === 0) {
      fetchAgents({ pageSize: 100 });
    }
  }, [open, isUser, fetchAgents]);

  // Reset the search box whenever the picker closes or switches type.
  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const options = useMemo<Option[]>(() => {
    if (isUser) return userResults.map(userOption);
    const q = query.trim().toLowerCase();
    return agents
      .filter(
        (a) =>
          a.allowAddToChannel ||
          isAdmin ||
          (!!a.owner && a.owner === currentUser?.name)
      )
      .map(agentOption)
      .filter(
        (o) =>
          q === "" ||
          o.label.toLowerCase().includes(q) ||
          o.memberId.toLowerCase().includes(q)
      );
  }, [isUser, userResults, agents, query, isAdmin, currentUser?.name]);

  function handleToggle(o: Option) {
    if (existingMemberIds.has(o.memberId)) return;
    onToggle(o.memberId);
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
              className={cn(
                "truncate",
                value.length === 0 && "text-control-placeholder"
              )}
            >
              {value.length > 0
                ? t("channel.selected-count", { count: value.length })
                : placeholder}
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
          {(isUser ? userResultsLoading : false) && (
            <div className="flex items-center justify-center gap-2 py-6 text-xs text-control-light">
              <Loader2 className="size-3.5 animate-spin" />
              {t("common.loading")}
            </div>
          )}
          {!(isUser ? userResultsLoading : false) && options.length === 0 && (
            <p className="py-6 text-center text-xs text-control-light">
              {t("channel.no-results")}
            </p>
          )}
          {!(isUser ? userResultsLoading : false) &&
            options.map((o) => {
              const joined = existingMemberIds.has(o.memberId);
              const selected = value.includes(o.memberId);
              return (
                <button
                  key={o.memberId}
                  type="button"
                  disabled={joined}
                  onClick={() => handleToggle(o)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
                    joined
                      ? "cursor-not-allowed text-control-light"
                      : "hover:bg-accent/10 text-main"
                  )}
                >
                  <span className="pointer-events-none flex shrink-0 items-center">
                    <Checkbox checked={selected} size="sm" tabIndex={-1} />
                  </span>
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
                </button>
              );
            })}
        </div>
      </PopoverContent>
    </Popover>
  );
}
