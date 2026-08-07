import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the store talks to a controllable commandServiceClient ---
const mock = vi.hoisted(() => ({
  listMessagesReplies: [] as Array<{
    messages: unknown[];
    currentVersion?: bigint;
  }>,
  listMessagesCalls: 0,
  listMessagesSideEffect: null as null | (() => void),
  // Long-poll semantics: each listConversationMessages call returns a promise
  // the test resolves/rejects via resolveNextListMessage/rejectNextListMessage,
  // so the self-scheduling watcher loop only advances when the test lets it.
  listMessagesResolvers: [] as Array<() => void>,
  listMessagesRejecters: [] as Array<(e: unknown) => void>,
  sendMessageReplies: [] as Array<unknown>,
  sendMessageCalls: 0,
  // When false, sendMessage stays pending until resolveNextSendMessage is
  // called, so a test can order the send echo after the watcher echo.
  sendMessageAutoResolve: true,
  sendMessageResolvers: [] as Array<() => void>,
  activityReplies: [] as Array<{ activities: unknown[] }>,
  activityCalls: 0,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listConversationMessages(_req: unknown, opts?: { signal?: AbortSignal }) {
      mock.listMessagesCalls += 1;
      mock.listMessagesSideEffect?.();
      const reply = mock.listMessagesReplies.shift() ?? {
        messages: [],
        currentVersion: 0n,
      };
      return new Promise((resolve, reject) => {
        mock.listMessagesResolvers.push(() => resolve(reply));
        mock.listMessagesRejecters.push(reject);
        opts?.signal?.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError"))
        );
      });
    },
    async sendMessage() {
      mock.sendMessageCalls += 1;
      const reply = mock.sendMessageReplies.shift() ?? {};
      if (mock.sendMessageAutoResolve) return reply;
      return new Promise((resolve) => {
        mock.sendMessageResolvers.push(() => resolve(reply));
      });
    },
    async fetchConversationActivity() {
      mock.activityCalls += 1;
      return mock.activityReplies.shift() ?? { activities: [] };
    },
    async getOrCreateConversation() {
      return { name: "conversations/seed" };
    },
  },
}));

// --- helpers ---

function backendMsg(
  overrides: Partial<{
    name: string;
    role: number;
    content: string;
    commandId: string;
  }> = {}
) {
  return {
    name: overrides.name ?? "m",
    role: overrides.role ?? 1,
    content: overrides.content ?? "",
    commandId: overrides.commandId ?? "",
    senderName: "u1",
    senderType: 1,
    mentions: [],
    attachments: [],
  };
}

function resolveNextListMessage() {
  mock.listMessagesResolvers.shift()?.();
}

function rejectNextListMessage() {
  mock.listMessagesRejecters.shift()?.(new Error("network down"));
}

function resolveNextSendMessage() {
  mock.sendMessageResolvers.shift()?.();
}

beforeEach(() => {
  // Clear any lingering watcher loops from prior tests, then reset store.
  const state = useAppStore.getState();
  for (const k of Object.keys(state.channelWatchers)) {
    state.stopWatchingChannel(k);
  }
  useAppStore.setState({
    conversations: {},
    chatMessages: {},
    chatLoading: {},
    channels: [],
    channelsLoading: false,
    channelMembersByConv: {},
    channelMembersLoading: {},
    agentActivities: {},
    channelWatchers: {},
  });
  mock.listMessagesReplies = [];
  mock.listMessagesCalls = 0;
  mock.listMessagesSideEffect = null;
  mock.listMessagesResolvers = [];
  mock.listMessagesRejecters = [];
  mock.sendMessageReplies = [];
  mock.sendMessageCalls = 0;
  mock.sendMessageAutoResolve = true;
  mock.sendMessageResolvers = [];
  mock.activityReplies = [];
  mock.activityCalls = 0;
});

afterEach(() => {
  const state = useAppStore.getState();
  for (const k of Object.keys(state.channelWatchers)) {
    state.stopWatchingChannel(k);
  }
});

describe("sendChannelMessage polling", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not start a second polling loop on send (NoDoublePolling)", async () => {
    // No watcher is running before the send.
    expect(Object.keys(useAppStore.getState().channelWatchers)).toHaveLength(0);

    mock.sendMessageReplies.push(
      backendMsg({ name: "m1", role: 1, content: "hello" })
    );

    await useAppStore.getState().sendChannelMessage("c", "hello");

    // sendChannelMessage must NOT start a watcher (the persistent watcher is
    // owned by the channel page; the old pollChannelMessages ran a concurrent
    // loop on top of it).
    expect(mock.sendMessageCalls).toBe(1);
    expect(Object.keys(useAppStore.getState().channelWatchers)).toHaveLength(0);

    // The channel page's watcher is idempotent: starting it twice yields a
    // single long-poll loop, proving only one poller runs per conversation.
    mock.listMessagesReplies.push({ messages: [] });
    mock.activityReplies.push({ activities: [] });
    useAppStore.getState().startWatchingChannel("conversations/c");
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(Object.keys(useAppStore.getState().channelWatchers)).toHaveLength(1);
    expect(mock.listMessagesCalls).toBe(1);
  });
});

describe("channel watcher long-poll loop", () => {
  it("re-issues the long poll after a response and merges new messages", async () => {
    mock.listMessagesReplies.push({
      messages: [backendMsg({ name: "m1", role: 1, content: "first" })],
      currentVersion: 1n,
    });
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(mock.listMessagesCalls).toBe(1);

    // Queue the second reply before resolving the first: the loop re-issues
    // immediately after a response, capturing the next reply at call time.
    mock.listMessagesReplies.push({
      messages: [backendMsg({ name: "m2", role: 1, content: "second" })],
      currentVersion: 2n,
    });
    resolveNextListMessage();
    await vi.waitFor(() => {
      expect(
        useAppStore.getState().chatMessages["conversations/c"]
      ).toHaveLength(1);
    });
    expect(mock.listMessagesCalls).toBe(2);

    resolveNextListMessage();
    await vi.waitFor(() => {
      expect(
        useAppStore.getState().chatMessages["conversations/c"]
      ).toHaveLength(2);
    });
    expect(useAppStore.getState().chatCurrentVersion["conversations/c"]).toBe(
      2n
    );
  });

  it("stopWatchingChannel aborts the in-flight long poll and stops the loop", async () => {
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(mock.listMessagesCalls).toBe(1);

    useAppStore.getState().stopWatchingChannel("conversations/c");
    expect(useAppStore.getState().channelWatchers).toEqual({});

    // The abort rejects the pending request; the loop exits without re-issuing.
    await new Promise((r) => setTimeout(r, 30));
    expect(mock.listMessagesCalls).toBe(1);
  });

  it("backs off and retries after a network error", async () => {
    vi.useFakeTimers();
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(mock.listMessagesCalls).toBe(1);

    rejectNextListMessage();
    // The catch path sleeps 1s before re-issuing the long poll.
    await vi.advanceTimersByTimeAsync(1000);
    expect(mock.listMessagesCalls).toBe(2);
    vi.useRealTimers();
  });
});

describe("send vs watcher echo race", () => {
  it("shows the sent message once when the watcher echo lands first", async () => {
    // The reply must be queued before the watcher issues its first request:
    // the mock captures the reply at call time.
    const echo = backendMsg({ name: "m1", role: 1, content: "hello" });
    mock.listMessagesReplies.push({ messages: [echo], currentVersion: 1n });
    mock.sendMessageReplies.push(echo);
    mock.sendMessageAutoResolve = false;

    // Long poll #1 is in flight when the user sends; the commit wakes it, so
    // its echo (same server id) can land before the send response.
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(mock.listMessagesCalls).toBe(1);

    const sendPromise = useAppStore.getState().sendChannelMessage("c", "hello");

    // Watcher wins the race and appends the echo first.
    resolveNextListMessage();
    await vi.waitFor(() => {
      expect(
        useAppStore.getState().chatMessages["conversations/c"]
      ).toHaveLength(1);
    });

    // The send append must dedup against the already-present echo.
    resolveNextSendMessage();
    await sendPromise;
    expect(useAppStore.getState().chatMessages["conversations/c"]).toHaveLength(
      1
    );
  });
});
