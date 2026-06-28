import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  AddChannelMemberRequestSchema,
  CreateChannelRequestSchema,
  FetchConversationActivityRequestSchema,
  ListChannelMembersRequestSchema,
  ListChannelsRequestSchema,
  ListConversationMessagesRequestSchema,
  RemoveChannelMemberRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { mergeMessages, toUiMessage } from "./chat";
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
  channelWatchers: {},

  async fetchChannels() {
    set({ channelsLoading: true });
    try {
      const res = await commandServiceClient.listChannels(
        create(ListChannelsRequestSchema, { pageSize: 100, pageToken: "" })
      );
      set({ channels: res.channels ?? [], channelsLoading: false });
    } catch {
      set({ channelsLoading: false });
    }
  },

  async createChannel(title) {
    const res = await commandServiceClient.createChannel(
      create(CreateChannelRequestSchema, { title })
    );
    set({ channels: [...get().channels, res] });
    return res;
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
        const res = await commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation: conversationName,
            pageSize: 200,
            pageToken: "",
          })
        );
        const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);
        // Reuse cached references for unchanged messages and skip the store
        // update entirely when nothing changed, so polling does not churn the
        // array identity (which would force a scroll snap and re-render).
        const prev = get().chatMessages[conversationName] ?? [];
        const merged = mergeMessages(prev, uiMsgs);
        if (merged !== prev) {
          set((state) => ({
            chatMessages: {
              ...state.chatMessages,
              [conversationName]: merged,
            },
          }));
        }
      } catch {
        // network error — will retry on next tick
      }

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
