import { equals } from "@bufbuild/protobuf";
import { mcpServerServiceClient, settingServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import { McpServerSchema } from "@/types/proto-es/v1/mcp_pb";
import { registerCleanup } from "./cleanup-registry";
import { sameList } from "./list-equals";
import type { AppSliceCreator, McpServerSlice } from "./types";

// Query cache key for this slice (ADR-1). The personal-MCP enable flag is
// read from settings INSIDE the queryFn, so a single key covers the merged
// view. staleTime: 0 keeps "action call == one explicit fetch" semantics so
// page-level pollers keep their cadence; concurrent duplicate fetches dedupe
// into the same in-flight promise.
const QUERY_KEY = ["mcpServers"];

// Logout clears the slice's Query cache. Wired up at the batch-3 unified
// release point; until then a logout-relogin can serve gcTime-stale data
// briefly, bounded by the 5-minute gcTime in query-client defaults.
export function invalidateMcpServersCache(): void {
  void queryClient.removeQueries({ queryKey: QUERY_KEY });
}

export const createMcpServerSlice: AppSliceCreator<McpServerSlice> = (
  set,
  get
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
      const res = await queryClient.fetchQuery({
        queryKey: QUERY_KEY,
        // Action semantics: exactly one RPC attempt per call — retries are
        // batch-3 page-level useQuery territory.
        retry: false,
        staleTime: 0,
        queryFn: async () => {
          const [wsRes, myRes, cfgRes] = await Promise.all([
            mcpServerServiceClient.listMcpServers({
              pageSize: params?.pageSize ?? 1000,
              pageToken: params?.pageToken ?? "",
            }),
            mcpServerServiceClient.listMyMcpServers({
              pageSize: params?.pageSize ?? 1000,
              pageToken: params?.pageToken ?? "",
            }),
            settingServiceClient.getSetting({
              name: "settings/user_mcp_config",
            }),
          ]);
          const v = cfgRes.value?.value;
          const personalEnabled =
            v?.case === "userMcpConfig" ? v.value.allowUserMcpServers : true;
          return {
            mcpServers: [
              ...(wsRes.mcpServers ?? []),
              ...(personalEnabled ? (myRes.mcpServers ?? []) : []),
            ],
            nextPageToken: wsRes.nextPageToken,
          };
        },
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all. One nuance vs the old flow: params
      // participate in the RPC inside queryFn, but the query key does not —
      // callers here all use the same defaults; if that ever changes, add
      // the varying params to the key.
      if (
        silent &&
        sameList(get().mcpServers, res.mcpServers, (a, b) =>
          equals(McpServerSchema, a, b)
        )
      ) {
        return { nextPageToken: res.nextPageToken };
      }
      set({
        mcpServers: res.mcpServers,
        mcpServersLoading: false,
      });
      return { nextPageToken: res.nextPageToken };
    } catch {
      if (!silent) set({ mcpServers: [], mcpServersLoading: false });
      return undefined;
    }
  },
});

// Audit 05 B7: registered with the unified cleanup registry so reset()/logout
// clears this slice's Query cache without a logout-site call.
registerCleanup(invalidateMcpServersCache);
