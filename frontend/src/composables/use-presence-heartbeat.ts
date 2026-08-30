import { useCallback, useEffect } from "react";
import { usePolling } from "@/lib/use-polling";
import { useAppStore } from "@/stores";

// Heartbeat cadence. 30s beats against the manager's 90s presence window: two
// missed beats are tolerated, and a background tab throttled by the browser to
// one tick per minute still stays inside the window. Deeply backgrounded tabs
// (Chrome intensive throttling) may fall out and show offline until the tab is
// visible again — the visibilitychange listener syncs immediately on return.
const PRESENCE_POLL_INTERVAL_MS = 30000;

// usePresenceHeartbeat keeps the signed-in user marked online and refreshes
// the online state the app renders: every tick sends one SyncPresence
// (server records the caller's own heartbeat; the request also queries every
// loaded human) and silently refreshes the agent roster so
// AgentStatus.ConnectionState stays current for the agent badges.
//
// The queried set is every human the UI can currently show presence for:
// left-rail DM peers, the loaded user roster (members/settings pages), and
// the user members of loaded channel rosters (chat members sheet, channel
// detail). Unloaded rosters simply aren't queried — their surfaces show no
// badge data until the next tick after they load.
//
// Mounted at the dashboard layout, not the chat page: "online" means the user
// has laelia open, the standard chat-app semantic. Heartbeating only from the
// chat route would wrongly show a user offline while they browse other pages.
// The badge UI itself stays in the feature components; they just read the store.
export function usePresenceHeartbeat() {
  const tick = useCallback(() => {
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
    void state.syncPresence([...names]);
    // Same cadence refreshes agent connection state for the agent badges.
    void state.fetchAgents({ pageSize: 100 }, { silent: true });
  }, []);
  // Immediate beat on mount so a freshly loaded page is online right away;
  // the shared primitive drives the cadence. Visibility-gated: a background
  // tab stops heartbeating, and returning to the foreground beats
  // immediately.
  useEffect(() => {
    tick();
  }, [tick]);
  usePolling(tick, PRESENCE_POLL_INTERVAL_MS);
}
