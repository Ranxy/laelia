import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  CommandEventType,
  CommandStatus,
  GetOrCreateConversationRequestSchema,
  ListConversationMessagesRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import {
  abortableSleep,
  finalizeAssistant,
  lastEventSeqNo,
  omitKey,
  toUiMessage,
} from "./chat-helpers";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

// Re-export so existing `./chat` imports of these helpers keep working.
export { mergeMessages, toUiMessage } from "./chat-helpers";

// Bound reconnect attempts after a transient stream error so a permanently
// dead command does not retry forever.
const STREAM_MAX_RETRIES = 5;
const STREAM_BACKOFF_MS = [500, 1000, 2000, 4000, 8000];

export const createChatSlice: AppSliceCreator<ChatSlice> = (set, get) => ({
  conversations: {},
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

      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map(toUiMessage);

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

    const res = await commandServiceClient.sendMessage(
      create(SendMessageRequestSchema, {
        conversation: conversation || "",
        content: instruction,
      })
    );

    const commandName = res.commandId
      ? `${agent}/commands/${res.commandId}`
      : undefined;

    if (conversation && commandName) {
      const commandId = res.commandId || undefined;
      const assistantMsg: ChatMessageUI = {
        id: `assistant-${commandName}`,
        role: "assistant",
        content: "",
        timestamp: new Date(),
        commandName,
        commandId,
        status: 1, // PENDING
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
        streamingContent: { ...state.streamingContent, [commandName]: "" },
        streamingEvents: { ...state.streamingEvents, [commandName]: [] },
        streamingStatus: { ...state.streamingStatus, [commandName]: 1 },
      }));
    }
    return res;
  },

  async streamChatCommand(commandName, conversation, signal) {
    // Reconnect loop: on a transient stream error, replay from the last
    // received event seq with bounded backoff. afterSeqNo is re-read each
    // attempt so the server replays only what we don't yet have.
    for (let attempt = 0; ; attempt++) {
      const afterSeqNo = lastEventSeqNo(get().streamingEvents[commandName]);
      const stream = commandServiceClient.watchCommandEvents(
        { name: commandName, afterSeqNo },
        { signal }
      );

      let cleanClose = false;
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
        cleanClose = true;
      } catch {
        // Transient network error or abort — replay below if not aborted.
      }

      if (signal?.aborted || cleanClose || attempt >= STREAM_MAX_RETRIES) break;
      await abortableSleep(STREAM_BACKOFF_MS[attempt], signal);
      if (signal?.aborted) break;
    }

    // Collect the final streaming state before cleaning up.
    const finalEvents = get().streamingEvents[commandName] ?? [];
    const finalStatus =
      get().streamingStatus[commandName] ?? CommandStatus.COMPLETED;
    const streamedContent = get().streamingContent[commandName] ?? "";
    // Cleanup: finalize the assistant message + drop this command's streaming
    // keys via a functional set so the patch merges into the LATEST
    // chatMessages[conversation] (a concurrent watcher poll or new send may
    // have written it since the stream closed; the old stale snapshot would
    // clobber it). On abort we still finalize the partial output but skip the
    // backend reload below.
    set((state) => {
      const msgs = state.chatMessages[conversation] ?? [];
      const finalized = finalizeAssistant(msgs, commandName, {
        streaming: false,
        content: streamedContent,
        events: finalEvents,
        status: finalStatus,
      });
      return {
        chatMessages:
          finalized === msgs
            ? state.chatMessages
            : { ...state.chatMessages, [conversation]: finalized },
        streamingContent: omitKey(state.streamingContent, commandName),
        streamingEvents: omitKey(state.streamingEvents, commandName),
        streamingStatus: omitKey(state.streamingStatus, commandName),
      };
    });
    if (signal?.aborted) return;

    // Reload the final assistant text (FinalSummary may not arrive via
    // TEXT_DELTA); retry while the backend may not have persisted it yet. Each
    // set merges into the latest cached list so a concurrent watcher write is
    // preserved.
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

        const fetched = (res.messages ?? []).map(toUiMessage);
        const assistantMsg = fetched.find((m) => m.commandId === cmdId);
        if (assistantMsg && assistantMsg.content) {
          set((state) => {
            const cur = state.chatMessages[conversation] ?? [];
            const out = cur.map((m) =>
              m.commandId === cmdId
                ? {
                    ...m,
                    ...assistantMsg,
                    commandName,
                    events: finalEvents,
                    status: finalStatus,
                    content: assistantMsg.content || streamedContent,
                  }
                : m
            );
            return {
              chatMessages: { ...state.chatMessages, [conversation]: out },
            };
          });
          return;
        }

        if (attempt < 2) await abortableSleep(500, signal);
        if (signal?.aborted) return;
      } catch {
        return;
      }
    }
  },

  resetStreaming(commandName) {
    const finalEvents = get().streamingEvents[commandName] ?? [];
    const finalContent = get().streamingContent[commandName] ?? "";

    set((state) => {
      const chatMessages = { ...state.chatMessages };
      for (const [conv, msgs] of Object.entries(chatMessages)) {
        const finalized = finalizeAssistant(msgs, commandName, {
          streaming: false,
          content: finalContent,
          events: finalEvents,
        });
        if (finalized !== msgs) chatMessages[conv] = finalized;
      }
      return {
        chatMessages,
        streamingContent: omitKey(state.streamingContent, commandName),
        streamingEvents: omitKey(state.streamingEvents, commandName),
        streamingStatus: omitKey(state.streamingStatus, commandName),
      };
    });
  },
});
