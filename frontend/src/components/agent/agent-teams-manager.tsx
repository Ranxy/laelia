import { Plus, Users } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { agentServiceClient, agentTeamServiceClient } from "@/connect";
import { avatarNameForAgentId, useAvatar } from "@/lib/avatar-cache";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import {
  type AgentTeam,
  type AgentTeamMember,
} from "@/types/proto-es/v1/agent_team_service_pb";
import { State } from "@/types/proto-es/v1/common_pb";

const AVATAR_STACK_LIMIT = 2;

export function AgentTeamsManager() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canList = useHasPermission("laelia.agentTeams.list");
  const canCreate = useHasPermission("laelia.agentTeams.create");
  const currentUserId = useAppStore((s) =>
    s.currentUser?.name?.split("/").pop()
  );

  const [teams, setTeams] = useState<AgentTeam[]>([]);
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [loading, setLoading] = useState(true);

  const activeAgents = useMemo(
    () => agents.filter((a) => a.state === State.ACTIVE),
    [agents]
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [teamRes, agentRes] = await Promise.all([
        agentTeamServiceClient.listAgentTeams({ pageSize: 1000 }),
        agentServiceClient.listAgents({ pageSize: 1000 }),
      ]);
      setTeams(teamRes.agentTeams ?? []);
      setAgents(agentRes.agents ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (canList) load();
    else setLoading(false);
  }, [canList, load]);

  const agentLabel = (name: string) => {
    const id = name.split("/").pop() ?? name;
    const agent = activeAgents.find((a) => a.handle === id || a.name === name);
    return agent ? agent.title || agent.handle || id : id;
  };

  const manageable = teams.filter((team) => team.canManage);
  const createTeam = () => {
    const userId = currentUserId ?? "";
    navigate(`/members/users/${userId}/teams/new`);
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-main">
            {t("settings.agentTeams.title")}
          </span>
          <span className="rounded-full bg-control-bg px-2 py-0.5 text-xs font-medium text-control">
            {manageable.length}
          </span>
        </div>
        {canCreate && (
          <Button size="sm" onClick={createTeam}>
            <Plus className="size-4" />
            {t("settings.agentTeams.create")}
          </Button>
        )}
      </div>

      {loading ? (
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      ) : manageable.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-control-border bg-background p-8 text-center">
          <Users className="size-10 text-control-light" />
          <div className="flex flex-col gap-1">
            <p className="text-sm font-semibold text-main">
              {t("settings.agentTeams.empty")}
            </p>
            <p className="max-w-[260px] text-xs leading-relaxed text-control-light">
              {t("settings.agentTeams.empty-description")}
            </p>
          </div>
          {canCreate && (
            <Button size="sm" variant="outline" onClick={createTeam}>
              <Plus className="size-4" />
              {t("settings.agentTeams.create")}
            </Button>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {manageable.map((team) => (
            <TeamCard
              key={team.name}
              team={team}
              agents={activeAgents}
              agentLabel={agentLabel}
              onClick={() => {
                const userId = team.owner.split("/").pop() ?? "";
                const teamId = team.name.split("/").pop() ?? "";
                navigate(`/members/users/${userId}/teams/${teamId}`);
              }}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function TeamCard({
  team,
  agents,
  agentLabel,
  onClick,
}: {
  team: AgentTeam;
  agents: AgentSummary[];
  agentLabel: (name: string) => string;
  onClick: () => void;
}) {
  const { t } = useTranslation();
  const memberCount = team.members.length;
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-3 rounded-lg border border-control-border bg-background p-3 text-left transition-colors hover:border-accent/40 hover:bg-control-bg/40"
    >
      <div className="flex w-16 shrink-0 items-center justify-start sm:w-20">
        <TeamMemberStack members={team.members} agents={agents} />
      </div>
      <div className="min-w-0 flex-1 flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="max-w-[120px] truncate text-sm font-semibold text-main sm:max-w-none">
            {team.title}
          </span>
          <Badge variant="secondary" className="shrink-0 text-xs">
            {t("settings.agentTeams.leader")}: {agentLabel(team.leaderAgent)}
          </Badge>
          <span className="ml-auto shrink-0 text-xs text-control-light">
            {t("settings.agentTeams.member-count", { count: memberCount })}
          </span>
        </div>
        {team.description ? (
          <p className="line-clamp-1 text-xs text-control-light">
            {team.description}
          </p>
        ) : null}
      </div>
    </button>
  );
}

function TeamMemberStack({
  members,
  agents,
}: {
  members: AgentTeamMember[];
  agents: AgentSummary[];
}) {
  const visible = members.slice(0, AVATAR_STACK_LIMIT);
  const extra = members.length - visible.length;
  return (
    <div className="flex shrink-0 items-center">
      {visible.map((m, i) => {
        const id = m.agent.split("/").pop() ?? "";
        const agent = agents.find((a) => a.handle === id);
        return (
          <div
            key={m.agent}
            className={
              i === 0 ? "" : "-ml-2 rounded-full border-2 border-background"
            }
          >
            <MemberAvatar
              agent={agent}
              seed={id || m.agent}
              label={agent?.title || agent?.handle || id}
              size={7}
            />
          </div>
        );
      })}
      {extra > 0 && (
        <div className="-ml-2 flex size-7 items-center justify-center rounded-full border-2 border-background bg-control-bg text-[10px] font-medium text-control">
          +{Math.min(extra, 99)}
        </div>
      )}
    </div>
  );
}

function MemberAvatar({
  agent,
  seed,
  label,
  size,
}: {
  agent?: AgentSummary;
  seed: string;
  label: string;
  size?: 6 | 7 | 8 | 10 | 12 | 14 | 16;
}) {
  const avatarSrc = useAvatar(avatarNameForAgentId(agent?.handle || seed));
  return (
    <div title={label}>
      <Avatar seed={seed} src={avatarSrc} size={size} />
    </div>
  );
}
