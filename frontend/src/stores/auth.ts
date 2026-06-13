import { api } from "@/react/api/client";
import type { ApiUser, AppSliceCreator, AuthSlice } from "./types";

export const createAuthSlice: AppSliceCreator<AuthSlice> = (set, get) => ({
  currentUser: null,
  token: null,
  isLoggedIn: false,
  sessionLoaded: false,

  async login(email: string, password: string) {
    const res = await api.post<{
      token: string;
      user?: ApiUser;
      requireResetPassword?: boolean;
    }>("/auth/login", { email, password, web: true });

    set({
      token: res.token,
      currentUser: res.user ?? null,
      isLoggedIn: true,
    });
  },

  async logout() {
    try {
      await api.post("/auth/logout");
    } finally {
      set({ token: null, currentUser: null, isLoggedIn: false });
    }
  },

  async register(email: string, title: string, password: string) {
    // proto: body:"user" → request body is the User object directly
    await api.post("/users", {
      email,
      title,
      password,
      userType: "USER",
    });
    // Auto-login after registration
    await get().login(email, password);
  },

  async fetchCurrentUser() {
    try {
      const user = await api.get<ApiUser>("/users/me");
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
