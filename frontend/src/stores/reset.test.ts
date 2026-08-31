import { afterEach, describe, expect, it, vi } from "vitest";
import { queryClient } from "@/lib/query-client";
import { registerCleanup } from "./cleanup-registry";
import { useAppStore } from "./index";
import type { AppStoreState } from "./types";

// Seeds a sentinel guaranteed to differ from one key's pristine initial
// value: array -> ["sentinel"], Record -> { __sentinel__: 1 }, string ->
// "__sentinel__", boolean -> flipped, number/bigint -> +1, null/undefined
// -> "sentinel". Cast as never when written: AppStoreState is a big
// intersection of slices, so generic sentinels never type-match a field.
function sentinelFor(key: keyof AppStoreState, pristine: AppStoreState) {
  // reset() aborts/stops watcher entries, so those two keys need valid
  // handles instead of generic sentinels.
  if (key === "channelWatchers") {
    return {
      "conversations/1": {
        ctrl: new AbortController(),
        badge: { stop: () => {} },
      },
    };
  }
  if (key === "threadWatchers") {
    return { "conversations/1": { ctrl: new AbortController() } };
  }
  const value: unknown = pristine[key];
  if (Array.isArray(value)) return ["sentinel"];
  if (value !== null && typeof value === "object") return { __sentinel__: 1 };
  switch (typeof value) {
    case "string":
      return "__sentinel__";
    case "boolean":
      return !value;
    case "number":
      return value + 1;
    case "bigint":
      return value + 1n;
    default:
      return "sentinel";
  }
}

describe("store reset", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("restores every slice to its pristine initial state on logout", () => {
    // Table-driven: derive the assertion surface from getInitialState()
    // itself so a new slice's forgotten cleanup can never escape this test
    // hand-picking fields. Its action closures are same-function after
    // reset (they stay bound to the live set/get), so excluding functions
    // — which also drops `reset` itself — leaves exactly the data slices.
    const pristine = useAppStore.getInitialState();
    const dataKeys = (Object.keys(pristine) as (keyof AppStoreState)[]).filter(
      (key) => typeof pristine[key] !== "function"
    );
    expect(dataKeys.length).toBeGreaterThan(0);

    // Mutate every data key away from pristine, then reset.
    const seed: Partial<AppStoreState> = {};
    for (const key of dataKeys) seed[key] = sentinelFor(key, pristine) as never;
    useAppStore.setState(seed);
    useAppStore.getState().reset();

    const state = useAppStore.getState();
    for (const key of dataKeys) {
      expect(
        state[key],
        `slice "${key}" must be restored to its pristine initial state`
      ).toEqual(pristine[key]);
    }
  });

  it("stops channel and thread watcher loops before wiping state", () => {
    const store = useAppStore;
    const abortSpy = vi.spyOn(AbortController.prototype, "abort");
    const clearSpy = vi.spyOn(globalThis, "clearInterval");
    const channelCtrl = new AbortController();
    const threadCtrl = new AbortController();
    const badgeTimer = setInterval(() => {}, 1000);

    store.setState({
      channelWatchers: {
        "conversations/1": {
          ctrl: channelCtrl,
          // The badge handle's stop() is what clears the interval now; the
          // fake timer here only feeds the clearInterval assertion below.
          badge: {
            stop: () => {
              clearInterval(badgeTimer);
            },
          },
        },
      },
      threadWatchers: { "conversations/1": { ctrl: threadCtrl } },
    });

    store.getState().reset();

    expect(abortSpy).toHaveBeenCalledTimes(2);
    expect(clearSpy).toHaveBeenCalledWith(badgeTimer);
    expect(store.getState().channelWatchers).toEqual({});
    expect(store.getState().threadWatchers).toEqual({});

    // Safety net in case the assertion above ever fails before clearing.
    clearInterval(badgeTimer);
  });

  it("runs registered cleanups and clears the Query cache on reset", () => {
    const cleanup = vi.fn();
    const unregister = registerCleanup(cleanup);
    try {
      // Seed Query cache entries owned by registered module cleanups
      // (api-provider / mcp, wired in their slice files via the registry).
      queryClient.setQueryData(["apiProviders"], {
        apiProviders: [],
        nextPageToken: "",
      });
      queryClient.setQueryData(["mcpServers"], {
        mcpServers: [],
        nextPageToken: "",
      });
      expect(queryClient.getQueryCache().getAll()).toHaveLength(2);

      useAppStore.getState().reset();

      expect(cleanup).toHaveBeenCalledOnce();
      expect(queryClient.getQueryCache().getAll()).toEqual([]);
    } finally {
      // Temporary registration: unsubscribe so later resets don't run it.
      unregister();
    }
  });

  it("contains a throwing cleanup and still runs the others", () => {
    const errorSpy = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const unregisterBoom = registerCleanup(() => {
      throw new Error("boom");
    });
    const after = vi.fn();
    const unregisterAfter = registerCleanup(after);
    try {
      useAppStore.getState().reset();

      expect(after).toHaveBeenCalledOnce();
      expect(errorSpy).toHaveBeenCalled();
    } finally {
      unregisterBoom();
      unregisterAfter();
      errorSpy.mockRestore();
    }
  });
});
