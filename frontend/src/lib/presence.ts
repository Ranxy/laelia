import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

// Presence derivation shared by the chat left-rail rows and the chat header
// badge. Agent online state is the authoritative AgentStatus.ConnectionState
// from the agent roster (only ONLINE counts; STOPPED/OFFLINE/ERROR/KICKED do
// not); human presence comes from the SyncPresence heartbeat map keyed by the
// "users/<handle>" resource name.
export function isAgentOnline(agent: {
  name: string;
  status?: { state?: AgentStatus_ConnectionState };
}): boolean {
  return agent.status?.state === AgentStatus_ConnectionState.ONLINE;
}

// peerPresenceOnline resolves the green badge state for a DM peer's resource
// name ("users/<handle>" or "agents/<id>"). Returns undefined (render no
// badge) when there is no peer or — for a user peer — no heartbeat data yet;
// offline peers stay plain.
export function peerPresenceOnline(
  peer: string | undefined,
  isAgentPeer: boolean,
  agents: Parameters<typeof isAgentOnline>[0][],
  onlineUsers: Record<string, boolean>
): boolean | undefined {
  if (!peer) return undefined;
  if (isAgentPeer) {
    return agents.some((a) => a.name === peer && isAgentOnline(a));
  }
  return onlineUsers[peer] === true;
}
