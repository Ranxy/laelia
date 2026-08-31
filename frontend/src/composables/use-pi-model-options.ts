import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { PiModel } from "@/types/proto-es/v1/agent_pb";

// Debounce window for the "fetch models when the user stops typing the api
// key" trigger. Unified with the machine profile page's cadence (02-P1-3);
// the agent profile page previously used 600ms.
const KEY_INPUT_FETCH_DEBOUNCE_MS = 400;

// usePiModelOptions loads the dynamic model list for a builtin-pi API
// provider via the manager (ListPiModels). deepseek requires the api_key;
// openrouter is public; custom requires an api_base_url. Results are cached
// per provider/baseUrl so toggling providers does not refetch. The model list
// is fetched only on explicit user actions (api key change via the debounced
// trigger, or the Refresh button) — never on entry.
export function usePiModelOptions() {
  const { t } = useTranslation();
  const [models, setModels] = useState<PiModel[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const cacheRef = useRef<Map<string, PiModel[]>>(new Map());
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined
  );

  // cancelPendingFetch drops a scheduled key-change fetch, if any. Call it
  // before an explicit fetch (Refresh / blur) and when cascades reset the
  // pi model fields.
  const cancelPendingFetch = useCallback(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = undefined;
    }
  }, []);

  const fetchModels = useCallback(
    async (provider: string, key: string, baseUrl = "") => {
      if (!provider) return;
      if (provider === "deepseek" && key.trim() === "") return;
      if (provider === "custom" && baseUrl.trim() === "") return;
      const cacheKey = `${provider}/${baseUrl}`;
      const cached = cacheRef.current.get(cacheKey);
      if (cached) {
        setModels(cached);
        setError("");
        return;
      }
      setLoading(true);
      setError("");
      try {
        const listPiModels = useAppStore.getState().listPiModels;
        const list = await listPiModels(provider, key, baseUrl);
        cacheRef.current.set(cacheKey, list);
        setModels(list);
      } catch (err) {
        const msg = describeError(err);
        setError(msg);
        toastManager.add({
          type: "error",
          title: t("agent.acp-config-pi-models-refresh-failed"),
          description: msg,
        });
      } finally {
        setLoading(false);
      }
    },
    [t]
  );

  // debouncedFetchModels schedules the key-change fetch: it fires once the
  // user stops typing the api key (with the values captured at schedule time).
  const debouncedFetchModels = useCallback(
    (provider: string, key: string, baseUrl = "") => {
      cancelPendingFetch();
      debounceRef.current = setTimeout(() => {
        void fetchModels(provider, key, baseUrl);
      }, KEY_INPUT_FETCH_DEBOUNCE_MS);
    },
    [cancelPendingFetch, fetchModels]
  );

  // invalidate drops a cache entry so the next fetch force-refetches (the
  // explicit "Refresh" button).
  const invalidate = useCallback((provider: string, baseUrl: string) => {
    cacheRef.current.delete(`${provider}/${baseUrl}`);
  }, []);

  // clear drops the current model list + error, used when a cascade reset
  // makes the previous list stale (mode/provider switch).
  const clear = useCallback(() => {
    setModels([]);
    setError("");
  }, []);

  // Clear any pending debounce when the hook unmounts.
  useEffect(() => cancelPendingFetch, [cancelPendingFetch]);

  return {
    models,
    loading,
    error,
    fetchModels,
    debouncedFetchModels,
    cancelPendingFetch,
    invalidate,
    clear,
  };
}
