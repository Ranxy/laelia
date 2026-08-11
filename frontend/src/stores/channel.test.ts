import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { useAppStore } from "./index";

// Mock @/connect so fetchMyChannels/fetchChannels talk to a controllable
// listChannels instead of the network.
const mock = vi.hoisted(() => ({
  channels: [] as Array<Record<string, unknown>>,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listChannels: vi.fn(async () => ({
      channels: mock.channels,
      nextPageToken: "",
    })),
  },
}));

beforeEach(() => {
  useAppStore.setState({ myChannels: [], myChannelsLoading: false });
  mock.channels = [];
});

describe("fetchMyChannels", () => {
  it("keeps only real channels (type 2) and includes closed ones", async () => {
    mock.channels = [
      { name: "conversations/c1", title: "Design", type: 2, closed: false },
      { name: "conversations/c2", title: "Retired", type: 2, closed: true },
      { name: "conversations/dm1", title: "Agent DM", type: 1, closed: false },
    ];

    await useAppStore.getState().fetchMyChannels();

    const list = useAppStore.getState().myChannels;
    expect(list.map((c) => c.name)).toEqual([
      "conversations/c1",
      "conversations/c2",
    ]);
    expect(list.find((c) => c.name === "conversations/c2")?.closed).toBe(true);
    expect(useAppStore.getState().myChannelsLoading).toBe(false);
  });

  it("does not touch the left-rail channel list", async () => {
    mock.channels = [{ name: "conversations/c1", title: "Design", type: 2 }];
    useAppStore.setState({
      channels: [{ name: "conversations/keep" }] as Conversation[],
    });

    await useAppStore.getState().fetchMyChannels();

    expect(useAppStore.getState().channels).toHaveLength(1);
    expect(useAppStore.getState().channels[0].name).toBe("conversations/keep");
  });
});
