import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { StoreApi } from "zustand";
import { commandServiceClient } from "@/connect";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandSource,
  CommandStatus,
  GetOrCreateConversationRequestSchema,
  ListConversationMessagesRequestSchema,
  SendCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type {
  AppSliceCreator,
  AppStoreState,
  ChatMessageUI,
  ChatSlice,
} from "./types";

function finalizeStreaming(
  set: StoreApi<AppStoreState>["setState"],
  get: StoreApi<AppStoreState>["getState"],
  commandName: string,
  conversation: string,
  overrideContent?: string,
  overrideEvents?: CommandEvent[]
) {
  const s = get();
  const finalContent = overrideContent ?? s.streamingContent[commandName] ?? "";
  const finalEvents = overrideEvents ?? s.streamingEvents[commandName] ?? [];
  const finalStatus = s.streamingStatus[commandName] ?? CommandStatus.COMPLETED;

  const messages = s.chatMessages[conversation] ?? [];
  const updated = messages.map((m) =>
    m.commandName === commandName
      ? {
          ...m,
          content: finalContent,
          streaming: false,
          status: finalStatus,
          events: finalEvents,
        }
      : m
  );

  const { [commandName]: _c, ...restContent } = s.streamingContent;
  const { [commandName]: _e, ...restEvents } = s.streamingEvents;
  const { [commandName]: _s, ...restStatus } = s.streamingStatus;

  set({
    chatMessages: { ...s.chatMessages, [conversation]: updated },
    streamingContent: restContent,
    streamingEvents: restEvents,
    streamingStatus: restStatus,
  });
}

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

      const uiMsgs: ChatMessageUI[] = (res.messages ?? []).map((msg) => ({
        id: msg.name,
        role: msg.role === 1 ? "user" : "assistant",
        content: msg.content,
        timestamp: msg.createdAt ? timestampDate(msg.createdAt) : new Date(),
        commandId: msg.commandId || undefined,
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

  async sendChatMessage(agent, instruction) {
    const tempId = crypto.randomUUID();
    const conversation = get().conversations[agent];
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

    let didFinalize = false;
    const finalize = (
      overrideContent?: string,
      overrideEvents?: CommandEvent[]
    ) => {
      if (didFinalize) return;
      didFinalize = true;
      finalizeStreaming(
        set,
        get,
        commandName,
        conversation,
        overrideContent,
        overrideEvents
      );
    };

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
          let finalContent = s.streamingContent[commandName] ?? "";
          if (event.payload.case === "finalSummary") {
            const summary = event.payload.value;
            if (finalContent === "" && summary.stopReason) {
              finalContent = summary.stopReason;
            }
          }
          set({
            streamingContent: {
              ...s.streamingContent,
              [commandName]: finalContent,
            },
            streamingEvents: {
              ...s.streamingEvents,
              [commandName]: nextEvents,
            },
            streamingStatus: {
              ...s.streamingStatus,
              [commandName]: CommandStatus.COMPLETED,
            },
          });
          finalize(finalContent, nextEvents);
        } else {
          set({
            streamingEvents: {
              ...s.streamingEvents,
              [commandName]: nextEvents,
            },
          });
        }
      }

      finalize();
    } catch {
      finalize();
    }
  },

  resetStreaming(commandName) {
    set((state) => {
      const { [commandName]: _c, ...content } = state.streamingContent;
      const { [commandName]: _e, ...events } = state.streamingEvents;
      const { [commandName]: _s, ...status } = state.streamingStatus;
      return {
        streamingContent: content,
        streamingEvents: events,
        streamingStatus: status,
      };
    });
  },
});
