import { useQuery } from "@tanstack/react-query";
import { commandServiceClient } from "@/connect";
import { usePolling } from "@/hooks/use-polling";
import { queryClient } from "@/lib/query-client";
import { useAppStore } from "@/stores";
import { registerCleanup } from "@/stores/cleanup-registry";

// Heartbeat cadence. 30s beats against the manager's 90s presence window: two
// missed beats are tolerated, and a background tab throttled by the browser to
// one tick per minute still stays inside the window. Deeply backgrounded tabs
// (Chrome intensive throttling) may fall out and show offline until the tab is
// visible again — the query's focus refetch beats immediately on return.
const PRESENCE_POLL_INTERVAL_MS = 30000;

// Query cache key for the human online map (ADR-1: the query key lives with
// the module that fetches it). THE HEARTBEAT IS THE SOLE FETCHER for this key
// — one SyncPresence per beat, mounted at the dashboard layout. Consumers read
// the cache through useOnlineUsers() (enabled: false), which never triggers an
// RPC of its own.
export const PRESENCES_QUERY_KEY = ["presences"] as const;

// maxSyncNames mirrors the server's per-request cap on SyncPresence names.
const MAX_SYNC_NAMES = 200;

// Only human peers ("users/<handle>") are tracked; agent peers are answered
// from the agents slice, whose ConnectionState is refreshed by the same tick's
// silent fetchAgents.
function collectWantedNames(): string[] {
  const state = useAppStore.getState();
  const names = new Set<string>();
  for (const c of state.channels) {
    if (c.peer) names.add(c.peer);
  }
  for (const u of state.users) {
    if (u.name) names.add(u.name);
  }
  for (const roster of Object.values(state.channelMembersByConv)) {
    for (const m of roster) {
      if (m.memberType === 1 && m.memberId) {
        names.add(`users/${m.memberId}`);
      }
    }
  }
  const wanted = [...names].filter((n) => n.startsWith("users/"));
  if (wanted.length > MAX_SYNC_NAMES) wanted.length = MAX_SYNC_NAMES;
  return wanted;
}

// usePresenceHeartbeat keeps the signed-in user marked online and feeds the
// online state the app renders: every beat sends one SyncPresence (the server
// records the caller's own heartbeat even with an empty query list; the
// request also queries every loaded human) and merges the returned presence
// states into the ["presences"] cache. The server records the heartbeat from
// the auth context, so the call itself is what keeps the signed-in user
// online.
//
// The queried set is every human the UI can currently show presence for:
// left-rail DM peers, the loaded user roster (members/settings pages), and
// the user members of loaded channel rosters (chat members sheet, channel
// detail). Unloaded rosters simply aren't queried — their surfaces show no
// badge data until the next beat after they load.
//
// Mounted at the dashboard layout, not the chat page: "online" means the user
// has laelia open, the standard chat-app semantic. Heartbeating only from the
// chat route would wrongly show a user offline while they browse other pages.
// The badge UI itself stays in the feature components; they read the map
// through useOnlineUsers().
export function usePresenceHeartbeat() {
  useQuery({
    queryKey: PRESENCES_QUERY_KEY,
    staleTime: 0, // freshness is the cadence itself; every focus beat matters
    retry: false, // one attempt per beat — the next beat retries
    refetchInterval: PRESENCE_POLL_INTERVAL_MS,
    // Visibility gating: a background tab stops heartbeating instead of
    // tearing down the timer, and returning to the foreground beats
    // immediately (Query's focus manager listens to visibilitychange, and
    // staleTime 0 makes every visibility beat eligible).
    refetchIntervalInBackground: false,
    queryFn: async () => {
      // The RPC fires even with an empty list — the server records the
      // signed-in user's own heartbeat from the auth context.
      const wanted = collectWantedNames();
      const res = await commandServiceClient.syncPresence({ names: wanted });
      // Merge over the previously known map so peers learned from rosters
      // that have since unloaded keep their last known state (the old slice
      // merged in place; reset clears via the cleanup registration below).
      const prev =
        queryClient.getQueryData<Record<string, boolean>>(
          PRESENCES_QUERY_KEY
        ) ?? EMPTY_PRESENCES;
      const next: Record<string, boolean> = { ...prev };
      for (const p of res.presences) next[p.name] = p.online;
      return next;
    },
  });
  // Same cadence refreshes agent connection state for the agent badges.
  usePolling(() => {
    const state = useAppStore.getState();
    void state.fetchAgents({ pageSize: 100 }, { silent: true });
  }, PRESENCE_POLL_INTERVAL_MS);
}

const EMPTY_PRESENCES: Record<string, boolean> = {};

// useOnlineUsers reads the online map the heartbeat maintains. Consumers that
// render presence badges (conversation list, members panels, chat headers)
// subscribe to the cache here without ever fetching: the dashboard layout's
// heartbeat is the only writer. Structural sharing keeps the returned map
// identity stable across beats that learn nothing new, so unchanged surfaces
// don't re-render.
export function useOnlineUsers(): Record<string, boolean> {
  const { data } = useQuery<Record<string, boolean>>({
    queryKey: PRESENCES_QUERY_KEY,
    enabled: false, // cache reader — the heartbeat hook owns the cadence
  });
  return data ?? EMPTY_PRESENCES;
}

// Logout/reset drops every learned presence: the cache would otherwise leak
// one account's online picture to the next user on the same tab (the store
// reset wiped this field before it moved into Query).
registerCleanup(() => {
  queryClient.removeQueries({ queryKey: PRESENCES_QUERY_KEY });
});
