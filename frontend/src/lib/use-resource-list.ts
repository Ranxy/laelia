import { useCallback, useEffect, useRef, useState } from "react";

// ---------------------------------------------------------------------------
// useResourceList — page-level token pagination with race protection.
//
// Extracted from the command/reminder list pages, which each hand-rolled the
// same pageToken/pageIndex state machine (and drifted: clear-vs-keep of the
// next cursor, missing race guards). The hook owns:
//   - the visited-page token stack + current page index
//   - a request sequence: stale responses (user raced pages/filters) are
//     dropped instead of overwriting newer state
//   - initial loading (skeleton) vs refreshing, so a filter change may show
//     the skeleton while a poll or page turn never does
//   - optional silent polling of the current page (no loading toggles)
// It never writes to the stores — the fetcher (usually a store action) owns
// where the rows also land.
// ---------------------------------------------------------------------------

export interface ResourceListPage<T> {
  rows: T[];
  nextPageToken: string;
}

export interface ResourceListResult<TRow> {
  rows: TRow[];
  pageIndex: number;
  canPrev: boolean;
  canNext: boolean;
  // The skeleton case: the current page has not been fetched yet for this
  // reset generation (first view / filter change). Pagination turns and
  // silent polls keep the previous rows on screen instead.
  loading: boolean;
  // A user-initiated fetch is in flight (page turn / filter change / reload).
  refreshing: boolean;
  error: boolean;
  nextPage(): void;
  prevPage(): void;
  // Back to page 1 + a fresh (non-silent) load — same semantics as a filter
  // reset but triggered imperatively (e.g. after a mutation).
  reload(): void;
}

export interface UseResourceListOptions<TRow> {
  // Performs one page fetch. Return undefined to signal failure (sets error).
  fetch: (
    pageToken: string,
    opts: { silent: boolean }
  ) => Promise<ResourceListPage<TRow> | undefined>;
  // Reset pagination to page 1 and reload (non-silent) whenever the key
  // changes (filter tabs, agent params, …).
  resetKey: string;
  // Background silent refresh cadence for the current page. 0 disables.
  pollMs?: number;
}

export function useResourceList<TRow>(
  opts: UseResourceListOptions<TRow>
): ResourceListResult<TRow> {
  const { fetch, resetKey, pollMs = 0 } = opts;
  // Callers rebuild the fetch callback every render; treat it as unstable so
  // effects below don't re-fire from it.
  const fetchRef = useRef(fetch);
  fetchRef.current = fetch;

  const [pageTokens, setPageTokens] = useState<string[]>([""]);
  const [pageIndex, setPageIndex] = useState(0);
  // Rows are null until the current generation's page has loaded; the page
  // maps that to its skeleton.
  const [rows, setRows] = useState<TRow[] | null>(null);
  const [nextPageToken, setNextPageToken] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState(false);

  // Bumped whenever resetKey changes so the load effect re-runs after the
  // pagination state was cleared.
  const [resetGen, setResetGen] = useState(0);
  // One shared request sequence: the newest request wins, stale replies are
  // dropped without touching state (03-B1: old pages could overwrite newer
  // filters/pagination).
  const seqRef = useRef(0);
  // Skip the reset effect on the first mount: the hook already starts on
  // page 1 without a generation bump (a bump there would double-fetch page 1).
  const lastResetKey = useRef(resetKey);

  const currentToken = pageTokens[pageIndex] ?? "";

  useEffect(() => {
    if (lastResetKey.current === resetKey) return; // initial mount: no-op
    lastResetKey.current = resetKey;
    seqRef.current += 1;
    setPageTokens([""]);
    setPageIndex(0);
    setNextPageToken("");
    setRows(null);
    setError(false);
    setResetGen((g) => g + 1);
  }, [resetKey]);

  const run = useCallback(async (silent: boolean, token: string) => {
    const seq = ++seqRef.current;
    if (!silent) setRefreshing(true);
    try {
      const res = await fetchRef.current(token, { silent });
      if (seq !== seqRef.current) return; // raced: a newer request exists
      if (!res) {
        setError(true);
        setRows([]);
        setNextPageToken("");
        return;
      }
      setError(false);
      setRows(res.rows);
      setNextPageToken(res.nextPageToken ?? "");
    } finally {
      if (seq === seqRef.current) setRefreshing(false);
    }
  }, []);

  // Non-silent load: first page, pagination turns, resets. The reset effect
  // bumps resetGen so this re-runs with the cleared pagination state; the
  // pre-reset run (if any) is dropped by the sequence guard.
  // biome-ignore lint/correctness/useExhaustiveDependencies: resetGen is the generation trigger for resets — the pre-reset run is dropped by the seq guard.
  useEffect(() => {
    void run(false, currentToken);
  }, [currentToken, resetGen, run]);

  useEffect(() => {
    if (!pollMs) return;
    // Silent poll of the current page: refreshes rows in place. The seq
    // guard means a poll racing a page turn cannot resurrect the old page.
    const handle = setInterval(() => void run(true, currentToken), pollMs);
    return () => clearInterval(handle);
  }, [pollMs, currentToken, run]);

  const nextPage = useCallback(() => {
    // Truncate any forward history (e.g. after going back a page) and push
    // the cursor for the page we are about to enter.
    const cut = pageTokens.slice(0, pageIndex + 1);
    cut.push(nextPageToken);
    setPageTokens(cut);
    setPageIndex(pageIndex + 1);
    // Clear until the new page loads so canNext doesn't double-trigger.
    setNextPageToken("");
  }, [nextPageToken, pageIndex, pageTokens]);

  const prevPage = useCallback(() => {
    setPageIndex((i) => Math.max(0, i - 1));
  }, []);

  const reload = useCallback(() => {
    seqRef.current += 1;
    setPageTokens([""]);
    setPageIndex(0);
    setRows(null);
    setNextPageToken("");
    setResetGen((g) => g + 1);
  }, []);

  return {
    rows: rows ?? [],
    pageIndex,
    canPrev: pageIndex > 0,
    canNext: nextPageToken !== "" && !refreshing,
    loading: rows === null,
    refreshing,
    error,
    nextPage,
    prevPage,
    reload,
  };
}
