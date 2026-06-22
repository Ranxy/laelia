import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { commandServiceClient } from "@/connect";
import {
  AddChannelMemberRequestSchema,
  CommandEventType,
  CommandSource,
  CommandStatus,
  CreateChannelRequestSchema,
  GetOrCreateConversationRequestSchema,
  ListChannelMembersRequestSchema,
  ListChannelsRequestSchema,
  ListConversationMessagesRequestSchema,
  RemoveChannelMemberRequestSchema,
  SendCommandRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

export const createChatSlice: AppSliceCreator<ChatSlice> = (set, get) => ({
  conversations: {},
  channels: [],
  channelsLoading: false,
  channelMembersByConv: {},
  channelMembersLoading: {},
  chatMessages: {},
  chatLoading: {},
  streamingContent: {},
  streamingEvents: {},
  streamingStatus: {},

  async getOrCreateConversation(agent) {
    const existing = get().conversations[agent];
    if (existing) return existing;

    const res = await commandServiceClient.getOrCreateConversation(
      create(GetOrCreateConversationRequestSchema, { agent })
    );
    set((state) => ({
      conversations: { ...state.conversations, [agent]: res.name },
    }));
    return res.name;
  },

  async loadMessages(conversation) {
    set((state) => ({
      chatLoading: { ...state.chatLoading, [conversation]: true },
    }));
    try {
      const res = await commandServiceClient.listConversationMessages(
        create(ListConversationMessagesRequestSchema, {
          conversation,
          pageSize: 200,
          pageToken: "",
        })
      );

      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map((msg) => ({
        id: msg.name,
        role: msg.role === 1 ? "user" : "assistant",
        content: msg.content,
        timestamp: msg.createdAt ? timestampDate(msg.createdAt) : new Date(),
        commandId: msg.commandId || undefined,
        senderName: msg.senderName || undefined,
        senderType: msg.senderType || undefined,
      }));

      set((state) => ({
        chatMessages: { ...state.chatMessages, [conversation]: uiMsgs },
        chatLoading: { ...state.chatLoading, [conversation]: false },
      }));
    } catch {
      set((state) => ({
        chatLoading: { ...state.chatLoading, [conversation]: false },
      }));
    }
  },

  async sendChatMessage(agent, instruction, conversationId) {
    const tempId = crypto.randomUUID();
    const conversation = conversationId || get().conversations[agent];
    const userMsg: ChatMessageUI = {
      id: tempId,
      role: "user",
      content: instruction,
      timestamp: new Date(),
    };
    if (conversation) {
      set((state) => ({
        chatMessages: {
          ...state.chatMessages,
          [conversation]: [
            ...(state.chatMessages[conversation] ?? []),
            userMsg,
          ],
        },
      }));
    }

    const res = await commandServiceClient.sendCommand(
      create(SendCommandRequestSchema, {
        agent,
        command: instruction,
        instruction,
        executorKind: 2,
        source: CommandSource.CHAT,
        conversationId: conversationId || "",
      })
    );

    if (conversation && res.name) {
      const assistantMsg: ChatMessageUI = {
        id: `assistant-${res.name}`,
        role: "assistant",
        content: "",
        timestamp: new Date(),
        commandName: res.name,
        commandId: res.name.split("/").pop(),
        status: res.status,
        streaming: true,
        events: [],
      };
      set((state) => ({
        chatMessages: {
          ...state.chatMessages,
          [conversation]: [
            ...(state.chatMessages[conversation] ?? []),
            assistantMsg,
          ],
        },
        streamingContent: { ...state.streamingContent, [res.name]: "" },
        streamingEvents: { ...state.streamingEvents, [res.name]: [] },
        streamingStatus: { ...state.streamingStatus, [res.name]: res.status },
      }));
    }

    return res;
  },

  async streamChatCommand(commandName, conversation, signal) {
    const state = get();
    const existing = state.streamingEvents[commandName];
    const afterSeqNo =
      existing && existing.length > 0
        ? existing[existing.length - 1].seqNo
        : -1;

    const stream = commandServiceClient.watchCommandEvents(
      { name: commandName, afterSeqNo },
      { signal }
    );

    try {
      for await (const event of stream) {
        if (signal?.aborted) break;

        const s = get();
        const prevEvents = s.streamingEvents[commandName] ?? [];
        const nextEvents = [...prevEvents, event];

        if (
          event.type === CommandEventType.TEXT_DELTA &&
          event.payload.case === "textDelta"
        ) {
          const prevContent = s.streamingContent[commandName] ?? "";
          const nextContent = prevContent + event.payload.value.content;
          set({
            streamingContent: {
              ...s.streamingContent,
              [commandName]: nextContent,
            },
            streamingEvents: {
              ...s.streamingEvents,
              [commandName]: nextEvents,
            },
          });
        } else if (event.type === CommandEventType.FINAL_SUMMARY) {
          set({
            streamingEvents: {
              ...s.streamingEvents,
              [commandName]: nextEvents,
            },
            streamingStatus: {
              ...s.streamingStatus,
              [commandName]: CommandStatus.COMPLETED,
            },
          });
        } else {
          set({
            streamingEvents: {
              ...s.streamingEvents,
              [commandName]: nextEvents,
            },
          });
        }
      }
    } catch {
      // Stream cancelled or network error
    }

    // Collect final streaming state before cleaning up
    const s = get();
    const finalEvents = s.streamingEvents[commandName] ?? [];
    const finalStatus =
      s.streamingStatus[commandName] ?? CommandStatus.COMPLETED;
    const streamedContent = s.streamingContent[commandName] ?? "";

    // Clean up streaming state
    const { [commandName]: _c, ...restContent } = s.streamingContent;
    const { [commandName]: _e, ...restEvents } = s.streamingEvents;
    const { [commandName]: _s, ...restStatus } = s.streamingStatus;

    // Mark the assistant message as not streaming, preserving events
    const messages = s.chatMessages[conversation] ?? [];
    const updated = messages.map((m) =>
      m.commandName === commandName
        ? {
            ...m,
            streaming: false,
            content: streamedContent,
            events: finalEvents,
            status: finalStatus,
          }
        : m
    );
    set({
      chatMessages: { ...s.chatMessages, [conversation]: updated },
      streamingContent: restContent,
      streamingEvents: restEvents,
      streamingStatus: restStatus,
    });

    // If aborted (user clicked Stop), skip backend reload
    if (signal?.aborted) return;

    // Reload from backend to get the actual final assistant text.
    // The assistant's response text is stored server-side via FinalSummary
    // which may not arrive via TEXT_DELTA events during streaming.
    // Retry a few times because the backend may not have persisted the
    // assistant message yet when the event stream closes.
    const cmdId = commandName.split("/").pop();
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const res = await commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation,
            pageSize: 200,
            pageToken: "",
          })
        );

        const s2 = get();
        const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map((msg) => ({
          id: msg.name,
          role: (msg.role === 1 ? "user" : "assistant") as "user" | "assistant",
          content: msg.content,
          timestamp: msg.createdAt ? timestampDate(msg.createdAt) : new Date(),
          commandId: msg.commandId || undefined,
        }));

        // Check if the assistant message for this command exists yet
        const assistantMsg = uiMsgs.find((m) => m.commandId === cmdId);
        if (assistantMsg && assistantMsg.content) {
          // Merge streaming events back into the reloaded assistant message
          const merged = uiMsgs.map((m) => {
            if (m.commandId === cmdId) {
              return {
                ...m,
                commandName,
                events: finalEvents,
                status: finalStatus,
                content: m.content || streamedContent,
              };
            }
            return m;
          });
          set({ chatMessages: { ...s2.chatMessages, [conversation]: merged } });
          return;
        }

        // Backend hasn't persisted the assistant message yet, wait and retry
        if (attempt < 2) {
          await new Promise((r) => setTimeout(r, 500));
        }
      } catch {
        // If reload fails, keep streamed content + events
        return;
      }
    }
  },

  resetStreaming(commandName) {
    const s = get();
    const finalEvents = s.streamingEvents[commandName] ?? [];
    const finalContent = s.streamingContent[commandName] ?? "";

    const { [commandName]: _c, ...restContent } = s.streamingContent;
    const { [commandName]: _e, ...restEvents } = s.streamingEvents;
    const { [commandName]: _s, ...restStatus } = s.streamingStatus;

    const chatMessages = { ...s.chatMessages };
    for (const [conv, msgs] of Object.entries(chatMessages)) {
      chatMessages[conv] = msgs.map((m) =>
        m.commandName === commandName
          ? {
              ...m,
              streaming: false,
              content: finalContent,
              events: finalEvents,
            }
          : m
      );
    }

    set({
      chatMessages,
      streamingContent: restContent,
      streamingEvents: restEvents,
      streamingStatus: restStatus,
    });
  },

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
    const channels = [...get().channels, res];
    set({ channels });
    return res;
  },

  async sendChannelMessage(conversationId, content) {
    const conversationName = `conversations/${conversationId}`;
    const res = await commandServiceClient.sendMessage(
      create(SendMessageRequestSchema, {
        conversation: conversationName,
        content,
      })
    );
    const chatMsg: ChatMessageUI = {
      id: res.name,
      role: "user",
      content: res.content,
      timestamp: res.createdAt ? timestampDate(res.createdAt) : new Date(),
      senderName: res.senderName || undefined,
      senderType: res.senderType || undefined,
    };
    set((state) => ({
      chatMessages: {
        ...state.chatMessages,
        [conversationName]: [
          ...(state.chatMessages[conversationName] ?? []),
          chatMsg,
        ],
      },
    }));
    return res;
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
