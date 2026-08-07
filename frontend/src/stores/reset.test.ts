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

  it("stops channel and thread watcher loops before wiping state", () => {
    const store = useAppStore;
    const abortSpy = vi.spyOn(AbortController.prototype, "abort");
    const clearSpy = vi.spyOn(globalThis, "clearInterval");
    const channelCtrl = new AbortController();
    const threadCtrl = new AbortController();
    const badgeTimer = setInterval(() => {}, 1000);

    store.setState({
      channelWatchers: {
        "conversations/1": { ctrl: channelCtrl, badgeTimer },
      },
      threadWatchers: { "conversations/1": { ctrl: threadCtrl } },
    });

    store.getState().reset();

    expect(abortSpy).toHaveBeenCalledTimes(2);
    expect(clearSpy).toHaveBeenCalledWith(badgeTimer);
    expect(store.getState().channelWatchers).toEqual({});
    expect(store.getState().threadWatchers).toEqual({});

    // Safety net in case the assertion above ever fails before clearing.
    clearInterval(badgeTimer);
  });
});
