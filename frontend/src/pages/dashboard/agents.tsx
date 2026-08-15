import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

type Lifecycle =
  | "waiting-connection"
  | "pending-config"
  | "ready"
  | "configured-offline"
  | "stopped";

// AgentLifecycleLike is the structural input agentLifecycle reads. It is
// satisfied by both AgentSummary (list view: top-level provider/executable)
// and the full Agent (profile view: info.acpConfig.provider/executable), so the
// same classifier serves both without branching on the concrete type.
export interface AgentLifecycleLike {
  status?: { state?: AgentStatus_ConnectionState };
  provider?: string;
  executable?: string;
  info?: { acpConfig?: { provider?: string; executable?: string } };
  enabled?: boolean;
}

// agentLifecycle classifies an agent's operational state for both the left-rail
// polling loop (we keep refreshing while any agent is non-ready) and the profile
// tab's lifecycle label. Exported so agent-profile can render the same label.
export function agentLifecycle(agent: AgentLifecycleLike): Lifecycle {
  // A stopped agent is not processing, regardless of its connection state.
  if (agent.enabled === false) return "stopped";
  const online = agent.status?.state === AgentStatus_ConnectionState.ONLINE;
  // An agent is "configured" when it has either a selected provider or a
  // custom executable. A built-in provider derives its command from the
  // registry, so executable is empty for it. AgentSummary surfaces these
  // top-level; the full Agent nests them under info.acpConfig.
  const provider = agent.provider ?? agent.info?.acpConfig?.provider ?? "";
  const executable =
    agent.executable ?? agent.info?.acpConfig?.executable ?? "";
  const configured = !!provider || !!executable;
  if (online && configured) return "ready";
  if (online && !configured) return "pending-config";
  if (!online && configured) return "configured-offline";
  return "waiting-connection";
}

export function lifecycleLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  state: Lifecycle
): string {
  return t(`agent.lifecycle.${state}`);
}
