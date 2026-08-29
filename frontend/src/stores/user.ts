import { create, equals } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { userServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import { State } from "@/types/proto-es/v1/common_pb";
import {
  CreateUserRequestSchema,
  DeleteUserRequestSchema,
  ListUsersRequestSchema,
  UndeleteUserRequestSchema,
  UpdateUserRequestSchema,
  UserSchema,
  UserType,
} from "@/types/proto-es/v1/user_service_pb";
import { sameList } from "./list-equals";
import type { AppSliceCreator, UserSlice } from "./types";

// Query cache key family for this slice (ADR-1: query keys live with the slice
// that fetches them). The key carries every parameter that selects a different
// server view — showDeleted (active roster vs recycle bin), filter (server-side
// search) and includeSystemBot (only the settings directory opts in) — so two
// distinct views never share one cache entry. pageSize and pageToken stay OUT
// of the key on purpose: the only pageToken caller (members.ts drainRoster)
// pages strictly sequentially, and keeping repeat calls of the same view on one
// key lets fetchQuery's in-flight merge dedupe them (e.g. a poll racing the
// user-list debounce). The tradeoff: two truly concurrent same-key calls with
// different paging params resolve with whichever queryFn registered first —
// today every same-key caller pair agrees on paging params, so the merged
// result matches what the merged-away call would have fetched.
// staleTime: 0 keeps "action call == one explicit fetch" semantics so
// page-level pollers keep their cadence; retry: false keeps the explicit
// failure path below deterministic.

// Logout clears the slice's Query cache (whole ["users", ...] family: both the
// active and the delete-pending entries). Wired up at the batch-3 unified
// release point; until then a logout-relogin can serve gcTime-stale data
// briefly, bounded by the 5-minute gcTime in query-client defaults.
export function invalidateUsersCache(): void {
  void queryClient.removeQueries({ queryKey: ["users"] });
}

export const createUserSlice: AppSliceCreator<UserSlice> = (set, get) => ({
  users: [],
  usersLoading: false,
  deletedUsers: [],
  deletedUsersLoading: false,

  async fetchUsers(params, opts) {
    const showDeleted = params?.showDeleted ?? false;
    const filter = params?.filter ?? "";
    const includeSystemBot = params?.includeSystemBot ?? false;
    const silent = opts?.silent;
    if (!silent) {
      set(showDeleted ? { deletedUsersLoading: true } : { usersLoading: true });
    }
    try {
      const res = await queryClient.fetchQuery({
        queryKey: ["users", showDeleted, filter, includeSystemBot],
        // Action semantics: exactly one RPC attempt per call — retries are
        // batch-3 page-level useQuery territory.
        retry: false,
        staleTime: 0,
        queryFn: async () => {
          const rpc = await userServiceClient.listUsers(
            create(ListUsersRequestSchema, {
              pageSize: params?.pageSize ?? 100,
              pageToken: params?.pageToken ?? "",
              showDeleted,
              filter,
              includeSystemBot,
            })
          );
          return {
            // `show_deleted=true` returns active + deleted; the recycle bin
            // only cares about soft-deleted users, so filter down to
            // state == DELETED. Filtering inside the queryFn keeps the cached
            // entry identical to the mirrored store field, so batch-3
            // useQuery consumers of this key read the same shape the recycle
            // bin renders.
            users: showDeleted
              ? rpc.users.filter((u) => u.state === State.DELETED)
              : rpc.users,
            nextPageToken: rpc.nextPageToken,
          };
        },
      });
      if (showDeleted) {
        // Skip the state update entirely when nothing changed, so unchanged
        // polls cause no re-render at all (the store field is a mirrored view
        // of the Query cache during the migration; components still subscribe
        // to the store).
        if (
          silent &&
          sameList(get().deletedUsers, res.users, (a, b) =>
            equals(UserSchema, a, b)
          )
        ) {
          set({ deletedUsersLoading: false });
          return { nextPageToken: res.nextPageToken };
        }
        set({
          deletedUsers: res.users,
          deletedUsersLoading: false,
        });
      } else if (
        silent &&
        sameList(get().users, res.users, (a, b) => equals(UserSchema, a, b))
      ) {
        set({ usersLoading: false });
      } else {
        set({ users: res.users, usersLoading: false });
      }
      return { nextPageToken: res.nextPageToken };
    } catch {
      // On a silent refresh, keep the existing list instead of wiping it on a
      // transient error; only an explicit load reports failure + clears.
      if (!silent) {
        if (showDeleted) set({ deletedUsers: [], deletedUsersLoading: false });
        else set({ users: [], usersLoading: false });
      }
      return undefined;
    }
  },

  async createUser(input) {
    const res = await userServiceClient.createUser(
      create(CreateUserRequestSchema, {
        user: create(UserSchema, {
          email: input.email,
          title: input.title,
          phone: input.phone ?? "",
          description: input.description ?? "",
          password: input.password,
          userType: UserType.USER,
        }),
      })
    );
    return res;
  },

  async updateUser(name, fields, maskPaths) {
    const res = await userServiceClient.updateUser(
      create(UpdateUserRequestSchema, {
        user: create(UserSchema, { name, ...fields }),
        updateMask: create(FieldMaskSchema, { paths: maskPaths }),
      })
    );
    return res;
  },

  async resetPassword(name, newPassword) {
    const res = await userServiceClient.updateUser(
      create(UpdateUserRequestSchema, {
        user: create(UserSchema, { name, password: newPassword }),
        updateMask: create(FieldMaskSchema, { paths: ["password"] }),
      })
    );
    return res;
  },

  async deleteUser(name) {
    await userServiceClient.deleteUser(
      create(DeleteUserRequestSchema, { name })
    );
  },

  async undeleteUser(name) {
    const res = await userServiceClient.undeleteUser(
      create(UndeleteUserRequestSchema, { name })
    );
    return res;
  },
});
