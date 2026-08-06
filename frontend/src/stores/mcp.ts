import { mcpServerServiceClient } from "@/connect";
import type { AppSliceCreator, McpServerSlice } from "./types";

export const createMcpServerSlice: AppSliceCreator<McpServerSlice> = (
  set,
  _get
) => ({
  mcpServers: [],
  mcpServersLoading: false,

  // fetchMcpServers is handler-gated server-side: admins/managers see every
  // server, other callers see only the servers they may use. The same list
  // feeds the settings page and the agent config form.
  async fetchMcpServers(params, opts) {
    const silent = opts?.silent;
    if (!silent) set({ mcpServersLoading: true });
    try {
      const res = await mcpServerServiceClient.listMcpServers({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      set({ mcpServers: res.mcpServers ?? [], mcpServersLoading: false });
      return { nextPageToken: res.nextPageToken };
    } catch {
      if (!silent) set({ mcpServers: [], mcpServersLoading: false });
      return undefined;
    }
  },
});
