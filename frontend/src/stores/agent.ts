import { create } from "@bufbuild/protobuf";
import { agentServiceClient } from "@/connect";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import {
  AgentSchema,
  CreateAgentRequestSchema,
  DeleteAgentRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { AgentSlice, AppSliceCreator } from "./types";

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set, get) => ({
  agents: [],
  agentsLoading: false,
  agentCache: {},

  async fetchAgents(params) {
    set({ agentsLoading: true });
    try {
      const res = await agentServiceClient.listAgents({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      const cache: Record<string, Agent> = {};
      for (const a of res.agents) {
        if (a.name) cache[a.name] = a;
      }
      set((state) => ({
        agents: res.agents,
        agentsLoading: false,
        agentCache: { ...state.agentCache, ...cache },
      }));
      return { nextPageToken: res.nextPageToken };
    } catch {
      set({ agents: [], agentsLoading: false });
      return undefined;
    }
  },

  async getAgent(name, opts) {
    if (!opts?.force) {
      const cached = get().agentCache[name];
      if (cached) return cached;
    }
    try {
      const res = await agentServiceClient.getAgent({ name });
      set((state) => ({
        agentCache: { ...state.agentCache, [name]: res },
      }));
      return res;
    } catch {
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
      agentCache: Object.fromEntries(
        Object.entries(state.agentCache).filter(([k]) => k !== name)
      ),
    }));
  },
});
