import { equals } from "@bufbuild/protobuf";
import { apiProviderServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import { ApiProviderSchema } from "@/types/proto-es/v1/api_provider_service_pb";
import { sameList } from "./list-equals";
import type { ApiProviderSlice, AppSliceCreator } from "./types";

// Query cache key for this slice (ADR-1: query keys live with the slice that
// fetches them). staleTime: 0 keeps "action call == one explicit fetch"
// semantics so page-level pollers keep their cadence; concurrent duplicate
// fetches dedupe into the same in-flight promise — this is the race
// protection the old hand-rolled paths lacked.
const QUERY_KEY = ["apiProviders"];

// Logout clears the slice's Query cache. Wired up at the batch-3 unified
// release point; until then a logout-relogin can serve gcTime-stale data
// briefly, bounded by the 5-minute gcTime in query-client defaults.
export function invalidateApiProvidersCache(): void {
  void queryClient.removeQueries({ queryKey: QUERY_KEY });
}

export const createAPIProviderSlice: AppSliceCreator<ApiProviderSlice> = (
  set,
  get
) => ({
  apiProviders: [],
  apiProvidersLoading: false,

  // listAPIProviders is handler-gated server-side: admins/managers see every
  // provider, other callers see only the providers they may use. The same list
  // feeds the settings page and the agent create/edit form dropdowns.
  async fetchApiProviders(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the dropdown swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ apiProvidersLoading: true });
    try {
      const res = await queryClient.fetchQuery({
        queryKey: QUERY_KEY,
        // Action semantics: exactly one RPC attempt per call — retries are a
        // page-level concern (batch 3 useQuery), and the explicit-failure
        // path below must fire deterministically.
        retry: false,
        staleTime: 0,
        queryFn: () =>
          apiProviderServiceClient.listAPIProviders({
            pageSize: params?.pageSize ?? 100,
            pageToken: params?.pageToken ?? "",
          }),
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all (the store field is a mirrored view
      // of the Query cache during the migration; components still subscribe
      // the store).
      if (
        silent &&
        sameList(get().apiProviders, res.apiProviders, (a, b) =>
          equals(ApiProviderSchema, a, b)
        )
      ) {
        return { nextPageToken: res.nextPageToken };
      }
      set({ apiProviders: res.apiProviders, apiProvidersLoading: false });
      return { nextPageToken: res.nextPageToken };
    } catch {
      // On a silent refresh, keep the existing list instead of wiping it on a
      // transient error; only an explicit load reports failure + clears.
      if (!silent) set({ apiProviders: [], apiProvidersLoading: false });
      return undefined;
    }
  },
});