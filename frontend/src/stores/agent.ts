import { create, equals } from "@bufbuild/protobuf";
import { agentServiceClient } from "@/connect";
import type { Agent, AgentProviderInfo } from "@/types/proto-es/v1/agent_pb";
import {
  AgentACPConfigSchema,
  AgentSchema,
  CreateAgentRequestSchema,
  DeleteAgentRequestSchema,
  RefreshAgentProvidersRequestSchema,
  RevokeAgentTokenRequestSchema,
  RotateAgentTokenRequestSchema,
  UpdateAgentACPConfigRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { AgentSlice, AppSliceCreator } from "./types";

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set, get) => ({
  agents: [],
  agentsLoading: false,
  agentCache: {},

  async fetchAgents(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ agentsLoading: true });
    try {
      const res = await agentServiceClient.listAgents({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all.
      if (silent && agentsEqual(get().agents, res.agents)) {
        return { nextPageToken: res.nextPageToken };
      }
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
      // On a silent refresh, keep the existing list instead of wiping it on a
      // transient error; only an explicit load reports failure + clears.
      if (!silent) set({ agents: [], agentsLoading: false });
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

  async rotateAgentToken(name: string, reason?: string) {
    const res = await agentServiceClient.rotateAgentToken(
      create(RotateAgentTokenRequestSchema, { name, reason: reason ?? "" })
    );
    set((state) => ({
      agentCache: Object.fromEntries(
        Object.entries(state.agentCache).filter(([k]) => k !== name)
      ),
    }));
    return res;
  },

  async revokeAgentToken(name: string, reason?: string) {
    await agentServiceClient.revokeAgentToken(
      create(RevokeAgentTokenRequestSchema, { name, reason: reason ?? "" })
    );
    set((state) => ({
      agentCache: Object.fromEntries(
        Object.entries(state.agentCache).filter(([k]) => k !== name)
      ),
    }));
  },

  async updateAgentACPConfig(
    name: string,
    acpConfig: {
      executable: string;
      args: string[];
      allowEnv: string[];
      provider: string;
      model: string;
      customEnv: Record<string, string>;
      personaPrompt: string;
    }
  ) {
    await agentServiceClient.updateAgentACPConfig(
      create(UpdateAgentACPConfigRequestSchema, {
        name,
        acpConfig: create(AgentACPConfigSchema, acpConfig),
      })
    );
  },

  async refreshAgentProviders(name: string): Promise<AgentProviderInfo[]> {
    const res = await agentServiceClient.refreshAgentProviders(
      create(RefreshAgentProvidersRequestSchema, { name })
    );
    // Force-refresh the cached agent so the new available_providers surface in
    // the UI immediately, then return the freshly discovered list to the caller.
    await get().getAgent(name, { force: true });
    return res.providers;
  },
});

// agentsEqual reports whether two agent lists are structurally identical, used
// to skip redundant state updates during background polling.
function agentsEqual(prev: Agent[], next: Agent[]): boolean {
  if (prev.length !== next.length) return false;
  for (let i = 0; i < prev.length; i++) {
    if (prev[i].name !== next[i].name) return false;
    if (!equals(AgentSchema, prev[i], next[i])) return false;
  }
  return true;
}
