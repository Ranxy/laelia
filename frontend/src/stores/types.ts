import type { StateCreator } from "zustand";
import type {
  Agent,
  CreateAgentResponse,
  RotateAgentTokenResponse,
} from "@/types/proto-es/v1/agent_pb";
import type {
  ChannelMember,
  ChatMessage,
  Command,
  CommandEvent,
  CommandOutput,
  Conversation,
} from "@/types/proto-es/v1/command_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

export interface ChatMessageUI {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: Date;
  commandName?: string;
  commandId?: string;
  status?: number;
  streaming?: boolean;
  events?: CommandEvent[];
  senderName?: string;
  senderType?: number;
}

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
  agentCache: Record<string, Agent>;

  fetchAgents: (params?: {
    pageSize?: number;
    pageToken?: string;
  }) => Promise<{ nextPageToken: string } | undefined>;
  getAgent: (
    name: string,
    opts?: { force?: boolean }
  ) => Promise<Agent | undefined>;
  createAgent: (
    title: string,
    labels?: Record<string, string>
  ) => Promise<CreateAgentResponse>;
  deleteAgent: (name: string) => Promise<void>;
  rotateAgentToken: (
    name: string,
    reason?: string
  ) => Promise<RotateAgentTokenResponse>;
  revokeAgentToken: (name: string, reason?: string) => Promise<void>;
  updateAgentACPConfig: (name: string, acpConfigYaml: string) => Promise<void>;
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
      allowDiff?: boolean;
      source?: number;
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

export interface ChatSlice {
  conversations: Record<string, string>;
  channels: Conversation[];
  channelsLoading: boolean;
  chatMessages: Record<string, ChatMessageUI[]>;
  chatLoading: Record<string, boolean>;
  streamingContent: Record<string, string>;
  streamingEvents: Record<string, CommandEvent[]>;
  streamingStatus: Record<string, number>;

  getOrCreateConversation: (agent: string) => Promise<string>;
  loadMessages: (conversation: string) => Promise<void>;
  sendChatMessage: (
    agent: string,
    instruction: string,
    conversationId?: string
  ) => Promise<ChatMessage>;
  streamChatCommand: (
    commandName: string,
    conversation: string,
    signal: AbortSignal
  ) => Promise<void>;
  resetStreaming: (commandName: string) => void;
  fetchChannels: () => Promise<void>;
  createChannel: (title: string) => Promise<Conversation>;
  sendChannelMessage: (
    conversationId: string,
    content: string
  ) => Promise<ChatMessage>;
  pollChannelMessages: (conversationName: string) => Promise<void>;
  listChannelMembers: (conversationId: string) => Promise<ChannelMember[]>;
  addChannelMember: (
    conversationId: string,
    memberType: number,
    memberId: string
  ) => Promise<ChannelMember>;
  removeChannelMember: (
    conversationId: string,
    memberType: number,
    memberId: string
  ) => Promise<void>;
  channelMembersByConv: Record<string, ChannelMember[]>;
  channelMembersLoading: Record<string, boolean>;
}

export type AppStoreState = AuthSlice & AgentSlice & CommandSlice & ChatSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
