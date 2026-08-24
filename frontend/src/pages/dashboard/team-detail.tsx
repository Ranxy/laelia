import { create } from "@bufbuild/protobuf";
import { ArrowLeft, Pencil, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import {
  TeamFormFields,
  type TeamFormValues,
} from "@/components/agent/agent-team-form";
import { Card } from "@/components/profile-common";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { agentServiceClient, agentTeamServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import {
  type AgentTeam,
  AgentTeamMemberSchema,
  AgentTeamRole,
  AgentTeamSchema,
} from "@/types/proto-es/v1/agent_team_service_pb";
import { State } from "@/types/proto-es/v1/common_pb";

function teamToForm(team: AgentTeam): TeamFormValues {
  return {
    title: team.title,
    description: team.description,
    teamPrompt: team.teamPrompt,
    members: (team.members ?? []).map((m) => ({
      agent: m.agent,
      role: m.role,
      responsibility: m.responsibility,
    })),
  };
}

function memberToProto(m: TeamFormValues["members"][number]) {
  return create(AgentTeamMemberSchema, {
    agent: m.agent,
    role: m.role,
    responsibility: m.responsibility,
  });
}

function agentIdFromName(name: string): string {
  return name.split("/").pop() ?? name;
}

export function TeamDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { userId, teamId } = useParams<{ userId: string; teamId: string }>();
  const currentUser = useAppStore((s) => s.currentUser);

  const isCreate = teamId === "new";
  const [team, setTeam] = useState<AgentTeam | null>(null);
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [teams, setTeams] = useState<AgentTeam[]>([]);
  const [form, setForm] = useState<TeamFormValues>({
    title: "",
    description: "",
    teamPrompt: "",
    members: [],
  });
  // Create mode starts in the editable state; edit mode starts read-only.
  const [editing, setEditing] = useState(isCreate);
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    if (!teamId) return;
    setLoading(true);
    try {
      const [agentData, teamListData] = await Promise.all([
        agentServiceClient.listAgents({ pageSize: 1000 }),
        agentTeamServiceClient.listAgentTeams({ pageSize: 1000 }),
      ]);
      setAgents(agentData.agents ?? []);
      setTeams(teamListData.agentTeams ?? []);
      if (!isCreate) {
        const teamData = await agentTeamServiceClient.getAgentTeam({
          name: `agentTeams/${teamId}`,
        });
        setTeam(teamData);
        setForm(teamToForm(teamData));
      }
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [teamId, t, isCreate]);

  useEffect(() => {
    void load();
  }, [load]);

  const activeAgents = useMemo(
    () => agents.filter((a) => a.state === State.ACTIVE),
    [agents]
  );
  const myAgents = useMemo(
    () => activeAgents.filter((a) => a.owner === currentUser?.name),
    [activeAgents, currentUser]
  );

  const teamByAgent = useMemo(() => {
    const map = new Map<string, string[]>();
    for (const tm of teams) {
      for (const m of tm.members ?? []) {
        const id = agentIdFromName(m.agent);
        const arr = map.get(id) ?? [];
        arr.push(tm.name);
        map.set(id, arr);
      }
    }
    return map;
  }, [teams]);

  const availableAgents = useMemo(() => {
    const selectedIds = new Set(
      form.members.map((m) => agentIdFromName(m.agent))
    );
    const editingTeamName = team?.name;
    return myAgents.filter((a) => {
      if (selectedIds.has(a.handle)) return true;
      const teamsForAgent = teamByAgent.get(a.handle) ?? [];
      const inOtherTeam = teamsForAgent.some(
        (name) => name !== editingTeamName
      );
      return !inOtherTeam;
    });
  }, [myAgents, teamByAgent, form.members, team]);

  const hasLeader = (form: TeamFormValues) =>
    form.members.some((m) => m.role === AgentTeamRole.LEADER);

  const updateMember = (
    nextForm: TeamFormValues,
    index: number,
    patch: Partial<TeamFormValues["members"][number]>
  ) => {
    const next = { ...nextForm, members: [...nextForm.members] };
    next.members[index] = { ...next.members[index], ...patch };
    if (patch.role === AgentTeamRole.LEADER) {
      next.members = next.members.map((m, i) =>
        i === index ? m : { ...m, role: AgentTeamRole.MEMBER }
      );
    }
    setForm(next);
  };

  const addMember = () => {
    const available = availableAgents.filter(
      (a) => !form.members.some((m) => agentIdFromName(m.agent) === a.handle)
    );
    if (available.length === 0) return;
    const first = available[0];
    setForm({
      ...form,
      members: [
        ...form.members,
        {
          agent: `agents/${first.handle}`,
          role: hasLeader(form) ? AgentTeamRole.MEMBER : AgentTeamRole.LEADER,
          responsibility: "",
        },
      ],
    });
  };

  const removeMember = (index: number) => {
    const next = { ...form, members: [...form.members] };
    next.members.splice(index, 1);
    setForm(next);
  };

  const handleSave = async () => {
    if (!form.title.trim()) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.title-required"),
      });
      return;
    }
    if (!hasLeader(form)) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.at-least-one-leader"),
      });
      return;
    }
    setSaving(true);
    try {
      const leader = form.members.find((m) => m.role === AgentTeamRole.LEADER);
      if (isCreate) {
        await agentTeamServiceClient.createAgentTeam({
          agentTeam: create(AgentTeamSchema, {
            title: form.title,
            description: form.description,
            teamPrompt: form.teamPrompt,
            leaderAgent: leader?.agent ?? "",
            members: form.members.map(memberToProto),
          }),
        });
        toastManager.add({
          type: "success",
          title: t("settings.agentTeams.created"),
        });
        navigate(`/members/users/${userId ?? ""}`);
      } else if (team) {
        await agentTeamServiceClient.updateAgentTeam({
          agentTeam: create(AgentTeamSchema, {
            name: team.name,
            title: form.title,
            description: form.description,
            teamPrompt: form.teamPrompt,
            leaderAgent: leader?.agent ?? "",
            members: form.members.map(memberToProto),
          }),
          updateMask: {
            paths: [
              "title",
              "description",
              "team_prompt",
              "leader_agent",
              "members",
            ],
          },
        });
        toastManager.add({
          type: "success",
          title: t("settings.agentTeams.saved"),
        });
        setEditing(false);
        await load();
      }
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.save-failed"),
        description: describeError(err),
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!team) return;
    setDeleting(true);
    try {
      await agentTeamServiceClient.deleteAgentTeam({ name: team.name });
      toastManager.add({
        type: "success",
        title: t("settings.agentTeams.deleted"),
      });
      navigate(`/members/users/${userId ?? ""}`);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agentTeams.delete-failed"),
        description: describeError(err),
      });
    } finally {
      setDeleting(false);
    }
  };

  const goBack = () => navigate(`/members/users/${userId ?? ""}`);

  if (loading) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      </div>
    );
  }

  if (!team && !isCreate) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">
          {t("settings.agentTeams.empty")}
        </p>
      </div>
    );
  }

  const pageTitle = isCreate
    ? t("settings.agentTeams.create")
    : editing
      ? t("settings.agentTeams.edit")
      : (team?.title ?? "");

  return (
    <div className="h-full overflow-y-auto px-3 py-3 sm:px-5 sm:py-5 pb-20 sm:pb-5">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Button variant="ghost" size="sm" onClick={goBack}>
            <ArrowLeft className="size-4" />
            {t("members.back")}
          </Button>
          <h1 className="truncate text-lg font-semibold text-main">
            {pageTitle}
          </h1>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {isCreate ? (
            <>
              <Button size="sm" onClick={handleSave} disabled={saving}>
                {saving ? t("common.saving") : t("common.create")}
              </Button>
              <Button size="sm" variant="outline" onClick={goBack}>
                <X className="size-4" />
                <span className="hidden sm:inline">{t("common.cancel")}</span>
              </Button>
            </>
          ) : editing ? (
            <>
              <Button size="sm" onClick={handleSave} disabled={saving}>
                {saving ? t("common.saving") : t("common.save")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  setEditing(false);
                  if (team) setForm(teamToForm(team));
                }}
              >
                <X className="size-4" />
                <span className="hidden sm:inline">{t("common.cancel")}</span>
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing(true)}
              >
                <Pencil className="size-4" />
                <span className="hidden sm:inline">
                  {t("settings.agentTeams.edit")}
                </span>
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-control-light hover:text-error"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 className="size-4" />
              </Button>
            </>
          )}
        </div>
      </header>

      <div className="flex flex-col gap-5">
        <Card title={t("settings.agentTeams.basic-info")}>
          <div className="flex flex-col gap-4">
            {(editing || isCreate) && (
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium text-control">
                  {t("settings.agentTeams.team-name")}
                </label>
                <Input
                  value={form.title}
                  onChange={(e) =>
                    setForm((f) => ({ ...f, title: e.target.value }))
                  }
                  placeholder={t("settings.agentTeams.team-name")}
                />
              </div>
            )}
            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-control">
                {t("settings.agentTeams.description")}
              </label>
              {editing || isCreate ? (
                <Input
                  value={form.description}
                  onChange={(e) =>
                    setForm((f) => ({ ...f, description: e.target.value }))
                  }
                  placeholder={t("settings.agentTeams.description")}
                />
              ) : form.description ? (
                <p className="whitespace-pre-wrap text-sm text-main">
                  {form.description}
                </p>
              ) : (
                <p className="text-sm italic text-control-light">
                  {t("settings.agentTeams.no-description")}
                </p>
              )}
            </div>
          </div>
        </Card>

        <TeamFormFields
          agents={availableAgents}
          form={form}
          disabled={!editing && !isCreate}
          onChange={setForm}
          onAdd={addMember}
          onRemove={removeMember}
          onUpdate={(i, patch) => updateMember(form, i, patch)}
        />
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>{t("settings.agentTeams.delete")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.agentTeams.delete-description")}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>{t("common.cancel")}</AlertDialogClose>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleting}
            >
              {deleting ? t("common.deleting") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
