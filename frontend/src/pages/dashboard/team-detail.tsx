import { create } from "@bufbuild/protobuf";
import { ArrowLeft, Pencil, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
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
import { agentTeamServiceClient } from "@/connect";
import {
  AGENT_TEAMS_QUERY_KEY,
  useAgentTeamsQuery,
} from "@/hooks/use-agent-teams";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { queryClient } from "@/lib/query-client";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { useAppStore } from "@/stores";
import type { AgentTeam } from "@/types/proto-es/v1/agent_team_service_pb";
import {
  AgentTeamMemberSchema,
  AgentTeamRole,
  AgentTeamSchema,
} from "@/types/proto-es/v1/agent_team_service_pb";
import { State } from "@/types/proto-es/v1/common_pb";

const EMPTY_TEAM_FORM: TeamFormValues = {
  title: "",
  description: "",
  teamPrompt: "",
  members: [],
};

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
  const teamName = isCreate ? null : `agentTeams/${teamId ?? ""}`;
  // The form derives from the fetched team; user edits live in `formDraft`
  // (cleared on team switch / save / cancel, so the derived values always
  // reflect the current server state — parity with the old load() seeding).
  const [formDraft, setFormDraft] = useState<TeamFormValues | null>(null);
  const [editing, setEditing] = useState(isCreate);
  const [saving, setSaving] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Reads moved onto the shared primitives: the teams directory is one
  // ["agent-teams"] cache entry (also consumed by the thread assignee dropdown
  // and the manager card), the team detail is a keyed single-item read, and
  // the agents come from the shared roster (fetchAgents) instead of a private
  // full-page copy.
  const teamsQuery = useAgentTeamsQuery({
    failureTitle: t("settings.agentTeams.load-failed"),
  });
  const teams = teamsQuery.items;
  const detailQuery = useResourceQuery<AgentTeam>({
    enabled: !isCreate && !!teamName,
    queryKey: ["agent-team", teamName],
    queryFn: async () => [
      await agentTeamServiceClient.getAgentTeam({ name: teamName ?? "" }),
    ],
    failureTitle: t("settings.agentTeams.load-failed"),
  });
  const team = detailQuery.items[0] ?? null;

  const agents = useAppStore((s) => s.agents);
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const agentsLoading = useAppStore((s) => s.agentsLoading);
  useEffect(() => {
    if (useAppStore.getState().agents.length === 0) {
      void fetchAgents({ pageSize: 1000 });
    }
  }, [fetchAgents]);

  const loading =
    teamsQuery.initialLoading ||
    (!isCreate && detailQuery.initialLoading) ||
    (agents.length === 0 && agentsLoading);

  // Derived form: drafts win while editing; otherwise the server team seeds it.
  const form: TeamFormValues =
    formDraft ?? (team ? teamToForm(team) : EMPTY_TEAM_FORM);
  // A stale draft from another team must never leak into the next page.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset keyed on the team identity; the body reads neither.
  useEffect(() => {
    setFormDraft(null);
  }, [teamName]);

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

  // First edit seeds the draft from the CURRENT effective form (not the empty
  // skeleton), so editing one field never drops the rest of the team.
  const updateForm = (patch: Partial<TeamFormValues>) => {
    setFormDraft({ ...form, ...patch });
  };

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
    setFormDraft(next);
  };

  const addMember = () => {
    const available = availableAgents.filter(
      (a) => !form.members.some((m) => agentIdFromName(m.agent) === a.handle)
    );
    if (available.length === 0) return;
    const first = available[0];
    setFormDraft({
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
    setFormDraft(next);
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
        // The team directory changed: the shared ["agent-teams"] entry feeds
        // the manager card and the thread assignee dropdown.
        void queryClient.invalidateQueries({ queryKey: AGENT_TEAMS_QUERY_KEY });
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
        // Re-read through the cache keys (the old load() refetched everything).
        setFormDraft(null);
        detailQuery.reload();
        teamsQuery.reload();
      }
    } catch (err) {
      void showErrorToast(err, t("settings.agentTeams.save-failed"));
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
      void queryClient.invalidateQueries({ queryKey: AGENT_TEAMS_QUERY_KEY });
      navigate(`/members/users/${userId ?? ""}`);
    } catch (err) {
      void showErrorToast(err, t("settings.agentTeams.delete-failed"));
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
                  setFormDraft(null);
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
                aria-label={t("settings.agentTeams.delete")}
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
                <label
                  htmlFor="team-title"
                  className="text-xs font-medium text-control"
                >
                  {t("settings.agentTeams.team-name")}
                </label>
                <Input
                  id="team-title"
                  value={form.title}
                  onChange={(e) => updateForm({ title: e.target.value })}
                  placeholder={t("settings.agentTeams.team-name")}
                />
              </div>
            )}
            <div className="flex flex-col gap-1.5">
              <label
                htmlFor="team-description"
                className="text-xs font-medium text-control"
              >
                {t("settings.agentTeams.description")}
              </label>
              {editing || isCreate ? (
                <Input
                  id="team-description"
                  value={form.description}
                  onChange={(e) => updateForm({ description: e.target.value })}
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
          onChange={setFormDraft}
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
