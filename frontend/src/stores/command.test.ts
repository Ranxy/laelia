import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the store talks to controllable watch streams ---
const mock = vi.hoisted(() => ({
  watchOutputs: [] as Array<{ commandId: string; seqNo: number }>,
  watchEvents: [] as Array<{ commandId: string; seqNo: number }>,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    async *watchCommand() {
      for (const output of mock.watchOutputs) yield output;
    },
    async *watchCommandEvents() {
      for (const event of mock.watchEvents) yield event;
    },
  },
}));

const NAME = "agents/a/commands/c";

beforeEach(() => {
  useAppStore.setState({
    commands: [],
    commandsLoading: false,
    activeOutputs: {},
    activeEvents: {},
  });
  mock.watchOutputs = [];
  mock.watchEvents = [];
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
