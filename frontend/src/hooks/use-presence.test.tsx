import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { queryClient as globalQueryClient } from "@/lib/query-client";
import { useAppStore } from "@/stores";
import {
  PRESENCE_QUERY_KEY,
  usePresenceHeartbeat,
  useUserPresence,
} from "./use-presence";

// --- mock @/connect so the heartbeat and the read loop talk to a
// --- controllable server -----------------------------------------------------
const mock = vi.hoisted(() => ({
  beats: 0,
  beatFail: false,
  readFail: false,
  // The server-defined whole-workspace presence the read loop gets back.
  presences: {} as Record<string, { online: boolean; seconds?: bigint }>,
}));

vi.mock("@/connect", () => ({
  presenceServiceClient: {
    async sendHeartbeat() {
      mock.beats += 1;
      if (mock.beatFail) throw new Error("boom");
      return {};
    },
    async listPresence() {
      if (mock.readFail) throw new Error("boom");
      return {
        presences: Object.entries(mock.presences).map(([name, p]) => ({
          name,
          online: p.online,
          lastSeenAt: p.seconds ? { seconds: p.seconds, nanos: 0 } : undefined,
        })),
      };
    },
  },
  // The heartbeat's same-cadence silent agents refresh rides the real slice
  // action → the global queryClient → this client.
  commandServiceClient: {
    async listAgents() {
      return { agents: [], nextPageToken: "" };
    },
  },
}));

// --- harness ----------------------------------------------------------------

function wrapperFor(client: QueryClient) {
  return function QueryWrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
  };
}

// Mount the heartbeat with a useUserPresence reader — production mounts the
// heartbeat once at the dashboard layout while consumers subscribe to the
// shared cache.
function mountBeatAndReader(name?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const useBeatWithReader = () => {
    usePresenceHeartbeat();
    return useUserPresence(name);
  };
  return {
    client,
    ...renderHook(useBeatWithReader, { wrapper: wrapperFor(client) }),
  };
}

beforeEach(() => {
  mock.beats = 0;
  mock.beatFail = false;
  mock.readFail = false;
  mock.presences = {};
  globalQueryClient.removeQueries({ queryKey: PRESENCE_QUERY_KEY });
});

// --- heartbeat contract ------------------------------------------------------

describe("usePresenceHeartbeat", () => {
  it("beats immediately on mount, regardless of what the store has loaded", async () => {
    // The redesign's core property: the heartbeat never consults the Zustand
    // store, so a refresh with an empty store still beats right away (the old
    // first-beat race sent an empty name list and echoed nothing back).
    mountBeatAndReader();
    await vi.waitFor(() => expect(mock.beats).toBe(1));
  });

  it("keeps beating after a failed beat (silent, retried next beat)", async () => {
    vi.useFakeTimers();
    try {
      mock.beatFail = true;
      mountBeatAndReader();
      await vi.advanceTimersByTimeAsync(0); // flush the mount beat
      expect(mock.beats).toBe(1);

      mock.beatFail = false;
      await vi.advanceTimersByTimeAsync(30_000); // the next cadence tick
      expect(mock.beats).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

// --- read contract -----------------------------------------------------------

describe("usePresenceMap", () => {
  it("resolves a user from the server-defined full map with last seen", async () => {
    mock.presences = {
      "users/alice": { online: true, seconds: 1700000000n },
    };
    const { result } = mountBeatAndReader("users/alice");

    await vi.waitFor(() => expect(result.current).toBeDefined());
    expect(result.current?.online).toBe(true);
    expect(result.current?.lastSeenAt?.getTime()).toBe(1700000000 * 1000);
  });

  it("answers offline for a user absent from the loaded map", async () => {
    mock.presences = { "users/alice": { online: true } };
    const { result } = mountBeatAndReader("users/bob");

    await vi.waitFor(() => expect(result.current).toBeDefined());
    // Absent from a full response = never heartbeated = offline, never
    // "unknown".
    expect(result.current?.online).toBe(false);
    expect(result.current?.lastSeenAt).toBeUndefined();
  });

  it("renders unknown (undefined) while the first fetch is in flight", () => {
    const { result } = mountBeatAndReader("users/alice");
    expect(result.current).toBeUndefined();
  });

  it("replaces the whole map every read — stale entries cannot survive", async () => {
    // Regression for the old merge-forever cache: a peer learned "online"
    // must drop out once the server stops reporting it, not stick around.
    mock.presences = { "users/alice": { online: true } };
    const { client, result } = mountBeatAndReader("users/alice");
    await vi.waitFor(() => expect(result.current?.online).toBe(true));

    mock.presences = {};
    await refetchPresenceMap(client);

    await vi.waitFor(() => expect(result.current?.online).toBe(false));
    expect(
      client.getQueryData<Record<string, unknown>>(PRESENCE_QUERY_KEY)
    ).toEqual({});
  });

  it("keeps the previous map while a read fails, then recovers", async () => {
    mock.presences = { "users/alice": { online: true } };
    const { client, result } = mountBeatAndReader("users/alice");
    await vi.waitFor(() => expect(result.current?.online).toBe(true));

    mock.readFail = true;
    await refetchPresenceMap(client);
    expect(result.current?.online).toBe(true); // frozen at last known

    mock.readFail = false;
    await refetchPresenceMap(client);
    expect(result.current?.online).toBe(true);
  });

  it("answers unknown for a missing name argument", () => {
    const { result } = mountBeatAndReader(undefined);
    expect(result.current).toBeUndefined();
  });
});

// --- reset contract ----------------------------------------------------------

describe("presence reset", () => {
  it("clears the presence cache on logout reset (cleanup registration)", () => {
    globalQueryClient.setQueryData(PRESENCE_QUERY_KEY, {
      "users/alice": { online: true },
    });

    useAppStore.getState().reset();

    expect(globalQueryClient.getQueryData(PRESENCE_QUERY_KEY)).toBeUndefined();
    expect(useAppStore.getState().channels).toEqual([]);
  });
});

// --- helpers -----------------------------------------------------------------

async function refetchPresenceMap(client: QueryClient) {
  await act(async () => {
    await client.refetchQueries({ queryKey: PRESENCE_QUERY_KEY });
  });
}
