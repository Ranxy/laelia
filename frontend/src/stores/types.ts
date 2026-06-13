import type { StateCreator } from "zustand";

export interface ApiUser {
  name: string;
  state: string;
  email: string;
  title: string;
  userType: string;
  password?: string;
  serviceKey?: string;
  phone?: string;
  profile?: {
    lastLoginTime?: string;
    lastChangePasswordTime?: string;
    source?: string;
  };
  groups?: string[];
}

export interface AuthSlice {
  currentUser: ApiUser | null;
  token: string | null;
  isLoggedIn: boolean;
  sessionLoaded: boolean;

  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  register: (email: string, title: string, password: string) => Promise<void>;
  fetchCurrentUser: () => Promise<void>;
  loadSession: () => Promise<void>;
}

export type AppStoreState = AuthSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
