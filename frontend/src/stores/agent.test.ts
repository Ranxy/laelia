import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentSummarySchema } from "@/types/proto-es/v1/agent_pb";
import { useAppStore } from "./index";

// --- mock @/connect: listAgents becomes a counting stub ---
const mock = vi.hoisted(() => {
  const counters = { list: 0 };
  return {
    counters,
    listAgents: vi.fn(async () => {
      counters.list++;
      return {
        agents: [
          create(AgentSummarySchema, { name: "agents/a1", title: "One" }),
        ],
        nextPageToken: "",
      };
    }),
  };
});

vi.mock("@/connect", () => ({
  agentServiceClient: {
    listAgents: mock.listAgents,
  },
}));

import { queryClient } from "@/lib/query-client";
import { invalidateAgentsCache } from "./agent";

beforeEach(() => {
  invalidateAgentsCache();
  useAppStore.setState({ agents: [], agentsLoading: false });
  mock.counters.list = 0;
});

describe("agent slice (queryClient-backed)", () => {
  it("dedupes concurrent in-flight fetches into one RPC round", async () => {
    const store = useAppStore.getState();
    const [r1, r2] = await Promise.all([
      store.fetchAgents(),
      store.fetchAgents(),
    ]);
    expect(r1).toEqual(r2);
    expect(r1).toBeDefined();
    expect(mock.counters.list).toBe(1);
    expect(useAppStore.getState().agents).toHaveLength(1);
  });

  it("a silent heartbeat call is still exactly one RPC per tick", async () => {
    // The presence heartbeat refetches silently every 30s; staleTime: 0 must
    // not serve a cached list in place of the tick's RPC.
    await useAppStore.getState().fetchAgents(undefined, { silent: true });
    expect(mock.counters.list).toBe(1);
    await useAppStore.getState().fetchAgents(undefined, { silent: true });
    expect(mock.counters.list).toBe(2);
  });

  it("keeps the previous store reference when a silent refresh is unchanged", async () => {
    await useAppStore.getState().fetchAgents();
    const before = useAppStore.getState().agents;
    await useAppStore.getState().fetchAgents(undefined, { silent: true });
    expect(useAppStore.getState().agents).toBe(before);
  });

  it("replaces the reference when a silent refresh brings changed data", async () => {
    await useAppStore.getState().fetchAgents();
    const before = useAppStore.getState().agents;
    mock.listAgents.mockImplementationOnce(async () => {
      mock.counters.list++;
      return {
        agents: [
          create(AgentSummarySchema, {
            name: "agents/a1",
            title: "One",
            description: "changed",
          }),
        ],
        nextPageToken: "",
      };
    });
    await useAppStore.getState().fetchAgents(undefined, { silent: true });
    expect(useAppStore.getState().agents).not.toBe(before);
    expect(useAppStore.getState().agents[0].description).toBe("changed");
  });

  it("clears the list on an explicit load failure but keeps it on silent", async () => {
    const store = useAppStore.getState();
    await store.fetchAgents();
    expect(useAppStore.getState().agents).toHaveLength(1);

    mock.listAgents.mockRejectedValueOnce(new Error("boom"));
    await store.fetchAgents(undefined, { silent: true });
    expect(useAppStore.getState().agents).toHaveLength(1);

    mock.listAgents.mockRejectedValueOnce(new Error("boom"));
    await expect(store.fetchAgents()).resolves.toBeUndefined();
    expect(useAppStore.getState().agents).toHaveLength(0);
    expect(useAppStore.getState().agentsLoading).toBe(false);
    // A later call recovers (the failed attempt did not poison the cache).
    await store.fetchAgents();
    expect(useAppStore.getState().agents).toHaveLength(1);
  });

  it("invalidateAgentsCache drops the slice's Query cache entry", async () => {
    await useAppStore.getState().fetchAgents();
    expect(queryClient.getQueryData(["agents"])).toBeDefined();
    invalidateAgentsCache();
    expect(queryClient.getQueryData(["agents"])).toBeUndefined();
  });
});
