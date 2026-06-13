import { api } from "@/react/api/client";
import type { AgentSlice, ApiAgent, AppSliceCreator } from "./types";

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set) => ({
  agents: [],
  agentsLoading: false,

  async fetchAgents(params) {
    set({ agentsLoading: true });
    try {
      const searchParams = new URLSearchParams();
      if (params?.pageSize) {
        searchParams.set("pageSize", String(params.pageSize));
      }
      if (params?.pageToken) {
        searchParams.set("pageToken", params.pageToken);
      }
      const qs = searchParams.toString();
      const res = await api.get<{
        agents: ApiAgent[];
        nextPageToken: string;
      }>(`/agents${qs ? `?${qs}` : ""}`);
      set({ agents: res.agents ?? [], agentsLoading: false });
      return { nextPageToken: res.nextPageToken };
    } catch {
      set({ agents: [], agentsLoading: false });
      return undefined;
    }
  },

  async createAgent(title: string, labels?: Record<string, string>) {
    const res = await api.post<ApiAgent>("/agents", { title, labels });
    return res;
  },

  async deleteAgent(name: string) {
    await api.delete<void>(`/${name}`);
    set((state) => ({
      agents: state.agents.filter((a) => a.name !== name),
    }));
  },
});
