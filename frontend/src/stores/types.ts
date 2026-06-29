import type { StateCreator } from "zustand";
import type {
  Agent,
  CreateAgentResponse,
  RotateAgentTokenResponse,
} from "@/types/proto-es/v1/agent_pb";
import type {
  AgentActivity,
  Attachment,
  ChannelMember,
  ChatMessage,
  Command,
  CommandEvent,
  CommandOutput,
  Conversation,
  Mention,
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
  mentions?: Mention[];
  attachments?: Attachment[];
}

export interface AuthSlice {
  currentUser: User | null;
  // `isLoggedIn` is an explicit flag consumed by the router guard and the
  // unauthenticated-redirect path (see `router/guard.ts`, `router/auth-redirect.ts`).
  // It is kept as stored state rather than derived from `currentUser !== null`
  // because the routing tests and redirect hook assert against it directly.
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

  fetchAgents: (
    params?: {
      pageSize?: number;
      pageToken?: string;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
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
  updateAgentACPConfig: (
    name: string,
    acpConfig: { executable: string; args: string[]; allowEnv: string[] }
  ) => Promise<void>;
}

export interface CommandSlice {
  commands: Command[];
  commandsLoading: boolean;
  activeOutputs: Record<string, CommandOutput[]>;
  activeEvents: Record<string, CommandEvent[]>;

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
}

// ChannelSlice owns channel conversations: the channel roster, per-conversation
// member rosters, agent activity polling, and the persistent per-conversation
// message watchers. It shares chatMessages/chatLoading with ChatSlice (both DM
// and channel messages live in those maps, keyed by conversation name).
export interface ChannelSlice {
  channels: Conversation[];
  channelsLoading: boolean;
  channelMembersByConv: Record<string, ChannelMember[]>;
  channelMembersLoading: Record<string, boolean>;
  agentActivities: Record<string, AgentActivity[]>;
  // Active per-conversation message-poll intervals, keyed by conversation
  // name. Held in store state (not a module-level registry) so it is testable
  // and survives HMR without leaking timers.
  channelWatchers: Record<string, ReturnType<typeof setInterval>>;

  fetchChannels: () => Promise<void>;
  createChannel: (title: string) => Promise<Conversation>;
  sendChannelMessage: (
    conversationId: string,
    content: string,
    mentions?: Mention[],
    attachments?: Attachment[]
  ) => Promise<ChatMessage>;
  fetchConversationActivity: (conversationId: string) => Promise<void>;
  startWatchingChannel: (conversationName: string) => void;
  stopWatchingChannel: (conversationName: string) => void;
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
}

export type AppStoreState = AuthSlice &
  AgentSlice &
  CommandSlice &
  ChatSlice &
  ChannelSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
