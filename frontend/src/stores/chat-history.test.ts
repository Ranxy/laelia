import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";
import type { ChatMessageUI } from "./types";

const mocks = vi.hoisted(() => ({
  listConversationMessages: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listConversationMessages: mocks.listConversationMessages,
  },
}));

function uiMsg(id: string, roomVersion: bigint): ChatMessageUI {
  return {
    id,
    role: "user",
    content: id,
    timestamp: new Date(0),
    roomVersion,
  };
}

beforeEach(() => {
  mocks.listConversationMessages.mockReset();
  useAppStore.setState({
    chatMessages: {},
    chatCurrentVersion: {},
    chatJumpByConv: {},
    chatJumpLoading: {},
    chatHasOlderByConv: {},
    chatHasNewerByConv: {},
  });
});

describe("chat history pagination", () => {
  it("re-reads the current window after the page resolves", async () => {
    useAppStore.setState({
      chatMessages: {
        "conversations/c": [uiMsg("m10", 10n), uiMsg("m11", 11n)],
      },
      chatJumpByConv: {
        "conversations/c": { messageId: "m10", roomVersion: 10n },
      },
    });

    let resolvePage: (value: unknown) => void = () => {};
    mocks.listConversationMessages.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolvePage = resolve;
        })
    );

    const loading = useAppStore.getState().loadOlderMessages("conversations/c");

    // A watcher append lands while the older page is in flight.
    useAppStore.setState({
      chatMessages: {
        "conversations/c": [
          uiMsg("m10", 10n),
          uiMsg("m11", 11n),
          uiMsg("m12", 12n),
        ],
      },
    });

    resolvePage({
      messages: [{ name: "m9", role: 1, content: "m9", roomVersion: 9n }],
    });
    await loading;

    expect(
      useAppStore.getState().chatMessages["conversations/c"].map((m) => m.id)
    ).toEqual(["m9", "m10", "m11", "m12"]);
  });

  it("ignores duplicate loads while a page is already in flight", async () => {
    useAppStore.setState({
      chatMessages: {
        "conversations/c": [uiMsg("m10", 10n)],
      },
      chatJumpLoading: { "conversations/c": true },
    });

    await useAppStore.getState().loadOlderMessages("conversations/c");

    expect(mocks.listConversationMessages).not.toHaveBeenCalled();
  });

  it("marks a full latest page as having older history", async () => {
    const messages = Array.from({ length: 100 }, (_, i) => ({
      name: `m${i + 1}`,
      role: 1,
      content: `m${i + 1}`,
      roomVersion: BigInt(i + 1),
    }));
    mocks.listConversationMessages.mockResolvedValue({
      messages,
      currentVersion: 100n,
      nextPageToken: "",
    });
    useAppStore.setState({
      chatMessages: {},
      chatCurrentVersion: { "conversations/c": 0n },
    });

    await useAppStore.getState().loadMessages("conversations/c");

    expect(useAppStore.getState().chatHasOlderByConv["conversations/c"]).toBe(
      true
    );
    expect(useAppStore.getState().chatHasNewerByConv["conversations/c"]).toBe(
      false
    );
  });

  it("does not mark older history for a partial latest page", async () => {
    const messages = Array.from({ length: 50 }, (_, i) => ({
      name: `m${i + 1}`,
      role: 1,
      content: `m${i + 1}`,
      roomVersion: BigInt(i + 1),
    }));
    mocks.listConversationMessages.mockResolvedValue({
      messages,
      currentVersion: 50n,
      nextPageToken: "",
    });
    useAppStore.setState({
      chatMessages: {},
      chatCurrentVersion: { "conversations/c": 0n },
    });

    await useAppStore.getState().loadMessages("conversations/c");

    expect(useAppStore.getState().chatHasOlderByConv["conversations/c"]).toBe(
      false
    );
    expect(useAppStore.getState().chatHasNewerByConv["conversations/c"]).toBe(
      false
    );
  });

  it("discards a page when the window edge moved while it was in flight", async () => {
    useAppStore.setState({
      chatMessages: {
        "conversations/c": [uiMsg("m10", 10n), uiMsg("m11", 11n)],
      },
      chatJumpByConv: {
        "conversations/c": { messageId: "m10", roomVersion: 10n },
      },
    });

    let resolvePage: (value: unknown) => void = () => {};
    mocks.listConversationMessages.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolvePage = resolve;
        })
    );

    const loading = useAppStore.getState().loadOlderMessages("conversations/c");

    // clearJump (or a new jump) replaced the window while the page was in
    // flight; the stale older page must not be merged onto the new window.
    useAppStore.setState({
      chatMessages: { "conversations/c": [uiMsg("m5", 5n)] },
      chatJumpByConv: { "conversations/c": null },
    });

    resolvePage({
      messages: [{ name: "m9", role: 1, content: "m9", roomVersion: 9n }],
    });
    await loading;

    expect(
      useAppStore.getState().chatMessages["conversations/c"].map((m) => m.id)
    ).toEqual(["m5"]);
    expect(useAppStore.getState().chatJumpLoading["conversations/c"]).toBe(
      false
    );
  });
});
