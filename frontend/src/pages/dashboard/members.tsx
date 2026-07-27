import { ChevronDown, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useMatch, useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { MemberSummary } from "@/stores/types";

// MembersPage is the two-column workspace directory. The left rail lists
// agents and humans as separate collapsible sections (each with a count and a
// "+" action); the right pane renders the selected member's detail via a
// nested route — an agent opens the AgentDetailLayout (profile/commands/
// reminders/chat tabs), a human opens the HumanDetailPage. The responsive
// rail+pane pattern mirrors machines.tsx: on small screens the rail is hidden
// once a member is selected and the detail goes full-width.
export function MembersPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const members = useAppStore((s) => s.members);
  const loading = useAppStore((s) => s.membersLoading);
  const error = useAppStore((s) => s.membersError);
  const fetchMembers = useAppStore((s) => s.fetchMembers);
  const machines = useAppStore((s) => s.machines);
  const fetchMachines = useAppStore((s) => s.fetchMachines);

  // useMatch reads the child-route params so the parent layout can highlight
  // the selected row (parent route elements don't receive child params via
  // useParams).
  const agentMatch = useMatch("/members/agents/:agentId");
  const userMatch = useMatch("/members/users/:userId");
  const selectedAgentId = agentMatch?.params.agentId;
  const selectedUserId = userMatch?.params.userId;
  const hasSelection = !!(selectedAgentId || selectedUserId);

  useEffect(() => {
    void fetchMembers();
  }, [fetchMembers]);

  // Load the machine roster so agent rows can show the owning machine's title
  // (member.subtitle is the machine resource name machines/{id}).
  useEffect(() => {
    void fetchMachines({ pageSize: 100 });
  }, [fetchMachines]);

  const machineTitleByName = useMemo(() => {
    const m = new Map<string, string>();
    for (const machine of machines) m.set(machine.name, machine.title);
    return m;
  }, [machines]);

  const agents = members.filter((m) => m.kind === "agent");
  const humans = members.filter((m) => m.kind === "user");

  const [agentsOpen, setAgentsOpen] = useState(true);
  const [humansOpen, setHumansOpen] = useState(true);

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Left rail: members directory. */}
      <aside
        className={cn(
          "shrink-0 flex-col border-r border-control-border overflow-hidden",
          hasSelection ? "hidden lg:flex lg:w-60" : "flex w-full lg:w-60"
        )}
      >
        <div className="flex items-center justify-between gap-2 border-b border-control-border px-3 py-3 shrink-0">
          <h1 className="text-sm font-semibold text-main truncate">
            {t("members.title")}
          </h1>
        </div>

        <div className="flex-1 overflow-y-auto py-1">
          {loading ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("common.loading")}
            </p>
          ) : error ? (
            <div className="flex flex-col gap-3 px-3 py-2">
              <Alert variant="error" description={t("members.load-failed")} />
              <Button
                variant="outline"
                size="sm"
                onClick={() => void fetchMembers()}
              >
                {t("common.retry")}
              </Button>
            </div>
          ) : members.length === 0 ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("members.no-data")}
            </p>
          ) : (
            <div className="flex flex-col">
              <SectionHeader
                label={t("members.section-agents")}
                count={agents.length}
                open={agentsOpen}
                onToggle={() => setAgentsOpen((v) => !v)}
                onAdd={() => navigate("/machines")}
                addLabel={t("members.add-agent")}
              />
              {agentsOpen &&
                agents.map((member) => (
                  <MemberRow
                    key={member.name}
                    member={member}
                    machineLabel={
                      member.subtitle
                        ? (machineTitleByName.get(member.subtitle) ??
                          member.subtitle.replace(/^machines\//, ""))
                        : ""
                    }
                    selected={
                      selectedAgentId === member.name.replace(/^agents\//, "")
                    }
                  />
                ))}

              <SectionHeader
                label={t("members.section-humans")}
                count={humans.length}
                open={humansOpen}
                onToggle={() => setHumansOpen((v) => !v)}
                onAdd={() => navigate("/settings/users")}
                addLabel={t("members.add-human")}
              />
              {humansOpen &&
                humans.map((member) => (
                  <MemberRow
                    key={member.name}
                    member={member}
                    selected={
                      selectedUserId === member.name.replace(/^users\//, "")
                    }
                  />
                ))}
            </div>
          )}
        </div>
      </aside>

      {/* Right pane: member detail (or empty state). */}
      <div
        className={cn(
          "min-w-0 flex-1 overflow-hidden",
          !hasSelection && "hidden lg:block"
        )}
      >
        <Outlet />
      </div>
    </div>
  );
}

// SectionHeader is the collapsible "Agents" / "Humans" header: chevron +
// uppercase label + count, with a trailing "+" affordance. Mirrors the
// reference HTML's section-toggle structure, rendered in the app's design
// system.
function SectionHeader({
  label,
  count,
  open,
  onToggle,
  onAdd,
  addLabel,
}: {
  label: string;
  count: number;
  open: boolean;
  onToggle: () => void;
  onAdd: () => void;
  addLabel: string;
}) {
  return (
    <div className="mb-1 mt-3 flex h-6 items-center justify-between gap-1 px-2">
      <button
        type="button"
        onClick={onToggle}
        className="flex h-6 min-w-0 flex-1 items-center gap-1 text-xs font-bold uppercase text-control tracking-widest hover:text-main transition-colors"
      >
        <ChevronDown
          className={cn(
            "size-3 shrink-0 transition-transform",
            !open && "-rotate-90"
          )}
        />
        <span className="truncate">{label}</span>
        <span className="font-mono normal-case tracking-normal text-control-light">
          {count}
        </span>
      </button>
      <Button
        variant="ghost"
        size="sm"
        onClick={onAdd}
        aria-label={addLabel}
        title={addLabel}
        className="size-6 shrink-0 p-0"
      >
        <Plus className="size-3.5" />
      </Button>
    </div>
  );
}

// MemberRow is a single directory row. Agents navigate to the embedded agent
// detail; humans navigate to the human profile. Selection is shown with the
// same left-border + bg highlight as machines.tsx.
function MemberRow({
  member,
  selected,
  machineLabel,
}: {
  member: MemberSummary;
  selected: boolean;
  machineLabel?: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isAgent = member.kind === "agent";
  const resourceId = member.name.replace(/^(agents|users)\//, "");
  const avatarName = isAgent
    ? avatarNameForAgentId(resourceId)
    : avatarNameForUserId(resourceId);
  const avatarSrc = useAvatar(avatarName);

  function open() {
    navigate(
      isAgent ? `/members/agents/${resourceId}` : `/members/users/${resourceId}`
    );
  }

  return (
    <button
      type="button"
      onClick={open}
      aria-label={
        isAgent
          ? t("members.row-open-agent", { title: member.title })
          : t("members.row-open-human", { title: member.title })
      }
      className={cn(
        "flex w-full items-center gap-2 px-3 py-2 text-left transition-colors border-l-2",
        selected
          ? "border-accent bg-control-bg"
          : "border-transparent hover:bg-control-bg/60"
      )}
    >
      <Avatar seed={resourceId || member.title} src={avatarSrc} />
      <div className="min-w-0 flex-1 flex flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-main">
          {member.title}
        </span>
        {isAgent && machineLabel ? (
          <span className="truncate text-xs text-control-light">
            {t("members.agent-subtitle", { machine: machineLabel })}
          </span>
        ) : null}
      </div>
      {isAgent ? (
        <ConnectionBadge state={member.connectionState} />
      ) : (
        <span className="text-xs text-control-light">
          {t("members.kind-user")}
        </span>
      )}
    </button>
  );
}
