import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { useAppStore } from "./index";

// --- mock @/connect so the store talks to a controllable commandServiceClient ---
const mock = vi.hoisted(() => ({
  // Per-call scripts for watchCommandEvents. Each call shifts one script off
  // the front; a script is an array of steps the iterator yields in order.
  streamScripts: [] as Array<
    Array<
      | { kind: "event"; event: unknown }
      | { kind: "throw"; error: Error }
      | { kind: "done" }
      | { kind: "await"; promise: Promise<unknown> }
    >
  >,
  watchCalls: [] as Array<{ name: string; afterSeqNo: number }>,
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
    watchCommandEvents: (req: { name: string; afterSeqNo: number }) => {
      mock.watchCalls.push({ name: req.name, afterSeqNo: req.afterSeqNo });
      const steps =
        mock.streamScripts.shift() ??
        ([] as (typeof mock.streamScripts)[number]);
      return {
        [Symbol.asyncIterator]() {
          let i = 0;
          return {
            next: () => {
              if (i >= steps.length) {
                return Promise.resolve({ done: true, value: undefined });
              }
              const step = steps[i++];
              if (step.kind === "throw") return Promise.reject(step.error);
              if (step.kind === "done")
                return Promise.resolve({ done: true, value: undefined });
              if (step.kind === "await")
                return step.promise as Promise<IteratorResult<unknown>>;
              return Promise.resolve({ done: false, value: step.event });
            },
          };
        },
      };
    },
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

const COMMAND = "agents/a/commands/cmd-1";
const CONV = "conversations/c";

type EventStep =
  | { kind: "event"; event: unknown }
  | { kind: "throw"; error: Error }
  | { kind: "done" }
  | { kind: "await"; promise: Promise<unknown> };

function textDelta(seqNo: number, content: string) {
  return {
    seqNo,
    type: CommandEventType.TEXT_DELTA,
    summary: "",
    payload: { case: "textDelta", value: { content, streamType: "STDOUT" } },
  };
}

function script(...steps: EventStep[]) {
  return steps;
}

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

// Seed an in-flight assistant message so streamChatCommand has something to
// finalize, plus the streaming keys it reads.
function seedStreaming() {
  useAppStore.setState({
    chatMessages: {
      [CONV]: [
        { id: "u1", role: "user", content: "hi", timestamp: new Date(0) },
        {
          id: `assistant-${COMMAND}`,
          role: "assistant",
          content: "",
          timestamp: new Date(0),
          commandName: COMMAND,
          commandId: "cmd-1",
          status: 1,
          streaming: true,
          events: [],
        },
      ],
    },
    streamingContent: { [COMMAND]: "" },
    streamingEvents: { [COMMAND]: [] },
    streamingStatus: { [COMMAND]: 1 },
  });
}

function assistant() {
  return useAppStore
    .getState()
    .chatMessages[CONV]?.find((m) => m.commandName === COMMAND);
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
    streamingContent: {},
    streamingEvents: {},
    streamingStatus: {},
    channels: [],
    channelsLoading: false,
    channelMembersByConv: {},
    channelMembersLoading: {},
    agentActivities: {},
    channelWatchers: {},
  });
  mock.streamScripts = [];
  mock.watchCalls = [];
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

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("streamChatCommand cleanup", () => {
  it("aborts before backend reload and preserves partial output (AbortGuardBeforeCleanup)", async () => {
    seedStreaming();
    const controller = new AbortController();

    // Deferred stream steps so the test controls exactly when each event
    // arrives, letting us abort between the partial token and stream end so
    // the abort guard is observed before the cleanup/reload path runs.
    let resolve0!: (v: { done: boolean; value: unknown }) => void;
    let resolve1!: (v: { done: boolean; value: unknown }) => void;
    const step0 = new Promise<{ done: boolean; value: unknown }>(
      (r) => (resolve0 = r)
    );
    const step1 = new Promise<{ done: boolean; value: unknown }>(
      (r) => (resolve1 = r)
    );

    mock.streamScripts.push(
      script(
        { kind: "await", promise: step0 },
        { kind: "await", promise: step1 }
      )
    );

    const done = useAppStore
      .getState()
      .streamChatCommand(COMMAND, CONV, controller.signal);

    // Deliver the partial token, let the body apply it, then abort before the
    // stream closes so the abort guard fires before the backend reload.
    resolve0({ done: false, value: textDelta(0, "partial") });
    await flush();
    controller.abort();
    resolve1({ done: true, value: undefined });
    await flush();
    await done;

    const a = assistant();
    expect(a).toBeDefined();
    expect(a?.streaming).toBe(false);
    expect(a?.content).toBe("partial");
    // Abort guard: no backend reload was issued.
    expect(mock.listMessagesCalls).toBe(0);
    // Streaming keys were cleaned up.
    expect(useAppStore.getState().streamingContent[COMMAND]).toBeUndefined();
  });

  it("merges into the latest cached list instead of overwriting it (CleanupMergesNotOverwrites)", async () => {
    seedStreaming();

    mock.streamScripts.push(
      script(
        { kind: "event", event: textDelta(0, "partial") },
        { kind: "done" }
      )
    );

    // The backend reload returns the assistant with final text. While the
    // reload is in flight, simulate a concurrent watcher poll appending an
    // "extra" message — the old code replaced the whole list with the fetched
    // one, clobbering it; the functional-set merge must preserve it.
    mock.listMessagesSideEffect = () => {
      mock.listMessagesSideEffect = null;
      useAppStore.setState((s) => ({
        chatMessages: {
          ...s.chatMessages,
          [CONV]: [
            ...(s.chatMessages[CONV] ?? []),
            {
              id: "extra-from-watcher",
              role: "assistant",
              content: "concurrent",
              timestamp: new Date(0),
            },
          ],
        },
      }));
    };
    mock.listMessagesReplies.push({
      messages: [
        backendMsg({ name: "u1", role: 1, content: "hi" }),
        backendMsg({
          name: "assistant-real",
          role: 2,
          content: "FINAL",
          commandId: "cmd-1",
        }),
      ],
    });

    await useAppStore
      .getState()
      .streamChatCommand(COMMAND, CONV, new AbortController().signal);

    const msgs = useAppStore.getState().chatMessages[CONV] ?? [];
    const a = msgs.find((m) => m.commandName === COMMAND);
    const extra = msgs.find((m) => m.id === "extra-from-watcher");
    expect(a?.content).toBe("FINAL");
    expect(a?.streaming).toBe(false);
    expect(extra).toBeDefined();
  });

  it("reconnects with bounded backoff and replays from the last seq (ReconnectAfterTransientError)", async () => {
    seedStreaming();

    // First attempt: one event, then a transient network error (throw).
    mock.streamScripts.push(
      script(
        { kind: "event", event: textDelta(0, "part1") },
        { kind: "throw", error: new Error("network drop") }
      )
    );
    // Second attempt (reconnect): one event, then clean close.
    mock.streamScripts.push(
      script({ kind: "event", event: textDelta(1, "part2") }, { kind: "done" })
    );
    // Backend reload returns the assistant with the full streamed text so the
    // reload exits on the first attempt.
    mock.listMessagesReplies.push({
      messages: [
        backendMsg({ name: "u1", role: 1, content: "hi" }),
        backendMsg({
          name: "assistant-real",
          role: 2,
          content: "part1part2",
          commandId: "cmd-1",
        }),
      ],
    });

    await useAppStore
      .getState()
      .streamChatCommand(COMMAND, CONV, new AbortController().signal);

    // The stream was opened twice: the second call must replay from after the
    // last received seq (0), not from -1, so events are not duplicated.
    expect(mock.watchCalls).toHaveLength(2);
    expect(mock.watchCalls[0].afterSeqNo).toBe(-1);
    expect(mock.watchCalls[1].afterSeqNo).toBe(0);

    const a = assistant();
    expect(a?.content).toBe("part1part2");
    expect(a?.streaming).toBe(false);
  });
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
