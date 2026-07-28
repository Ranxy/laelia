import type { StateCreator } from "zustand";
import type {
  Agent,
  AgentProviderInfo,
  AgentStatus_ConnectionState,
  AgentSummary,
  CreateAgentResponse,
  RotateAgentTokenResponse,
} from "@/types/proto-es/v1/agent_pb";
import type {
  Activity,
  ActivityCategory,
  ActivityState,
  AgentActivity,
  Attachment,
  ChannelMember,
  ChatMessage,
  Command,
  CommandEvent,
  CommandOutput,
  Conversation,
  Mention,
  Reminder,
} from "@/types/proto-es/v1/command_pb";
import type {
  Machine,
  MachineSummary,
  RotateMachineTokenResponse,
} from "@/types/proto-es/v1/machine_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

export interface ChatMessageUI {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: Date;
  commandName?: string;
  commandId?: string;
  agentId?: string;
  status?: number;
  streaming?: boolean;
  events?: CommandEvent[];
  senderName?: string;
  senderType?: number;
  // principalId is the decimal principal id of the message's author (the
  // {user} segment of a user's "users/{user}" resource name). Used to tell
  // the current user's own messages apart from other users' messages in shared
  // channels; absent on the optimistic placeholder created at send time.
  principalId?: string;
  mentions?: Mention[];
  attachments?: Attachment[];
  // threadRoot is the bare UUID of the thread's root message; set on thread
  // replies, absent on main-channel messages and on roots themselves.
  threadRoot?: string;
  // threadReplyCount is the number of replies under this message; set on root
  // messages, 0/absent otherwise. Drives the "N replies · View thread" entry.
  threadReplyCount?: number;
  // task is non-null when this message is a channel task (a row exists in the
  // task table for it). Populated for root messages; absent for replies and
  // non-task messages. Drives the inline "[task #N status=...]" badge.
  task?: TaskInfoUI;
  // roomVersion is the message's room_version (its monotonic position in the
  // conversation). Used by the Activity detail embed to scroll to the user's
  // last-read position: the first message whose room_version exceeds the
  // requesting user's read cursor. Absent on the optimistic send placeholder.
  roomVersion?: bigint;
}

// TaskInfoUI is the UI mirror of laelia.v1.TaskInfo attached to a task root
// message. status is the numeric TaskStatus enum value (see lib/task-status).
export interface TaskInfoUI {
  taskNumber: number;
  status: number;
  assigneeName?: string;
  assigneeResourceId?: string;
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

// UserSlice owns the workspace user roster (active + recycled) and the
// user-management mutations. It wraps userServiceClient; permission gating for
// mutating RPCs is enforced server-side (laelia.users.update/delete) and the UI
// hides the controls for callers lacking `laelia.users.update` (see
// useHasPermission in stores/auth).
export interface UserSlice {
  users: User[];
  usersLoading: boolean;
  deletedUsers: User[];
  deletedUsersLoading: boolean;

  fetchUsers: (
    params?: {
      pageSize?: number;
      pageToken?: string;
      showDeleted?: boolean;
      filter?: string;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  createUser: (input: {
    email: string;
    title: string;
    password: string;
    phone?: string;
    description?: string;
  }) => Promise<User>;
  updateUser: (
    name: string,
    fields: {
      title?: string;
      email?: string;
      phone?: string;
      description?: string;
    },
    maskPaths: string[]
  ) => Promise<User>;
  resetPassword: (name: string, newPassword: string) => Promise<User>;
  deleteUser: (name: string) => Promise<void>;
  undeleteUser: (name: string) => Promise<User>;
}

// AgentACPConfigInput is the user-configurable ACP config shared by
// createAgent (optional, sets the config at creation time) and
// updateAgentACPConfig (replaces the config). Mirrors AgentACPConfig.
export interface AgentACPConfigInput {
  executable: string;
  args: string[];
  allowEnv: string[];
  provider: string;
  model: string;
  customEnv: Record<string, string>;
  personaPrompt: string;
  // builtin-pi runtime fields (only meaningful when provider === "builtin-pi"):
  // apiProvider is the LLM API provider ("deepseek" | "openrouter"); apiKey is
  // the plaintext LLM API key. apiKey may be left empty on update to keep the
  // existing stored key (the server treats empty as "preserve").
  apiProvider: string;
  apiKey: string;
}

export interface AgentSlice {
  agents: AgentSummary[];
  agentsLoading: boolean;

  fetchAgents: (
    params?: {
      pageSize?: number;
      pageToken?: string;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  getAgent: (name: string) => Promise<Agent | undefined>;
  // createAgent binds a new agent to a machine. The machine app picks the agent
  // up automatically over its MachineChannel — no bootstrap token is returned
  // (CreateAgentResponse.bootstrapToken is empty under the machine-hosts-many
  // model). acpConfig optionally sets the agent's provider/model/persona/env at
  // creation time so the agent is fully configured without a second visit to the
  // agent profile; when omitted the agent is created with the server default.
  createAgent: (
    title: string,
    machine: string,
    acpConfig?: AgentACPConfigInput,
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
    acpConfig: AgentACPConfigInput
  ) => Promise<void>;
  refreshAgentProviders: (name: string) => Promise<AgentProviderInfo[]>;
}

// MachineSlice owns the machine roster and the machine-management mutations.
// A machine authenticates once (registration token) and hosts every agent bound
// to it; token rotate/revoke and provider discovery are machine-scoped.
export interface MachineSlice {
  machines: MachineSummary[];
  machinesLoading: boolean;

  fetchMachines: (
    params?: {
      pageSize?: number;
      pageToken?: string;
      showDeleted?: boolean;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  getMachine: (name: string) => Promise<Machine | undefined>;
  createMachine: (
    title: string,
    labels?: Record<string, string>
  ) => Promise<{ machine?: Machine; registrationToken: string }>;
  deleteMachine: (name: string) => Promise<void>;
  rotateMachineToken: (
    name: string,
    reason?: string
  ) => Promise<RotateMachineTokenResponse>;
  revokeMachineToken: (name: string, reason?: string) => Promise<void>;
  forceDisconnectMachine: (name: string, reason?: string) => Promise<void>;
  refreshMachineProviders: (name: string) => Promise<AgentProviderInfo[]>;
  listMachineAgents: (name: string) => Promise<AgentSummary[]>;
}

// MemberSummary is one row in the flat Members directory: a human user or an
// agent. Agents carry their connection state for a status dot; the subtitle is
// the agent's owning machine resource name (or a user's email).
export interface MemberSummary {
  kind: "user" | "agent";
  name: string;
  title: string;
  subtitle: string;
  connectionState?: AgentStatus_ConnectionState;
}

// MembersSlice owns the flat workspace directory that merges the user roster
// and the agent roster into a single contacts list (not grouped by machine).
export interface MembersSlice {
  members: MemberSummary[];
  membersLoading: boolean;
  // membersError is set when either source roster fetch failed; the Members page
  // shows an error + retry instead of an empty list.
  membersError: boolean;

  fetchMembers: () => Promise<
    | {
        usersNextPageToken: string;
        agentsNextPageToken: string;
      }
    | undefined
  >;
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
  // Last-seen conversation.current_version per conversation, keyed by
  // conversation name. Captured by loadMessages and advanced by the channel
  // watcher; used as the afterVersion cursor so the watcher polls only for
  // messages newer than what it has already seen instead of re-fetching the
  // whole list every tick.
  chatCurrentVersion: Record<string, bigint>;

  getOrCreateConversation: (agent: string) => Promise<string>;
  // getOrCreateUserUserDM opens (or reuses) the 1:1 DM between the calling
  // user and a peer user (resource name "users/{id}"). Returns the
  // conversation name "conversations/{id}".
  getOrCreateUserUserDM: (peerUser: string) => Promise<string>;
  loadMessages: (conversation: string) => Promise<void>;
  sendChatMessage: (
    agent: string,
    instruction: string,
    conversationId?: string
  ) => Promise<ChatMessage>;
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
  // Unread message counts per conversation, keyed by conversation name
  // (`conversations/{id}`). Populated by fetchChannels from the backend and
  // cleared locally by markConversationRead; drives the left-rail badges.
  unreadByConv: Record<string, number>;
  // Active per-conversation message-poll intervals, keyed by conversation
  // name. Held in store state (not a module-level registry) so it is testable
  // and survives HMR without leaking timers.
  channelWatchers: Record<string, ReturnType<typeof setInterval>>;
  // Channels an agent is a member of, keyed by agent resource name
  // (`agents/{id}`). Populated by fetchChannelsForAgent for the agent detail
  // page's Chat tab; unread is always 0 here (the backend does not compute a
  // per-agent unread for this view).
  agentChannelsByAgent: Record<string, Conversation[]>;
  agentChannelsLoading: boolean;

  fetchChannels: () => Promise<void>;
  fetchChannelsForAgent: (agentName: string) => Promise<void>;
  createChannel: (title: string) => Promise<Conversation>;
  markConversationRead: (conversationId: string) => Promise<void>;
  sendChannelMessage: (
    conversationId: string,
    content: string,
    mentions?: Mention[],
    attachments?: Attachment[],
    asTask?: boolean
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

// ThreadSlice owns the right-side thread panel state: per-thread cached
// messages + current_version, the active thread root (which thread panel is
// open), and the per-thread polling watchers. Thread roots/reply ids are bare
// UUIDs (the backend uses chat_message.id, not a resource name). The active
// thread panel is scoped to one conversation at a time (activeThreadConversation).
export interface ThreadSlice {
  threadByRoot: Record<
    string,
    { messages: ChatMessageUI[]; currentVersion: bigint; loading: boolean }
  >;
  activeThreadRoot: string | null;
  activeThreadConversation: string | null;
  threadWatchers: Record<string, ReturnType<typeof setInterval>>;

  openThread: (conversation: string, rootMessageId: string) => Promise<void>;
  closeThread: () => void;
  // Loads a thread snapshot into threadByRoot without opening the thread
  // panel or starting a watcher (used by the preview comment aside).
  loadThreadMessages: (
    conversation: string,
    rootMessageId: string
  ) => Promise<void>;
  sendThreadMessage: (
    conversationId: string,
    rootMessageId: string,
    content: string,
    mentions?: Mention[],
    attachments?: Attachment[]
  ) => Promise<ChatMessage>;
}

// PreviewSlice owns the markdown file preview overlay: the active preview
// (file + decoded text + status) and the actions to open/close it. The overlay
// is mounted once at the dashboard layout and reads `activePreview`.
export interface PreviewSlice {
  activePreview: {
    conversation: string; // "conversations/{id}"
    conversationId: string; // bare id
    // Thread root the previewed attachment belongs to (the attachment owner's
    // threadRoot, or the owner's own id when it is a root). Used in Phase 2 to
    // route section-anchored comments as replies on this thread.
    rootMessageId: string;
    attachment: Attachment;
    content: string;
    status: "loading" | "ready" | "error" | "too-large";
    // When set, the overlay scrolls to this heading id once the markdown DOM
    // is ready (cross-scenario anchor jump: a comment's anchor chip clicked
    // outside the overlay opens the preview already scrolled to its section).
    // Consumed and cleared by the overlay after the first ready render.
    scrollToSectionId?: string;
  } | null;

  openFilePreview: (
    conversation: string,
    rootMessageId: string,
    attachment: Attachment,
    scrollToSectionId?: string
  ) => Promise<void>;
  closeFilePreview: () => void;
}

// ImagePreviewSlice owns the image lightbox overlay: the active image (file +
// decoded blob URL + status) and the actions to open/close it. The overlay is
// mounted once at the dashboard layout and reads `activeImage`. The blob URL
// is created on open and revoked on close so we don't leak object URLs.
export interface ImagePreviewSlice {
  activeImage: {
    attachment: Attachment;
    blobUrl: string | null;
    status: "loading" | "ready" | "error";
  } | null;

  openImagePreview: (attachment: Attachment) => Promise<void>;
  closeImagePreview: () => void;
}

// TaskSlice owns the channel task board panel: per-conversation task listings
// (cached as ChatMessageUI so they reuse MessageRow's task badge), panel open
// state, and the convert-message-to-task mutation. Tasks live in the same
// chatMessages flow as regular messages (a task IS a message with metadata);
// this slice is only the panel's separate view onto the task subset.
export interface TaskCountsUI {
  todo: number;
  inProgress: number;
  inReview: number;
  done: number;
}

export interface TaskSlice {
  tasksByConv: Record<string, ChatMessageUI[]>;
  // nextPageToken per conversation: "" means no more (older) pages to load.
  tasksNextPageToken: Record<string, string>;
  // Per-status totals per conversation, from ListTaskCounts, so the board
  // summary stays accurate regardless of how many tasks the paginated list has
  // loaded into tasksByConv.
  taskCountsByConv: Record<string, TaskCountsUI>;
  tasksLoading: Record<string, boolean>;
  tasksPanelOpen: Record<string, boolean>;

  toggleTasksPanel: (conversationId: string) => void;
  closeTasksPanel: (conversationId: string) => void;
  loadTasks: (conversationId: string, statusFilter?: number[]) => Promise<void>;
  // loadMoreTasks appends the next (older) page to tasksByConv; a no-op when
  // there is no next page or a load is already in flight.
  loadMoreTasks: (conversationId: string) => Promise<void>;
  loadTaskCounts: (conversationId: string) => Promise<void>;
  convertMessageToTask: (
    conversationId: string,
    messageId: string
  ) => Promise<void>;
}

export interface ReminderSlice {
  reminders: Reminder[];
  remindersLoading: boolean;

  listReminders: (
    agent: string,
    params?: {
      pageSize?: number;
      pageToken?: string;
      statusFilter?: number[];
      silent?: boolean;
    }
  ) => Promise<{ reminders: Reminder[]; nextPageToken: string } | undefined>;
  getReminder: (name: string) => Promise<Reminder | undefined>;
  updateReminder: (
    name: string,
    fields: {
      fireAt?: Date;
      cronExpr?: string;
      tz?: string;
      taskContent?: string;
    }
  ) => Promise<Reminder | undefined>;
  cancelReminder: (name: string) => Promise<Reminder | undefined>;
}

export interface ActivitySlice {
  activities: Activity[];
  activitiesLoading: boolean;

  listActivities: (params?: {
    filter?: ActivityCategory[];
    readStateFilter?: ActivityState;
    pageSize?: number;
    pageToken?: string;
    silent?: boolean;
  }) => Promise<{ activities: Activity[]; nextPageToken: string } | undefined>;
  markActivityDone: (name: string) => Promise<Activity | undefined>;
}

export type AppStoreState = AuthSlice &
  AgentSlice &
  MachineSlice &
  MembersSlice &
  CommandSlice &
  ChatSlice &
  ChannelSlice &
  ThreadSlice &
  TaskSlice &
  ReminderSlice &
  ActivitySlice &
  UserSlice &
  PreviewSlice &
  ImagePreviewSlice;

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
