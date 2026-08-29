import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { applyChannelThreadSummaries } from "./channel";
import { useAppStore } from "./index";
import type { ChatMessageUI } from "./types";

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

describe("applyChannelThreadSummaries", () => {
  function rootMsg(id: string): ChatMessageUI {
    return { id, role: "user", content: id, timestamp: new Date(0) };
  }

  function reply(id: string, content: string): ChatMessageUI {
    return {
      id,
      role: "assistant",
      content,
      timestamp: new Date(0),
      threadRoot: "root-1",
      senderName: "Agent",
    };
  }

  it("merges count, unread count, and preview onto the root row", () => {
    const prev = [rootMsg("root-1"), reply("r2", "old reply")];
    const preview = [reply("r1", "first"), reply("r2", "latest")];

    const next = applyChannelThreadSummaries(prev, [
      {
        rootMessage: "root-1",
        replyCount: 14,
        newReplyCount: 2,
        preview,
      },
    ]);

    expect(next[0].threadReplyCount).toBe(14);
    expect(next[0].threadNewReplyCount).toBe(2);
    expect(next[0].threadPreview?.map((r) => r.id)).toEqual(["r1", "r2"]);
    // Reply rows never carry the badge/preview.
    expect(next[1].threadReplyCount).toBeUndefined();
    expect(next[1].threadPreview).toBeUndefined();
  });

  it("clears the summary when a thread's summary disappears", () => {
    const root = rootMsg("root-1");
    const prev = applyChannelThreadSummaries(
      [root],
      [{ rootMessage: "root-1", replyCount: 3, newReplyCount: 1, preview: [] }]
    );

    const next = applyChannelThreadSummaries(prev, []);

    expect(next[0].threadReplyCount).toBe(0);
    expect(next[0].threadNewReplyCount).toBe(0);
    expect(next[0].threadPreview).toBeUndefined();
  });

  it("keeps the previous array reference when nothing changed", () => {
    const rootWithPreview = applyChannelThreadSummaries(
      [rootMsg("root-1")],
      [{ rootMessage: "root-1", replyCount: 2, newReplyCount: 0, preview: [] }]
    )[0];
    const prev = [rootWithPreview];

    const next = applyChannelThreadSummaries(prev, [
      { rootMessage: "root-1", replyCount: 2, newReplyCount: 0, preview: [] },
    ]);

    expect(next).toBe(prev);
  });

  it("re-merges when a preview reply changes", () => {
    const rootWithPreview = applyChannelThreadSummaries(
      [rootMsg("root-1")],
      [
        {
          rootMessage: "root-1",
          replyCount: 1,
          newReplyCount: 0,
          preview: [reply("r1", "first")],
        },
      ]
    )[0];
    const prev = [rootWithPreview];

    const next = applyChannelThreadSummaries(prev, [
      {
        rootMessage: "root-1",
        replyCount: 2,
        newReplyCount: 1,
        preview: [reply("r1", "first"), reply("r2", "newest")],
      },
    ]);

    expect(next[0].threadPreview?.map((r) => r.id)).toEqual(["r1", "r2"]);
    expect(next[0].threadNewReplyCount).toBe(1);
  });
});
