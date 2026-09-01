import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAgentTeamsQuery } from "@/hooks/use-agent-teams";
import { taskStatusLabel } from "@/lib/task-status";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";
import { TaskStatus } from "@/types/proto-es/v1/command_pb";

// ---------------------------------------------------------------------------
// ThreadTaskControls — the status + assignee dropdowns of a task thread's
// header. Extracted from the former monolithic ThreadHeader: it owns the
// assignee-roster loading (channel members for users/agents, plus the agent
// teams) and the update/assign busy states. Rendered only for manageable
// task roots (canManageTask at the call site), which is why its effects
// always run.
// ---------------------------------------------------------------------------

export function ThreadTaskControls({
  channelId,
  rootMsg,
}: {
  channelId: string;
  rootMsg: ChatMessageUI;
}) {
  const { t } = useTranslation();
  const updateTaskStatus = useAppStore((s) => s.updateTaskStatus);
  const assignTask = useAppStore((s) => s.assignTask);
  const listChannelMembers = useAppStore((s) => s.listChannelMembers);
  const conversationName = `conversations/${channelId}`;
  const members =
    useAppStore((s) => s.channelMembersByConv[conversationName]) ?? [];
  const [statusUpdating, setStatusUpdating] = useState(false);
  const [assigning, setAssigning] = useState(false);

  // Teams ride the shared ["agent-teams"] cache (AgentTeamsManager and
  // TeamDetailPage consume the same entry), replacing this component's private
  // full-list fetch.
  const { items: teams } = useAgentTeamsQuery({
    failureTitle: t("settings.agentTeams.load-failed"),
  });

  // Load the channel roster for the assignee dropdown on first render of a
  // task thread (the members panel may not have been opened yet).
  // biome-ignore lint/correctness/useExhaustiveDependencies: same fetch discipline as the pre-split header — one load per mount / channel change; the roster guard keeps repeat mounts from re-fetching.
  useEffect(() => {
    if (members.length === 0) {
      void listChannelMembers(channelId);
    }
  }, [channelId]);

  const handleStatusChange = async (value: string | null) => {
    if (value == null) return;
    const status = Number(value);
    if (status === rootMsg.task?.status) return;
    setStatusUpdating(true);
    try {
      await updateTaskStatus(channelId, rootMsg.id, status);
      toastManager.add({
        type: "success",
        title: t("channelTask.status-change-success"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("channelTask.status-change-error"),
        description:
          err instanceof Error
            ? err.message
            : t("channelTask.status-change-error-description"),
      });
    } finally {
      setStatusUpdating(false);
    }
  };

  const handleAssigneeChange = async (value: string | null) => {
    if (value == null) return;
    // value is "<memberType>:<memberId>".
    const [memberType, memberId] = value.split(":");
    if (!memberType || !memberId) return;
    setAssigning(true);
    try {
      await assignTask(channelId, rootMsg.id, Number(memberType), memberId);
      toastManager.add({
        type: "success",
        title: t("channelTask.assignee-success"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("channelTask.assignee-error"),
        description:
          err instanceof Error
            ? err.message
            : t("channelTask.assignee-error-description"),
      });
    } finally {
      setAssigning(false);
    }
  };

  return (
    <>
      {/* Status dropdown: move the task between any of the four statuses.
          DONE closes the task (sets completed_at). */}
      <Select
        value={String(rootMsg.task?.status ?? TaskStatus.TODO)}
        onValueChange={(v) => void handleStatusChange(v)}
        disabled={statusUpdating}
      >
        <SelectTrigger
          size="xs"
          className="shrink-0"
          aria-label={t("channelTask.status-change-aria") ?? ""}
        >
          <SelectValue>
            {(value) => taskStatusLabel(Number(value), t)}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={String(TaskStatus.TODO)}>
            {t("channelTask.status-todo")}
          </SelectItem>
          <SelectItem value={String(TaskStatus.IN_PROGRESS)}>
            {t("channelTask.status-in-progress")}
          </SelectItem>
          <SelectItem value={String(TaskStatus.IN_REVIEW)}>
            {t("channelTask.status-in-review")}
          </SelectItem>
          <SelectItem value={String(TaskStatus.DONE)}>
            {t("channelTask.status-done")}
          </SelectItem>
        </SelectContent>
      </Select>

      {/* Assignee dropdown: assign any channel member (user or agent) as
          the task's owner. */}
      <Select
        value={
          rootMsg.task?.assigneeType && rootMsg.task.assigneeResourceId
            ? `${rootMsg.task.assigneeType}:${rootMsg.task.assigneeResourceId}`
            : ""
        }
        onValueChange={(v) => void handleAssigneeChange(v)}
        disabled={assigning}
      >
        <SelectTrigger
          size="xs"
          className="shrink-0"
          aria-label={t("channelTask.assignee-aria") ?? ""}
        >
          <SelectValue>
            {() =>
              rootMsg.task?.assigneeName ||
              t("channelTask.assignee-placeholder")
            }
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {members.map((m) => (
            <SelectItem
              key={`${m.memberType}:${m.memberId}`}
              value={`${m.memberType}:${m.memberId}`}
            >
              {m.displayName}
            </SelectItem>
          ))}
          {teams.length > 0 && (
            <SelectItem key="team-separator" value="team-separator" disabled>
              {t("channelTask.assignee-teams")}
            </SelectItem>
          )}
          {teams.map((team) => {
            const teamId = team.name.split("/").pop() ?? "";
            return (
              <SelectItem key={team.name} value={`3:${teamId}`}>
                {team.title}
              </SelectItem>
            );
          })}
        </SelectContent>
      </Select>
    </>
  );
}
