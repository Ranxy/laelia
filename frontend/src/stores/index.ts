import { create } from "zustand";
import type { AppStoreState } from "./types";
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

export const useAppStore = create<AppStoreState>()((...args) => ({
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
}));
