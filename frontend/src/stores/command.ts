import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  CancelCommandRequestSchema,
  SteerCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, CommandSlice } from "./types";

// Cached command runtime data (outputs + events) is bounded: a full stdout
// stream can be several MB, and without a cap a long session accumulates
// every visited command's data until logout. Detail pages release on unmount;
// this LRU cap is the safety net for pages left open or rapid navigation.
const MAX_TRACKED_COMMANDS = 8;

// Recency order (most recent first) of tracked commands. Bumped on watch
// start and on every chunk, so a streaming command is never the eviction
// candidate while it is producing output.
let commandRecency: string[] = [];

function bumpCommandRecency(name: string): void {
  commandRecency = [name, ...commandRecency.filter((k) => k !== name)];
}

// pruneCommandCaches drops the least-recently-used tracked commands (never
// the one currently streaming) when the tracked count exceeds the cap. It
// reads fresh state via get() and writes back through set only when something
// was actually dropped, so chunks are appended with a single set as before.
function pruneCommandCaches(
  protect: string,
  get: Parameters<AppSliceCreator<CommandSlice>>[1],
  set: Parameters<AppSliceCreator<CommandSlice>>[0]
): void {
  if (commandRecency.length <= MAX_TRACKED_COMMANDS) return;
  // The currently streaming command is always kept; the rest fall out once
  // they drop past MAX_TRACKED_COMMANDS in recency.
  const keep = new Set<string>(commandRecency.slice(0, MAX_TRACKED_COMMANDS));
  keep.add(protect);
  const s = get();
  const outputs = { ...s.activeOutputs };
  const events = { ...s.activeEvents };
  let removed = false;
  for (const key of Object.keys(outputs)) {
    if (!keep.has(key)) {
      delete outputs[key];
      removed = true;
    }
  }
  for (const key of Object.keys(events)) {
    if (!keep.has(key)) {
      delete events[key];
      removed = true;
    }
  }
  if (removed) {
    set({ activeOutputs: outputs, activeEvents: events });
  }
}

export const createCommandSlice: AppSliceCreator<CommandSlice> = (
  set,
  get
) => ({
  commands: [],
  commandsLoading: false,
  activeOutputs: {},
  activeEvents: {},

  async cancelCommand(name) {
    const res = await commandServiceClient.cancelCommand(
      create(CancelCommandRequestSchema, { name })
    );
    set((state) => ({
      commands: state.commands.map((c) => (c.name === name ? res : c)),
    }));
    return res;
  },

  async steerCommand(name, text) {
    const res = await commandServiceClient.steerCommand(
      create(SteerCommandRequestSchema, { name, text })
    );
    set((state) => ({
      commands: state.commands.map((c) => (c.name === name ? res : c)),
    }));
    return res;
  },

  async listCommands(agent, params) {
    set({ commandsLoading: true });
    try {
      const res = await commandServiceClient.listCommands({
        agent,
        pageSize: params?.pageSize ?? 50,
        pageToken: params?.pageToken ?? "",
        status: params?.status ?? 0,
      });
      set({ commands: res.commands, commandsLoading: false });
      return { commands: res.commands, nextPageToken: res.nextPageToken };
    } catch {
      set({ commands: [], commandsLoading: false });
      return undefined;
    }
  },

  async getCommand(name) {
    const res = await commandServiceClient.getCommand({ name });
    return res;
  },

  async watchCommand(name, signal) {
    const state = get();
    const existing = state.activeOutputs[name];
    const afterSeqNo =
      existing && existing.length > 0
        ? existing[existing.length - 1].seqNo
        : -1;
    bumpCommandRecency(name);

    const stream = commandServiceClient.watchCommand(
      { name, afterSeqNo },
      { signal }
    );

    try {
      for await (const output of stream) {
        if (signal?.aborted) break;
        bumpCommandRecency(name);
        const s = get();
        const prev = s.activeOutputs[name] ?? [];
        set({
          activeOutputs: {
            ...s.activeOutputs,
            [name]: [...prev, output],
          },
        });
        pruneCommandCaches(name, get, set);
      }
      return !signal?.aborted;
    } catch {
      // stream cancelled or network error
      return false;
    }
  },

  async watchCommandEvents(name, signal) {
    const state = get();
    const existing = state.activeEvents[name];
    const afterSeqNo =
      existing && existing.length > 0
        ? existing[existing.length - 1].seqNo
        : -1;
    bumpCommandRecency(name);

    const stream = commandServiceClient.watchCommandEvents(
      { name, afterSeqNo },
      { signal }
    );

    try {
      for await (const event of stream) {
        if (signal?.aborted) break;
        bumpCommandRecency(name);
        const s = get();
        const prev = s.activeEvents[name] ?? [];
        set({
          activeEvents: {
            ...s.activeEvents,
            [name]: [...prev, event],
          },
        });
        pruneCommandCaches(name, get, set);
      }
      return !signal?.aborted;
    } catch {
      // stream cancelled or network error
      return false;
    }
  },

  releaseCommand(name) {
    commandRecency = commandRecency.filter((k) => k !== name);
    const s = get();
    const outputs = { ...s.activeOutputs };
    const events = { ...s.activeEvents };
    if (!(name in outputs) && !(name in events)) return;
    delete outputs[name];
    delete events[name];
    set({ activeOutputs: outputs, activeEvents: events });
  },
});
