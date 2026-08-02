import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  GetOrCreateConversationRequestSchema,
  GetOrCreateUserUserDMRequestSchema,
  ListConversationMessagesRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat-helpers";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

// Re-export so existing `./chat` imports of these helpers keep working.
export { appendNewMessages, toUiMessage } from "./chat-helpers";

export const createChatSlice: AppSliceCreator<ChatSlice> = (set, get) => ({
  conversations: {},
  chatMessages: {},
  chatLoading: {},
  chatCurrentVersion: {},
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

  async getOrCreateUserUserDM(peerUser) {
    const res = await commandServiceClient.getOrCreateUserUserDM(
      create(GetOrCreateUserUserDMRequestSchema, { peerUser })
    );
    return res.name;
  },

  async loadMessages(conversation) {
    set((state) => ({
      chatLoading: { ...state.chatLoading, [conversation]: true },
    }));
    try {
      // No version filter: the backend returns the latest N messages in
      // chronological order (newest at the bottom), plus the conversation's
      // current_version. We cache that version so the channel watcher can poll
      // incrementally (after_version) instead of re-fetching the whole list.
      const res = await commandServiceClient.listConversationMessages(
        create(ListConversationMessagesRequestSchema, {
          conversation,
          pageSize: 200,
          pageToken: "",
        })
      );

      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);

      set((state) => ({
        chatMessages: { ...state.chatMessages, [conversation]: uiMsgs },
        chatCurrentVersion: {
          ...state.chatCurrentVersion,
          [conversation]: res.currentVersion,
        },
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

    const res = await commandServiceClient.sendMessage(
      create(SendMessageRequestSchema, {
        conversation: conversation || "",
        content: instruction,
      })
    );
    if (conversation) {
      // Reconcile the optimistic placeholder with the server echo: remove the
      // temp row and add the committed message (deduped by id). Without this,
      // the channel watcher's append-by-id dedup sees the echo as a new message
      // (different id than the placeholder) and the DM shows the user's
      // instruction twice.
      set((state) => {
        const current = state.chatMessages[conversation] ?? [];
        const withoutTemp = current.filter((m) => m.id !== tempId);
        const merged = appendNewMessages(withoutTemp, [toUiMessage(res)]);
        return {
          chatMessages: { ...state.chatMessages, [conversation]: merged },
        };
      });
    }
    return res;
  },
});
