import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the store talks to a controllable commandServiceClient ---
const mock = vi.hoisted(() => ({
  listMessagesReplies: [] as Array<{ messages: unknown[] }>,
  listMessagesCalls: 0,
  listMessagesSideEffect: null as null | (() => void),
  sendMessageReplies: [] as Array<unknown>,
  sendMessageCalls: 0,
  activityReplies: [] as Array<{ activities: unknown[] }>,
  activityCalls: 0,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    async listConversationMessages() {
      mock.listMessagesCalls += 1;
      mock.listMessagesSideEffect?.();
      const reply = mock.listMessagesReplies.shift() ?? { messages: [] };
      return reply;
    },
    async sendMessage() {
      mock.sendMessageCalls += 1;
      return mock.sendMessageReplies.shift() ?? {};
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

beforeEach(() => {
  // Clear any lingering watcher intervals from prior tests, then reset store.
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
  mock.sendMessageReplies = [];
  mock.sendMessageCalls = 0;
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
    // single interval, proving only one poller runs per conversation.
    mock.listMessagesReplies.push({ messages: [] });
    mock.activityReplies.push({ activities: [] });
    useAppStore.getState().startWatchingChannel("conversations/c");
    useAppStore.getState().startWatchingChannel("conversations/c");
    expect(Object.keys(useAppStore.getState().channelWatchers)).toHaveLength(1);
  });
});
