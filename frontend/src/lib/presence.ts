import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

// Presence derivation shared by the chat left-rail rows and the chat header
// badge. Agent online state is the authoritative AgentStatus.ConnectionState
// from the agent roster (only ONLINE counts; STOPPED/OFFLINE/ERROR/KICKED do
// not); human presence comes from the presence map the read loop maintains
// (useUserPresence / usePresenceMap in hooks/use-presence.ts), keyed by the
// "users/<handle>" resource name.
export function isAgentOnline(agent: {
  name: string;
  status?: { state?: AgentStatus_ConnectionState };
}): boolean {
  return agent.status?.state === AgentStatus_ConnectionState.ONLINE;
}

// agentPeerOnline answers whether a DM peer that is an agent ("agents/<id>")
// is online per the agent roster's connection state.
export function agentPeerOnline(
  peer: string | undefined,
  agents: Parameters<typeof isAgentOnline>[0][]
): boolean {
  if (!peer) return false;
  return agents.some((a) => a.name === peer && isAgentOnline(a));
}

// Translate is the slice of the i18n translator the helper needs, kept loose
// so the lib module stays framework-neutral.
type Translate = (key: string, opts?: Record<string, unknown>) => string;

// formatLastSeen renders the offline "last seen" hint for a user's last
// heartbeat: relative wording inside a week, a locale date beyond it. The
// compact unit style ("5m ago" / "5 分钟前") keeps every phrase number-agnostic,
// so a single catalog form per locale carries all counts (same convention as
// roles.permission-count).
export function formatLastSeen(
  lastSeenAt: Date,
  t: Translate,
  now: Date = new Date()
): string {
  const minutes = Math.floor((now.getTime() - lastSeenAt.getTime()) / 60000);
  if (minutes < 1) return t("chat.presence-last-just-now");
  if (minutes < 60) return t("chat.presence-last-minutes", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("chat.presence-last-hours", { count: hours });
  const days = Math.floor(hours / 24);
  if (days < 7) return t("chat.presence-last-days", { count: days });
  return t("chat.presence-last-date", {
    time: lastSeenAt.toLocaleDateString(),
  });
}
