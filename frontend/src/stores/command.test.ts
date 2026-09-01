import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

type ScriptChunk = { commandId: string; seqNo: number };
// One scripted stream invocation (used by the reconnect tests): yields
// `chunks` in order, then throws when `fail` is set or closes cleanly
// otherwise. The script lists are consumed one entry per stream call; with
// the list empty (or exhausted) the mock falls back to the plain
// watchOutputs/watchEvents arrays, matching the original mock shape.
type StreamScript = { chunks: ScriptChunk[]; fail: boolean };

// --- mock @/connect so the store talks to controllable watch streams ---
const mock = vi.hoisted(() => ({
  watchOutputs: [] as Array<{ commandId: string; seqNo: number }>,
  watchEvents: [] as Array<{ commandId: string; seqNo: number }>,
  outputScript: [] as StreamScript[],
  eventScript: [] as StreamScript[],
  outputCalls: [] as Array<{ afterSeqNo: number }>,
  eventCalls: [] as Array<{ afterSeqNo: number }>,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    async *watchCommand(input: { afterSeqNo: number }) {
      mock.outputCalls.push({ afterSeqNo: input?.afterSeqNo ?? -1 });
      const script = mock.outputScript.shift();
      if (!script) {
        for (const output of mock.watchOutputs) yield output;
        return;
      }
      for (const output of script.chunks) yield output;
      if (script.fail) throw new Error("watch stream reset");
    },
    async *watchCommandEvents(input: { afterSeqNo: number }) {
      mock.eventCalls.push({ afterSeqNo: input?.afterSeqNo ?? -1 });
      const script = mock.eventScript.shift();
      if (!script) {
        for (const event of mock.watchEvents) yield event;
        return;
      }
      for (const event of script.chunks) yield event;
      if (script.fail) throw new Error("watch stream reset");
    },
  },
}));

const NAME = "agents/a/commands/c";

beforeEach(() => {
  useAppStore.setState({
    activeOutputs: {},
    activeEvents: {},
  });
  mock.watchOutputs = [];
  mock.watchEvents = [];
  mock.outputScript = [];
  mock.eventScript = [];
  mock.outputCalls = [];
  mock.eventCalls = [];
});

describe("command watch streams", () => {
  it("resolves true when the output stream ends normally", async () => {
    mock.watchOutputs = [
      { commandId: "c", seqNo: 1 },
      { commandId: "c", seqNo: 2 },
    ];

    await expect(useAppStore.getState().watchCommand(NAME)).resolves.toBe(true);
    expect(useAppStore.getState().activeOutputs[NAME]).toHaveLength(2);
  });

  it("resolves false when the output stream is aborted", async () => {
    mock.watchOutputs = [{ commandId: "c", seqNo: 1 }];
    const controller = new AbortController();
    controller.abort();

    await expect(
      useAppStore.getState().watchCommand(NAME, controller.signal)
    ).resolves.toBe(false);
    expect(useAppStore.getState().activeOutputs[NAME] ?? []).toHaveLength(0);
  });

  it("resolves true when the events stream ends normally", async () => {
    mock.watchEvents = [{ commandId: "c", seqNo: 1 }];

    await expect(useAppStore.getState().watchCommandEvents(NAME)).resolves.toBe(
      true
    );
    expect(useAppStore.getState().activeEvents[NAME]).toHaveLength(1);
  });

  it("resolves false when the events stream is aborted", async () => {
    mock.watchEvents = [{ commandId: "c", seqNo: 1 }];
    const controller = new AbortController();
    controller.abort();

    await expect(
      useAppStore.getState().watchCommandEvents(NAME, controller.signal)
    ).resolves.toBe(false);
    expect(useAppStore.getState().activeEvents[NAME] ?? []).toHaveLength(0);
  });
});

describe("command cache bounds", () => {
  it("releaseCommand drops the cached output/events for one command", async () => {
    mock.watchOutputs = [{ commandId: "c", seqNo: 1 }];
    await useAppStore.getState().watchCommand(NAME);
    expect(useAppStore.getState().activeOutputs[NAME]).toHaveLength(1);

    useAppStore.getState().releaseCommand(NAME);
    expect(useAppStore.getState().activeOutputs[NAME]).toBeUndefined();
    expect(useAppStore.getState().activeEvents[NAME]).toBeUndefined();
    // Releasing an unknown command is a no-op.
    useAppStore.getState().releaseCommand(NAME);
  });

  it("caps tracked commands via the LRU window", async () => {
    for (let i = 0; i < 9; i++) {
      const name = `agents/a/commands/c${i}`;
      mock.watchOutputs = [{ commandId: "c", seqNo: 1 }];
      await useAppStore.getState().watchCommand(name);
    }
    const outputs = useAppStore.getState().activeOutputs;
    expect(Object.keys(outputs)).toHaveLength(8);
    // The stalest tracked command was evicted; the freshest survives.
    expect(outputs["agents/a/commands/c0"]).toBeUndefined();
    expect(outputs["agents/a/commands/c8"]).toHaveLength(1);
    useAppStore.setState({ activeOutputs: {}, activeEvents: {} });
  });
});

const out = (seqNo: number): ScriptChunk => ({ commandId: "c", seqNo });
const ev = (seqNo: number): ScriptChunk => ({ commandId: "c", seqNo });

describe("command watch reconnect", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reconnects after a transport error and resumes from the last cached seqNo", async () => {
    vi.useFakeTimers();
    mock.outputScript = [
      { chunks: [out(1), out(2)], fail: true },
      { chunks: [out(3)], fail: false },
    ];

    const done = useAppStore.getState().watchCommand(NAME);
    await vi.advanceTimersByTimeAsync(1_000); // first backoff step

    await expect(done).resolves.toBe(true);
    // The reconnect opened the stream after (not before) the last appended
    // chunk, so nothing is replayed and nothing is lost.
    expect(mock.outputCalls).toEqual([{ afterSeqNo: -1 }, { afterSeqNo: 2 }]);
    expect(
      useAppStore.getState().activeOutputs[NAME]?.map((c) => c.seqNo)
    ).toEqual([1, 2, 3]);
  });

  it("resolves false after 5 failed reconnect attempts", async () => {
    vi.useFakeTimers();
    // Initial connection + 5 reconnects all fail.
    mock.outputScript = Array.from({ length: 6 }, () => ({
      chunks: [],
      fail: true,
    }));

    const done = useAppStore.getState().watchCommand(NAME);
    // 1s + 2s + 4s + 8s + 8s of backoff, plus slack for the final failure.
    await vi.advanceTimersByTimeAsync(30_000);

    await expect(done).resolves.toBe(false);
    expect(mock.outputCalls).toHaveLength(6);
    // No chunk landed, and none of the failures wiped unrelated state.
    expect(useAppStore.getState().activeOutputs[NAME] ?? []).toHaveLength(0);
  });

  it("resolves true on a clean close without reconnecting", async () => {
    mock.eventScript = [{ chunks: [ev(1), ev(2)], fail: false }];

    await expect(useAppStore.getState().watchCommandEvents(NAME)).resolves.toBe(
      true
    );
    // Only one stream was opened: a clean close ends the loop immediately.
    expect(mock.eventCalls).toHaveLength(1);
    expect(
      useAppStore.getState().activeEvents[NAME]?.map((c) => c.seqNo)
    ).toEqual([1, 2]);
  });
});
