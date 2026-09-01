import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { showErrorToast } from "@/lib/toast-errors";

// ---------------------------------------------------------------------------
// useResourceQuery — the settings domain's read primitive (ADR-1).
//
// The settings CRUD pages used to hand-roll `load()` (loading flag + one
// toast on failure + manual reload) with no cross-page cache: the same user /
// group / role directories were refetched page-to-page and a slow response
// could overwrite a newer one. This hook puts those reads on TanStack Query —
// the shared queryClient dedupes concurrent fetches, orders responses, caches
// per key, and lets mutations invalidate by key — and adds a flattened view:
//
//   - { items, initialLoading, refreshing, error, reload } — initial loading
//     renders the page skeleton; a refetch keeps the rows on screen
//     (the 01-P5 "saving one row flashes the whole table" fix).
//   - One failure toast per failure episode: entering the error state toasts
//     once; follow-up automatic refetches of the still-broken query stay
//     quiet until a result clears the error.
//
// Directory reads (workspace users / groups shared by member editors and IAM
// labels) pass a higher `staleTime` so pages share one cache entry per key;
// page-scoped resources use the default fresh-reads staleTime (mutations
// reload explicitly anyway).
// ---------------------------------------------------------------------------

const EMPTY_ITEMS: never[] = [];

export interface UseResourceQueryOptions<TItem> {
  // false keeps the query idle — settings pages gate on the list permission
  // and render their PermissionNotice instead of fetching.
  enabled?: boolean;
  // Every value that selects a different server view must live in the key,
  // never only in the closure, or two views share one cache entry.
  queryKey: readonly unknown[];
  queryFn: (signal: AbortSignal) => Promise<TItem[]>;
  // Already-translated failure title, toasted once when the query enters the
  // error state. Pass the t() translation at the call site — e.g. with the
  // page's load-failed key — so the key stays statically visible to
  // scripts/check-react-i18n.mjs (same contract as showErrorToast; this hook
  // itself stays free of i18n imports).
  failureTitle: string;
  // 0 = fresh reads (settings pages reload after mutations anyway); shared
  // directories pass a higher value to dedupe page-to-page navigation.
  staleTime?: number;
}

export interface ResourceQueryResult<TItem> {
  items: TItem[];
  // No cached data yet — render the skeleton instead of an empty table.
  initialLoading: boolean;
  // A refetch is in flight while usable data is already on screen.
  refreshing: boolean;
  error: boolean;
  // Explicit post-mutation refresh.
  reload(): void;
}

export function useResourceQuery<TItem>(
  opts: UseResourceQueryOptions<TItem>
): ResourceQueryResult<TItem> {
  const { enabled = true, queryKey, failureTitle, staleTime = 0 } = opts;
  // Callers build the fetch callback inline; treat it as unstable so the
  // query never restarts from it.
  const fnRef = useRef(opts.queryFn);
  fnRef.current = opts.queryFn;

  const query = useQuery({
    queryKey,
    enabled,
    staleTime,
    // Action semantics: one RPC attempt per (re)load — retries are a page
    // concern, and the failure toast must fire deterministically.
    retry: false,
    queryFn: ({ signal }) => fnRef.current(signal),
  });

  const notifiedRef = useRef(false);
  useEffect(() => {
    if (query.isError) {
      if (!notifiedRef.current) {
        notifiedRef.current = true;
        void showErrorToast(query.error, failureTitle);
      }
    } else {
      notifiedRef.current = false;
    }
  }, [query.isError, query.error, failureTitle]);

  return {
    items: query.data ?? EMPTY_ITEMS,
    initialLoading: query.isPending && enabled,
    refreshing: query.isFetching && !query.isPending,
    error: query.isError,
    reload: () => {
      void query.refetch();
    },
  };
}
