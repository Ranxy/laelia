import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the presence slice talks to a controllable server ---
const mock = vi.hoisted(() => ({
  // The canned SyncPresence response: map of name -> online, served per call.
  online: {} as Record<string, boolean>,
  // Captured call args for assertions on what the slice asks the server.
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
  },
}));

beforeEach(() => {
  useAppStore.setState({ onlineUsers: {} });
  mock.online = {};
  mock.calls = [];
  mock.fail = false;
});

describe("syncPresence", () => {
  it("records the returned online state per user", async () => {
    mock.online = { "users/alice": true };

    await useAppStore.getState().syncPresence(["users/alice", "users/bob"]);

    expect(useAppStore.getState().onlineUsers).toEqual({
      "users/alice": true,
      "users/bob": false,
    });
  });

  it("ignores non-user names (agent presence comes from the agents slice)", async () => {
    await useAppStore.getState().syncPresence(["agents/rei", "users/alice"]);

    expect(mock.calls[0].names).toEqual(["users/alice"]);
    expect(useAppStore.getState().onlineUsers["agents/rei"]).toBeUndefined();
  });

  it("still heartbeats with an empty query (keeps the caller online)", async () => {
    await useAppStore.getState().syncPresence(["agents/rei"]);

    // Agent names are dropped, but the call itself fires: the server records
    // the signed-in user's own heartbeat from the auth context.
    expect(mock.calls).toHaveLength(1);
    expect(mock.calls[0].names).toEqual([]);
    expect(useAppStore.getState().onlineUsers).toEqual({});
  });

  it("skips the store write when the online map is unchanged", async () => {
    useAppStore.setState({ onlineUsers: { "users/alice": true } });
    mock.online = { "users/alice": true };
    const before = useAppStore.getState().onlineUsers;

    await useAppStore.getState().syncPresence(["users/alice"]);

    expect(useAppStore.getState().onlineUsers).toBe(before);
  });

  it("keeps the last known state on a failed sync", async () => {
    mock.online = { "users/alice": true };
    await useAppStore.getState().syncPresence(["users/alice"]);
    expect(useAppStore.getState().onlineUsers["users/alice"]).toBe(true);

    mock.fail = true;
    await useAppStore.getState().syncPresence(["users/alice"]);

    expect(useAppStore.getState().onlineUsers["users/alice"]).toBe(true);
  });

  it("caps the query at the server's 200-name limit", async () => {
    const names = Array.from({ length: 300 }, (_, i) => `users/u${i}`);

    await useAppStore.getState().syncPresence(names);

    expect(mock.calls[0].names).toHaveLength(200);
  });
});
