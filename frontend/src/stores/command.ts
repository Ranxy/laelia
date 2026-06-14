import { create } from "@bufbuild/protobuf";
import { commandServiceClient } from "@/connect";
import {
  CancelCommandRequestSchema,
  SendCommandRequestSchema,
} from "@/types/proto-es/v1/command_pb";
import type { AppSliceCreator, CommandSlice } from "./types";

export const createCommandSlice: AppSliceCreator<CommandSlice> = (
  set,
  get
) => ({
  commands: [],
  commandsLoading: false,
  activeOutputs: new Map(),
  watchingCommands: new Set(),

  async sendCommand(agent, command, opts) {
    const res = await commandServiceClient.sendCommand(
      create(SendCommandRequestSchema, {
        agent,
        command,
        env: opts?.env ?? {},
        workingDir: opts?.workingDir ?? "",
        timeoutSeconds: opts?.timeoutSeconds ?? 0,
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

  async watchCommand(name) {
    const state = get();
    if (state.watchingCommands.has(name)) return;

    set((s) => {
      s.watchingCommands.add(name);
      return {};
    });

    const existing = state.activeOutputs.get(name);
    const afterSeqNo =
      existing && existing.length > 0 ? existing[existing.length - 1].seqNo : 0;

    const stream = commandServiceClient.watchCommand({ name, afterSeqNo });

    for await (const output of stream) {
      set((s) => {
        const prev = s.activeOutputs.get(name) ?? [];
        s.activeOutputs.set(name, [...prev, output]);
        return {};
      });
    }

    set((s) => {
      s.watchingCommands.delete(name);
      return {};
    });
  },

  unwatchCommand(name) {
    set((s) => {
      s.watchingCommands.delete(name);
      return {};
    });
  },
});
