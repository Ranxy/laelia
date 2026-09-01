import { agentTeamServiceClient } from "@/connect";
import { useResourceQuery } from "@/hooks/use-resource-query";
import type { AgentTeam } from "@/types/proto-es/v1/agent_team_service_pb";

// ---------------------------------------------------------------------------
// useAgentTeamsQuery — the agent-teams list read (09 章 §2.2 收敛).
//
// The full list (pageSize 1000, the agent-team directory is small) was fetched
// independently by ThreadTaskControls' assignee dropdown, the AgentTeamsManager
// card and TeamDetailPage's cross-team availability map — three uncached
// copies of the same RPC. One shared ["agent-teams"] cache entry now serves
// all three; mutations invalidate by key (AGENT_TEAMS_QUERY_KEY).
//
// Read primitive is useResourceQuery, so the family semantics apply: one
// failure toast per failure episode with the caller's failureTitle, and
// explicit reload() after mutations.
// ---------------------------------------------------------------------------

export const AGENT_TEAMS_QUERY_KEY = ["agent-teams"] as const;

// Directory-sized page: the list feeds cross-team membership maps, so it must
// not truncate (parity with the pre-convergence per-page fetches).
const PAGE_SIZE = 1000;

// Teams change at human/editing pace; a shared window keeps the
// chat ↔ members hops cache-hit without hiding edits (mutations reload).
const STALE_TIME = 30_000;

const EMPTY_TEAMS: never[] = [];

export interface UseAgentTeamsQueryOptions {
  // false keeps the read idle (the manager card gates on the list permission).
  enabled?: boolean;
  // Already-translated failure title, toasted once per failure episode.
  failureTitle: string;
}

export function useAgentTeamsQuery(
  opts: UseAgentTeamsQueryOptions
): ReturnType<typeof useResourceQuery<AgentTeam>> {
  const { enabled = true, failureTitle } = opts;
  return useResourceQuery<AgentTeam>({
    enabled,
    queryKey: AGENT_TEAMS_QUERY_KEY,
    queryFn: async () =>
      (await agentTeamServiceClient.listAgentTeams({ pageSize: PAGE_SIZE }))
        .agentTeams ?? EMPTY_TEAMS,
    failureTitle,
    staleTime: STALE_TIME,
  });
}
