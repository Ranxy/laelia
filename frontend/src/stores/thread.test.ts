import { beforeEach, describe, expect, it } from "vitest";
import { useAppStore } from "./index";
import type { ChatMessageUI } from "./types";

// Thread cache pruning is sync store logic (closeThread path), so these
// tests only need the store — no RPC is hit.

function threadState() {
  return {
    messages: [] as ChatMessageUI[],
    currentVersion: 0n,
    loading: false,
  };
}

describe("thread cache bounds", () => {
  beforeEach(() => {
    useAppStore.setState({
      threadByRoot: {},
      threadWatchers: {},
      activeThreadRoot: null,
      activeThreadConversation: null,
    });
  });

  it("evicts the stalest cached thread when the cap is exceeded on close", () => {
    const threads: Record<string, ReturnType<typeof threadState>> = {};
    for (let i = 1; i <= 9; i++) threads[`t${i}`] = threadState();
    useAppStore.setState({
      threadByRoot: threads,
      threadWatchers: {},
      activeThreadRoot: "t9",
    });

    useAppStore.getState().closeThread();

    const s = useAppStore.getState();
    expect(Object.keys(s.threadByRoot)).toHaveLength(8);
    expect(s.threadByRoot.t1).toBeUndefined();
    // The just-closed (freshest) thread survives for a quick reopen.
    expect(s.threadByRoot.t9).toBeDefined();
    expect(s.activeThreadRoot).toBeNull();
  });
});
