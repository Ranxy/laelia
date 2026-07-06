import { create } from "zustand";
import type { AppStoreState } from "./types";
import { createAuthSlice } from "./auth";
import { createAgentSlice } from "./agent";
import { createCommandSlice } from "./command";
import { createChatSlice } from "./chat";
import { createChannelSlice } from "./channel";
import { createTaskSlice } from "./task";
import { createReminderSlice } from "./reminder";
import { createThreadSlice } from "./thread";
import { createUserSlice } from "./user";
import { createImagePreviewSlice } from "./image-preview";
import { createPreviewSlice } from "./preview";

export const useAppStore = create<AppStoreState>()((...args) => ({
  ...createAuthSlice(...args),
  ...createAgentSlice(...args),
  ...createCommandSlice(...args),
  ...createChatSlice(...args),
  ...createChannelSlice(...args),
  ...createThreadSlice(...args),
  ...createTaskSlice(...args),
  ...createReminderSlice(...args),
  ...createUserSlice(...args),
  ...createPreviewSlice(...args),
  ...createImagePreviewSlice(...args),
}));
