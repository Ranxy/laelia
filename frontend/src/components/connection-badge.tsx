import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { StatusBadge } from "@/components/ui/status-badge";
import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

const connectionStateEntry: Partial<
  Record<AgentStatus_ConnectionState, StatusBadgeEntry>
> = {
  [AgentStatus_ConnectionState.STOPPED]: {
    variant: "secondary",
    labelKey: "agent.lifecycle.stopped",
  },
  [AgentStatus_ConnectionState.ONLINE]: {
    variant: "success",
    labelKey: "agent.status-online",
  },
  [AgentStatus_ConnectionState.ERROR]: {
    variant: "error",
    labelKey: "agent.status-error",
  },
};

interface ConnectionBadgeProps {
  state?: AgentStatus_ConnectionState;
  // enabled=false means the agent is stopped and processes no messages; it
  // takes precedence over the connection state so a stopped agent never shows
  // as Online/Offline.
  enabled?: boolean;
}

function ConnectionBadge({ state, enabled }: ConnectionBadgeProps) {
  const resolved =
    enabled === false || state === AgentStatus_ConnectionState.STOPPED
      ? AgentStatus_ConnectionState.STOPPED
      : state;
  return (
    <StatusBadge
      mapping={connectionStateEntry}
      status={resolved}
      fallback={{ variant: "secondary", labelKey: "agent.status-offline" }}
    />
  );
}

export { ConnectionBadge };
