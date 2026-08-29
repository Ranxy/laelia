import { equals } from "@bufbuild/protobuf";
import { apiProviderServiceClient } from "@/connect";
import { ApiProviderSchema } from "@/types/proto-es/v1/api_provider_service_pb";
import { sameList } from "./list-equals";
import type { ApiProviderSlice, AppSliceCreator } from "./types";

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
      const res = await apiProviderServiceClient.listAPIProviders({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all.
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
