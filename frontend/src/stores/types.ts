import type { StateCreator } from "zustand";
import type { ActivitySlice } from "./activity";
import type { AgentSlice } from "./agent";
import type { ApiProviderSlice } from "./api-provider";
import type { AuthSlice } from "./auth";
import type { ChannelSlice } from "./channel";
import type { ChatSlice } from "./chat";
import type { CommandSlice } from "./command";
import type { ImagePreviewSlice } from "./image-preview";
import type { MachineSlice } from "./machine";
import type { McpServerSlice } from "./mcp";
import type { MembersSlice } from "./members";
import type { PresenceSlice } from "./presence";
import type { PreviewSlice } from "./preview";
import type { ReminderSlice } from "./reminder";
import type { SettingSlice } from "./setting";
import type { TaskSlice } from "./task";
import type { ThreadSlice } from "./thread";
import type { UserSlice } from "./user";
import type { WorkspaceSlice } from "./workspace";

// Store composition (audit 05 §1.1 types.ts split): every slice
// interface lives next to its implementation (e.g. ChannelSlice in
// ./channel) and shared UI-facing models in ./ui-models. This file
// keeps only the store shape contract:
export type AppStoreState = AuthSlice &
  ApiProviderSlice &
  McpServerSlice &
  AgentSlice &
  MachineSlice &
  WorkspaceSlice &
  MembersSlice &
  CommandSlice &
  ChatSlice &
  ChannelSlice &
  ThreadSlice &
  TaskSlice &
  ReminderSlice &
  ActivitySlice &
  UserSlice &
  SettingSlice &
  PreviewSlice &
  ImagePreviewSlice &
  PresenceSlice & {
    // reset restores every slice to its pristine initial state (clearing
    // watcher intervals first) so a logout can never leak one principal's
    // cached data to the next user signing in on the same tab.
    reset: () => void;
  };

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
