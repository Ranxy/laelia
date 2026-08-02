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
      // When a previous load (or the watcher) already populated this
      // conversation, fetch only messages newer than the cached cursor so
      // switching A→B→A does not re-download the whole 200-message history.
      // A first load (version 0) still returns the latest N messages.
      const afterVersion = get().chatCurrentVersion[conversation] ?? 0n;
      const res = await commandServiceClient.listConversationMessages(
        create(ListConversationMessagesRequestSchema, {
          conversation,
          pageSize: 200,
          pageToken: "",
          afterVersion,
        })
      );

      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);

      set((state) => {
        const prev = state.chatMessages[conversation] ?? [];
        // Merge rather than replace so a re-entry cannot wipe rows the watcher
        // appended or an optimistic send is still awaiting its echo.
        const merged = appendNewMessages(prev, uiMsgs);
        return {
          chatMessages: { ...state.chatMessages, [conversation]: merged },
          chatCurrentVersion: {
            ...state.chatCurrentVersion,
            [conversation]: res.currentVersion,
          },
          chatLoading: { ...state.chatLoading, [conversation]: false },
        };
      });
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
