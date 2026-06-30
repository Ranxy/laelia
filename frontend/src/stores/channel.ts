import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  AddChannelMemberRequestSchema,
  CreateChannelRequestSchema,
  FetchConversationActivityRequestSchema,
  ListChannelMembersRequestSchema,
  ListChannelsRequestSchema,
  ListChannelThreadsRequestSchema,
  ListConversationMessagesRequestSchema,
  MarkConversationReadRequestSchema,
  RemoveChannelMemberRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat";
import type { AppSliceCreator, ChannelSlice, ChatMessageUI } from "./types";

const WATCHER_POLL_INTERVAL_MS = 2000;

export const createChannelSlice: AppSliceCreator<ChannelSlice> = (
  set,
  get
) => ({
  channels: [],
  channelsLoading: false,
  channelMembersByConv: {},
  channelMembersLoading: {},
  agentActivities: {},
  unreadByConv: {},
  channelWatchers: {},

  async fetchChannels() {
    set({ channelsLoading: true });
    try {
      const res = await commandServiceClient.listChannels(
        create(ListChannelsRequestSchema, { pageSize: 100, pageToken: "" })
      );
      const list = res.channels ?? [];
      const unreadByConv: Record<string, number> = {};
      for (const c of list) unreadByConv[c.name] = c.unreadCount ?? 0;
      set({ channels: list, unreadByConv, channelsLoading: false });
    } catch {
      set({ channelsLoading: false });
    }
  },

  async createChannel(title) {
    const res = await commandServiceClient.createChannel(
      create(CreateChannelRequestSchema, { title })
    );
    set((state) => ({
      channels: [...state.channels, res],
      unreadByConv: { ...state.unreadByConv, [res.name]: res.unreadCount ?? 0 },
    }));
    return res;
  },

  async markConversationRead(conversationId) {
    const conversation = `conversations/${conversationId}`;
    try {
      await commandServiceClient.markConversationRead(
        create(MarkConversationReadRequestSchema, { conversation })
      );
      set((s) => ({
        unreadByConv: { ...s.unreadByConv, [conversation]: 0 },
      }));
    } catch {
      // network error — the next fetchChannels tick will reconcile
    }
  },

  async sendChannelMessage(conversationId, content, mentions, attachments) {
    const conversationName = `conversations/${conversationId}`;
    const res = await commandServiceClient.sendMessage(
      create(SendMessageRequestSchema, {
        conversation: conversationName,
        content,
        mentions,
        attachments,
      })
    );
    const chatMsg: ChatMessageUI = toUiMessage(res);
    set((state) => ({
      chatMessages: {
        ...state.chatMessages,
        [conversationName]: [
          ...(state.chatMessages[conversationName] ?? []),
          chatMsg,
        ],
      },
    }));

    // Agent replies arrive asynchronously on the agent's bidi stream; the
    // frontend has no push channel, so the persistent watcher started by the
    // channel page polls listConversationMessages every 2s and surfaces them.
    // We deliberately do NOT start a second polling loop here (the old
    // pollChannelMessages ran a concurrent 30s/2s loop on top of the watcher —
    // a double poll hitting the same conversation every 2s per send).
    return res;
  },

  async fetchConversationActivity(conversationId) {
    const conversationName = conversationId.startsWith("conversations/")
      ? conversationId
      : `conversations/${conversationId}`;
    try {
      const res = await commandServiceClient.fetchConversationActivity(
        create(FetchConversationActivityRequestSchema, {
          conversation: conversationName,
        })
      );
      set((state) => ({
        agentActivities: {
          ...state.agentActivities,
          [conversationName]: res.activities ?? [],
        },
      }));
    } catch {
      // network error — will retry on next poll
    }
  },

  startWatchingChannel(conversationName) {
    // Already watching — avoid duplicate intervals (idempotent).
    if (get().channelWatchers[conversationName]) return;

    const poll = async () => {
      try {
        // Incremental fetch: ask only for messages with room_version strictly
        // after the last version we saw (captured by loadMessages). This returns
        // a small delta (usually 0–2 messages) instead of the whole history, so
        // large conversations no longer re-download and re-merge the entire list
        // every tick. When no cursor is set yet (e.g. loadMessages failed) this
        // falls back to afterVersion=0, which the backend serves as latest-N.
        const afterVersion = get().chatCurrentVersion[conversationName] ?? 0n;
        const res = await commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation: conversationName,
            pageSize: 200,
            pageToken: "",
            afterVersion,
          })
        );
        const delta: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
        const prev = get().chatMessages[conversationName] ?? [];
        // Append only messages we don't already have. This dedups the optimistic
        // send already in the list against its server echo, and returns the same
        // reference when nothing new arrived so subscribers bail out.
        const merged = appendNewMessages(prev, delta);
        const nextVersion = res.currentVersion;
        const prevVersion = get().chatCurrentVersion[conversationName] ?? 0n;
        if (merged !== prev || nextVersion !== prevVersion) {
          set((state) => ({
            chatMessages:
              merged !== prev
                ? {
                    ...state.chatMessages,
                    [conversationName]: merged,
                  }
                : state.chatMessages,
            chatCurrentVersion: {
              ...state.chatCurrentVersion,
              [conversationName]: nextVersion,
            },
          }));
        }
      } catch {
        // network error — will retry on next tick
      }

      // Refresh root-message reply-count badges. Thread replies are excluded
      // from the message delta above (server-side), so the watcher cannot
      // observe a changed reply count on its own. This separate summary poll
      // covers replies that arrive while the thread panel is closed — notably
      // an async agent reply to a thread the user has left.
      refreshChannelThreadCounts(set, get, conversationName);

      // Also poll agent activity.
      get().fetchConversationActivity(conversationName);
    };

    // Run immediately, then on interval. The interval handle lives in store
    // state so it is testable and stoppable across HMR.
    poll();
    const handle = setInterval(poll, WATCHER_POLL_INTERVAL_MS);
    set((state) => ({
      channelWatchers: { ...state.channelWatchers, [conversationName]: handle },
    }));
  },

  stopWatchingChannel(conversationName) {
    const handle = get().channelWatchers[conversationName];
    if (handle) {
      clearInterval(handle);
      set((state) => {
        const channelWatchers = { ...state.channelWatchers };
        delete channelWatchers[conversationName];
        return { channelWatchers };
      });
    }
  },

  async listChannelMembers(conversationId) {
    const convName = `conversations/${conversationId}`;
    set((s) => ({
      channelMembersLoading: { ...s.channelMembersLoading, [convName]: true },
    }));
    try {
      const res = await commandServiceClient.listChannelMembers(
        create(ListChannelMembersRequestSchema, { conversation: convName })
      );
      const members = res.members ?? [];
      set((s) => ({
        channelMembersByConv: {
          ...s.channelMembersByConv,
          [convName]: members,
        },
        channelMembersLoading: {
          ...s.channelMembersLoading,
          [convName]: false,
        },
      }));
      return members;
    } catch {
      set((s) => ({
        channelMembersLoading: {
          ...s.channelMembersLoading,
          [convName]: false,
        },
      }));
      return [];
    }
  },

  async addChannelMember(conversationId, memberType, memberId) {
    const convName = `conversations/${conversationId}`;
    const newMember = await commandServiceClient.addChannelMember(
      create(AddChannelMemberRequestSchema, {
        conversation: convName,
        memberType,
        memberId,
      })
    );
    set((s) => ({
      channelMembersByConv: {
        ...s.channelMembersByConv,
        [convName]: [...(s.channelMembersByConv[convName] ?? []), newMember],
      },
    }));
    return newMember;
  },

  async removeChannelMember(conversationId, memberType, memberId) {
    const convName = `conversations/${conversationId}`;
    await commandServiceClient.removeChannelMember(
      create(RemoveChannelMemberRequestSchema, {
        conversation: convName,
        memberType,
        memberId,
      })
    );
    set((s) => ({
      channelMembersByConv: {
        ...s.channelMembersByConv,
        [convName]: (s.channelMembersByConv[convName] ?? []).filter(
          (m) => !(m.memberType === memberType && m.memberId === memberId)
        ),
      },
    }));
  },
});

// refreshChannelThreadCounts fetches the channel's active-thread summaries and
// writes each root's total reply count back into the main message list, so the
// "N replies" badge on root messages stays fresh. The message watcher's delta
// (above) excludes thread replies, so without this a reply that lands while the
// thread panel is closed — e.g. an async agent reply — would never update the
// badge. No-op when the list is empty or no root's count changed (same-reference
// bail-out so subscribers don't churn). Failures are swallowed and retried next
// tick; they must not abort the surrounding poll.
async function refreshChannelThreadCounts(
  set: Parameters<AppSliceCreator<ChannelSlice>>[0],
  get: Parameters<AppSliceCreator<ChannelSlice>>[1],
  conversationName: string
) {
  const prev = get().chatMessages[conversationName];
  if (!prev || prev.length === 0) return;
  let threads: { rootMessage: string; replyCount: number }[];
  try {
    const res = await commandServiceClient.listChannelThreads(
      create(ListChannelThreadsRequestSchema, {
        conversation: conversationName,
      })
    );
    threads = (res.threads ?? []).map((t) => ({
      rootMessage: t.rootMessage,
      replyCount: t.replyCount,
    }));
  } catch {
    return; // network error — retry next tick
  }
  const countById = new Map(threads.map((t) => [t.rootMessage, t.replyCount]));
  let changed = false;
  const next = prev.map((m) => {
    if (m.threadRoot) return m; // replies never carry the badge
    const count = countById.get(m.id) ?? 0;
    if ((m.threadReplyCount ?? 0) === count) return m;
    changed = true;
    return { ...m, threadReplyCount: count };
  });
  if (changed) {
    set((state) => ({
      chatMessages: { ...state.chatMessages, [conversationName]: next },
    }));
  }
}
