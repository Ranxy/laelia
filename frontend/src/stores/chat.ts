import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  AddReactionRequestSchema,
  GetOrCreateConversationRequestSchema,
  GetOrCreateUserUserDMRequestSchema,
  ListConversationMessagesRequestSchema,
  RemoveReactionRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat-helpers";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

// Re-export so existing `./chat` imports of these helpers keep working.
export { appendNewMessages, toUiMessage } from "./chat-helpers";

// Max pages the incremental delta fetch will follow. The backend caps each
// after_version page at 100 (pageSize 200 is clamped), so a burst of >100 new
// messages needs pagination to avoid dropping the newest ones.
const MAX_DELTA_PAGES = 10;

// fetchConversationDelta pulls the full incremental delta for a conversation
// (all messages with room_version > afterVersion), following nextPageToken so a
// >pageSize burst is not truncated. Returns the cursor to advance to: the
// server's current version on a complete read, or the last fetched message's
// room_version when the delta was too big to finish in one pass — so the next
// poll continues the catch-up instead of permanently skipping the remainder.
//
// opts.waitMs turns the first page into a long poll: when no new messages
// exist the server holds the request until one lands or waitMs elapses, then
// returns the empty delta with the current version. Pagination pages never
// carry waitMs (they must drain a burst without holding the connection).
// opts.signal aborts the in-flight request (used by the watcher loops).
export async function fetchConversationDelta(
  conversation: string,
  afterVersion: bigint,
  opts?: { waitMs?: number; signal?: AbortSignal }
): Promise<{ uiMsgs: ChatMessageUI[]; currentVersion: bigint }> {
  let pageToken = "";
  let uiMsgs: ChatMessageUI[] = [];
  let currentVersion = afterVersion;
  for (let page = 0; page < MAX_DELTA_PAGES; page++) {
    const res = await commandServiceClient.listConversationMessages(
      create(ListConversationMessagesRequestSchema, {
        conversation,
        pageSize: 200,
        pageToken,
        afterVersion,
        waitMs: page === 0 ? (opts?.waitMs ?? 0) : 0,
      }),
      opts?.signal ? { signal: opts.signal } : undefined
    );
    currentVersion = res.currentVersion;
    const pageMsgs = (res.messages ?? []).map(toUiMessage);
    if (pageMsgs.length > 0) uiMsgs = uiMsgs.concat(pageMsgs);
    const next = res.nextPageToken ?? "";
    if (!next) return { uiMsgs, currentVersion };
    pageToken = next;
  }
  // More pages remain past the cap — advance only to the last message actually
  // received so a later poll continues from here.
  const last = uiMsgs[uiMsgs.length - 1];
  return { uiMsgs, currentVersion: last?.roomVersion ?? currentVersion };
}

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
      // Follows nextPageToken so a >100-message burst while away is not
      // truncated (the newest messages were previously dropped permanently).
      const { uiMsgs, currentVersion } = await fetchConversationDelta(
        conversation,
        afterVersion
      );

      set((state) => {
        const prev = state.chatMessages[conversation] ?? [];
        // Merge rather than replace so a re-entry cannot wipe rows the watcher
        // appended or an optimistic send is still awaiting its echo.
        const merged = appendNewMessages(prev, uiMsgs);
        return {
          chatMessages: { ...state.chatMessages, [conversation]: merged },
          chatCurrentVersion: {
            ...state.chatCurrentVersion,
            [conversation]: currentVersion,
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

  async toggleReaction(conversation, messageId, emoji) {
    const message = `${conversation}/messages/${messageId}`;
    const msg = get().chatMessages[conversation]?.find(
      (m) => m.id === messageId
    );
    // `reacted` is caller-relative (computed server-side for the current
    // user), so toggling is: remove if I already reacted, else add.
    const reacted = msg?.reactions?.find((r) => r.emoji === emoji)?.reacted;
    const res = reacted
      ? await commandServiceClient.removeReaction(
          create(RemoveReactionRequestSchema, { message, emoji })
        )
      : await commandServiceClient.addReaction(
          create(AddReactionRequestSchema, { message, emoji })
        );
    const reactions = res.reactions;
    set((state) => ({
      chatMessages: {
        ...state.chatMessages,
        [conversation]: (state.chatMessages[conversation] ?? []).map((m) =>
          m.id === messageId ? { ...m, reactions } : m
        ),
      },
    }));
  },
});
