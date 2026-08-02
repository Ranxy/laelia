import { afterEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

describe("store reset", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("restores every slice to its pristine initial state on logout", () => {
    const store = useAppStore;
    // Simulate the accumulated per-user state a logged-in session leaves behind.
    store.setState({
      currentUser: { name: "users/1" } as never,
      isLoggedIn: true,
      sessionLoaded: true,
      agents: [{ name: "agents/1", title: "a" }] as never,
      chatMessages: { "conversations/1": [] },
      unreadByConv: { "conversations/1": 3 },
      reminders: [{ name: "reminders/1" }] as never,
      activities: [{ name: "activities/1" }] as never,
      channelMembersByConv: { "conversations/1": [] },
    });

    store.getState().reset();

    const s = store.getState();
    expect(s.currentUser).toBeNull();
    expect(s.isLoggedIn).toBe(false);
    expect(s.sessionLoaded).toBe(false);
    expect(s.agents).toEqual([]);
    expect(s.chatMessages).toEqual({});
    expect(s.unreadByConv).toEqual({});
    expect(s.reminders).toEqual([]);
    expect(s.activities).toEqual([]);
    expect(s.channelMembersByConv).toEqual({});
  });

  it("stops channel and thread watcher intervals before wiping state", () => {
    const store = useAppStore;
    const clearSpy = vi.spyOn(globalThis, "clearInterval");
    const channelTimer = setInterval(() => {}, 1000);
    const threadTimer = setInterval(() => {}, 1000);

    store.setState({
      channelWatchers: { "conversations/1": channelTimer },
      threadWatchers: { "conversations/1": threadTimer },
    });

    store.getState().reset();

    expect(clearSpy).toHaveBeenCalledWith(channelTimer);
    expect(clearSpy).toHaveBeenCalledWith(threadTimer);
    expect(store.getState().channelWatchers).toEqual({});
    expect(store.getState().threadWatchers).toEqual({});

    // Safety net in case the assertion above ever fails before clearing.
    clearInterval(channelTimer);
    clearInterval(threadTimer);
  });
});
