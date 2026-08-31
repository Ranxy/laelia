import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the chat slice talks to a controllable commandServiceClient ---
const mocks = vi.hoisted(() => ({
  getOrCreateConversation: vi.fn(),
  sendMessage: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    getOrCreateConversation: mocks.getOrCreateConversation,
    sendMessage: mocks.sendMessage,
  },
}));

beforeEach(() => {
  mocks.getOrCreateConversation.mockReset();
  mocks.sendMessage.mockReset();
  // reset() runs the cleanup chat.ts registered for its module-level
  // agent→conversation cache, so every test starts with a cold cache.
  useAppStore.getState().reset();
});

describe("chat conversation cache", () => {
  it("RPCs once per agent and serves repeat calls from the cache", async () => {
    mocks.getOrCreateConversation.mockResolvedValue({
      name: "conversations/c1",
    });

    const store = useAppStore.getState();
    await expect(store.getOrCreateConversation("agents/1")).resolves.toBe(
      "conversations/c1"
    );
    await expect(store.getOrCreateConversation("agents/1")).resolves.toBe(
      "conversations/c1"
    );

    expect(mocks.getOrCreateConversation).toHaveBeenCalledTimes(1);
    expect(mocks.getOrCreateConversation).toHaveBeenCalledWith(
      expect.objectContaining({ agent: "agents/1" })
    );
  });

  it("keeps separate entries for different agents", async () => {
    mocks.getOrCreateConversation
      .mockResolvedValueOnce({ name: "conversations/a" })
      .mockResolvedValueOnce({ name: "conversations/b" });

    const store = useAppStore.getState();
    await expect(store.getOrCreateConversation("agents/1")).resolves.toBe(
      "conversations/a"
    );
    await expect(store.getOrCreateConversation("agents/2")).resolves.toBe(
      "conversations/b"
    );
    // Both repeat calls are cache hits, one per agent entry.
    await expect(store.getOrCreateConversation("agents/1")).resolves.toBe(
      "conversations/a"
    );
    await expect(store.getOrCreateConversation("agents/2")).resolves.toBe(
      "conversations/b"
    );
    expect(mocks.getOrCreateConversation).toHaveBeenCalledTimes(2);
  });

  it("reset() clears the module cache so the next call RPCs again", async () => {
    mocks.getOrCreateConversation.mockResolvedValue({
      name: "conversations/c1",
    });
    await useAppStore.getState().getOrCreateConversation("agents/1");

    // reset() runs chat.ts's registered cleanup, sweeping the module cache.
    useAppStore.getState().reset();

    mocks.getOrCreateConversation.mockResolvedValue({
      name: "conversations/c2",
    });
    await expect(
      useAppStore.getState().getOrCreateConversation("agents/1")
    ).resolves.toBe("conversations/c2");
    expect(mocks.getOrCreateConversation).toHaveBeenCalledTimes(2);
  });

  it("sendChatMessage without an explicit conversationId uses the cached conversation", async () => {
    mocks.getOrCreateConversation.mockResolvedValue({
      name: "conversations/cc",
    });
    await useAppStore.getState().getOrCreateConversation("agents/1");

    mocks.sendMessage.mockResolvedValue({
      name: "conversations/cc/messages/m1",
      role: 1,
      content: "hi",
    });
    await useAppStore.getState().sendChatMessage("agents/1", "hi");

    // The cached conversation flows into the send, and no extra
    // getOrCreateConversation RPC was made beyond the seeding call.
    expect(mocks.sendMessage).toHaveBeenCalledTimes(1);
    expect(mocks.sendMessage).toHaveBeenCalledWith(
      expect.objectContaining({
        conversation: "conversations/cc",
        content: "hi",
      })
    );
    expect(mocks.getOrCreateConversation).toHaveBeenCalledTimes(1);
  });
});
