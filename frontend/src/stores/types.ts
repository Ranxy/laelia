import type { StateCreator } from "zustand";
import type {
  LlmAgentConfigSetting,
  PasswordRestrictionSetting,
  S3ConfigSetting,
  SMTPSetting,
  UserMcpConfigSetting,
  WorkspaceProfileSetting,
} from "@/types/proto-es/store/setting_pb";
import type {
  Agent,
  AgentProviderInfo,
  AgentStatus_ConnectionState,
  AgentSummary,
  CreateAgentResponse,
  PiModel,
  RotateAgentTokenResponse,
  TransferAgentOwnershipResponse,
  WorkspaceEntry,
  WorkspaceReadResponse,
} from "@/types/proto-es/v1/agent_pb";
import type { ApiProvider } from "@/types/proto-es/v1/api_provider_service_pb";
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
  Reaction,
  Reminder,
} from "@/types/proto-es/v1/command_pb";
import type {
  Machine,
  MachineSummary,
  MachineWorkspaceSummary,
} from "@/types/proto-es/v1/machine_pb";
import type { McpServer } from "@/types/proto-es/v1/mcp_pb";
import type {
  ChatPreferences,
  User,
} from "@/types/proto-es/v1/user_service_pb";

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
  // principalId is the mention handle of the message's author (the
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
  // reactions are this message's emoji reactions, aggregated per emoji with a
  // caller-relative `reacted` flag (whether the current user reacted). Drives
  // the reaction bar under the message; empty/absent when there are none.
  reactions?: Reaction[];
}

// TaskInfoUI is the UI mirror of laelia.v1.TaskInfo attached to a task root
// message. status is the numeric TaskStatus enum value (see lib/task-status).
export interface TaskInfoUI {
  taskNumber: number;
  status: number;
  assigneeName?: string;
  assigneeResourceId?: string;
  // assigneeType distinguishes the assignee kind: 1=user, 2=agent. 0/absent
  // when unassigned.
  assigneeType?: number;
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
  verifyEmail: (token: string) => Promise<void>;
  resendVerificationEmail: (email: string) => Promise<void>;
  fetchCurrentUser: () => Promise<void>;
  loadSession: () => Promise<void>;
}

// UserSlice owns the workspace user roster (active + recycled) and the
// user-management mutations. It wraps userServiceClient; permission gating for
// mutating RPCs is enforced server-side (laelia.users.update/delete) and the UI
// hides the controls for callers lacking `laelia.users.update` (see
// useHasPermission in stores/permissions).
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
      /** Include the internal SYSTEM_BOT account; only the settings user
       *  directory opts in. Defaults to false everywhere else. */
      includeSystemBot?: boolean;
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
      chatPreferences?: ChatPreferences;
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
  // protocol declares the ACP protocol generation for a custom provider:
  // "" (inferred), "acp-v1" (session) or "acp-v2" (thread). Ignored for
  // built-in providers.
  protocol: string;
  customEnv: Record<string, string>;
  personaPrompt: string;
  // builtin-pi runtime fields (only meaningful when provider === "builtin-pi"):
  // apiProvider is the LLM API provider ("deepseek" | "openrouter"); apiKey is
  // the plaintext LLM API key. apiKey may be left empty on update to keep the
  // existing stored key (the server treats empty as "preserve").
  //
  // Global-provider mode (preferred): globalProvider references a managed API
  // provider ("apiProviders/{id}") and globalProviderEntry one of its (key,
  // model) entries. When both are set the server resolves the key at the daemon
  // boundary and the inline apiProvider/apiKey are ignored.
  apiProvider?: string;
  apiKey?: string;
  apiBaseUrl?: string;
  globalProvider?: string;
  globalProviderEntry?: string;
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
  // allowAddToChannel controls whether other users may add this agent to a
  // channel; when false (default) only the agent's owner or a workspace admin
  // may add it.
  createAgent: (
    title: string,
    machine: string,
    acpConfig?: AgentACPConfigInput,
    labels?: Record<string, string>,
    allowAddToChannel?: boolean
  ) => Promise<CreateAgentResponse>;
  // updateAgent patches the agent's mutable flag fields (allow_add_to_channel,
  // follow_owner_permissions, can_manage_channel_members); only the keys present
  // in `fields` are sent. Authorized server-side for the agent's owner or a
  // workspace admin.
  updateAgent: (
    name: string,
    fields: {
      allowAddToChannel?: boolean;
      followOwnerPermissions?: boolean;
      canManageChannelMembers?: boolean;
    }
  ) => Promise<Agent>;
  deleteAgent: (name: string) => Promise<void>;
  // stopAgent stops an agent: its machine runner is torn down and it processes
  // no session messages until startAgent. The agent row is preserved.
  stopAgent: (name: string) => Promise<void>;
  // startAgent resumes a stopped agent so it processes messages again.
  startAgent: (name: string) => Promise<void>;
  rotateAgentToken: (
    name: string,
    reason?: string
  ) => Promise<RotateAgentTokenResponse>;
  revokeAgentToken: (name: string, reason?: string) => Promise<void>;
  updateAgentACPConfig: (
    name: string,
    acpConfig: AgentACPConfigInput
  ) => Promise<void>;
  // updateAgentMcpConfig replaces the MCP servers enabled on an agent. Only
  // servers the caller may use are accepted server-side.
  updateAgentMcpConfig: (name: string, mcpServers: string[]) => Promise<void>;
  // transferAgentOwnership reassigns the agent's owner to another user.
  // Unilateral and immediately effective (the target user does not accept);
  // authorized server-side for the current owner or a workspace admin.
  transferAgentOwnership: (
    name: string,
    newOwner: string,
    reason?: string
  ) => Promise<TransferAgentOwnershipResponse>;
  refreshAgentProviders: (name: string) => Promise<AgentProviderInfo[]>;
  // listPiModels proxies an LLM API provider's model-listing API through the
  // manager (CORS + key hygiene). Fetched dynamically so the model list is never
  // hardcoded. apiKey is required for deepseek and custom; ignored for
  // openrouter. apiBaseUrl is required for custom.
  listPiModels: (
    apiProvider: string,
    apiKey: string,
    apiBaseUrl?: string
  ) => Promise<PiModel[]>;
}

// MachineSlice owns the machine roster and the machine-management mutations.
// A machine authenticates via the device-code flow (no bootstrap token) and
// hosts every agent bound to it; rename/transfer, revoke and provider
// discovery are machine-scoped.
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
  updateMachine: (name: string, title: string) => Promise<Machine>;
  transferMachineOwnership: (
    name: string,
    newOwner: string,
    reason?: string
  ) => Promise<void>;
  deleteMachine: (name: string) => Promise<void>;
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
  enabled?: boolean;
}

// MembersSlice owns the flat workspace directory that merges the user roster
// and the agent roster into a single contacts list (not grouped by machine).
export interface MembersSlice {
  members: MemberSummary[];
  membersLoading: boolean;
  // membersError is set when either source roster fetch failed; the Members page
  // shows an error + retry instead of an empty list.
  membersError: boolean;

  fetchMembers: (params?: { silent?: boolean }) => Promise<
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
  // steerCommand injects a follow-up message into the in-flight turn of a
  // running command. Best-effort: executors without mid-turn steering ignore
  // it. Throws when the command is not running or the agent is unreachable.
  steerCommand: (name: string, text: string) => Promise<Command>;
  listCommands: (
    agent: string,
    params?: { pageSize?: number; pageToken?: string; status?: number }
  ) => Promise<{ commands: Command[]; nextPageToken: string } | undefined>;
  getCommand: (name: string) => Promise<Command | undefined>;
  // watchCommand/watchCommandEvents resolve with true when the server closed
  // the stream normally (e.g. the command finished), false when the stream was
  // aborted by the caller or failed with an error.
  watchCommand: (name: string, signal?: AbortSignal) => Promise<boolean>;
  watchCommandEvents: (name: string, signal?: AbortSignal) => Promise<boolean>;
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
  // toggleReaction adds (or, if the caller already reacted, removes) the
  // caller's emoji reaction on a message in a conversation, then updates the
  // local message's reactions from the server's response. Lightweight: it
  // never bumps the room version or wakes agents.
  toggleReaction: (
    conversation: string,
    messageId: string,
    emoji: string
  ) => Promise<void>;
}

// ChannelSlice owns channel conversations: the channel roster, per-conversation
// member rosters, agent activity polling, and the persistent per-conversation
// message watchers. It shares chatMessages/chatLoading with ChatSlice (both DM
// and channel messages live in those maps, keyed by conversation name).
export interface ChannelSlice {
  channels: Conversation[];
  channelsLoading: boolean;
  // Every real channel (type 2) the user joined or created, including closed
  // ones, for the members page roster. Kept separate from `channels` (the
  // left-rail list, which hides closed conversations and includes DMs) so the
  // two views never overwrite each other; fetched on demand instead of
  // polled.
  myChannels: Conversation[];
  myChannelsLoading: boolean;
  channelMembersByConv: Record<string, ChannelMember[]>;
  channelMembersLoading: Record<string, boolean>;
  agentActivities: Record<string, AgentActivity[]>;
  // Unread message counts per conversation, keyed by conversation name
  // (`conversations/{id}`). Populated by fetchChannels from the backend and
  // cleared locally by markConversationRead; drives the left-rail badges.
  unreadByConv: Record<string, number>;
  // Active per-conversation message watchers, keyed by conversation name.
  // Each handle owns the AbortController that cancels the in-flight long poll
  // and the 5s badge/activity interval. Held in store state (not a module-level
  // registry) so it is testable and survives HMR without leaking timers.
  channelWatchers: Record<
    string,
    { ctrl: AbortController; badgeTimer: ReturnType<typeof setInterval> }
  >;
  // Channels an agent is a member of, keyed by agent resource name
  // (`agents/{id}`). Populated by fetchChannelsForAgent for the agent detail
  // page's Chat tab; unread is always 0 here (the backend does not compute a
  // per-agent unread for this view).
  agentChannelsByAgent: Record<string, Conversation[]>;
  agentChannelsLoading: boolean;

  fetchChannels: () => Promise<void>;
  // Fetches the user's channel roster (type 2 only, closed included) for the
  // members page. silent suppresses the loading flag so a cached refresh does
  // not flash the spinner.
  fetchMyChannels: (opts?: { silent?: boolean }) => Promise<void>;
  fetchChannelsForAgent: (agentName: string) => Promise<void>;
  createChannel: (title: string) => Promise<Conversation>;
  markConversationRead: (conversationId: string) => Promise<void>;
  // Pin or unpin a conversation for the current user. Pinned channels/DMs sort
  // to the top of the left-rail list and stay there regardless of last message
  // time. Optimistically reorders the local channel list; refetches on error.
  setConversationPinned: (
    conversationId: string,
    pinned: boolean
  ) => Promise<void>;
  // Close or reopen a conversation for the current user. Closing hides the
  // channel/DM from the left-rail list (the conversation and its messages are
  // untouched); the backend clears the flag on the next main-channel message,
  // so a closed chat reappears automatically on new activity. Optimistically
  // removes the row locally; reopening refetches so the row lands at its
  // server position.
  setConversationClosed: (
    conversationId: string,
    closed: boolean
  ) => Promise<void>;
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
    memberIds: string[]
  ) => Promise<ChannelMember[]>;
  addChannelGroup: (
    conversationId: string,
    groupName: string
  ) => Promise<ChannelMember[]>;
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
  // Active per-thread long-poll watchers, keyed by thread root id. Each
  // handle owns the AbortController that cancels the in-flight request.
  threadWatchers: Record<string, { ctrl: AbortController }>;

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

// PreviewSlice owns the markdown/html file preview overlay: the active
// preview (file + decoded text + status) and the actions to open/close it.
// The overlay is mounted once at the dashboard layout and reads
// `activePreview`; `kind` selects the renderer (markdown DOM vs sandboxed
// iframe).
export interface PreviewSlice {
  activePreview: {
    kind: "markdown" | "html";
    conversation: string; // "conversations/{id}"
    conversationId: string; // bare id
    // Thread root the previewed attachment belongs to (the attachment owner's
    // threadRoot, or the owner's own id when it is a root). Used to route
    // section-anchored comments as replies on this thread.
    rootMessageId: string;
    attachment: Attachment;
    content: string;
    status: "loading" | "ready" | "error" | "too-large";
    // When set, the overlay scrolls to this anchor once the preview DOM is
    // ready (cross-scenario anchor jump: a comment's anchor chip clicked
    // outside the overlay opens the preview already positioned at its
    // section). For markdown it is a heading DOM id; for html it is a locate
    // spec ("html:y:{y}") paired with `scrollToQuote`. Consumed and cleared
    // by the overlay after the first ready render.
    scrollToAnchorId?: string;
    // For html previews, the quoted text to locate when jumping to an anchor.
    scrollToQuote?: string;
  } | null;

  openFilePreview: (
    conversation: string,
    rootMessageId: string,
    attachment: Attachment,
    scrollToAnchorId?: string,
    scrollToQuote?: string
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
  // updateTaskStatus moves a task to any of the four statuses. The caller's
  // thread root is patched with the authoritative response and the board +
  // counts reload; throws on failure so the UI can surface the error.
  updateTaskStatus: (
    conversationId: string,
    rootMessageId: string,
    status: number
  ) => Promise<void>;
  // assignTask assigns a task to a channel member (user or agent). The caller's
  // thread root is patched with the authoritative response and the board +
  // counts reload; throws on failure so the UI can surface the error.
  assignTask: (
    conversationId: string,
    rootMessageId: string,
    memberType: number,
    memberId: string
  ) => Promise<void>;
  // closeTask marks a task DONE (terminal) from the UI. Kept for the agent
  // tool path; the UI now uses updateTaskStatus(DONE) instead.
  closeTask: (conversationId: string, rootMessageId: string) => Promise<void>;
  // patchTaskThreadAndRefresh patches the open thread's root with the
  // authoritative task message returned by a task mutation, then reloads the
  // board + counts. Shared by updateTaskStatus / assignTask / closeTask.
  patchTaskThreadAndRefresh: (
    conversationId: string,
    rootMessageId: string,
    res: { message?: import("@/types/proto-es/v1/command_pb").ChatMessage }
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
  // nextPageToken for the current filter. "" means the server has no more pages.
  activitiesNextPageToken: string;

  listActivities: (params?: {
    filter?: ActivityCategory[];
    readStateFilter?: ActivityState;
    pageSize?: number;
    pageToken?: string;
    silent?: boolean;
  }) => Promise<{ activities: Activity[]; nextPageToken: string } | undefined>;
  // loadMoreActivities appends the next page to the current filtered list. It is
  // a no-op when there is no next page or a load is already in flight.
  loadMoreActivities: (params: {
    filter?: ActivityCategory[];
    readStateFilter?: ActivityState;
    pageSize?: number;
  }) => Promise<void>;
  markActivityDone: (name: string) => Promise<Activity | undefined>;
}

// ApiProviderSlice owns the global LLM API provider roster. The backend
// handler-gates ListApiProviders: admins/managers see every provider, other
// callers see only the providers they may use, so the same list feeds both the
// settings page and the agent create/edit form dropdowns.
export interface ApiProviderSlice {
  apiProviders: ApiProvider[];
  apiProvidersLoading: boolean;

  fetchApiProviders: (
    params?: { pageSize?: number; pageToken?: string },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
}

// McpServerSlice owns the workspace MCP server roster. The backend
// handler-gates ListMcpServers: admins/managers see every server, other
// callers see only the servers they may use.
export interface McpServerSlice {
  mcpServers: McpServer[];
  mcpServersLoading: boolean;

  fetchMcpServers: (
    params?: { pageSize?: number; pageToken?: string },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
}

// SettingSlice owns the workspace-level settings (workspace profile, SMTP, S3,
// LLM agent, user MCP, password restriction). Configs are cached as full proto
// messages after fetch; update methods merge only the mask-listed paths into
// the stored value server-side (bytebase-style field-level update) and refresh
// the cache from the authoritative response.
export interface SettingSlice {
  workspaceProfile?: WorkspaceProfileSetting;
  smtpConfig?: SMTPSetting;
  s3Config?: S3ConfigSetting;
  llmAgentConfig?: LlmAgentConfigSetting;
  userMcpConfig?: UserMcpConfigSetting;
  passwordRestriction?: PasswordRestrictionSetting;

  fetchWorkspaceProfile: () => Promise<WorkspaceProfileSetting | undefined>;
  fetchSmtpConfig: () => Promise<SMTPSetting | undefined>;
  fetchS3Config: () => Promise<S3ConfigSetting | undefined>;
  fetchLlmAgentConfig: () => Promise<LlmAgentConfigSetting | undefined>;
  fetchUserMcpConfig: () => Promise<UserMcpConfigSetting | undefined>;
  fetchPasswordRestriction: () => Promise<
    PasswordRestrictionSetting | undefined
  >;

  updateWorkspaceProfile: (
    patch: Partial<WorkspaceProfileSetting>,
    paths: string[]
  ) => Promise<WorkspaceProfileSetting | undefined>;
  updateSmtpConfig: (
    patch: Partial<SMTPSetting>,
    paths: string[]
  ) => Promise<SMTPSetting | undefined>;
  updateS3Config: (
    patch: Partial<S3ConfigSetting>,
    paths: string[]
  ) => Promise<S3ConfigSetting | undefined>;
  updateLlmAgentConfig: (
    patch: Partial<LlmAgentConfigSetting>,
    paths: string[]
  ) => Promise<LlmAgentConfigSetting | undefined>;
  updateUserMcpConfig: (
    patch: Partial<UserMcpConfigSetting>,
    paths: string[]
  ) => Promise<UserMcpConfigSetting | undefined>;
  updatePasswordRestriction: (
    patch: Partial<PasswordRestrictionSetting>,
    paths: string[]
  ) => Promise<PasswordRestrictionSetting | undefined>;
}

// WorkspaceSlice exposes the workspace browser RPCs (agent file tree + file
// preview, machine workspace list + delete). Authorization is handler-gated
// server-side; the UI additionally hides the workspace tabs without
// canEdit/canManage.
export interface WorkspaceSlice {
  listAgentWorkspaceDir: (
    name: string,
    dirPath: string,
    includeHidden: boolean
  ) => Promise<WorkspaceEntry[]>;
  readAgentWorkspaceFile: (
    name: string,
    path: string
  ) => Promise<WorkspaceReadResponse>;
  listMachineWorkspaces: (name: string) => Promise<MachineWorkspaceSummary[]>;
}

export type AppStoreState = AuthSlice &
  ApiProviderSlice &
  McpServerSlice &
  AgentSlice &
  MachineSlice &
  WorkspaceSlice &
  MembersSlice &
  CommandSlice &
  ChatSlice &
  ChannelSlice &
  ThreadSlice &
  TaskSlice &
  ReminderSlice &
  ActivitySlice &
  UserSlice &
  SettingSlice &
  PreviewSlice &
  ImagePreviewSlice & {
    // reset restores every slice to its pristine initial state (clearing
    // watcher intervals first) so a logout can never leak one principal's
    // cached data to the next user signing in on the same tab.
    reset: () => void;
  };

export type AppSliceCreator<Slice> = StateCreator<AppStoreState, [], [], Slice>;
