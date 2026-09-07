import { useQuery } from "@tanstack/react-query";
import { presenceServiceClient } from "@/connect";
import { usePolling } from "@/hooks/use-polling";
import { queryClient } from "@/lib/query-client";
import { useAppStore } from "@/stores";
import { registerCleanup } from "@/stores/cleanup-registry";

// Heartbeat cadence. 30s beats against the manager's 90s presence window: two
// missed beats are tolerated, and a background tab throttled by the browser to
// one tick per minute still stays inside the window. Deeply backgrounded tabs
// may fall out and show offline until the tab is visible again — the read
// loop's focus refetch and the polling visibility handler both beat
// immediately on return.
const PRESENCE_POLL_INTERVAL_MS = 30000;

// Query cache key for the whole-workspace human presence map (ADR-1: the
// query key lives with the module that fetches it). The map is fetched
// wholesale — the server defines the set — so no read can couple to whatever
// a particular UI surface happens to have loaded.
export const PRESENCE_QUERY_KEY = ["presence"] as const;

// UserPresence is one human's last-known presence: the server-computed online
// flag plus the last heartbeat time (absent when the user has never been
// seen), which powers the offline "last seen" hints.
export type UserPresence = {
  online: boolean;
  lastSeenAt?: Date;
};

const EMPTY_PRESENCES: Record<string, UserPresence> = {};

const presenceQueryFn = async (): Promise<Record<string, UserPresence>> => {
  const res = await presenceServiceClient.listPresence({});
  // Whole-map replace: the server owns the set, so the previous cache never
  // survives a beat and stale entries cannot linger. (The old heartbeat-echo
  // design merged per-request responses in place, which let last-known state
  // from unloaded rosters stick around forever.)
  const next: Record<string, UserPresence> = {};
  for (const p of res.presences) {
    next[p.name] = {
      online: p.online,
      lastSeenAt: p.lastSeenAt?.seconds
        ? new Date(Number(p.lastSeenAt.seconds) * 1000)
        : undefined,
    };
  }
  return next;
};

// usePresenceHeartbeat keeps the signed-in user marked online: every beat
// sends one fire-and-forget heartbeat (identity comes from the auth context;
// a failed beat is silent, the next beat retries). Mounted at the dashboard
// layout, not the chat page: "online" means the user has laelia open, the
// standard chat-app semantic. Heartbeating only from the chat route would
// wrongly show a user offline while they browse other pages. It is a pure
// write loop — deliberately not a useQuery — so a failing read path can never
// stop heartbeats and vice versa.
//
// The same cadence refreshes the agent roster for the agent badges (agent
// presence is the authoritative AgentStatus.ConnectionState, not heartbeats).
export function usePresenceHeartbeat() {
  usePolling(
    () => {
      try {
        void presenceServiceClient.sendHeartbeat({}).catch(() => {});
      } catch {
        // A synchronous transport failure must not take down the layout —
        // presence is best-effort and the next beat retries.
      }
    },
    PRESENCE_POLL_INTERVAL_MS,
    { fireOnMount: true }
  );
  usePolling(() => {
    const state = useAppStore.getState();
    void state.fetchAgents({ pageSize: 100 }, { silent: true });
  }, PRESENCE_POLL_INTERVAL_MS);
}

// usePresenceMap reads the presence map the read loop maintains. Every
// mounted consumer shares one query instance; the refetchInterval is the
// cadence itself, and a consumer mounting while the cache is stale refetches
// immediately — new surfaces get fresh data without waiting a beat. Multiple
// observers only share the cache more eagerly; each read is a tiny
// whole-workspace SELECT.
export function usePresenceMap(): {
  presences: Record<string, UserPresence>;
  isPending: boolean;
} {
  const { data, isPending } = useQuery({
    queryKey: PRESENCE_QUERY_KEY,
    queryFn: presenceQueryFn,
    staleTime: PRESENCE_POLL_INTERVAL_MS,
    refetchInterval: PRESENCE_POLL_INTERVAL_MS,
    refetchIntervalInBackground: false,
    retry: false, // one attempt per beat — the next beat retries
  });
  return { presences: data ?? EMPTY_PRESENCES, isPending };
}

// useUserPresence resolves one user's presence with the three states the UI
// needs:
// - undefined: the map has not loaded yet (first fetch in flight) — surfaces
//   must render NO badge, because "no data yet" is not "offline";
// - { online: false }: the map is loaded and the user is absent from it
//   (never heartbeated) or last heartbeated beyond the TTL — offline;
// - { online: true, lastSeenAt? }: online.
export function useUserPresence(
  name: string | undefined
): UserPresence | undefined {
  const { presences, isPending } = usePresenceMap();
  if (isPending || !name) return undefined;
  return presences[name] ?? { online: false };
}

// Logout/reset drops the learned presence map: the cache would otherwise
// leak one account's online picture to the next user on the same tab.
registerCleanup(() => {
  queryClient.removeQueries({ queryKey: PRESENCE_QUERY_KEY });
});
