// ---------------------------------------------------------------------------
// async-memo-cache — the one module-level "Map cache + inflight dedupe +
// invalidation broadcast" primitive (06 R-03).
//
// avatar-cache and image-blob-cache used to hand-roll the same machinery with
// diverging edge behavior: the avatar cache leaked entries without bound and
// wrote stale results back after an invalidate (B-02), and neither had a
// generation guard. This primitive owns all of it:
//
//   - load() shares one in-flight promise per key;
//   - every invalidate bumps a generation — an in-flight fetch that finishes
//     after an invalidate still resolves for its original caller but is NOT
//     written back, so a logout/upload race can never resurrect stale data;
//   - invalidate() invokes onDrop("invalidate") — safe to release resources
//     (revoke object URLs) because subscribers are told to refetch;
//   - capacity eviction invokes onDrop("capacity") — the dropped value may
//     still be displayed somewhere, so drop handlers must NOT release its
//     resources (a bounded leak, at most maxEntries);
//   - subscribe/getVersion plug into useSyncExternalStore for consumers that
//     render from the cache.
//
// The fetcher contract: resolve null for "fetch failed / nothing there".
// Nulls are NOT cached — negative-caching policy (avatar 404s) belongs to the
// call site.
// ---------------------------------------------------------------------------

export type CacheDropReason = "invalidate" | "capacity";

export interface AsyncMemoCacheOptions<TValue> {
  fetch: (key: string) => Promise<TValue | null>;
  // FIFO capacity cap; 0 (default) keeps every entry until invalidated.
  maxEntries?: number;
  onDrop?: (value: TValue, key: string, reason: CacheDropReason) => void;
}

export interface AsyncMemoCache<TValue> {
  get(key: string): TValue | undefined;
  load(key: string): Promise<TValue | null>;
  invalidate(key?: string): void;
  subscribe(listener: () => void): () => void;
  getVersion(): number;
  size(): number;
}

export function createAsyncMemoCache<TValue>(
  opts: AsyncMemoCacheOptions<TValue>
): AsyncMemoCache<TValue> {
  const entries = new Map<string, TValue>();
  const inflight = new Map<string, Promise<TValue | null>>();
  const listeners = new Set<() => void>();
  // Bumped by every invalidate; in-flight fetches stamp their generation and
  // refuse to land when it has moved on (B-02).
  let generation = 0;

  const notify = () => {
    for (const listener of listeners) listener();
  };

  const setEntry = (key: string, value: TValue) => {
    if (
      opts.maxEntries &&
      entries.size >= opts.maxEntries &&
      !entries.has(key)
    ) {
      const oldest = entries.keys().next().value;
      if (oldest !== undefined) {
        const dropped = entries.get(oldest);
        entries.delete(oldest);
        if (dropped !== undefined) opts.onDrop?.(dropped, oldest, "capacity");
      }
    }
    entries.set(key, value);
  };

  return {
    get(key) {
      return entries.get(key);
    },

    load(key) {
      const cached = entries.get(key);
      if (cached !== undefined) return Promise.resolve(cached);
      const existing = inflight.get(key);
      if (existing) return existing;

      const gen = generation;
      // Wrapped in a holder so the finally block can compare identities
      // (invalidate may have replaced this fetch with a newer one).
      const entry: { promise: Promise<TValue | null> } = {
        promise: Promise.resolve(null),
      };
      entry.promise = (async () => {
        try {
          const value = await opts.fetch(key);
          // Superseded by a later invalidate: hand the value to this caller
          // but leave the cache alone (a fresh load() will fetch anew).
          if (gen !== generation) return value;
          if (value !== null) setEntry(key, value);
          return value;
        } finally {
          if (inflight.get(key) === entry.promise) inflight.delete(key);
        }
      })();
      inflight.set(key, entry.promise);
      return entry.promise;
    },

    invalidate(key) {
      generation++;
      if (key === undefined) {
        for (const [k, v] of entries) opts.onDrop?.(v, k, "invalidate");
        entries.clear();
        inflight.clear();
      } else {
        const v = entries.get(key);
        if (v !== undefined) {
          entries.delete(key);
          opts.onDrop?.(v, key, "invalidate");
        }
        inflight.delete(key);
      }
      notify();
    },

    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    getVersion() {
      return generation;
    },

    size() {
      return entries.size;
    },
  };
}
