import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import type {
  Attachment,
  ChatMessage,
  Mention,
} from "@/types/proto-es/v1/command_pb";
import {
  ListThreadMessagesRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat-helpers";
import { LONG_POLL_MS, startLongPollLoop } from "./chat-watcher";
import type { AppSliceCreator } from "./types";
import type { ChatMessageUI } from "./ui-models";

// ThreadSlice owns the right-side thread panel state: per-thread cached
// messages + current_version, the active thread root (which thread panel is
// open), and the per-thread polling watchers. Thread roots/reply ids are bare
// UUIDs (the backend uses chat_message.id, not a resource name). The active
// thread panel is scoped to one conversation at a time (activeThreadConversation).
export interface ThreadSlice {
  threadByRoot: Record<
    string,
    { messages: ChatMessageUI[]; currentVersion: bigint; loading: boolean }
  >;
  activeThreadRoot: string | null;
  activeThreadConversation: string | null;
  // Active per-thread long-poll watchers, keyed by thread root id. Each
  // handle owns the AbortController that cancels the in-flight request.
  threadWatchers: Record<string, { ctrl: AbortController }>;

  openThread: (conversation: string, rootMessageId: string) => Promise<void>;
  closeThread: () => void;
  // Loads a thread snapshot into threadByRoot without opening the thread
  // panel or starting a watcher (used by the preview comment aside).
  loadThreadMessages: (
    conversation: string,
    rootMessageId: string
  ) => Promise<void>;
  sendThreadMessage: (
    conversationId: string,
    rootMessageId: string,
    content: string,
    mentions?: Mention[],
    attachments?: Attachment[],
    optimisticId?: string
  ) => Promise<ChatMessage>;

  // Optimistic message mutations for the thread composer's optimistic send
  // pipeline (same contract as ChatSlice.appendChatMessage, scoped to one
  // thread snapshot; the thread snapshot is created on demand when a send
  // races ahead of openThread's initial load).
  appendThreadMessage: (rootMessageId: string, msg: ChatMessageUI) => void;
  patchThreadMessage: (
    rootMessageId: string,
    messageId: string,
    patch: Partial<ChatMessageUI>
  ) => void;
  removeThreadMessage: (rootMessageId: string, messageId: string) => void;
}

// Bounded thread cache: each cached thread holds up to 200 messages, so an
// unbounded map grows with every thread the user ever opened. Eviction is
// lazy (after opening a thread / closing the panel): the just-closed thread
// stays cached for a quick reopen, and the stalest thread beyond the cap
// returns to "never opened" state — reopening simply reloads it.
const MAX_CACHED_THREADS = 8;

// ThreadState is the per-thread cache entry shape (see ThreadSlice).
interface ThreadState {
  messages: ChatMessageUI[];
  currentVersion: bigint;
  loading: boolean;
}

// emptyThreadState is the pristine per-thread snapshot used when an optimistic
// write lands before openThread's initial load returns (or the snapshot was
// evicted). Kept in one place so the slice's invariant shape never drifts.
const emptyThreadState = (): ThreadState => ({
  messages: [],
  currentVersion: 0n,
  loading: false,
});

function pruneThreadCache(
  threads: Record<string, ThreadState>,
  activeRoot: string | null
): Record<string, ThreadState> {
  const keys = Object.keys(threads);
  if (keys.length <= MAX_CACHED_THREADS) return threads;
  // JS object insertion order makes keys[] oldest-first. Drop the oldest
  // entries over the cap, never the active thread.
  const doomed = keys
    .filter((k) => k !== activeRoot)
    .slice(0, keys.length - MAX_CACHED_THREADS);
  if (doomed.length === 0) return threads;
  const next = { ...threads };
  for (const k of doomed) delete next[k];
  return next;
}

export const createThreadSlice: AppSliceCreator<ThreadSlice> = (set, get) => ({
  threadByRoot: {},
  activeThreadRoot: null,
  activeThreadConversation: null,
  threadWatchers: {},

  async openThread(conversation, rootMessageId) {
    // If switching to a different thread, stop the previous watcher first so
    // we don't leave a polling loop running against an unmounted panel.
    const prevRoot = get().activeThreadRoot;
    if (prevRoot && prevRoot !== rootMessageId) {
      stopWatcher(set, get, prevRoot);
    }

    set({
      activeThreadRoot: rootMessageId,
      activeThreadConversation: conversation,
    });

    // Initial load: fetch the latest N messages so the panel has the root +
    // recent replies immediately, then start incremental polling.
    try {
      const res = await commandServiceClient.listThreadMessages(
        create(ListThreadMessagesRequestSchema, {
          conversation,
          threadRoot: rootMessageId,
          pageSize: 200,
          pageToken: "",
        })
      );
      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
      set((state) => ({
        threadByRoot: pruneThreadCache(
          {
            ...state.threadByRoot,
            [rootMessageId]: {
              messages: uiMsgs,
              currentVersion: res.currentVersion,
              loading: false,
            },
          },
          rootMessageId
        ),
      }));
      // The root (messages[0]) carries the authoritative total reply count;
      // sync it back into the main channel list so the "N replies" badge on the
      // root stays fresh without the main watcher having to observe thread
      // activity (it only polls new main-channel messages).
      syncRootReplyCount(set, get, conversation, rootMessageId, uiMsgs[0]);
    } catch {
      set((state) => ({
        threadByRoot: pruneThreadCache(
          {
            ...state.threadByRoot,
            [rootMessageId]: {
              messages: state.threadByRoot[rootMessageId]?.messages ?? [],
              currentVersion:
                state.threadByRoot[rootMessageId]?.currentVersion ?? 0n,
              loading: false,
            },
          },
          rootMessageId
        ),
      }));
    }

    if (get().activeThreadRoot !== rootMessageId) return; // closed mid-load
    startWatcher(set, get, conversation, rootMessageId);
  },

  closeThread() {
    const root = get().activeThreadRoot;
    if (root) stopWatcher(set, get, root);
    set((state) => ({
      activeThreadRoot: null,
      activeThreadConversation: null,
      // Lazy eviction: the just-closed thread stays cached for a quick
      // reopen; the stalest snapshots over the cap are dropped here.
      threadByRoot: pruneThreadCache(state.threadByRoot, null),
    }));
  },

  // loadThreadMessages fetches a thread's messages into threadByRoot without
  // opening the thread panel (no activeThreadRoot, no polling watcher). Used
  // by the markdown preview's comment aside, which only needs the current
  // snapshot — the commenter's own reply is optimistically appended by
  // sendThreadMessage, and reopening reloads. This avoids the side effect of
  // openThread, which would pop the thread panel open behind the overlay.
  async loadThreadMessages(conversation, rootMessageId) {
    try {
      const res = await commandServiceClient.listThreadMessages(
        create(ListThreadMessagesRequestSchema, {
          conversation,
          threadRoot: rootMessageId,
          pageSize: 200,
          pageToken: "",
        })
      );
      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
      set((state) => ({
        threadByRoot: pruneThreadCache(
          {
            ...state.threadByRoot,
            [rootMessageId]: {
              messages: uiMsgs,
              currentVersion: res.currentVersion,
              loading: false,
            },
          },
          state.activeThreadRoot
        ),
      }));
      syncRootReplyCount(set, get, conversation, rootMessageId, uiMsgs[0]);
    } catch {
      set((state) => ({
        threadByRoot: pruneThreadCache(
          {
            ...state.threadByRoot,
            [rootMessageId]: {
              messages: state.threadByRoot[rootMessageId]?.messages ?? [],
              currentVersion:
                state.threadByRoot[rootMessageId]?.currentVersion ?? 0n,
              loading: false,
            },
          },
          state.activeThreadRoot
        ),
      }));
    }
  },

  async sendThreadMessage(
    conversationId,
    rootMessageId,
    content,
    mentions,
    attachments,
    optimisticId?
  ) {
    const conversationName = `conversations/${conversationId}`;
    const res = await commandServiceClient.sendMessage(
      create(SendMessageRequestSchema, {
        conversation: conversationName,
        content,
        mentions,
        attachments,
        threadRoot: rootMessageId,
      })
    );
    const chatMsg: ChatMessageUI = toUiMessage(res);
    set((state) => {
      const current = state.threadByRoot[rootMessageId]?.messages ?? [];
      const withoutOptimistic = optimisticId
        ? current.filter((m) => m.id !== optimisticId)
        : current;
      return {
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: {
            ...(state.threadByRoot[rootMessageId] ?? emptyThreadState()),
            messages: appendNewMessages(withoutOptimistic, [chatMsg]),
          },
        },
      };
    });
    // Optimistically bump the root's reply count in the main channel list so the
    // "N replies" badge updates instantly. The thread watcher's next poll
    // replaces it with the authoritative count from the backend.
    bumpRootReplyCount(set, get, conversationName, rootMessageId, +1);
    return res;
  },

  // Optimistic-message actions (audit 05 D5): the thread composer's optimistic
  // send pipeline now routes through these slice actions instead of inlining
  // useAppStore.setState surgery in the component, so the slice's invariants
  // (ensure-thread literal, id dedup via appendNewMessages, same-reference
  // bail-outs) live in exactly one place.
  appendThreadMessage(rootMessageId, msg) {
    set((state) => {
      const thread = state.threadByRoot[rootMessageId] ?? emptyThreadState();
      const merged = appendNewMessages(thread.messages, [msg]);
      if (merged === thread.messages) return {};
      return {
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: { ...thread, messages: merged },
        },
      };
    });
  },

  patchThreadMessage(rootMessageId, messageId, patch) {
    set((state) => {
      const thread = state.threadByRoot[rootMessageId];
      const list = thread?.messages;
      if (!list) return {};
      const idx = list.findIndex((m) => m.id === messageId);
      if (idx < 0) return {};
      // Apply only when at least one patched key differs, so repeated no-op
      // patches (e.g. upload progress ticks) keep the row reference stable and
      // subscribers bail out.
      const changed = Object.keys(patch).some(
        (k) =>
          list[idx][k as keyof ChatMessageUI] !==
          patch[k as keyof ChatMessageUI]
      );
      if (!changed) return {};
      const messages = [...list];
      messages[idx] = { ...list[idx], ...patch };
      return {
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: { ...thread, messages },
        },
      };
    });
  },

  removeThreadMessage(rootMessageId, messageId) {
    set((state) => {
      const thread = state.threadByRoot[rootMessageId];
      if (!thread) return {};
      const filtered = thread.messages.filter((m) => m.id !== messageId);
      if (filtered.length === thread.messages.length) return {};
      return {
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: { ...thread, messages: filtered },
        },
      };
    });
  },
});

// startWatcher begins long-polling for new thread replies. Each request asks
// only for replies with room_version after the last seen version, dedups
// against the cached list, and advances the cursor. The request is held by the
// server until a new reply lands or the 25s timeout elapses, then re-issued
// immediately — the old 2s interval at ~1/12 the request rate. The loop
// scaffolding (re-issue, backoff, visibility gating) comes from the shared
// chat-watcher module.
function startWatcher(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  conversation: string,
  root: string
) {
  if (get().threadWatchers[root]) return;

  const ctrl = new AbortController();
  startLongPollLoop({
    signal: ctrl.signal,
    round: async () => {
      if (get().activeThreadRoot !== root) return; // panel closed/switched
      const afterVersion = get().threadByRoot[root]?.currentVersion ?? 0n;
      const res = await commandServiceClient.listThreadMessages(
        create(ListThreadMessagesRequestSchema, {
          conversation,
          threadRoot: root,
          pageSize: 200,
          pageToken: "",
          afterVersion,
          waitMs: LONG_POLL_MS,
        }),
        { signal: ctrl.signal }
      );
      const delta: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
      const prev = get().threadByRoot[root]?.messages ?? [];
      const merged = appendNewMessages(prev, delta);
      const nextVersion = res.currentVersion;
      const prevVersion = get().threadByRoot[root]?.currentVersion ?? 0n;
      if (merged !== prev || nextVersion !== prevVersion) {
        // Bail if the watcher was stopped/reset while this poll was in flight
        // (see the channel watcher's guard).
        if (ctrl.signal.aborted) return;
        set((state) => ({
          threadByRoot: {
            ...state.threadByRoot,
            [root]: {
              messages: merged,
              currentVersion: nextVersion,
              loading: false,
            },
          },
        }));
      }
      // Root reply-count freshness on the main channel list is owned by the
      // channel watcher (refreshChannelThreadCounts), so this watcher does not
      // also write back to the main list — it only maintains the thread's own
      // messages.
    },
  });

  set((state) => ({
    threadWatchers: { ...state.threadWatchers, [root]: { ctrl } },
  }));
}

function stopWatcher(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  root: string
) {
  const watcher = get().threadWatchers[root];
  if (watcher) {
    watcher.ctrl.abort();
    set((state) => {
      const threadWatchers = { ...state.threadWatchers };
      delete threadWatchers[root];
      return { threadWatchers };
    });
  }
}

// syncRootReplyCount writes the authoritative total reply count from a thread
// response's root message into the main channel list (`chatMessages[conv]`),
// so the "N replies" badge on the root stays fresh. The main channel watcher
// only polls new main-channel messages (thread replies are excluded server-side),
// so it would never observe a changed reply count on its own. No-op if the root
// isn't in the main list or the count is already current. `rootMsg` may be null
// (e.g. an empty delta) — then nothing is synced.
function syncRootReplyCount(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  conversation: string,
  rootId: string,
  rootMsg: ChatMessageUI | undefined
) {
  if (!rootMsg) return;
  const count = rootMsg.threadReplyCount ?? 0;
  updateRootReplyCount(set, get, conversation, rootId, (prev) =>
    prev === count ? prev : count
  );
}

// bumpRootReplyCount adjusts the root's reply count in the main channel list by
// a delta (e.g. +1 on optimistic send). Used when we don't yet have the
// authoritative total from the backend.
function bumpRootReplyCount(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  conversation: string,
  rootId: string,
  delta: number
) {
  updateRootReplyCount(set, get, conversation, rootId, (prev) =>
    Math.max(0, (prev ?? 0) + delta)
  );
}

// updateRootReplyCount finds the root message in the main channel list and
// applies `fn` to its current threadReplyCount, replacing the row only if the
// value changes (so subscribers bail out on no-ops).
function updateRootReplyCount(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  conversation: string,
  rootId: string,
  fn: (prev: number | undefined) => number
) {
  const list = get().chatMessages[conversation];
  if (!list) return;
  const idx = list.findIndex((m) => m.id === rootId);
  if (idx < 0) return;
  const next = fn(list[idx].threadReplyCount);
  if (next === list[idx].threadReplyCount) return;
  const updated = [...list];
  updated[idx] = { ...updated[idx], threadReplyCount: next };
  set((state) => ({
    chatMessages: { ...state.chatMessages, [conversation]: updated },
  }));
}
