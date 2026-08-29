import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { queryClient } from "@/lib/query-client";
import { State } from "@/types/proto-es/v1/common_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useAppStore } from "./index";

// --- mock @/connect: listUsers becomes a counting stub echoing the request ---
const mock = vi.hoisted(() => {
  const counters = { list: 0 };
  return {
    counters,
    listUsers: vi.fn(
      async (req: { showDeleted?: boolean; filter?: string }) => {
        counters.list++;
        if (req.showDeleted) {
          // A real show_deleted=true response carries active + deleted rows;
          // the slice must filter to state == DELETED.
          return {
            users: [
              create(UserSchema, { name: "users/u1", state: State.DELETED }),
              create(UserSchema, { name: "users/u2", state: State.ACTIVE }),
            ],
            nextPageToken: "",
          };
        }
        return {
          users: [create(UserSchema, { name: `users/${req.filter || "all"}` })],
          nextPageToken: "",
        };
      }
    ),
  };
});

vi.mock("@/connect", () => ({
  userServiceClient: {
    listUsers: mock.listUsers,
  },
}));

import { invalidateUsersCache } from "./user";

beforeEach(() => {
  invalidateUsersCache();
  useAppStore.setState({
    users: [],
    usersLoading: false,
    deletedUsers: [],
    deletedUsersLoading: false,
  });
  mock.counters.list = 0;
});

describe("user slice (queryClient-backed)", () => {
  it("dedupes concurrent in-flight fetches into one RPC round", async () => {
    const store = useAppStore.getState();
    const [r1, r2] = await Promise.all([
      store.fetchUsers(),
      store.fetchUsers(),
    ]);
    // Both callers resolve successfully with the same pageToken contract.
    expect(r1).toEqual(r2);
    expect(r1).toBeDefined();
    expect(mock.counters.list).toBe(1);
    expect(useAppStore.getState().users).toHaveLength(1);
  });

  it("keeps distinct views on distinct cache entries (showDeleted/filter participate in the key)", async () => {
    const store = useAppStore.getState();
    // Different filter / showDeleted values select different server views;
    // none of these four may merge into a shared in-flight request.
    await Promise.all([
      store.fetchUsers(),
      store.fetchUsers({ showDeleted: true }),
      store.fetchUsers({ filter: "a" }),
      store.fetchUsers({ filter: "b" }),
    ]);
    expect(mock.counters.list).toBe(4);
  });

  it("writes the soft-deleted filter view into deletedUsers", async () => {
    await useAppStore.getState().fetchUsers({ showDeleted: true });
    const { deletedUsers, users } = useAppStore.getState();
    expect(deletedUsers.map((u) => u.name)).toEqual(["users/u1"]);
    expect(users).toHaveLength(0);
  });

  it("keeps the previous store reference when a silent refresh is unchanged", async () => {
    const store = useAppStore.getState();
    // Active roster. The second call spells out pageSize (still the 100
    // default) so the same-key silent refresh exercises the equal-bailout.
    await store.fetchUsers();
    const beforeUsers = useAppStore.getState().users;
    await store.fetchUsers({ pageSize: 100 }, { silent: true });
    expect(useAppStore.getState().users).toBe(beforeUsers);

    // Recycle bin (the second show_deleted list)
    await store.fetchUsers({ showDeleted: true });
    const beforeDeleted = useAppStore.getState().deletedUsers;
    await store.fetchUsers({ showDeleted: true }, { silent: true });
    expect(useAppStore.getState().deletedUsers).toBe(beforeDeleted);
  });

  it("clears the list on an explicit load failure but keeps it on silent", async () => {
    const store = useAppStore.getState();
    await store.fetchUsers();
    expect(useAppStore.getState().users).toHaveLength(1);

    mock.listUsers.mockRejectedValueOnce(new Error("boom"));
    await store.fetchUsers(undefined, { silent: true });
    expect(useAppStore.getState().users).toHaveLength(1);

    mock.listUsers.mockRejectedValueOnce(new Error("boom"));
    await expect(store.fetchUsers()).resolves.toBeUndefined();
    expect(useAppStore.getState().users).toHaveLength(0);
    expect(useAppStore.getState().usersLoading).toBe(false);
    // A later call recovers (the failed attempt did not poison the cache).
    await store.fetchUsers();
    expect(useAppStore.getState().users).toHaveLength(1);
  });

  it("clears deletedUsers on an explicit trash-load failure and recovers", async () => {
    const store = useAppStore.getState();
    await store.fetchUsers({ showDeleted: true });
    expect(useAppStore.getState().deletedUsers).toHaveLength(1);

    mock.listUsers.mockRejectedValueOnce(new Error("boom"));
    await expect(
      store.fetchUsers({ showDeleted: true })
    ).resolves.toBeUndefined();
    expect(useAppStore.getState().deletedUsers).toHaveLength(0);
    expect(useAppStore.getState().deletedUsersLoading).toBe(false);

    await store.fetchUsers({ showDeleted: true });
    expect(queryClient.getQueryData(["users", true, "", false])).toBeDefined();
    invalidateUsersCache();
    expect(
      queryClient.getQueryData(["users", true, "", false])
    ).toBeUndefined();
  });
});
