import { create } from "zustand";
import type { AppSliceCreator, AppStoreState } from "./types";
import { createAuthSlice } from "./auth";
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

// sliceInitialState extracts only the initial state fields (non-action values)
// from a slice creator, for the store-wide reset used on logout. Slice creators
// only reference set/get inside actions — never at creation time — so no-op
// arguments are safe here.
function sliceInitialState<Slice>(
  creator: AppSliceCreator<Slice>
): Partial<Slice> {
  const run = creator as unknown as (
    set: (partial: unknown) => void,
    get: () => unknown
  ) => Record<string, unknown>;
  const slice = run(
    () => {},
    () => ({})
  );
  const state: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(slice)) {
    if (typeof value !== "function") state[key] = value;
  }
  return state as Partial<Slice>;
}

// The pristine initial state of every slice, captured once at module load so
// reset() can restore it cheaply.
const initialState: Partial<AppStoreState> = {
  ...sliceInitialState(createAuthSlice),
  ...sliceInitialState(createAgentSlice),
  ...sliceInitialState(createMachineSlice),
  ...sliceInitialState(createMembersSlice),
  ...sliceInitialState(createCommandSlice),
  ...sliceInitialState(createChatSlice),
  ...sliceInitialState(createChannelSlice),
  ...sliceInitialState(createThreadSlice),
  ...sliceInitialState(createTaskSlice),
  ...sliceInitialState(createReminderSlice),
  ...sliceInitialState(createActivitySlice),
  ...sliceInitialState(createUserSlice),
  ...sliceInitialState(createPreviewSlice),
  ...sliceInitialState(createImagePreviewSlice),
};

export const useAppStore = create<AppStoreState>()((...args) => {
  const [set, get] = args;
  return {
    ...createAuthSlice(...args),
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
      // keep polling (and re-writing) the freshly reset store.
      for (const id of Object.values(get().channelWatchers)) clearInterval(id);
      for (const id of Object.values(get().threadWatchers)) clearInterval(id);
      set(initialState);
    },
  };
});
