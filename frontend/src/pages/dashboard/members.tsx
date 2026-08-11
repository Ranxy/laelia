import { ChevronDown, Hash, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useMatch, useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SearchInput } from "@/components/ui/search-input";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { MemberSummary } from "@/stores/types";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

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
  const myChannels = useAppStore((s) => s.myChannels);
  const myChannelsLoading = useAppStore((s) => s.myChannelsLoading);
  const fetchMyChannels = useAppStore((s) => s.fetchMyChannels);

  // useMatch reads the child-route params so the parent layout can highlight
  // the selected row (parent route elements don't receive child params via
  // useParams).
  const agentMatch = useMatch("/members/agents/:agentId/*");
  const userMatch = useMatch("/members/users/:userId/*");
  const channelMatch = useMatch("/members/channels/:channelId/*");
  const selectedAgentId = agentMatch?.params.agentId;
  const selectedUserId = userMatch?.params.userId;
  const selectedChannelId = channelMatch?.params.channelId;
  const hasSelection = !!(
    selectedAgentId ||
    selectedUserId ||
    selectedChannelId
  );

  useEffect(() => {
    // Keep the already-rendered roster visible when returning to this page:
    // refresh silently when a cached roster exists, otherwise show loading.
    const hasCached = useAppStore.getState().members.length > 0;
    void fetchMembers({ silent: hasCached });
  }, [fetchMembers]);

  // Load the channel roster (all joined/created channels, closed included) so
  // the Channels section has an entry point back into closed conversations.
  useEffect(() => {
    const hasCached = useAppStore.getState().myChannels.length > 0;
    void fetchMyChannels({ silent: hasCached });
  }, [fetchMyChannels]);

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

  const [agentsOpen, setAgentsOpen] = useState(true);
  const [humansOpen, setHumansOpen] = useState(true);
  // The Channels section starts collapsed: it is a recovery entry point for
  // closed chats, not a primary browsing surface.
  const [channelsOpen, setChannelsOpen] = useState(false);
  const [query, setQuery] = useState("");

  const normalizedQuery = query.trim().toLowerCase();
  const searching = normalizedQuery.length > 0;
  // Client-side directory search: match the display title or the member id
  // (the segment after "agents/" / "users/"), case-insensitively.
  const matchesQuery = (m: MemberSummary) =>
    !searching ||
    m.title.toLowerCase().includes(normalizedQuery) ||
    m.name
      .replace(/^(agents|users)\//, "")
      .toLowerCase()
      .includes(normalizedQuery);

  const agents = members.filter((m) => m.kind === "agent" && matchesQuery(m));
  const humans = members.filter((m) => m.kind === "user" && matchesQuery(m));
  // While searching, hide sections that have no matches instead of showing an
  // empty group; when nothing matches at all, a dedicated message appears.
  const showAgents = !searching || agents.length > 0;
  const showHumans = !searching || humans.length > 0;

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Left rail: members directory. */}
      <aside
        className={cn(
          "shrink-0 flex-col border-r border-control-border overflow-hidden",
          hasSelection ? "hidden lg:flex lg:w-60" : "flex w-full lg:w-60"
        )}
      >
        <div className="hidden lg:flex items-center justify-between gap-2 border-b border-control-border px-3 py-3 shrink-0">
          <h1 className="text-sm font-semibold text-main truncate">
            {t("members.title")}
          </h1>
        </div>

        <div className="shrink-0 px-3 py-2">
          <SearchInput
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("members.search-placeholder")}
            aria-label={t("members.search-placeholder")}
            className="h-8 rounded-full text-sm"
          />
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
          ) : searching && agents.length === 0 && humans.length === 0 ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("members.no-search-results", { query: query.trim() })}
            </p>
          ) : (
            <div className="flex flex-col">
              {showAgents && (
                <>
                  <SectionHeader
                    label={t("members.section-agents")}
                    count={agents.length}
                    open={agentsOpen}
                    onToggle={() => setAgentsOpen((v) => !v)}
                    onAdd={() => navigate("/machines")}
                    addLabel={t("members.add-agent")}
                  />
                  {agentsOpen && (
                    <div className="divide-y divide-control-border/50">
                      {agents.map((member) => (
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
                            selectedAgentId ===
                            member.name.replace(/^agents\//, "")
                          }
                        />
                      ))}
                    </div>
                  )}
                </>
              )}
              {showHumans && (
                <>
                  <SectionHeader
                    label={t("members.section-humans")}
                    count={humans.length}
                    open={humansOpen}
                    onToggle={() => setHumansOpen((v) => !v)}
                    onAdd={() => navigate("/settings/users")}
                    addLabel={t("members.add-human")}
                  />
                  {humansOpen && (
                    <div className="divide-y divide-control-border/50">
                      {humans.map((member) => (
                        <MemberRow
                          key={member.name}
                          member={member}
                          selected={
                            selectedUserId ===
                            member.name.replace(/^users\//, "")
                          }
                        />
                      ))}
                    </div>
                  )}
                </>
              )}
            </div>
          )}

          {/* Channels roster — all joined/created channels including closed
              ones, so a closed conversation always has an entry point back.
              Independent of the member directory: it has its own fetch/loading
              state and is unaffected by the member search. */}
          <div className="flex flex-col">
            <SectionHeader
              label={t("members.section-channels")}
              count={myChannels.length}
              open={channelsOpen}
              onToggle={() => setChannelsOpen((v) => !v)}
            />
            {myChannelsLoading && channelsOpen && (
              <p className="px-3 py-2 text-sm text-control-light">
                {t("common.loading")}
              </p>
            )}
            {!myChannelsLoading &&
              channelsOpen &&
              (myChannels.length === 0 ? (
                <p className="px-3 py-2 text-sm text-control-light">
                  {t("members.channels-empty")}
                </p>
              ) : (
                <div className="divide-y divide-control-border/50">
                  {myChannels.map((channel) => (
                    <ChannelRow
                      key={channel.name}
                      channel={channel}
                      selected={
                        selectedChannelId ===
                        channel.name.replace(/^conversations\//, "")
                      }
                    />
                  ))}
                </div>
              ))}
          </div>
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
  onAdd?: () => void;
  addLabel?: string;
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
      {onAdd && addLabel && (
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
      )}
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
          ? "border-l-accent bg-control-bg"
          : "border-l-transparent hover:bg-control-bg/60"
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

// ChannelRow is a single channels-roster row: a channel opens its detail page
// (where a closed channel can be reopened from the Message action).
function ChannelRow({
  channel,
  selected,
}: {
  channel: Conversation;
  selected: boolean;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const resourceId = channel.name.replace(/^conversations\//, "");

  return (
    <button
      type="button"
      onClick={() => navigate(`/members/channels/${resourceId}`)}
      aria-label={t("members.row-open-channel", { title: channel.title })}
      className={cn(
        "flex w-full items-center gap-2 px-3 py-2 text-left transition-colors border-l-2",
        selected
          ? "border-l-accent bg-control-bg"
          : "border-l-transparent hover:bg-control-bg/60"
      )}
    >
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-control-bg text-control">
        <Hash className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <span className="truncate text-sm font-medium text-main">
          {channel.title}
        </span>
      </div>
      <span className="text-xs text-control-light">
        {t("channel.members", { count: channel.memberCount ?? 0 })}
      </span>
    </button>
  );
}
