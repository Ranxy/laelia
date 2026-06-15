import type { StateCreator } from "zustand";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import type {
  Command,
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

export interface AuthSlice {
  currentUser: User | null;
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
  agents: Agent[];
  agentsLoading: boolean;

  fetchAgents: (params?: {
    pageSize?: number;
    pageToken?: string;
  }) => Promise<{ nextPageToken: string } | undefined>;
  createAgent: (
    title: string,
    labels?: Record<string, string>
  ) => Promise<Agent>;
  deleteAgent: (name: string) => Promise<void>;
}

export interface CommandSlice {
  commands: Command[];
  commandsLoading: boolean;
  activeOutputs: Record<string, CommandOutput[]>;
  activeEvents: Record<string, CommandEvent[]>;

  sendCommand: (
    agent: string,
    command: string,
    opts?: {
      env?: Record<string, string>;
      workingDir?: string;
      timeoutSeconds?: number;
      executorKind?: number;
      instruction?: string;
      profile?: string;
      allowDiff?: boolean;
    }
  ) => Promise<Command>;
  cancelCommand: (name: string) => Promise<Command>;
  listCommands: (
    agent: string,
    params?: { pageSize?: number; pageToken?: string; status?: number }
  ) => Promise<{ commands: Command[]; nextPageToken: string } | undefined>;
  getCommand: (name: string) => Promise<Command | undefined>;
  watchCommand: (name: string, signal?: AbortSignal) => Promise<void>;
  watchCommandEvents: (name: string, signal?: AbortSignal) => Promise<void>;
  respondPermission: (name: string, optionId: string) => Promise<void>;
}

export type AppStoreState = AuthSlice & AgentSlice & CommandSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
