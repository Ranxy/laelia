import { Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { agentServiceClient, agentTeamServiceClient } from "@/connect";
import { Avatar } from "@/components/chat/avatar";
import { avatarNameForAgentId, useAvatar } from "@/lib/avatar-cache";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import { State } from "@/types/proto-es/v1/common_pb";
import { type AgentTeam, type AgentTeamMember } from "@/types/proto-es/v1/agent_team_service_pb";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import { Button } from "@/components/ui/button";

export function AgentTeamsManager() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canList = useHasPermission("laelia.agentTeams.list");
  const canCreate = useHasPermission("laelia.agentTeams.create");
  const currentUserId = useAppStore((s) => s.currentUser?.name?.split("/").pop());

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

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-control">
          {t("settings.agentTeams.title")}
          <span className="ml-2 font-mono text-xs text-control-light">
            {manageable.length}
          </span>
        </span>
        {canCreate && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              const userId = currentUserId ?? "";
              navigate(`/members/users/${userId}/teams/new`);
            }}
          >
            <Plus className="size-4" />
            {t("settings.agentTeams.create")}
          </Button>
        )}
      </div>

      {loading ? (
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      ) : manageable.length === 0 ? (
        <p className="text-sm text-control-light">
          {t("settings.agentTeams.empty")}
        </p>
      ) : (
        <div className="flex flex-col gap-2">
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
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-3 rounded-md border border-control-border bg-background px-3 py-2 text-left transition-colors hover:border-accent/40 hover:bg-control-bg/60"
    >
      <TeamMemberStack members={team.members} agents={agents} />
      <div className="min-w-0 flex-1 flex flex-col gap-0.5">
        <span className="truncate text-sm font-semibold text-main">
          {team.title}
        </span>
        {team.description && (
          <span className="truncate text-xs text-control-light">
            {team.description}
          </span>
        )}
        <span className="truncate text-xs text-control-light">
          {t("settings.agentTeams.leader")}: {agentLabel(team.leaderAgent)}
        </span>
      </div>
    </button>
  );
}

// Stacked member avatars using the agents' real avatars. Shows up to 3, then a
// +N overflow indicator.
function TeamMemberStack({
  members,
  agents,
}: {
  members: AgentTeamMember[];
  agents: AgentSummary[];
}) {
  const visible = members.slice(0, 3);
  const extra = members.length - visible.length;
  return (
    <div className="flex shrink-0 items-center">
      {visible.map((m, i) => {
        const id = m.agent.split("/").pop() ?? "";
        const agent = agents.find((a) => a.handle === id);
        return (
          <div key={m.agent} className={i === 0 ? "" : "-ml-1.5"}>
            <MemberAvatar
              agent={agent}
              seed={id || m.agent}
              label={agent?.title || agent?.handle || id}
            />
          </div>
        );
      })}
      {extra > 0 && (
        <div className="-ml-1.5 flex size-6 items-center justify-center rounded-full border border-background bg-control-bg text-[10px] font-medium text-control">
          +{extra}
        </div>
      )}
    </div>
  );
}

function MemberAvatar({
  agent,
  seed,
  label,
}: {
  agent?: AgentSummary;
  seed: string;
  label: string;
}) {
  const avatarSrc = useAvatar(avatarNameForAgentId(agent?.handle || seed));
  return (
    <div title={label} className="size-6">
      <Avatar seed={seed} src={avatarSrc} />
    </div>
  );
}
