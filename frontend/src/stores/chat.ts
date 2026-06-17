import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { commandServiceClient } from "@/connect";
import {
  CommandSource,
  GetOrCreateConversationRequestSchema,
  ListConversationMessagesRequestSchema,
  SendCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

export const createChatSlice: AppSliceCreator<ChatSlice> = (set, get) => ({
  conversations: {},
  chatMessages: [],
  chatLoading: false,

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
    set({ chatLoading: true });
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
        commandName: msg.commandId
          ? `agents/__/commands/${msg.commandId}`
          : undefined,
      }));

      set({ chatMessages: uiMsgs, chatLoading: false });
    } catch {
      set({ chatLoading: false });
    }
  },

  async sendChatMessage(agent, instruction) {
    const tempId = crypto.randomUUID();
    const userMsg: ChatMessageUI = {
      id: tempId,
      role: "user",
      content: instruction,
      timestamp: new Date(),
    };
    set((state) => ({
      chatMessages: [...state.chatMessages, userMsg],
    }));

    const res = await commandServiceClient.sendCommand(
      create(SendCommandRequestSchema, {
        agent,
        command: instruction,
        instruction,
        executorKind: 2,
        source: CommandSource.CHAT,
      })
    );

    return res;
  },
});
