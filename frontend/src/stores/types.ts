import type { StoreApi } from "zustand";
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

// ---- Cross-slice write grants ---------------------------------------------
// The slices share one flat store, so nothing in `set`'s shape stops a slice
// from writing another slice's fields. The creator type below flips that
// default: a slice's `set` only accepts its own interface fields plus the
// cross-slice grant named in its creator type. The four grants here are the
// complete inventory of cross-slice writes; a new one must be added to this
// register first, with a reason (audit 05 §1.1 ②).

// channel.ts's message watcher merges poll deltas and jump-window sentinels
// into the shared chat message maps (DM and channel messages live in
// ChatSlice's maps, keyed by conversation name).
export type ChannelCross = Pick<
  ChatSlice,
  "chatMessages" | "chatCurrentVersion" | "chatHasNewerByConv"
>;

// thread.ts patches reply-count freshness onto root messages in the main
// channel list (the main watcher polls messages only, never thread replies).
export type ThreadCross = Pick<ChatSlice, "chatMessages">;

// task.ts's task mutations patch the authoritative task message onto the
// open thread's snapshot in ThreadSlice.
export type TaskCross = Pick<ThreadSlice, "threadByRoot">;

// members.ts derives its directory from the user + agent rosters and writes
// the fully-drained rosters back so every roster consumer sees all pages.
export type MembersCross = Pick<UserSlice, "users"> &
  Pick<AgentSlice, "agents">;

// SliceSet is the mutator handed to one slice: reads stay full-store (get
// returns AppStoreState) while writes are constrained to the slice's own
// fields plus its declared cross-slice grant. The overloads (not a union)
// keep object-literal argument checking; a direct `set({ ... })` with a
// field outside the grant fails the type-check. Caveat: TypeScript does not
// excess-property-check object literals returned from arrow updaters, so
// `set((state) => ({ ...foreignField }))` can only be caught by review —
// the grant register above is the checkable source of truth.
export interface SliceSet<Slice, Cross extends object = object> {
  (partial: Partial<Slice & Cross>): void;
  (partial: (state: AppStoreState) => Partial<Slice & Cross>): void;
}

// Mirrors zustand's StateCreator<AppStoreState, [], [], Slice> but narrows
// `set` through SliceSet (see the grant register above).
export type AppSliceCreator<Slice, Cross extends object = object> = (
  set: SliceSet<Slice, Cross>,
  get: () => AppStoreState,
  store: StoreApi<AppStoreState>
) => Slice;
