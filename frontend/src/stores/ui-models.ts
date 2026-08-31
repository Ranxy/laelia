// Shared UI-facing models owned by the store domain but consumed by
// components: message rows, task mirrors, member directory rows and
// ACP config inputs. Slice interfaces live next to their slice
// implementations; the store composition (AppStoreState /
// AppSliceCreator) lives in ./types.
import type { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";
import type {
  Attachment,
  CommandEvent,
  Mention,
  Reaction,
} from "@/types/proto-es/v1/command_pb";

export interface ChatMessageUI {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: Date;
  commandName?: string;
  commandId?: string;
  agentId?: string;
  status?: number;
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
  // threadPreview holds the latest (≤3) replies of this message's thread,
  // oldest first, synced from the ListChannelThreads poll. Set on root
  // messages once the channel's thread summary has arrived; drives the inline
  // thread preview under the root message in the channel list.
  threadPreview?: ChatMessageUI[];
  // threadNewReplyCount is the number of replies in this root's thread beyond
  // the current user's read cursor (own replies excluded), synced from
  // ListChannelThreads. 0/absent when the user is caught up. Drives the
  // "N replies · M new" hint above the inline preview.
  threadNewReplyCount?: number;
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
  // sending marks a locally-created optimistic message that is still being
  // uploaded/sent. The UI shows a "sending" indicator and, while files are
  // still uploading, per-attachment progress from uploadProgress.
  sending?: boolean;
  // uploadProgress maps a pending attachment's id to its upload percentage
  // (0-100). Only present on optimistic messages with in-flight files.
  uploadProgress?: Record<string, number>;
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

// MemberSummary is one row in the flat Members directory: a human user or an
// agent. Agents carry their connection state for a status dot; the subtitle is
// the agent's owner display name (or a user's email).
export interface MemberSummary {
  kind: "user" | "agent";
  name: string;
  title: string;
  subtitle: string;
  connectionState?: AgentStatus_ConnectionState;
  enabled?: boolean;
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
  // Optional context configuration for a custom builtin-pi provider. Only
  // meaningful when provider === "builtin-pi" and apiProvider === "custom".
  // contextWindow is the context window size in tokens; maxTokens is the max
  // output tokens. Zero/undefined means let pi infer from the model. These are
  // bigint because the proto declares int64.
  contextWindow?: bigint;
  maxTokens?: bigint;
}
