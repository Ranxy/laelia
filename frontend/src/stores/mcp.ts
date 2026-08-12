import { mcpServerServiceClient, settingServiceClient } from "@/connect";
import type { AppSliceCreator, McpServerSlice } from "./types";

export const createMcpServerSlice: AppSliceCreator<McpServerSlice> = (
  set,
  _get
) => ({
  mcpServers: [],
  mcpServersLoading: false,

  // fetchMcpServers merges the workspace servers the caller may use with the
  // caller's own personal servers (skipped while the personal-MCP setting is
  // disabled). The combined list feeds the agent config form.
  async fetchMcpServers(params, opts) {
    const silent = opts?.silent;
    if (!silent) set({ mcpServersLoading: true });
    try {
      const [wsRes, myRes, cfgRes] = await Promise.all([
        mcpServerServiceClient.listMcpServers({
          pageSize: params?.pageSize ?? 1000,
          pageToken: params?.pageToken ?? "",
        }),
        mcpServerServiceClient.listMyMcpServers({
          pageSize: params?.pageSize ?? 1000,
          pageToken: params?.pageToken ?? "",
        }),
        settingServiceClient.getSetting({ name: "settings/user_mcp_config" }),
      ]);
      const v = cfgRes.value?.value;
      const personalEnabled =
        v?.case === "userMcpConfig" ? v.value.allowUserMcpServers : true;
      set({
        mcpServers: [
          ...(wsRes.mcpServers ?? []),
          ...(personalEnabled ? (myRes.mcpServers ?? []) : []),
        ],
        mcpServersLoading: false,
      });
      return { nextPageToken: wsRes.nextPageToken };
    } catch {
      if (!silent) set({ mcpServers: [], mcpServersLoading: false });
      return undefined;
    }
  },
});
