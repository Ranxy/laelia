import { ChevronDown, Plus, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/chat/avatar";
import { Card } from "@/components/profile-common";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { avatarNameForAgentId, useAvatar } from "@/lib/avatar-cache";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import { AgentTeamRole } from "@/types/proto-es/v1/agent_team_service_pb";

export interface TeamFormValues {
  title: string;
  description: string;
  teamPrompt: string;
  members: TeamMemberForm[];
}

export interface TeamMemberForm {
  agent: string;
  role: AgentTeamRole;
  responsibility: string;
}

export function TeamFormFields({
  agents,
  form,
  disabled,
  onAdd,
  onRemove,
  onUpdate,
  onChange,
}: {
  agents: AgentSummary[];
  form: TeamFormValues;
  disabled?: boolean;
  onAdd?: () => void;
  onRemove?: (index: number) => void;
  onUpdate?: (index: number, patch: Partial<TeamMemberForm>) => void;
  onChange?: (form: TeamFormValues) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-5">
      <Card title={t("settings.agentTeams.teamPrompt")}>
        {disabled ? (
          form.teamPrompt ? (
            <p className="whitespace-pre-wrap text-sm text-main">
              {form.teamPrompt}
            </p>
          ) : (
            <p className="text-sm italic text-control-light">
              {t("settings.agentTeams.no-prompt")}
            </p>
          )
        ) : (
          <Textarea
            value={form.teamPrompt}
            onChange={(e) =>
              onChange?.({ ...form, teamPrompt: e.target.value })
            }
            placeholder={t("settings.agentTeams.teamPrompt")}
            className="min-h-[120px]"
          />
        )}
      </Card>

      <Card
        title={t("settings.agentTeams.members")}
        actions={
          !disabled && onAdd ? (
            <Button type="button" variant="outline" size="sm" onClick={onAdd}>
              <Plus className="size-4" />
              {t("settings.agentTeams.add-member")}
            </Button>
          ) : undefined
        }
      >
        <MemberEditor
          agents={agents}
          form={form}
          disabled={disabled}
          onAdd={onAdd}
          onRemove={onRemove}
          onUpdate={onUpdate}
          t={t}
        />
      </Card>
    </div>
  );
}

function MemberEditor({
  agents,
  form,
  disabled,
  onAdd,
  onRemove,
  onUpdate,
  t,
}: {
  agents: AgentSummary[];
  form: TeamFormValues;
  disabled?: boolean;
  onAdd?: () => void;
  onRemove?: (index: number) => void;
  onUpdate?: (index: number, patch: Partial<TeamMemberForm>) => void;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  if (form.members.length === 0) {
    const hasAvailableAgents = agents.length > 0;
    return (
      <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-control-border p-6 text-center">
        <p className="text-sm text-control-light">
          {hasAvailableAgents
            ? t("settings.agentTeams.no-members")
            : t("settings.agentTeams.no-agents")}
        </p>
        {!disabled && onAdd && hasAvailableAgents && (
          <Button type="button" variant="outline" size="sm" onClick={onAdd}>
            <Plus className="size-4" />
            {t("settings.agentTeams.add-member")}
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {form.members.map((m, i) =>
        disabled ? (
          <ReadMemberRow
            key={`${m.agent}-${i}`}
            agents={agents}
            member={m}
            t={t}
          />
        ) : (
          <EditMemberRow
            key={`${m.agent}-${i}`}
            agents={agents}
            member={m}
            index={i}
            form={form}
            onUpdate={onUpdate}
            onRemove={onRemove}
            t={t}
          />
        )
      )}
    </div>
  );
}

function ReadMemberRow({
  agents,
  member,
  t,
}: {
  agents: AgentSummary[];
  member: TeamMemberForm;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  const id = member.agent.split("/").pop() ?? "";
  const agent = agents.find((a) => a.handle === id);
  const avatarSrc = useAvatar(avatarNameForAgentId(agent?.handle || id));
  const name = agent?.title || agent?.handle || id;
  return (
    <div className="flex items-start gap-3 py-2">
      <div className="size-8 shrink-0">
        <Avatar seed={id || member.agent} src={avatarSrc} />
      </div>
      <div className="min-w-0 flex-1 flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-main">{name}</span>
          {member.role === AgentTeamRole.LEADER && (
            <Badge variant="secondary" className="text-xs">
              {t("settings.agentTeams.leader")}
            </Badge>
          )}
        </div>
        {member.responsibility ? (
          <p className="text-xs text-control-light">{member.responsibility}</p>
        ) : (
          <p className="text-xs italic text-control-light">
            {t("settings.agentTeams.no-responsibility")}
          </p>
        )}
      </div>
    </div>
  );
}

function EditMemberRow({
  agents,
  member,
  index,
  form,
  onUpdate,
  onRemove,
  t,
}: {
  agents: AgentSummary[];
  member: TeamMemberForm;
  index: number;
  form: TeamFormValues;
  onUpdate?: (index: number, patch: Partial<TeamMemberForm>) => void;
  onRemove?: (index: number) => void;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-control-border p-3">
      <div className="min-w-0">
        <AgentSelect
          agents={agents}
          value={member.agent}
          exclude={form.members.map((x) => x.agent)}
          onChange={(agent) => onUpdate?.(index, { agent })}
        />
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <Select
          value={String(member.role)}
          onValueChange={(v) =>
            onUpdate?.(index, { role: Number(v) as AgentTeamRole })
          }
        >
          <SelectTrigger className="w-full shrink-0 sm:w-32">
            <SelectValue>
              {(value) =>
                value === String(AgentTeamRole.LEADER)
                  ? t("settings.agentTeams.leader")
                  : t("settings.agentTeams.member")
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={String(AgentTeamRole.LEADER)}>
              {t("settings.agentTeams.leader")}
            </SelectItem>
            <SelectItem value={String(AgentTeamRole.MEMBER)}>
              {t("settings.agentTeams.member")}
            </SelectItem>
          </SelectContent>
        </Select>
        <Input
          placeholder={t("settings.agentTeams.responsibility")}
          value={member.responsibility}
          onChange={(e) =>
            onUpdate?.(index, { responsibility: e.target.value })
          }
          className="min-w-0 flex-1"
        />
        {onRemove && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="hidden shrink-0 text-control-light hover:text-error sm:inline-flex"
            onClick={() => onRemove(index)}
          >
            <Trash2 className="size-4" />
          </Button>
        )}
      </div>
      {onRemove && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full text-error hover:text-error sm:hidden"
          onClick={() => onRemove(index)}
        >
          <Trash2 className="size-4" />
          {t("common.remove")}
        </Button>
      )}
    </div>
  );
}

function AgentSelect({
  agents,
  value,
  exclude,
  onChange,
}: {
  agents: AgentSummary[];
  value: string;
  exclude: string[];
  onChange: (agentName: string) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [open]);

  const selected = agents.find((a) => `agents/${a.handle}` === value);
  const available = agents.filter(
    (a) =>
      !exclude.includes(`agents/${a.handle}`) || `agents/${a.handle}` === value
  );

  return (
    <div ref={containerRef} className="relative min-w-0 flex-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 rounded-md border border-control-border bg-transparent px-3 py-2 text-left text-sm hover:bg-control-bg"
      >
        {selected ? (
          <AgentOptionContent agent={selected} />
        ) : (
          <span className="text-control-placeholder">
            {t("settings.agentTeams.select-agent")}
          </span>
        )}
        <ChevronDown className="ml-auto size-4 shrink-0 text-control-light" />
      </button>
      {open && (
        <div className="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-auto rounded-lg border border-control-border bg-background py-1 shadow-md">
          {available.length === 0 ? (
            <div className="px-3 py-2 text-xs text-control-placeholder">
              {t("settings.agentTeams.no-agents")}
            </div>
          ) : (
            available.map((agent) => (
              <AgentOption
                key={agent.name}
                agent={agent}
                selected={`agents/${agent.handle}` === value}
                onSelect={() => {
                  onChange(`agents/${agent.handle}`);
                  setOpen(false);
                }}
              />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function AgentOption({
  agent,
  selected,
  onSelect,
}: {
  agent: AgentSummary;
  selected: boolean;
  onSelect: () => void;
}) {
  const { t } = useTranslation();
  const avatarSrc = useAvatar(avatarNameForAgentId(agent.handle || ""));
  return (
    <button
      type="button"
      onMouseDown={(e) => {
        e.preventDefault();
        onSelect();
      }}
      className="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-control-bg"
    >
      <div className="size-6 shrink-0">
        <Avatar seed={agent.handle || agent.name} src={avatarSrc} size={6} />
      </div>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-main">
          {agent.title || agent.handle}
        </span>
        {agent.description && (
          <span className="block truncate text-xs text-control-placeholder">
            {agent.description}
          </span>
        )}
      </span>
      {selected && <X className="size-3.5 shrink-0 text-control-light" />}
      <span className="shrink-0 rounded bg-control-bg px-1.5 py-0.5 text-[10px] font-medium text-control">
        {t("chat.agent")}
      </span>
    </button>
  );
}

function AgentOptionContent({ agent }: { agent: AgentSummary }) {
  const avatarSrc = useAvatar(avatarNameForAgentId(agent.handle || ""));
  return (
    <span className="flex min-w-0 items-center gap-3">
      <div className="size-6 shrink-0">
        <Avatar seed={agent.handle || agent.name} src={avatarSrc} size={6} />
      </div>
      <span className="truncate text-main">{agent.title || agent.handle}</span>
    </span>
  );
}
