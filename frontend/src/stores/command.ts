import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  CancelCommandRequestSchema,
  CommandSource,
  RespondPermissionRequestSchema,
  SendCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, ChatMessage, CommandSlice } from "./types";

export const createCommandSlice: AppSliceCreator<CommandSlice> = (
  set,
  get
) => ({
  commands: [],
  commandsLoading: false,
  activeOutputs: {},
  activeEvents: {},
  chatMessages: {},
  chatLoading: false,

  async sendCommand(agent, command, opts) {
    const res = await commandServiceClient.sendCommand(
      create(SendCommandRequestSchema, {
        agent,
        command,
        env: opts?.env ?? {},
        workingDir: opts?.workingDir ?? "",
        timeoutSeconds: opts?.timeoutSeconds ?? 0,
        executorKind: opts?.executorKind ?? 0,
        instruction: opts?.instruction ?? "",
        profile: opts?.profile ?? "",
        allowDiff: opts?.allowDiff ?? false,
        source: opts?.source ?? CommandSource.MANUAL,
      })
    );
    return res;
  },

  async cancelCommand(name) {
    const res = await commandServiceClient.cancelCommand(
      create(CancelCommandRequestSchema, { name })
    );
    set((state) => ({
      commands: state.commands.map((c) => (c.name === name ? res : c)),
    }));
    return res;
  },

  async listCommands(agent, params) {
    set({ commandsLoading: true });
    try {
      const res = await commandServiceClient.listCommands({
        agent,
        pageSize: params?.pageSize ?? 50,
        pageToken: params?.pageToken ?? "",
        status: params?.status ?? 0,
      });
      set({ commands: res.commands, commandsLoading: false });
      return { commands: res.commands, nextPageToken: res.nextPageToken };
    } catch {
      set({ commands: [], commandsLoading: false });
      return undefined;
    }
  },

  async getCommand(name) {
    const res = await commandServiceClient.getCommand({ name });
    return res;
  },

  async watchCommand(name, signal) {
    const state = get();
    const existing = state.activeOutputs[name];
    const afterSeqNo =
      existing && existing.length > 0
        ? existing[existing.length - 1].seqNo
        : -1;

    const stream = commandServiceClient.watchCommand(
      { name, afterSeqNo },
      { signal }
    );

    try {
      for await (const output of stream) {
        if (signal?.aborted) break;
        const s = get();
        const prev = s.activeOutputs[name] ?? [];
        set({
          activeOutputs: {
            ...s.activeOutputs,
            [name]: [...prev, output],
          },
        });
      }
    } catch {
      // stream cancelled (aborted on unmount) or network error — ignore
    }
  },

  async watchCommandEvents(name, signal) {
    const state = get();
    const existing = state.activeEvents[name];
    const afterSeqNo =
      existing && existing.length > 0
        ? existing[existing.length - 1].seqNo
        : -1;

    const stream = commandServiceClient.watchCommandEvents(
      { name, afterSeqNo },
      { signal }
    );

    try {
      for await (const event of stream) {
        if (signal?.aborted) break;
        const s = get();
        const prev = s.activeEvents[name] ?? [];
        set({
          activeEvents: {
            ...s.activeEvents,
            [name]: [...prev, event],
          },
        });
      }
    } catch {
      // stream cancelled (aborted on unmount) or network error — ignore
    }
  },

  async respondPermission(name, optionId) {
    await commandServiceClient.respondPermission(
      create(RespondPermissionRequestSchema, { name, optionId })
    );
  },

  async sendChatMessage(agent, instruction) {
    const res = await commandServiceClient.sendCommand(
      create(SendCommandRequestSchema, {
        agent,
        command: instruction,
        instruction,
        executorKind: 2, // ACP
        source: CommandSource.CHAT,
      })
    );

    const existing = get().chatMessages[agent] ?? [];
    const userMsg: ChatMessage = {
      id: res.name ?? crypto.randomUUID(),
      role: "user",
      content: instruction,
      timestamp: new Date(),
      commandName: res.name,
      status: res.status,
    };
    set({
      chatMessages: {
        ...get().chatMessages,
        [agent]: [...existing, userMsg],
      },
    });

    return res;
  },

  async loadChatHistory(agent, _limit) {
    set({ chatLoading: true });
    try {
      const res = await commandServiceClient.listCommands({
        agent,
        pageSize: 100,
        pageToken: "",
        status: 0,
      } as Parameters<typeof commandServiceClient.listCommands>[0]);

      const chatMsgs: ChatMessage[] = [];
      for (const cmd of (res.commands ?? []).reverse()) {
        if (cmd.source !== CommandSource.CHAT) continue;
        if (cmd.instruction) {
          chatMsgs.push({
            id: cmd.name + ":user",
            role: "user",
            content: cmd.instruction,
            timestamp: cmd.createdAt
              ? new Date(cmd.createdAt.toString())
              : new Date(),
            commandName: cmd.name,
          });
        }
        if (cmd.finalSummary) {
          chatMsgs.push({
            id: cmd.name + ":assistant",
            role: "assistant",
            content: cmd.finalSummary,
            timestamp: cmd.completedAt
              ? new Date(cmd.completedAt.toString())
              : new Date(),
            commandName: cmd.name,
            status: cmd.status,
          });
        }
      }

      set({
        chatMessages: { ...get().chatMessages, [agent]: chatMsgs },
        chatLoading: false,
      });
    } catch {
      set({ chatLoading: false });
    }
  },
});
