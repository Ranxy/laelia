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

export interface ApiAgent {
  name: string;
  state: string;
  title: string;
  token?: string;
  info?: {
    agentType?: string;
    hostname?: string;
    os?: string;
    arch?: string;
    ip?: string;
    version?: string;
    labels?: Record<string, string>;
  };
  status?: {
    state: string;
    lastHeartbeatTime?: string;
    connectedTime?: string;
    errorMessage?: string;
  };
  createdAt?: string;
  labels?: Record<string, string>;
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

export interface AgentSlice {
  agents: ApiAgent[];
  agentsLoading: boolean;

  fetchAgents: (params?: {
    pageSize?: number;
    pageToken?: string;
  }) => Promise<{ nextPageToken: string } | undefined>;
  createAgent: (
    name: string,
    labels?: Record<string, string>
  ) => Promise<ApiAgent>;
  deleteAgent: (name: string) => Promise<void>;
}

export type AppStoreState = AuthSlice & AgentSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
