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
import { createSettingSlice } from "./setting";
import { createThreadSlice } from "./thread";
import { createUserSlice } from "./user";
import { createImagePreviewSlice } from "./image-preview";
import { createPreviewSlice } from "./preview";

// ---------------------------------------------------------------------------
// Swipe-back preview: store freeze
//
// The mobile swipe-back gesture renders the back-target page underneath the
// current one (via useRoutes).  That preview instance mounts fresh and its
// useEffect calls fetch functions (fetchChannels, fetchMachines, …) which
// update the store.  The REAL page (still mounted as the parent route)
// subscribes to the same store and re-renders on every update — the user
// sees a "flash" right after the gesture commits.
//
// While the preview is active we freeze the store (all `set` calls become
// no-ops) so the preview instance's fetches cannot trigger a re-render of
// the real page.  The store is un-frozen shortly after the gesture ends.
// ---------------------------------------------------------------------------
let suppressLoadingFlags = false;

export function setSuppressLoadingFlags(value: boolean): void {
  suppressLoadingFlags = value;
}

export const useAppStore = create<AppStoreState>()((...args) => {
  const [originalSet, get] = args;
  // Wrap set so ALL store updates are suppressed while the swipe-back preview
  // is active.  The preview renders a separate component instance whose
  // useEffect calls fetch functions (fetchChannels, fetchMachines, …).  Those
  // fetches update the store (channels, machines, unreadByConv, …) which makes
  // the REAL page (still mounted as the parent route) re-render — the user sees
  // a "flash" right after the gesture commits.  By freezing the store during
  // the gesture (and briefly after, until the preview's in-flight fetch
  // completes), we prevent that re-render entirely.
  const set = ((partial: unknown, replace?: boolean) => {
    if (suppressLoadingFlags) return; // no-op: store is frozen
    originalSet(partial as never, replace as never);
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
    ...createSettingSlice(...wrappedArgs),
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
