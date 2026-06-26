import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { commandServiceClient } from "@/connect";
import {
  AddChannelMemberRequestSchema,
  CommandEventType,
  CommandStatus,
  CreateChannelRequestSchema,
  FetchConversationActivityRequestSchema,
  GetOrCreateConversationRequestSchema,
  ListChannelMembersRequestSchema,
  ListChannelsRequestSchema,
  ListConversationMessagesRequestSchema,
  RemoveChannelMemberRequestSchema,
  SendMessageRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, ChatMessageUI, ChatSlice } from "./types";

// Module-level map of active channel poll intervals, keyed by conversation name.
const channelWatchers: Record<string, ReturnType<typeof setInterval>> = {};

// mergeMessages reconciles a freshly polled message list with the cached one.
// Unchanged messages keep their previous object reference so React.memo can skip
// re-rendering them, and an unchanged list returns the exact same reference so
// the store setter (and its subscribers) can bail out entirely.
function mergeMessages(
  prev: ChatMessageUI[],
  next: ChatMessageUI[]
): ChatMessageUI[] {
  if (prev.length === 0 || next.length === 0) return next;
  if (prev[0].id !== next[0].id) return next;

  const prevById = new Map(prev.map((m) => [m.id, m]));
  let changed = false;
  const out: ChatMessageUI[] = [];
  for (const n of next) {
    const p = prevById.get(n.id);
    if (
      p &&
      p.content === n.content &&
      p.role === n.role &&
      p.senderName === n.senderName &&
      p.senderType === n.senderType
    ) {
      out.push(p);
    } else {
      out.push(n);
      changed = true;
    }
  }
  return changed || out.length !== prev.length ? out : prev;
}

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
  agentActivities: {},

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
        mentions: msg.mentions,
        attachments: msg.attachments,
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

    // Return shape compatible with callers that read .name for commandName
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
          attachments: msg.attachments,
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
    const chatMsg: ChatMessageUI = {
      id: res.name,
      role: "user",
      content: res.content,
      timestamp: res.createdAt ? timestampDate(res.createdAt) : new Date(),
      senderName: res.senderName || undefined,
      senderType: res.senderType || undefined,
      mentions: res.mentions,
      attachments: res.attachments,
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

    // Phase 2: poll for agent responses in channel conversations.
    // Agents receive NewMessagesAvailable on their bidi stream and respond
    // asynchronously; the frontend has no push mechanism, so we poll.
    get().pollChannelMessages(conversationName);

    return res;
  },

  // pollChannelMessages periodically reloads messages for a conversation
  // until new messages appear (count increases) or the timeout expires.
  async pollChannelMessages(conversationName) {
    const POLL_INTERVAL_MS = 2000;
    const POLL_TIMEOUT_MS = 30000;
    const start = Date.now();

    const currentCount = get().chatMessages[conversationName]?.length ?? 0;

    while (Date.now() - start < POLL_TIMEOUT_MS) {
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));

      try {
        // Poll for new messages.
        const res = await commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation: conversationName,
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
          attachments: msg.attachments,
        }));

        if (uiMsgs.length > currentCount) {
          set((state) => ({
            chatMessages: {
              ...state.chatMessages,
              [conversationName]: uiMsgs,
            },
          }));
          return;
        }
      } catch {
        // network error — keep polling
      }

      // Poll for agent execution status in parallel.
      get().fetchConversationActivity(conversationName);
    }
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
      set({
        agentActivities: {
          ...get().agentActivities,
          [conversationName]: res.activities ?? [],
        },
      });
    } catch {
      // network error — will retry on next poll
    }
  },

  startWatchingChannel(conversationName) {
    // Already watching — avoid duplicate intervals.
    if (channelWatchers[conversationName]) return;

    const POLL_INTERVAL_MS = 2000;

    const poll = async () => {
      try {
        const res = await commandServiceClient.listConversationMessages(
          create(ListConversationMessagesRequestSchema, {
            conversation: conversationName,
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
          attachments: msg.attachments,
        }));
        // Reuse cached references for unchanged messages and skip the store
        // update entirely when nothing changed, so polling does not churn the
        // array identity (which would force a scroll snap and re-render).
        const prev = get().chatMessages[conversationName] ?? [];
        const merged = mergeMessages(prev, uiMsgs);
        if (merged !== prev) {
          set({
            chatMessages: {
              ...get().chatMessages,
              [conversationName]: merged,
            },
          });
        }
      } catch {
        // network error — will retry on next tick
      }

      // Also poll agent activity.
      get().fetchConversationActivity(conversationName);
    };

    // Run immediately, then on interval.
    poll();
    channelWatchers[conversationName] = setInterval(poll, POLL_INTERVAL_MS);
  },

  stopWatchingChannel(conversationName) {
    const id = channelWatchers[conversationName];
    if (id) {
      clearInterval(id);
      delete channelWatchers[conversationName];
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
