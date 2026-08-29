import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { McpServerSchema } from "@/types/proto-es/v1/mcp_pb";
import { useAppStore } from "./index";

// --- mock @/connect: the slice's three RPCs become counting stubs ---
const mock = vi.hoisted(() => {
  const counters = { ws: 0, my: 0, cfg: 0 };
  return {
    counters,
    listMcpServers: vi.fn(async () => {
      counters.ws++;
      return {
        mcpServers: [create(McpServerSchema, { name: "mcp-servers/ws-a" })],
        nextPageToken: "",
      };
    }),
    listMyMcpServers: vi.fn(async () => {
      counters.my++;
      return { mcpServers: [], nextPageToken: "" };
    }),
    getSetting: vi.fn(async () => ({
      value: {
        case: "userMcpConfig" as const,
        value: { allowUserMcpServers: true },
      },
    })),
  };
});

vi.mock("@/connect", () => ({
  mcpServerServiceClient: {
    listMcpServers: mock.listMcpServers,
    listMyMcpServers: mock.listMyMcpServers,
  },
  settingServiceClient: {
    getSetting: mock.getSetting,
  },
}));

import { queryClient } from "@/lib/query-client";
import { invalidateMcpServersCache } from "./mcp";

beforeEach(() => {
  invalidateMcpServersCache();
  useAppStore.setState({ mcpServers: [], mcpServersLoading: false });
  mock.counters.ws = 0;
  mock.counters.my = 0;
  mock.counters.cfg = 0;
});

describe("mcp slice (queryClient-backed)", () => {
  it("dedupes concurrent in-flight fetches into one RPC round", async () => {
    const store = useAppStore.getState();
    const [r1, r2] = await Promise.all([
      store.fetchMcpServers(),
      store.fetchMcpServers(),
    ]);
    // Both callers resolve successfully with the same pageToken contract.
    expect(r1).toEqual(r2);
    expect(r1).toBeDefined();
    expect(mock.counters.ws).toBe(1);
    expect(mock.counters.my).toBe(1);
    expect(useAppStore.getState().mcpServers).toHaveLength(1);
    void queryClient;
  });

  it("keeps the previous store reference when a silent refresh is unchanged", async () => {
    await useAppStore.getState().fetchMcpServers();
    const before = useAppStore.getState().mcpServers;
    await useAppStore.getState().fetchMcpServers(undefined, { silent: true });
    expect(useAppStore.getState().mcpServers).toBe(before);
  });

  it("clears the list on an explicit load failure but keeps it on silent", async () => {
    await useAppStore.getState().fetchMcpServers();
    expect(useAppStore.getState().mcpServers).toHaveLength(1);

    mock.listMcpServers.mockRejectedValueOnce(new Error("boom"));
    await useAppStore.getState().fetchMcpServers(undefined, { silent: true });
    expect(useAppStore.getState().mcpServers).toHaveLength(1);

    mock.listMcpServers.mockRejectedValueOnce(new Error("boom"));
    await expect(
      useAppStore.getState().fetchMcpServers()
    ).resolves.toBeUndefined();
    expect(useAppStore.getState().mcpServers).toHaveLength(0);
    expect(useAppStore.getState().mcpServersLoading).toBe(false);
    // A later call recovers (the failed attempt did not poison the cache).
    await useAppStore.getState().fetchMcpServers();
    expect(useAppStore.getState().mcpServers).toHaveLength(1);
  });
});
