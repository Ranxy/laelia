import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./index";

// --- mock @/connect so the members slice talks to controllable rosters ---
const mock = vi.hoisted(() => ({
  listUsersError: false as boolean,
  listAgentsError: false as boolean,
  // Captured call args so tests can assert the roster never asks for the
  // internal SYSTEM_BOT account (includeSystemBot stays false by default).
  listUsersCalls: [] as Array<{ includeSystemBot?: boolean }>,
}));

vi.mock("@/connect", () => ({
  userServiceClient: {
    async listUsers(args: { includeSystemBot?: boolean } = {}) {
      mock.listUsersCalls.push(args);
      if (mock.listUsersError) throw new Error("boom");
      return {
        users: [{ name: "users/1", title: "Human One", email: "h1@x" }],
        nextPageToken: "",
      };
    },
  },
  agentServiceClient: {
    async listAgents() {
      if (mock.listAgentsError) throw new Error("boom");
      return {
        agents: [
          {
            name: "agents/a",
            title: "Agent A",
            machine: "machines/m1",
            status: { state: 2 },
            enabled: true,
          },
        ],
        nextPageToken: "",
      };
    },
  },
  machineServiceClient: {},
}));

beforeEach(() => {
  useAppStore.setState({
    members: [],
    membersLoading: false,
    membersError: false,
    users: [],
    agents: [],
  });
  mock.listUsersError = false;
  mock.listAgentsError = false;
  mock.listUsersCalls = [];
});

describe("fetchMembers", () => {
  it("populates the merged roster and flips loading for an explicit load", async () => {
    await useAppStore.getState().fetchMembers();

    const state = useAppStore.getState();
    expect(state.membersLoading).toBe(false);
    expect(state.membersError).toBe(false);
    expect(state.members).toEqual([
      {
        kind: "agent",
        name: "agents/a",
        title: "Agent A",
        subtitle: "machines/m1",
        connectionState: 2,
        enabled: true,
      },
      {
        kind: "user",
        name: "users/1",
        title: "Human One",
        subtitle: "h1@x",
      },
    ]);
  });

  it("keeps the cached roster visible during a silent refresh", async () => {
    useAppStore.setState({
      members: [
        {
          kind: "user",
          name: "users/1",
          title: "Human One",
          subtitle: "h1@x",
        },
      ],
      membersLoading: false,
    });

    await useAppStore.getState().fetchMembers({ silent: true });

    const state = useAppStore.getState();
    expect(state.membersLoading).toBe(false);
    expect(state.members.length).toBe(2);
  });

  it("never requests the system bot from the user roster", async () => {
    await useAppStore.getState().fetchMembers();

    expect(mock.listUsersCalls.length).toBeGreaterThan(0);
    for (const call of mock.listUsersCalls) {
      expect(call.includeSystemBot).toBe(false);
    }
  });

  it("keeps cached members on a silent refresh failure", async () => {
    useAppStore.setState({
      members: [
        {
          kind: "user",
          name: "users/1",
          title: "Human One",
          subtitle: "h1@x",
        },
      ],
      membersLoading: false,
      membersError: false,
    });
    mock.listUsersError = true;

    await expect(
      useAppStore.getState().fetchMembers({ silent: true })
    ).resolves.toBeUndefined();

    const state = useAppStore.getState();
    expect(state.members).toHaveLength(1);
    expect(state.membersLoading).toBe(false);
    expect(state.membersError).toBe(false);
  });
});
