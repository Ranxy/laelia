import { describe, expect, it, vi } from "vitest";
import { createAsyncMemoCache } from "./async-memo-cache";

// The shared module-level cache primitive behind avatar-cache and
// image-blob-cache. These tests pin the edge behaviors the two hand-rolled
// implementations used to get wrong (06 R-03/B-02).

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("createAsyncMemoCache", () => {
  it("caches fetched values and dedupes concurrent loads", async () => {
    const fetch = vi.fn(async (key: string) => `value:${key}`);
    const cache = createAsyncMemoCache<string>({ fetch });

    const [a, b] = await Promise.all([cache.load("k"), cache.load("k")]);
    expect(a).toBe("value:k");
    expect(b).toBe("value:k");
    expect(fetch).toHaveBeenCalledTimes(1);

    // A later load is served from the cache.
    await expect(cache.load("k")).resolves.toBe("value:k");
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(cache.get("k")).toBe("value:k");
  });

  it("does not write an in-flight result back after an invalidate (B-02)", async () => {
    const gate = deferred<string>();
    const fetch = vi.fn(() => gate.promise);
    const cache = createAsyncMemoCache<string>({ fetch });

    const pending = cache.load("k");
    cache.invalidate();
    gate.resolve("stale");
    await expect(pending).resolves.toBe("stale");

    // The stale result is returned to its caller but never lands in the
    // cache: a logout/upload race cannot resurrect the old value.
    expect(cache.get("k")).toBeUndefined();
    // A new load starts a fresh fetch instead of trusting the stale entry.
    const onDrop = vi.fn();
    const cache2 = createAsyncMemoCache<string>({
      fetch: async () => "fresh",
      onDrop,
    });
    await cache2.load("k");
    expect(cache2.get("k")).toBe("fresh");
  });

  it("drops a single key on invalidate and releases it via onDrop", async () => {
    const onDrop = vi.fn();
    const cache = createAsyncMemoCache<string>({
      fetch: async (key) => `value:${key}`,
      onDrop,
    });
    await cache.load("a");
    await cache.load("b");

    cache.invalidate("a");

    expect(cache.get("a")).toBeUndefined();
    expect(cache.get("b")).toBe("value:b");
    expect(onDrop).toHaveBeenCalledWith("value:a", "a", "invalidate");
  });

  it("drops every key on a whole-cache invalidate and notifies subscribers", async () => {
    const onDrop = vi.fn();
    const cache = createAsyncMemoCache<string>({
      fetch: async (key) => `value:${key}`,
      onDrop,
    });
    await cache.load("a");
    await cache.load("b");
    const versions: number[] = [];
    const unsubscribe = cache.subscribe(() =>
      versions.push(cache.getVersion())
    );
    const before = cache.getVersion();

    cache.invalidate();

    expect(cache.size()).toBe(0);
    expect(onDrop).toHaveBeenCalledTimes(2);
    expect(cache.getVersion()).toBe(before + 1);
    expect(versions).toEqual([before + 1]);
    unsubscribe();
  });

  it("evicts the oldest entry at capacity with a capacity drop", async () => {
    const onDrop = vi.fn();
    const cache = createAsyncMemoCache<string>({
      fetch: async (key) => `value:${key}`,
      maxEntries: 2,
      onDrop,
    });
    await cache.load("a");
    await cache.load("b");
    await cache.load("c");

    expect(cache.get("a")).toBeUndefined();
    expect(cache.get("b")).toBe("value:b");
    expect(cache.get("c")).toBe("value:c");
    expect(onDrop).toHaveBeenCalledWith("value:a", "a", "capacity");

    // Re-setting an existing key must not evict anything.
    onDrop.mockClear();
    await cache.load("b");
    expect(onDrop).not.toHaveBeenCalled();
  });

  it("does not cache null results, so failures refetch", async () => {
    const fetch = vi.fn(async () => null);
    const cache = createAsyncMemoCache<string>({ fetch });

    await expect(cache.load("k")).resolves.toBeNull();
    await expect(cache.load("k")).resolves.toBeNull();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(cache.size()).toBe(0);
  });
});
