import { create } from "@bufbuild/protobuf";
import { authServiceClient, userServiceClient } from "@/connect";
import { invalidateAvatar } from "@/lib/avatar-cache";
import { invalidateImageBlobs } from "@/lib/image-blob-cache";
import {
  LoginRequestSchema,
  LogoutRequestSchema,
  ResendVerificationEmailRequestSchema,
  VerifyEmailRequestSchema,
} from "@/types/proto-es/v1/auth_service_pb";
import {
  CreateUserRequestSchema,
  UserSchema,
  UserType,
} from "@/types/proto-es/v1/user_service_pb";
import type { AppSliceCreator, AuthSlice } from "./types";

export const createAuthSlice: AppSliceCreator<AuthSlice> = (set, get) => ({
  currentUser: null,
  isLoggedIn: false,
  sessionLoaded: false,

  async login(email: string, password: string) {
    const res = await authServiceClient.login(
      create(LoginRequestSchema, { email, password, web: true })
    );

    // Seed the session from the login response so navigation can proceed.
    // The login response's User omits caller-scoped fields (permissions,
    // workspace_admin, debug_mode) which GetCurrentUser is the only endpoint
    // that populates. Re-fetch the current user to fill them in — otherwise
    // permission-gated UI such as the Settings sidebar group stays hidden
    // until a full page refresh triggers loadSession -> GetCurrentUser.
    set({
      currentUser: res.user ?? null,
      isLoggedIn: true,
    });
    try {
      const user = await userServiceClient.getCurrentUser({});
      set({ currentUser: user });
    } catch {
      // Keep the login-response user; the session cookie is already set, so
      // the caller stays logged in even if the enrichment fetch fails.
    }
  },

  async logout() {
    try {
      await authServiceClient.logout(create(LogoutRequestSchema));
    } finally {
      // Wipe every slice so a different user signing in on the same tab never
      // sees the previous principal's cached messages/channels/rosters.
      get().reset();
      // Keep sessionLoaded true so the router guard does not re-show the
      // initial loading spinner on the way to the sign-in page.
      set({ sessionLoaded: true });
      // Avatar blob URLs and the cached image blobs are module-level caches
      // that a store reset cannot reach; clear them so they don't survive
      // across users.
      invalidateAvatar();
      invalidateImageBlobs();
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
    // No auto-login: when the workspace requires email verification the
    // account is created unverified and can only sign in after the user
    // clicks the link in the verification email. The signup page decides
    // what to show next from GetWorkspaceInfo.require_email_verification.
  },

  async verifyEmail(token: string) {
    await authServiceClient.verifyEmail(
      create(VerifyEmailRequestSchema, { token })
    );
  },

  async resendVerificationEmail(email: string) {
    await authServiceClient.resendVerificationEmail(
      create(ResendVerificationEmailRequestSchema, { email })
    );
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
