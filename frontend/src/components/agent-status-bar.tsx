import { Bot, Brain, Loader2, MessageSquare, Play, Wrench } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AgentActivity } from "@/types/proto-es/v1/command_pb";

const MAX_VISIBLE = 3;

function statusIcon(status: string) {
  switch (status) {
    case "starting":
      return <Play className="size-3" />;
    case "thinking":
      return <Brain className="size-3" />;
    case "output":
      return <MessageSquare className="size-3" />;
    default:
      // Tool name or unknown — show a wrench.
      return <Wrench className="size-3" />;
  }
}

function statusColor(status: string): string {
  switch (status) {
    case "idle":
      return "text-control-placeholder";
    case "offline":
      return "text-control-placeholder";
    case "starting":
      return "text-blue-400";
    case "thinking":
      return "text-violet-400";
    case "output":
      return "text-emerald-400";
    default:
      // Tool name — amber.
      return "text-amber-400";
  }
}

function agentPill(activity: AgentActivity) {
  const active = activity.status !== "idle" && activity.status !== "offline";
  return (
    <span
      key={activity.agentId}
      className={cn(
        "inline-flex items-center gap-1 shrink-0 text-xs",
        active ? "text-main" : "text-control-placeholder"
      )}
    >
      <Bot className={cn("size-3", active && statusColor(activity.status))} />
      <span className="font-medium">
        {activity.displayName || activity.agentId}
      </span>
      {active && (
        <>
          <span className="text-control-placeholder">·</span>
          <span
            className={cn(
              "inline-flex items-center gap-0.5",
              statusColor(activity.status)
            )}
          >
            {statusIcon(activity.status)}
            <span>{activity.status}</span>
          </span>
        </>
      )}
      {!active && (
        <>
          <span className="text-control-placeholder">·</span>
          <span className="text-control-placeholder">idle</span>
        </>
      )}
    </span>
  );
}

export function AgentStatusBar({
  activities,
  loading,
}: {
  activities: AgentActivity[];
  loading?: boolean;
}) {
  if (loading) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-control-placeholder shrink-0">
        <Loader2 className="size-3 animate-spin" />
        <span>agents</span>
      </span>
    );
  }

  if (!activities || activities.length === 0) {
    return null;
  }

  const active = activities.filter(
    (a) => a.status !== "idle" && a.status !== "offline"
  );
  const idle = activities.filter(
    (a) => a.status === "idle" || a.status === "offline"
  );

  // Show active agents first, then a summary of idle count if any.
  const visible = active.slice(0, MAX_VISIBLE);
  const overflow = active.length - MAX_VISIBLE;

  return (
    <span className="inline-flex items-center gap-2 text-xs shrink-0 min-w-0">
      {visible.map(agentPill)}
      {overflow > 0 && (
        <span className="text-control-placeholder shrink-0">
          +{overflow} more
        </span>
      )}
      {active.length === 0 && idle.length > 0 && (
        <span className="text-control-placeholder inline-flex items-center gap-1">
          <Bot className="size-3" />
          <span>{idle.length} idle</span>
        </span>
      )}
    </span>
  );
}
