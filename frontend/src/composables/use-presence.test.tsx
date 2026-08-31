import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { queryClient as globalQueryClient } from "@/lib/query-client";
import { useAppStore } from "@/stores";
import {
  PRESENCES_QUERY_KEY,
  useOnlineUsers,
  usePresenceHeartbeat,
} from "./use-presence-heartbeat";

// --- mock @/connect so the heartbeat talks to a controllable server ---------
const mock = vi.hoisted(() => ({
  online: {} as Record<string, boolean>,
  calls: [] as Array<{ names: string[] }>,
  fail: false as boolean,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    async syncPresence(args: { names: string[] }) {
      mock.calls.push(args);
      if (mock.fail) throw new Error("boom");
      return {
        presences: args.names.map((name) => ({
          name,
          online: mock.online[name] ?? false,
        })),
      };
    },
    // The heartbeat's same-cadence silent agents refresh rides the real slice
    // action → the global queryClient → this client.
    async listAgents() {
      return { agents: [], nextPageToken: "" };
    },
  },
}));

// --- harness ----------------------------------------------------------------

function setupStore(rosters: {
  channels?: unknown[];
  users?: unknown[];
  channelMembersByConv?: Record<string, unknown[]>;
}) {
  useAppStore.setState({
    channels: (rosters.channels ?? []) as never,
    users: (rosters.users ?? []) as never,
    channelMembersByConv: (rosters.channelMembersByConv ?? {}) as never,
  });
}

function wrapperFor(client: QueryClient) {
  return function QueryWrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

// Mount the heartbeat with a reader of the same cache — production mounts
// exactly one client, so the hook under test and useOnlineUsers share it.
function mountBeatAndReader(opts?: {
  channels?: unknown[];
  users?: unknown[];
  channelMembersByConv?: Record<string, unknown[]>;
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  if (opts) setupStore(opts);
  const useBeatWithReader = () => {
    usePresenceHeartbeat();
    return useOnlineUsers();
  };
  return {
    client,
    ...renderHook(useBeatWithReader, { wrapper: wrapperFor(client) }),
  };
}

// Trigger one extra beat deterministically (the cadence test uses fake timers
// instead).
async function refetchPresence(client: QueryClient) {
  await act(async () => {
    await client.refetchQueries({ queryKey: PRESENCES_QUERY_KEY });
  });
}

beforeEach(() => {
  mock.online = {};
  mock.calls = [];
  mock.fail = false;
  useAppStore.setState({ channels: [], users: [], channelMembersByConv: {} });
  globalQueryClient.removeQueries({ queryKey: PRESENCES_QUERY_KEY });
});

// --- heartbeat contract ------------------------------------------------------

describe("usePresenceHeartbeat", () => {
  it("beats immediately on mount with every human name the UI can show", async () => {
    const { result } = mountBeatAndReader({
      channels: [{ peer: "users/alice" }],
      users: [{ name: "users/bob" }],
      channelMembersByConv: {
        c1: [{ memberType: 1, memberId: "carol" }],
      },
    });

    await vi.waitFor(() => expect(mock.calls.length).toBe(1));
    expect(mock.calls[0].names.sort().join()).toBe(
      "users/alice,users/bob,users/carol"
    );
    // The online map lands in the cache for readers.
    await vi.waitFor(() =>
      expect(result.current).toEqual({
        "users/alice": false,
        "users/bob": false,
        "users/carol": false,
      })
    );
  });

  it("drops agent names — their online signal lives on the agents slice", async () => {
    mountBeatAndReader({
      channels: [{ peer: "agents/rei" }, { peer: "users/alice" }],
    });
    await vi.waitFor(() => expect(mock.calls.length).toBe(1));
    expect(mock.calls[0].names).toEqual(["users/alice"]);
  });

  it("still heartbeats with an empty query list (keeps the caller online)", async () => {
    mountBeatAndReader();

    await vi.waitFor(() => expect(mock.calls.length).toBe(1));
    expect(mock.calls[0].names).toEqual([]);
  });

  it("caps the query at the server's 200-name limit", async () => {
    mountBeatAndReader({
      users: Array.from({ length: 300 }, (_, i) => ({ name: `users/u${i}` })),
    });

    await vi.waitFor(() => expect(mock.calls.length).toBe(1));
    expect(mock.calls[0].names).toHaveLength(200);
  });

  it("keeps the last known map when a beat fails", async () => {
    mock.online = { "users/alice": true };
    const { client, result } = mountBeatAndReader({
      users: [{ name: "users/alice" }],
    });
    await vi.waitFor(() => expect(result.current["users/alice"]).toBe(true));

    mock.fail = true;
    await refetchPresence(client);

    expect(mock.calls.length).toBe(2); // the failed beat did fire
    expect(result.current["users/alice"]).toBe(true); // nothing was wiped
  });

  it("merges over peers learned from rosters that have since unloaded", async () => {
    mock.online = { "users/alice": true };
    const { client, result } = mountBeatAndReader({
      users: [{ name: "users/alice" }],
    });
    await vi.waitFor(() => expect(result.current["users/alice"]).toBe(true));

    setupStore({ users: [] });
    mock.online = {};
    await refetchPresence(client);

    // Roster gone → the beat queries nobody, but the learned peer persists.
    expect(mock.calls.at(-1)?.names).toEqual([]);
    expect(result.current["users/alice"]).toBe(true);
  });

  it("keeps the map identity stable across a beat that learns nothing new", async () => {
    mock.online = { "users/alice": true };
    const { client, result } = mountBeatAndReader({
      users: [{ name: "users/alice" }],
    });
    await vi.waitFor(() => expect(result.current["users/alice"]).toBe(true));

    const before = result.current;
    await refetchPresence(client);

    expect(result.current).toEqual(before);
    expect(result.current).toBe(before); // structural sharing — no re-render churn
  });
});

// --- reader contract ---------------------------------------------------------

describe("useOnlineUsers", () => {
  it("reads the heartbeat's cache without fetching on its own behalf", async () => {
    mock.online = { "users/bob": false };
    const { client: heartbeatClient, result } = mountBeatAndReader({
      users: [{ name: "users/bob" }],
    });
    await vi.waitFor(() => expect(result.current["users/bob"]).toBe(false));

    // The reader hook (enabled: false) replays the shared cache — production
    // mounts one client, so the standalone reader sees the same entry.
    expect(heartbeatClient.getQueryData(PRESENCES_QUERY_KEY)).toEqual({
      "users/bob": false,
    });
    // No beat of its own was triggered by the extra reader.
    expect(mock.calls.length).toBe(1);
  });
});

// --- reset contract ----------------------------------------------------------

describe("presence reset", () => {
  it("clears the presence cache on logout reset (cleanup registration)", () => {
    globalQueryClient.setQueryData(PRESENCES_QUERY_KEY, {
      "users/alice": true,
    });

    useAppStore.getState().reset();

    expect(globalQueryClient.getQueryData(PRESENCES_QUERY_KEY)).toBeUndefined();
    expect(useAppStore.getState().channels).toEqual([]);
  });
});
