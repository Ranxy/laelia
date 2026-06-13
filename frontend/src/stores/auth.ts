import { create } from "@bufbuild/protobuf";
import { authServiceClient, userServiceClient } from "@/connect";
import {
  LoginRequestSchema,
  LogoutRequestSchema,
} from "@/types/proto-es/v1/auth_service_pb";
import {
  CreateUserRequestSchema,
  UserSchema,
  UserType,
} from "@/types/proto-es/v1/user_service_pb";
import type { AppSliceCreator, AuthSlice } from "./types";

export const createAuthSlice: AppSliceCreator<AuthSlice> = (set, get) => ({
  currentUser: null,
  token: null,
  isLoggedIn: false,
  sessionLoaded: false,

  async login(email: string, password: string) {
    const res = await authServiceClient.login(
      create(LoginRequestSchema, { email, password, web: true })
    );

    set({
      token: res.token,
      currentUser: res.user ?? null,
      isLoggedIn: true,
    });
  },

  async logout() {
    try {
      await authServiceClient.logout(create(LogoutRequestSchema));
    } finally {
      set({ token: null, currentUser: null, isLoggedIn: false });
    }
  },

  async register(email: string, title: string, password: string) {
    await userServiceClient.createUser(
      create(CreateUserRequestSchema, {
        user: create(UserSchema, {
          email,
          title,
          password,
          userType: UserType.USER,
        }),
      })
    );
    await get().login(email, password);
  },

  async fetchCurrentUser() {
    try {
      const user = await userServiceClient.getCurrentUser({});
      set({ currentUser: user, isLoggedIn: true });
    } catch {
      set({ currentUser: null, isLoggedIn: false });
    }
  },

  async loadSession() {
    try {
      await get().fetchCurrentUser();
    } catch {
      // Not logged in — that's fine
    } finally {
      set({ sessionLoaded: true });
    }
  },
});
