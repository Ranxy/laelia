import { apiProviderServiceClient } from "@/connect";
import type { ApiProviderSlice, AppSliceCreator } from "./types";

export const createApiProviderSlice: AppSliceCreator<ApiProviderSlice> = (
  set,
  _get
) => ({
  apiProviders: [],
  apiProvidersLoading: false,

  // listApiProviders is handler-gated server-side: admins/managers see every
  // provider, other callers see only the providers they may use. The same list
  // feeds the settings page and the agent create/edit form dropdowns.
  async fetchApiProviders(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the dropdown swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ apiProvidersLoading: true });
    try {
      const res = await apiProviderServiceClient.listApiProviders({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
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
