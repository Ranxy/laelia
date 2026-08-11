import { create } from "zustand";
import type { AppStoreState } from "./types";
import { createAuthSlice } from "./auth";
import { createAPIProviderSlice } from "./api-provider";
import { createMcpServerSlice } from "./mcp";
import { createAgentSlice } from "./agent";
import { createMachineSlice } from "./machine";
import { createWorkspaceSlice } from "./workspace";
import { createMembersSlice } from "./members";
import { createCommandSlice } from "./command";
import { createChatSlice } from "./chat";
import { createChannelSlice } from "./channel";
import { createTaskSlice } from "./task";
import { createReminderSlice } from "./reminder";
import { createActivitySlice } from "./activity";
import { createThreadSlice } from "./thread";
import { createUserSlice } from "./user";
import { createImagePreviewSlice } from "./image-preview";
import { createPreviewSlice } from "./preview";

// ---------------------------------------------------------------------------
// Swipe-back preview: loading-flag suppression
//
// The mobile swipe-back gesture renders the back-target page underneath the
// current one (via useRoutes).  That preview instance mounts fresh and its
// useEffect calls fetch functions (fetchMachines, fetchChannels, …) which set
// loading flags (machinesLoading, channelsLoading, …) to true.  The REAL page
// (still mounted as the parent route) subscribes to those flags and briefly
// swaps its content to a "Loading…" state — the user sees a flash right after
// the gesture commits.
//
// While the preview is active we strip loading-flag keys from every store
// `set` call so the preview's fetch silently refreshes data without flipping
// the real page into a loading state.  Data keys (machines, channels, …) pass
// through unchanged.
// ---------------------------------------------------------------------------
const LOADING_FLAGS = new Set([
  "activitiesLoading",
  "agentChannelsLoading",
  "agentsLoading",
  "apiProvidersLoading",
  "channelMembersLoading",
  "channelsLoading",
  "chatLoading",
  "commandsLoading",
  "deletedUsersLoading",
  "machinesLoading",
  "mcpServersLoading",
  "membersLoading",
  "myChannelsLoading",
  "remindersLoading",
  "tasksLoading",
  "usersLoading",
]);

let suppressLoadingFlags = false;

export function setSuppressLoadingFlags(value: boolean): void {
  suppressLoadingFlags = value;
}

function stripLoadingFlags<T extends Record<string, unknown>>(
  partial: T
): T | null {
  if (!suppressLoadingFlags) return partial;
  const filtered: Record<string, unknown> = {};
  let hasData = false;
  for (const key in partial) {
    if (LOADING_FLAGS.has(key)) continue;
    filtered[key] = partial[key];
    hasData = true;
  }
  return hasData ? (filtered as T) : null;
}

export const useAppStore = create<AppStoreState>()((...args) => {
  const [originalSet, get] = args;
  // Wrap set so loading-flag updates are stripped while the swipe-back preview
  // is active.  Function-form updates are resolved first so the filtering
  // always sees a plain object.
  const set = ((partial: unknown, replace?: boolean) => {
    if (!suppressLoadingFlags) {
      originalSet(partial as never, replace as never);
      return;
    }
    if (typeof partial === "function") {
      originalSet((state: unknown) => {
        const resolved = (partial as (s: unknown) => unknown)(state);
        const filtered = stripLoadingFlags(resolved as Record<string, unknown>);
        return (filtered ?? {}) as never;
      }, replace as never);
      return;
    }
    const filtered = stripLoadingFlags(partial as Record<string, unknown>);
    if (filtered) originalSet(filtered as never, replace as never);
  }) as typeof originalSet;

  const wrappedArgs = [set, ...args.slice(1)] as typeof args;

  return {
    ...createAuthSlice(...wrappedArgs),
    ...createAPIProviderSlice(...wrappedArgs),
    ...createMcpServerSlice(...wrappedArgs),
    ...createAgentSlice(...wrappedArgs),
    ...createMachineSlice(...wrappedArgs),
    ...createWorkspaceSlice(...wrappedArgs),
    ...createMembersSlice(...wrappedArgs),
    ...createCommandSlice(...wrappedArgs),
    ...createChatSlice(...wrappedArgs),
    ...createChannelSlice(...wrappedArgs),
    ...createThreadSlice(...wrappedArgs),
    ...createTaskSlice(...wrappedArgs),
    ...createReminderSlice(...wrappedArgs),
    ...createActivitySlice(...wrappedArgs),
    ...createUserSlice(...wrappedArgs),
    ...createPreviewSlice(...wrappedArgs),
    ...createImagePreviewSlice(...wrappedArgs),
    reset: () => {
      // Stop every watcher interval before wiping state so orphaned timers can't
      // keep polling (and re-writing) the freshly reset store. getInitialState()
      // restores the pristine creation-time state (including the same action
      // closures, which are still bound to the live set/get).
      for (const w of Object.values(get().channelWatchers)) {
        w.ctrl.abort();
        clearInterval(w.badgeTimer);
      }
      for (const w of Object.values(get().threadWatchers)) w.ctrl.abort();
      set(useAppStore.getInitialState());
    },
  };
});
