import { ChevronDown, Plus, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import { AgentTeamRole } from "@/types/proto-es/v1/agent_team_service_pb";
import { Avatar } from "@/components/chat/avatar";
import { avatarNameForAgentId, useAvatar } from "@/lib/avatar-cache";
import { Button } from "@/components/ui/button";
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

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
    <div className="flex flex-col gap-4">
      <FieldRow label={t("settings.agentTeams.title")}>
        <Input
          value={form.title}
          disabled={disabled}
          onChange={(e) =>
            onChange?.({ ...form, title: e.target.value })
          }
        />
      </FieldRow>
      <FieldRow label={t("settings.agentTeams.description")}>
        <Input
          value={form.description}
          disabled={disabled}
          onChange={(e) =>
            onChange?.({ ...form, description: e.target.value })
          }
        />
      </FieldRow>
      <FieldRow label={t("settings.agentTeams.teamPrompt")}>
        <Textarea
          value={form.teamPrompt}
          disabled={disabled}
          onChange={(e) =>
            onChange?.({ ...form, teamPrompt: e.target.value })
          }
        />
      </FieldRow>
      <MemberEditor
        agents={agents}
        form={form}
        disabled={disabled}
        onAdd={onAdd}
        onRemove={onRemove}
        onUpdate={onUpdate}
        t={t}
      />
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
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">
          {t("settings.agentTeams.members")}
        </span>
        {!disabled && onAdd && (
          <Button type="button" variant="outline" size="sm" onClick={onAdd}>
            <Plus className="size-4" />
            {t("settings.agentTeams.add-member")}
          </Button>
        )}
      </div>
      {form.members.map((m, i) => (
        <div key={i} className="flex flex-col gap-2 rounded-md border p-2">
          <div className="flex items-center gap-2">
            <AgentSelect
              agents={agents}
              value={m.agent}
              exclude={form.members.map((x) => x.agent)}
              disabled={disabled}
              onChange={(agent) => onUpdate?.(i, { agent })}
            />
            <Select
              value={String(m.role)}
              disabled={disabled}
              onValueChange={(v) =>
                onUpdate?.(i, { role: Number(v) as AgentTeamRole })
              }
            >
              <SelectTrigger className="w-32">
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
            {!disabled && onRemove && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => onRemove(i)}
              >
                <Trash2 className="size-4" />
              </Button>
            )}
          </div>
          <Input
            placeholder={t("settings.agentTeams.responsibility")}
            value={m.responsibility}
            disabled={disabled}
            onChange={(e) =>
              onUpdate?.(i, { responsibility: e.target.value })
            }
          />
        </div>
      ))}
    </div>
  );
}

function AgentSelect({
  agents,
  value,
  exclude,
  disabled,
  onChange,
}: {
  agents: AgentSummary[];
  value: string;
  exclude: string[];
  disabled?: boolean;
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
      !exclude.includes(`agents/${a.handle}`) ||
      `agents/${a.handle}` === value
  );

  return (
    <div ref={containerRef} className="relative min-w-0 flex-1">
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 rounded-md border border-control-border bg-transparent px-2 py-1.5 text-left text-sm hover:bg-control-bg disabled:cursor-not-allowed disabled:opacity-70"
      >
        {selected ? (
          <AgentOptionContent agent={selected} />
        ) : (
          <span className="text-control-placeholder">
            {t("settings.agentTeams.select-agent")}
          </span>
        )}
        {!disabled && (
          <ChevronDown className="ml-auto size-4 shrink-0 text-control-light" />
        )}
      </button>
      {open && !disabled && (
        <div className="absolute left-0 right-0 z-30 mt-1 max-h-60 overflow-auto rounded border border-control-border bg-background py-1 shadow-md">
          {available.length === 0 ? (
            <div className="px-2 py-1.5 text-xs text-control-placeholder">
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
      className="flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm hover:bg-control-bg"
    >
      <Avatar seed={agent.handle || agent.name} src={avatarSrc} />
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
    <span className="flex min-w-0 items-center gap-2">
      <Avatar seed={agent.handle || agent.name} src={avatarSrc} />
      <span className="truncate text-main">{agent.title || agent.handle}</span>
    </span>
  );
}
