import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  CancelCommandRequestSchema,
  RespondPermissionRequestSchema,
  SendCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, CommandSlice } from "./types";

export const createCommandSlice: AppSliceCreator<CommandSlice> = (
  set,
  get
) => ({
  commands: [],
  commandsLoading: false,
  activeOutputs: {},
  activeEvents: {},

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
        allowDiff: opts?.allowDiff ?? false,
        source: opts?.source ?? 0,
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
      // stream cancelled or network error
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
      // stream cancelled or network error
    }
  },

  async respondPermission(name, optionId) {
    await commandServiceClient.respondPermission(
      create(RespondPermissionRequestSchema, { name, optionId })
    );
  },
});
