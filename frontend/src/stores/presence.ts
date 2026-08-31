import { commandServiceClient } from "@/connect";
import type { AppSliceCreator } from "./types";

// PresenceSlice owns the chat page's human online state, keyed by the user's
// resource name ("users/<handle>"). Populated by the 30s SyncPresence
// heartbeat tick in ChatLayout; agents are not tracked here (their online
// signal is the agents slice's connection state).
export interface PresenceSlice {
  onlineUsers: Record<string, boolean>;
  // syncPresence records the signed-in user's heartbeat and refreshes the
  // online state of the given DM peers. Non-"users/" names are ignored.
  syncPresence: (names: string[]) => Promise<void>;
}

// maxSyncNames mirrors the server's per-request cap on SyncPresence names.
const MAX_SYNC_NAMES = 200;

// PresenceSlice owns the human online state shown as the chat page's green
// badge. The web frontend heartbeats via SyncPresence every 30s (the
// use-presence-heartbeat tick mounted at the dashboard layout): the call both
// records the signed-in user's own presence and returns the online state of
// the requested DM peers. Agents are deliberately not tracked here — their
// online signal is the authoritative AgentStatus.ConnectionState on the
// agents slice, refreshed by the same tick's silent fetchAgents.
export const createPresenceSlice: AppSliceCreator<PresenceSlice> = (
  set,
  get
) => ({
  onlineUsers: {},

  async syncPresence(names) {
    // Only human peers ("users/<handle>") are tracked; agent peers are
    // answered from the agents slice. The RPC itself still fires with an
    // empty list — the server records the caller's own heartbeat from the
    // auth context, so this call is what keeps the signed-in user online.
    const wanted = [...new Set(names.filter((n) => n.startsWith("users/")))];
    if (wanted.length > MAX_SYNC_NAMES) wanted.length = MAX_SYNC_NAMES;

    try {
      const res = await commandServiceClient.syncPresence({ names: wanted });
      const current = get().onlineUsers;
      // Merge into a fresh map; skip the state write entirely when nothing
      // changed, so a steady-state poll causes no re-render at all.
      const next = { ...current };
      let changed = false;
      for (const name of wanted) {
        const online = res.presences.some((p) => p.name === name && p.online);
        if (current[name] !== online) changed = true;
        next[name] = online;
      }
      if (changed) set({ onlineUsers: next });
    } catch {
      // A failed sync keeps the last known state; the next tick retries.
    }
  },
});
