import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { userServiceClient } from "@/connect";
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
import type { AppSliceCreator, UserSlice } from "./types";

export const createUserSlice: AppSliceCreator<UserSlice> = (set) => ({
  users: [],
  usersLoading: false,
  deletedUsers: [],
  deletedUsersLoading: false,

  async fetchUsers(params, opts) {
    const showDeleted = params?.showDeleted ?? false;
    const silent = opts?.silent;
    if (!silent) {
      set(showDeleted ? { deletedUsersLoading: true } : { usersLoading: true });
    }
    try {
      const res = await userServiceClient.listUsers(
        create(ListUsersRequestSchema, {
          pageSize: params?.pageSize ?? 100,
          pageToken: params?.pageToken ?? "",
          showDeleted,
          filter: params?.filter ?? "",
        })
      );
      if (showDeleted) {
        // `show_deleted=true` returns active + deleted; the recycle bin only
        // cares about soft-deleted users, so filter down to state == DELETED.
        set({
          deletedUsers: res.users.filter((u) => u.state === State.DELETED),
          deletedUsersLoading: false,
        });
      } else {
        set({ users: res.users, usersLoading: false });
      }
      return { nextPageToken: res.nextPageToken };
    } catch {
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
