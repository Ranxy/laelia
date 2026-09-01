import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import type {
  Command,
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import {
  CancelCommandRequestSchema,
  SteerCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { registerCleanup } from "./cleanup-registry";
import { sleep } from "./delay";
import type { AppSliceCreator } from "./types";

export interface CommandSlice {
  activeOutputs: Record<string, CommandOutput[]>;
  activeEvents: Record<string, CommandEvent[]>;

  cancelCommand: (name: string) => Promise<Command>;
  // steerCommand injects a follow-up message into the in-flight turn of a
  // running command. Best-effort: executors without mid-turn steering ignore
  // it. Throws when the command is not running or the agent is unreachable.
  steerCommand: (name: string, text: string) => Promise<Command>;
  getCommand: (name: string) => Promise<Command | undefined>;
  // watchCommand/watchCommandEvents resolve with true when the server closed
  // the stream normally (e.g. the command finished), false when the stream was
  // aborted by the caller or failed with an error.
  watchCommand: (name: string, signal?: AbortSignal) => Promise<boolean>;
  watchCommandEvents: (name: string, signal?: AbortSignal) => Promise<boolean>;
  // releaseCommand drops the cached output/events for one command so a long
  // session cannot accumulate every visited command's full stdout in memory.
  // Command detail pages call it on unmount; the LRU cap in the slice is the
  // wider safety net.
  releaseCommand: (name: string) => void;
}

// Cached command runtime data (outputs + events) is bounded: a full stdout
// stream can be several MB, and without a cap a long session accumulates
// every visited command's data until logout. Detail pages release on unmount;
// this LRU cap is the safety net for pages left open or rapid navigation.
const MAX_TRACKED_COMMANDS = 8;

// Watch reconnect policy. Unlike the channel conversation polls, whose cadence
// the server paces with wait_ms long-poll holds (see channel.ts WATCHER_*),
// the command watch RPCs carry no wait_ms: the manager holds each stream open
// for as long as the command runs, so the stream ends only when the command
// finishes (clean close — nothing to reconnect) or when the transport dies
// (network blip, proxy reset). On a transport death the watch re-opens its own
// stream after exponential backoff of 1s→2s→4s→8s (base 1s mirrors
// WATCHER_RETRY_DELAY_MS in channel.ts, capped at 8s), resuming from the last
// seqNo still cached in the store; after RECONNECT_MAX_ATTEMPTS failed
// reconnects it gives up and resolves false. The command-detail caller keeps
// that failure silent; reloading the page reopens the watch.
const RECONNECT_MAX_ATTEMPTS = 5;
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 8000;

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
  get: Parameters<typeof createCommandSlice>[1],
  set: Parameters<typeof createCommandSlice>[0]
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

export const createCommandSlice: AppSliceCreator<CommandSlice> = (set, get) => {
  // Unified release (audit 05 B7): the cleanup registry fires this on every
  // reset()/logout(). commandRecency lives at module scope where the store
  // wipe cannot reach it; the two tracked maps are cleared through this
  // slice's own set so the release stays slice-encapsulated.
  registerCleanup(() => {
    commandRecency = [];
    set({ activeOutputs: {}, activeEvents: {} });
  });
  return {
    activeOutputs: {},
    activeEvents: {},

    async cancelCommand(name) {
      return commandServiceClient.cancelCommand(
        create(CancelCommandRequestSchema, { name })
      );
    },

    async steerCommand(name, text) {
      return commandServiceClient.steerCommand(
        create(SteerCommandRequestSchema, { name, text })
      );
    },

    async getCommand(name) {
      const res = await commandServiceClient.getCommand({ name });
      return res;
    },

    async watchCommand(name, signal) {
      // sleep() requires a signal; when the caller passed none there is nothing
      // to abort on, so hand it one that never fires.
      const neverAbort = new AbortController().signal;

      // Self-scheduling reconnect loop: a clean stream end means the server
      // finished the command (resolve, no reconnect); a transport error backs
      // off and re-opens the stream. afterSeqNo is re-read from get() at the
      // top of every pass, so a reconnect resumes right after the last chunk
      // already in the store — no replay, no gap. Chunks cached during the
      // disconnect stay in place; the reconnect only appends.
      for (let attempts = 0; ; attempts++) {
        if (signal?.aborted) return false;
        bumpCommandRecency(name);
        const existing = get().activeOutputs[name];
        const afterSeqNo =
          existing && existing.length > 0
            ? existing[existing.length - 1].seqNo
            : -1;

        try {
          const stream = commandServiceClient.watchCommand(
            { name, afterSeqNo },
            { signal }
          );
          for await (const output of stream) {
            if (signal?.aborted) return false;
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
          // Clean close: the server ended the stream (command finished).
          return !signal?.aborted;
        } catch {
          if (signal?.aborted) return false;
          if (attempts >= RECONNECT_MAX_ATTEMPTS) return false;
          await sleep(
            Math.min(RECONNECT_BASE_MS * 2 ** attempts, RECONNECT_MAX_MS),
            signal ?? neverAbort
          );
        }
      }
    },

    async watchCommandEvents(name, signal) {
      const neverAbort = new AbortController().signal;

      // Same self-scheduling reconnect loop as watchCommand (see there).
      for (let attempts = 0; ; attempts++) {
        if (signal?.aborted) return false;
        bumpCommandRecency(name);
        const existing = get().activeEvents[name];
        const afterSeqNo =
          existing && existing.length > 0
            ? existing[existing.length - 1].seqNo
            : -1;

        try {
          const stream = commandServiceClient.watchCommandEvents(
            { name, afterSeqNo },
            { signal }
          );
          for await (const event of stream) {
            if (signal?.aborted) return false;
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
          // Clean close: the server ended the stream (command finished).
          return !signal?.aborted;
        } catch {
          if (signal?.aborted) return false;
          if (attempts >= RECONNECT_MAX_ATTEMPTS) return false;
          await sleep(
            Math.min(RECONNECT_BASE_MS * 2 ** attempts, RECONNECT_MAX_MS),
            signal ?? neverAbort
          );
        }
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
  };
};
