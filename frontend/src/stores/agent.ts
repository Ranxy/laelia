import { create } from "@bufbuild/protobuf";
import { agentServiceClient } from "@/connect";
import {
  AgentSchema,
  CreateAgentRequestSchema,
  DeleteAgentRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { AgentSlice, AppSliceCreator } from "./types";

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set) => ({
  agents: [],
  agentsLoading: false,

  async fetchAgents(params) {
    set({ agentsLoading: true });
    try {
      const res = await agentServiceClient.listAgents({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      set({ agents: res.agents, agentsLoading: false });
      return { nextPageToken: res.nextPageToken };
    } catch {
      set({ agents: [], agentsLoading: false });
      return undefined;
    }
  },

  async createAgent(title: string, labels?: Record<string, string>) {
    const res = await agentServiceClient.createAgent(
      create(CreateAgentRequestSchema, {
        agent: create(AgentSchema, { title, labels }),
      })
    );
    return res;
  },

  async deleteAgent(name: string) {
    await agentServiceClient.deleteAgent(
      create(DeleteAgentRequestSchema, { name })
    );
    set((state) => ({
      agents: state.agents.filter((a) => a.name !== name),
    }));
  },
});
