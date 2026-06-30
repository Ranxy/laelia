import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  ListThreadMessagesRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat-helpers";
import type { AppSliceCreator, ChatMessageUI, ThreadSlice } from "./types";

// Same cadence as the channel watcher; thread panels poll for new replies.
const THREAD_POLL_INTERVAL_MS = 2000;

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
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: {
            messages: uiMsgs,
            currentVersion: res.currentVersion,
            loading: false,
          },
        },
      }));
    } catch {
      set((state) => ({
        threadByRoot: {
          ...state.threadByRoot,
          [rootMessageId]: {
            messages: state.threadByRoot[rootMessageId]?.messages ?? [],
            currentVersion:
              state.threadByRoot[rootMessageId]?.currentVersion ?? 0n,
            loading: false,
          },
        },
      }));
    }

    if (get().activeThreadRoot !== rootMessageId) return; // closed mid-load
    startWatcher(set, get, conversation, rootMessageId);
  },

  closeThread() {
    const root = get().activeThreadRoot;
    if (root) stopWatcher(set, get, root);
    set({ activeThreadRoot: null, activeThreadConversation: null });
  },

  async sendThreadMessage(
    conversationId,
    rootMessageId,
    content,
    mentions,
    attachments
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
    set((state) => ({
      threadByRoot: {
        ...state.threadByRoot,
        [rootMessageId]: {
          ...(state.threadByRoot[rootMessageId] ?? {
            messages: [],
            currentVersion: 0n,
            loading: false,
          }),
          messages: appendNewMessages(
            state.threadByRoot[rootMessageId]?.messages ?? [],
            [chatMsg]
          ),
        },
      },
    }));
    return res;
  },
});

// startWatcher begins incremental polling for new thread replies. Each tick
// asks only for replies with room_version after the last seen version, dedups
// against the cached list, and advances the cursor.
function startWatcher(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  conversation: string,
  root: string
) {
  if (get().threadWatchers[root]) return;

  const poll = async () => {
    if (get().activeThreadRoot !== root) return; // panel closed/switched
    try {
      const afterVersion = get().threadByRoot[root]?.currentVersion ?? 0n;
      const res = await commandServiceClient.listThreadMessages(
        create(ListThreadMessagesRequestSchema, {
          conversation,
          threadRoot: root,
          pageSize: 200,
          pageToken: "",
          afterVersion,
        })
      );
      const delta: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
      const prev = get().threadByRoot[root]?.messages ?? [];
      const merged = appendNewMessages(prev, delta);
      const nextVersion = res.currentVersion;
      const prevVersion = get().threadByRoot[root]?.currentVersion ?? 0n;
      if (merged !== prev || nextVersion !== prevVersion) {
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
    } catch {
      // network error — retry on next tick
    }
  };

  poll();
  const handle = setInterval(poll, THREAD_POLL_INTERVAL_MS);
  set((state) => ({
    threadWatchers: { ...state.threadWatchers, [root]: handle },
  }));
}

function stopWatcher(
  set: Parameters<AppSliceCreator<ThreadSlice>>[0],
  get: Parameters<AppSliceCreator<ThreadSlice>>[1],
  root: string
) {
  const handle = get().threadWatchers[root];
  if (handle) {
    clearInterval(handle);
    set((state) => {
      const threadWatchers = { ...state.threadWatchers };
      delete threadWatchers[root];
      return { threadWatchers };
    });
  }
}
