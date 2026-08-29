import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MachineSummarySchema } from "@/types/proto-es/v1/machine_pb";
import { useAppStore } from "./index";

// --- mock @/connect: listMachines becomes a counting stub ---
const mock = vi.hoisted(() => {
  const counters = { list: 0 };
  return {
    counters,
    listMachines: vi.fn(async () => {
      counters.list++;
      return {
        machines: [
          create(MachineSummarySchema, { name: "machines/m1", title: "M1" }),
        ],
        nextPageToken: "",
      };
    }),
  };
});

vi.mock("@/connect", () => ({
  machineServiceClient: {
    listMachines: mock.listMachines,
  },
}));

import { queryClient } from "@/lib/query-client";
import { invalidateMachinesCache } from "./machine";

beforeEach(() => {
  invalidateMachinesCache();
  useAppStore.setState({ machines: [], machinesLoading: false });
  mock.counters.list = 0;
});

describe("machine slice (queryClient-backed)", () => {
  it("dedupes concurrent in-flight fetches into one RPC round", async () => {
    const store = useAppStore.getState();
    const [r1, r2] = await Promise.all([
      store.fetchMachines(),
      store.fetchMachines(),
    ]);
    expect(r1).toEqual(r2);
    expect(r1).toBeDefined();
    expect(mock.counters.list).toBe(1);
    expect(useAppStore.getState().machines).toHaveLength(1);
  });

  it("keeps distinct showDeleted views on distinct cache entries", async () => {
    const store = useAppStore.getState();
    // showDeleted participates in the key, so the active and recycled views
    // must not merge into a shared in-flight request.
    await Promise.all([
      store.fetchMachines({ showDeleted: true }),
      store.fetchMachines(),
    ]);
    expect(mock.counters.list).toBe(2);
  });

  it("keeps the previous store reference when a silent refresh is unchanged", async () => {
    await useAppStore.getState().fetchMachines();
    const before = useAppStore.getState().machines;
    await useAppStore.getState().fetchMachines(undefined, { silent: true });
    expect(useAppStore.getState().machines).toBe(before);
  });

  it("clears the list on an explicit load failure but keeps it on silent", async () => {
    const store = useAppStore.getState();
    await store.fetchMachines();
    expect(useAppStore.getState().machines).toHaveLength(1);

    mock.listMachines.mockRejectedValueOnce(new Error("boom"));
    await store.fetchMachines(undefined, { silent: true });
    expect(useAppStore.getState().machines).toHaveLength(1);

    mock.listMachines.mockRejectedValueOnce(new Error("boom"));
    await expect(store.fetchMachines()).resolves.toBeUndefined();
    expect(useAppStore.getState().machines).toHaveLength(0);
    expect(useAppStore.getState().machinesLoading).toBe(false);
    // A later call recovers (the failed attempt did not poison the cache).
    await store.fetchMachines();
    expect(useAppStore.getState().machines).toHaveLength(1);
  });

  it("invalidateMachinesCache drops the slice's Query cache entry", async () => {
    await useAppStore.getState().fetchMachines();
    expect(queryClient.getQueryData(["machines", false])).toBeDefined();
    invalidateMachinesCache();
    expect(queryClient.getQueryData(["machines", false])).toBeUndefined();
  });
});
