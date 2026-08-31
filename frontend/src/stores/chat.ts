import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import type { ChatMessage } from "@/types/proto-es/v1/command_pb";
import {
  AddReactionRequestSchema,
  GetOrCreateConversationRequestSchema,
  GetOrCreateUserUserDMRequestSchema,
  ListConversationMessagesRequestSchema,
  RemoveReactionRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat-helpers";
import { registerCleanup } from "./cleanup-registry";
import type { AppSliceCreator } from "./types";
import type { ChatMessageUI } from "./ui-models";

export interface ChatSlice {
  chatMessages: Record<string, ChatMessageUI[]>;
  chatLoading: Record<string, boolean>;
  // Last-seen conversation.current_version per conversation, keyed by
  // conversation name. Captured by loadMessages and advanced by the channel
  // watcher; used as the afterVersion cursor so the watcher polls only for
  // messages newer than what it has already seen instead of re-fetching the
  // whole list every tick.
  chatCurrentVersion: Record<string, bigint>;
  // Bidirectional history-window state, shared by the normal latest window
  // and a file-drawer jump window. chatJumpByConv holds the active jump anchor
  // (null in the normal window); chatHasOlder/Newer tell the UI whether more
  // history is available in each direction for incremental loading.
  // chatJumpLoading guards the in-flight page load for either direction (the
  // name is historical).
  chatJumpByConv: Record<
    string,
    { messageId: string; roomVersion: bigint } | null
  >;
  chatJumpLoading: Record<string, boolean>;
  chatHasOlderByConv: Record<string, boolean>;
  chatHasNewerByConv: Record<string, boolean>;

  getOrCreateConversation: (agent: string) => Promise<string>;
  // getOrCreateUserUserDM opens (or reuses) the 1:1 DM between the calling
  // user and a peer user (resource name "users/{id}"). Returns the
  // conversation name "conversations/{id}".
  getOrCreateUserUserDM: (peerUser: string) => Promise<string>;
  loadMessages: (conversation: string) => Promise<void>;
  sendChatMessage: (
    agent: string,
    instruction: string,
    conversationId?: string
  ) => Promise<ChatMessage>;
  // toggleReaction adds (or, if the caller already reacted, removes) the
  // caller's emoji reaction on a message in a conversation, then updates the
  // local message's reactions from the server's response. Lightweight: it
  // never bumps the room version or wakes agents.
  toggleReaction: (
    conversation: string,
    messageId: string,
    emoji: string
  ) => Promise<void>;
  // jumpToMessage replaces the conversation's message list with a focused
  // window around the given message (the file's carrying position) so the user
  // can jump to an old message without loading the whole history. Older/newer
  // pages are loaded incrementally via loadOlderMessages / loadNewerMessages.
  jumpToMessage: (
    conversation: string,
    messageId: string,
    roomVersion: bigint
  ) => Promise<void>;
  loadOlderMessages: (conversation: string) => Promise<void>;
  loadNewerMessages: (conversation: string) => Promise<void>;
  // clearJump exits jump mode and reloads the latest messages for the
  // conversation.
  clearJump: (conversation: string) => Promise<void>;

  // Optimistic message mutations for the chat composer (top-level channel/DM
  // sends). append/patch/remove keep the slice's invariants (id dedup via
  // appendNewMessages, same-reference bail-outs) out of the component: the
  // composer calls these instead of inlining useAppStore.setState surgery.
  appendChatMessage: (conversation: string, msg: ChatMessageUI) => void;
  patchChatMessage: (
    conversation: string,
    messageId: string,
    patch: Partial<ChatMessageUI>
  ) => void;
  removeChatMessage: (conversation: string, messageId: string) => void;
}

// Agent→conversation cache backing getOrCreateConversation/sendChatMessage.
// Deliberately module-scoped instead of store state: nothing subscribes to it,
// so it exists only to skip repeat getOrCreateConversation RPCs. Cleared on
// logout/reset via the cleanup registry (registration at the file bottom).
const conversationIdByAgent: Record<string, string> = {};

// Max pages the incremental delta fetch will follow. The backend caps each
// after_version page at 100 (pageSize 200 is clamped), so a burst of >100 new
// messages needs pagination to avoid dropping the newest ones.
const MAX_DELTA_PAGES = 10;

// The backend clamps ListConversationMessages page_size to 100. A latest-N
// load that returns a full page may therefore still have older history.
const LATEST_PAGE_SIZE = 100;

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

// mergeMessages dedupes by id and sorts by room_version ascending. Used to
// assemble a focused jump window (older + target + newer) and to prepend/append
// incremental pages while the user scrolls.
function mergeMessages(msgs: ChatMessageUI[]): ChatMessageUI[] {
  const byId = new Map<string, ChatMessageUI>();
  for (const m of msgs) byId.set(m.id, m);
  return [...byId.values()].sort((a, b) =>
    Number((a.roomVersion ?? 0n) - (b.roomVersion ?? 0n))
  );
}

export const createChatSlice: AppSliceCreator<ChatSlice> = (set, get) => ({
  chatMessages: {},
  chatLoading: {},
  chatCurrentVersion: {},
  chatJumpByConv: {},
  chatJumpLoading: {},
  chatHasOlderByConv: {},
  chatHasNewerByConv: {},
  async getOrCreateConversation(agent) {
    const existing = conversationIdByAgent[agent];
    if (existing) return existing;

    const res = await commandServiceClient.getOrCreateConversation(
      create(GetOrCreateConversationRequestSchema, { agent })
    );
    conversationIdByAgent[agent] = res.name;
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
      // A first load (version 0) returns the latest N messages and establishes
      // the bidirectional window: hasOlder is inferred from a full latest page,
      // and hasNewer is false because the window ends at the live tail.
      const isLatestLoad = afterVersion === 0n;
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
          ...(isLatestLoad
            ? {
                chatHasOlderByConv: {
                  ...state.chatHasOlderByConv,
                  [conversation]: uiMsgs.length >= LATEST_PAGE_SIZE,
                },
                chatHasNewerByConv: {
                  ...state.chatHasNewerByConv,
                  [conversation]: false,
                },
              }
            : {}),
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
    const conversation = conversationId || conversationIdByAgent[agent];
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

  async jumpToMessage(conversation, messageId, roomVersion) {
    set((state) => ({
      chatJumpLoading: { ...state.chatJumpLoading, [conversation]: true },
    }));
    try {
      const pageSize = 30;
      // Fetch older, newer, and the target itself in parallel for a snappy jump.
      const [beforeRes, afterRes, targetRes] = await Promise.all([
        commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation,
            pageSize,
            beforeVersion: roomVersion,
          })
        ),
        commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation,
            pageSize,
            afterVersion: roomVersion,
          })
        ),
        // The target itself: the latest message strictly before roomVersion+1.
        commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation,
            pageSize: 1,
            beforeVersion: roomVersion + 1n,
          })
        ),
      ]);
      const before = (beforeRes.messages ?? []).map(toUiMessage);
      const after = (afterRes.messages ?? []).map(toUiMessage);
      const target = (targetRes.messages ?? [])
        .map(toUiMessage)
        .find((m) => m.id === messageId);
      const merged = mergeMessages([
        ...before,
        ...(target ? [target] : []),
        ...after,
      ]);
      // Keep the watcher cursor at the conversation's real latest version, not
      // the newest message in this focused window. The window may have unloaded
      // newer pages; pointing the cursor at the window edge would make the
      // watcher drain all of them in one burst and defeat incremental loading.
      const currentVersion = afterRes.currentVersion;
      set((state) => ({
        chatMessages: { ...state.chatMessages, [conversation]: merged },
        chatCurrentVersion: {
          ...state.chatCurrentVersion,
          [conversation]: currentVersion,
        },
        chatJumpByConv: {
          ...state.chatJumpByConv,
          [conversation]: { messageId, roomVersion },
        },
        chatHasOlderByConv: {
          ...state.chatHasOlderByConv,
          [conversation]: before.length >= pageSize,
        },
        chatHasNewerByConv: {
          ...state.chatHasNewerByConv,
          [conversation]: after.length >= pageSize,
        },
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
    } catch (err) {
      set((state) => ({
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
      throw err;
    }
  },

  async loadOlderMessages(conversation) {
    if (get().chatJumpLoading[conversation]) return;
    const msgs = get().chatMessages[conversation] ?? [];
    if (msgs.length === 0) return;
    const beforeVersion = msgs[0].roomVersion ?? 0n;
    // The jump anchor doubles as a window generation: jumpToMessage replaces
    // this object and clearJump nulls it, so an in-flight page can detect that
    // the window it was loading for no longer exists.
    const jumpAnchor = get().chatJumpByConv[conversation];
    const pageSize = 30;
    set((state) => ({
      chatJumpLoading: { ...state.chatJumpLoading, [conversation]: true },
    }));
    try {
      const res = await commandServiceClient.listConversationMessages(
        create(ListConversationMessagesRequestSchema, {
          conversation,
          pageSize,
          beforeVersion,
        })
      );
      const older = (res.messages ?? []).map(toUiMessage);
      if (get().chatJumpByConv[conversation] !== jumpAnchor) {
        set((state) => ({
          chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
        }));
        return;
      }
      // Re-read after the await: the watcher or an optimistic send may have
      // appended messages while the page was in flight, and merging onto the
      // stale snapshot would drop them.
      const current = get().chatMessages[conversation] ?? [];
      const merged = mergeMessages([...older, ...current]);
      set((state) => ({
        chatMessages: { ...state.chatMessages, [conversation]: merged },
        chatHasOlderByConv: {
          ...state.chatHasOlderByConv,
          [conversation]: older.length >= pageSize,
        },
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
    } catch {
      set((state) => ({
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
    }
  },

  async loadNewerMessages(conversation) {
    if (get().chatJumpLoading[conversation]) return;
    const msgs = get().chatMessages[conversation] ?? [];
    if (msgs.length === 0) return;
    const afterVersion = msgs[msgs.length - 1].roomVersion ?? 0n;
    const jumpAnchor = get().chatJumpByConv[conversation];
    const pageSize = 30;
    set((state) => ({
      chatJumpLoading: { ...state.chatJumpLoading, [conversation]: true },
    }));
    try {
      const res = await commandServiceClient.listConversationMessages(
        create(ListConversationMessagesRequestSchema, {
          conversation,
          pageSize,
          afterVersion,
        })
      );
      const newer = (res.messages ?? []).map(toUiMessage);
      if (get().chatJumpByConv[conversation] !== jumpAnchor) {
        set((state) => ({
          chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
        }));
        return;
      }
      // Re-read after the await for the same reason as loadOlderMessages.
      const current = get().chatMessages[conversation] ?? [];
      const merged = mergeMessages([...current, ...newer]);
      set((state) => ({
        chatMessages: { ...state.chatMessages, [conversation]: merged },
        chatHasNewerByConv: {
          ...state.chatHasNewerByConv,
          [conversation]: newer.length >= pageSize,
        },
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
    } catch {
      set((state) => ({
        chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      }));
    }
  },

  async clearJump(conversation) {
    // Reset the message list and cursor so the next loadMessages fetches the
    // latest page instead of merging onto the focused jump window.
    set((state) => ({
      chatMessages: { ...state.chatMessages, [conversation]: [] },
      chatCurrentVersion: { ...state.chatCurrentVersion, [conversation]: 0n },
      chatJumpByConv: { ...state.chatJumpByConv, [conversation]: null },
      chatJumpLoading: { ...state.chatJumpLoading, [conversation]: false },
      chatHasOlderByConv: {
        ...state.chatHasOlderByConv,
        [conversation]: false,
      },
      chatHasNewerByConv: {
        ...state.chatHasNewerByConv,
        [conversation]: false,
      },
    }));
    await get().loadMessages(conversation);
  },

  // appendChatMessage appends an optimistic composer message to the
  // conversation (id-deduped so a watcher echo racing the append can't
  // duplicate it) and keeps the append a same-reference no-op when the
  // message is already present. Shared by the channel and DM composers via
  // the shared useChatComposer pipeline — components no longer inline
  // useAppStore.setState surgery.
  appendChatMessage(conversation, msg) {
    set((state) => {
      const prev = state.chatMessages[conversation] ?? [];
      const merged = appendNewMessages(prev, [msg]);
      if (merged === prev) return {};
      return {
        chatMessages: { ...state.chatMessages, [conversation]: merged },
      };
    });
  },

  // patchChatMessage replaces one message row with {...row, ...patch}, only
  // when at least one patched key differs — repeated no-op patches (upload
  // progress ticks landing on an already-updated row) keep the row reference
  // stable so subscribers bail out.
  patchChatMessage(conversation, messageId, patch) {
    set((state) => {
      const list = state.chatMessages[conversation];
      if (!list) return {};
      const idx = list.findIndex((m) => m.id === messageId);
      if (idx < 0) return {};
      const changed = Object.keys(patch).some(
        (k) =>
          list[idx][k as keyof ChatMessageUI] !==
          patch[k as keyof ChatMessageUI]
      );
      if (!changed) return {};
      const updated = [...list];
      updated[idx] = { ...list[idx], ...patch };
      return {
        chatMessages: { ...state.chatMessages, [conversation]: updated },
      };
    });
  },

  // removeChatMessage drops one message row (the optimistic placeholder on a
  // failed send or an empty send). No-op when the row is already gone.
  removeChatMessage(conversation, messageId) {
    set((state) => {
      const list = state.chatMessages[conversation];
      if (!list) return {};
      const filtered = list.filter((m) => m.id !== messageId);
      if (filtered.length === list.length) return {};
      return {
        chatMessages: { ...state.chatMessages, [conversation]: filtered },
      };
    });
  },
});

// Registered so logout/reset clears the module agent→conversation cache
// (contract: synchronous and idempotent — restart from a fresh RPC after
// reset; sweeping keys on every reset is both).
registerCleanup(() => {
  for (const agent of Object.keys(conversationIdByAgent)) {
    delete conversationIdByAgent[agent];
  }
});
