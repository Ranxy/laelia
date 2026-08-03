import { create } from "zustand";
import type { AppStoreState } from "./types";
import { createAuthSlice } from "./auth";
import { createApiProviderSlice } from "./api-provider";
import { createAgentSlice } from "./agent";
import { createMachineSlice } from "./machine";
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

export const useAppStore = create<AppStoreState>()((...args) => {
  const [set, get] = args;
  return {
    ...createAuthSlice(...args),
    ...createApiProviderSlice(...args),
    ...createAgentSlice(...args),
    ...createMachineSlice(...args),
    ...createMembersSlice(...args),
    ...createCommandSlice(...args),
    ...createChatSlice(...args),
    ...createChannelSlice(...args),
    ...createThreadSlice(...args),
    ...createTaskSlice(...args),
    ...createReminderSlice(...args),
    ...createActivitySlice(...args),
    ...createUserSlice(...args),
    ...createPreviewSlice(...args),
    ...createImagePreviewSlice(...args),
    reset: () => {
      // Stop every watcher interval before wiping state so orphaned timers can't
      // keep polling (and re-writing) the freshly reset store. getInitialState()
      // restores the pristine creation-time state (including the same action
      // closures, which are still bound to the live set/get).
      for (const id of Object.values(get().channelWatchers)) clearInterval(id);
      for (const id of Object.values(get().threadWatchers)) clearInterval(id);
      set(useAppStore.getInitialState());
    },
  };
});
