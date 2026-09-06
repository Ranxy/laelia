# Chat Feature Design

> Status: verified and updated against the current code on 2026-09-06. Main changes: the original per-agent command-driven chat design was superseded by the unified conversation/channel model and the message-driven architecture (see `docs/plan/message-driven-architecture-redesign.md`); this doc now describes the current implementation and keeps the original design only as a historical note.

## 1. Overview

Chat is a unified conversation surface where users and LLM-driven agents talk in direct messages (user↔agent, agent↔agent, user↔user) and multi-member channels. Every message is a `chat_message` row; the `conversation` row is the unit of membership, IAM authorization, room versioning, and per-member cursors. Agents are autonomous room participants: the manager wakes them when a conversation changes, the agent pulls the update through its durable per-channel cursor, runs one LLM session (the `command` row is that session's execution anchor), and posts its reply itself through `PostMessage` with base-version optimistic concurrency. All intermediate reasoning, tool calls, and events are persisted on the command and remain visible in the command detail views.

Historical note — superseded design (kept for context; the original text described this as the plan):

- **Removed:** `SendCommand(source=CHAT)` and the `CommandSource` enum (`proto/v1/v1/command.proto` now reserves the `source`/`executor_kind` fields on `Command`, and `LATEST.sql` drops the `command.source_type` / `command.executor_kind` columns — "all commands now execute via ACP and originate from chat messages").
- **Changed:** assistant messages are no longer created by `Dispatcher.HandleResult` from `result.final_summary`; the agent posts its own reply via `PostMessage` (with `command_id` linking back to its session command). `HandleResult` only finalizes the command row.
- **Removed:** send-time light context injection. `GetRecentChatMessages` and `buildLightChatContext` still exist in the store/API but have no production callers (only their own tests).
- **Removed:** the embedded HTTP MCP server at `127.0.0.1:{port}` that exposed `search_chat_history` / `get_command_context` as MCP tools. Replaced by `laelia-machine` CLI subcommands (e.g. `message search`, `command context`) that talk over a unix-socket daemon; MCP is now reserved for managed MCP servers (see section 6).

### Key Design Decisions (current)

| Decision | Rationale |
|---|---|
| Dedicated `chat_message` table, not reused `command` | The `command` table carries execution artifacts (exit code, env, working dir, outputs) irrelevant to chat. `chat_message` is the conversation surface; `command` is the execution anchor. |
| `conversation` with four types + `conversation_member_meta` membership index | 1=DM (user+agent), 2=channel, 3=AGENT_DM (two agents, SYSTEM_BOT owner), 4=USER_DM (two users). `agent_id` is nullable; channels and user/agent DMs have no owning agent. Membership, pin/close/mute state, and the authorization index live in `conversation_member_meta`; actual authorization lives in the conversation IAM policy. |
| `chat_message.command_id` nullable FK | Assistant (AGENT-sender) replies reference the session command that produced them; user/system messages have none. Enables "View details" drill-down without forcing every message through the command lifecycle. |
| Assistant message created by the agent via `PostMessage` | The agent is in the room and decides when/what to post. `base_version` optimistic concurrency: a stale draft returns the new messages instead of committing (the agent then revises, sends as-is, discards, or force-sends). |
| Room version + per-member cursors | `conversation.version` bumps on every message; agents track `agent_channel_cursor`, users `user_channel_cursor`. Unread counts, long-poll deltas, and the drain loop are all version-driven. |
| No server-side context injection | The agent pulls the history it needs (`laelia-machine message read` / `message search` CLI tools over the daemon socket). Server-side prompt stuffing was removed. |
| `SearchChatHistory` searches `chat_message.search_text` (markdown-stripped, GIN trigram) plus attachment file names, scoped to caller-readable conversations | Role-aware via `sender_type`, ranked by matched tokens, returns snippet + thread context; no heuristic user/assistant detection. |
| Chat tools delivered as `laelia-machine` CLI subcommands over a unix-socket daemon | Replaces the former embedded HTTP MCP server: the LLM invokes the agent binary from its shell, the CLI forwards each call over the socket to the daemon, which holds the live (rotating) machine token and forwards to the manager over ConnectRPC. This keeps the long-lived token out of the subprocess environment. |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ Frontend (React 19 + Vite)                                        │
│  /chat (ChatLayout) · /chat/:conversationId (ChatConversationPage)│
│  ┌─────────────┐   SendMessage / PostMessage-equivalent RPCs      │
│  │ Chat UI     │────────────────────────────────────────────────► │
│  │ (left-rail  │   ListConversationMessages (latest-N / delta /   │
│  │  conversation│        before-version, wait_ms long poll)       │
│  │  list,      │◄─────────────────────────────────────────────────│
│  │  composer,  │   SearchChatHistory · tasks · reactions · files  │
│  │  thread     │                                                  │
│  │  panel)     │   25s self-rescheduling long-poll watcher        │
│  └─────────────┘────────────────────────────────────────────────► │
└──────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ Laelia Manager                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ CommandService (api/v1)                                      │ │
│  │  SendMessage (user) / PostMessage (agent, base_version)      │ │
│  │  → createMessage → CreateChatMessageBumpVersion              │ │
│  │    (bump conversation.version, wake roomhub watchers,        │ │
│  │     notify agent members, generate activity)                 │ │
│  │  ListConversationMessages (long poll via roomhub)            │ │
│  │  ListChannelUpdates / ListThreadUpdates / AckProcessedVersion│ │
│  │  SearchChatHistory · GetCommandContext                       │ │
│  └───────────────────────────────┬──────────────────────────────┘ │
│                                  │                                 │
│  ┌───────────────────────────────▼──────────────────────────────┐ │
│  │ Dispatcher                                                   │ │
│  │  NotifyNewMessages / NotifyThreadMention → NewMessagesAvailable│
│  │  HandleBeginSession: cursor-based drain gate; creates the    │ │
│  │    session's RUNNING command (system-bot principal)          │ │
│  │  HandleResult: finalizes the command only — never writes     │ │
│  │    chat messages                                             │ │
│  └───────────────────────────────┬──────────────────────────────┘ │
│                                  │                                 │
│  ┌───────────────────────────────▼──────────────────────────────┐ │
│  │ Store (PostgreSQL) — conversation, conversation_member_meta, │ │
│  │  chat_message, agent_channel_cursor, user_channel_cursor,    │ │
│  │  command, command_output, command_event, command_conversation│ │
│  └──────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
         │ Bidirectional gRPC stream (AgentStream / machine channel)
         ▼
┌──────────────────────────────────────────────────────────────────┐
│ Laelia Machine (per host, outbound-only connection)               │
│  ┌────────────────────────────┐  ┌─────────────────────────────┐ │
│  │ Drain loop (client runner) │  │ Daemon (unix-socket HTTP)   │ │
│  │  ListChannelUpdates →      │  │  /message/*  /thread/*      │ │
│  │  BeginSession → run LLM    │  │  /task/*  /reminder/*       │ │
│  │  turn; CLI tools:          │◄─│  /channel/*  /file/*        │ │
│  │  laelia-machine message …  │  │  /command/context  /members │ │
│  │  (LLM calls them from its  │  │  forwards → manager Connect │ │
│  │  shell)                    │  │  RPC with machine token     │ │
│  └────────────────────────────┘  └─────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │ Executor (ACP stdio acp path / acp2 v2 thread path / pi)   │  │
│  │  ACP session/new carries mcpServers = [laelia-mcp stdio    │  │
│  │  proxy (`laelia-machine mcp-proxy`)] for managed MCP only  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### Data Flow for a Chat Message (current)

```
1. Frontend opens /chat → ListChannels (left rail with unread counts) →
   opens a conversation → ListConversationMessages (latest N, chronological)
   → the channel watcher self-reschedules: one long poll every 25s
   (ListConversationMessages after_version=<cursor>, wait_ms=25000).

2. User types a message → SendMessage(conversation, content, ...):
   a. Handler validates membership/authorization (IAM), archived state, and
      thread_root (if replying), resolves attachments, parses @mentions.
   b. createMessage → CreateChatMessageBumpVersion: bumps
      conversation.version, inserts the chat_message carrying that
      room_version, and clears per-member close flags.
   c. roomNotifier wakes the roomhub waiters → long-poll watchers return
      immediately with the delta.
   d. notifyConversationAgents → dispatcher.NotifyNewMessages →
      NewMessagesAvailable on each member agent's stream.
   e. GenerateActivityForMessage creates per-user activity rows.

3. Agent drain loop reacts (or polls on its own cadence):
   a. `laelia-machine message check` → ListChannelUpdates: conversations
      whose room_version is beyond the agent's agent_channel_cursor.
   b. `laelia-machine message ack` → BeginSession: the manager creates the
      session's RUNNING command (system-bot principal, no conversation yet)
      and returns its command_id; idle when nothing to process.
   c. Agent reads new messages (`laelia-machine message read`) — the read
      links the session command to the conversation (LinkCommandConversation)
      so the channel header can show it as "running".
   d. LLM session composes the reply; the agent may search history
      (`laelia-machine message search` → SearchChatHistory) or fetch the
      execution context of its own command (`laelia-machine command context`).

4. Agent posts the reply:
   a. `laelia-machine message send` → PostMessage(conversation, content,
      base_version=<cursor>, command_id, thread_root?, attachments?).
   b. If base_version == conversation.version: the assistant chat_message
      (role=2, sender_type=AGENT, command_id) is committed atomically with a
      version bump; the posting agent's cursor advances past its own message.
   c. If the room moved on: Committed=false, response carries the new
      messages; the agent revises, sends as-is, discards, or force-sends.

5. Dispatcher.HandleResult (session end) only updates the command row
   (status, exit code, final_summary, result struct) and closes watchers —
   it does not create chat messages.

6. The frontend sees the reply through the long-poll watcher (roomhub wake),
   not by polling command status; thread replies are excluded from the main
   delta and arrive via the thread watcher / ListChannelThreads badges.
```

---

## 3. Data Model

### 3.1 conversation table

Current shape (from `backend/manager/migration/migration/LATEST.sql`, abridged):

```sql
CREATE TABLE conversation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER REFERENCES agent(id),   -- nullable: only type-1 DMs have an owning agent
    title TEXT NOT NULL DEFAULT '',
    type SMALLINT NOT NULL DEFAULT 1,        -- 1=DM, 2=channel, 3=AGENT_DM, 4=USER_DM
    created_by INTEGER NOT NULL REFERENCES principal(id),
    owner_id INTEGER NOT NULL REFERENCES principal(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived BOOLEAN NOT NULL DEFAULT false,
    archived_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1,       -- room version; bumps on every new chat_message
    -- agent_dm_a / agent_dm_b (type 3) and user_dm_a / user_dm_b (type 4):
    -- ordered (lo < hi) dedup columns backing the partial unique indexes below.
    CHECK (agent_dm_a IS NULL OR agent_dm_b IS NULL OR agent_dm_a < agent_dm_b)
);

CREATE UNIQUE INDEX idx_conversation_dm_unique
  ON conversation(agent_id, created_by) WHERE type = 1;
CREATE UNIQUE INDEX idx_conversation_channel_title_unique
  ON conversation(title) WHERE type = 2;
CREATE UNIQUE INDEX idx_conversation_agent_dm_unique
  ON conversation(agent_dm_a, agent_dm_b) WHERE type = 3;
CREATE UNIQUE INDEX idx_conversation_user_dm_unique
  ON conversation(user_dm_a, user_dm_b) WHERE type = 4;
```

- The original unique index `idx_conversation_agent_principal` (agent_id, created_by, type) was dropped when channels arrived; type-1 DM dedup is now `idx_conversation_dm_unique`.
- `GetOrCreateDirectConversation` (`store/conversation.go`) uses `INSERT ... ON CONFLICT (agent_id, created_by) WHERE type = 1 DO NOTHING RETURNING ...`; the losing racer re-reads the winning row. The same race-free pattern backs `GetOrCreateAgentDM` and `GetOrCreateUserUserDM`.
- Creating a DM seeds both members' cursors (`SeedCursorOnJoin` / `SeedUserReadCursorOnJoin`) so only future messages count as new.
- Archived channels (owner-level `archived`) are read-only, hidden from rosters, and stay searchable.

### 3.2 conversation_member_meta table (was "conversation_member (future)")

The future-work member table is now real and central:

```sql
CREATE TABLE conversation_member_meta (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,          -- 1=USER, 2=AGENT (store.MemberTypeUser/Agent)
    member_id TEXT NOT NULL,                -- user handle, or agent resource_id
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pinned BOOLEAN NOT NULL DEFAULT FALSE,  -- per-member UI state
    pinned_at TIMESTAMPTZ,
    closed BOOLEAN NOT NULL DEFAULT FALSE,  -- hidden from the left rail until new activity
    closed_at TIMESTAMPTZ,
    muted BOOLEAN NOT NULL DEFAULT FALSE,   -- silences activity/push except direct @mentions
    muted_at TIMESTAMPTZ,
    PRIMARY KEY (conversation_id, member_type, member_id)
);
```

Every membership write is dual-maintained with the conversation IAM policy (`policy` table, resource_type=CONVERSATION) in one transaction. Member roles are OWNER/ADMIN/MEMBER (`store.MemberRole*`). Agent memberships store the agent's `resource_id` (its handle, e.g. `rei-agent-1`), which is also the @mention id.

### 3.3 chat_message table

```sql
CREATE TABLE chat_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id),
    role SMALLINT NOT NULL DEFAULT 1,       -- 1=USER, 2=ASSISTANT (legacy; sender_type is authoritative)
    content TEXT NOT NULL,
    command_id UUID REFERENCES command(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sender_agent_id INTEGER REFERENCES agent(id),  -- set for AGENT senders
    room_version BIGINT NOT NULL DEFAULT 0,        -- conversation.version at message creation
    sender_type SMALLINT NOT NULL DEFAULT 1,       -- 1=USER, 2=AGENT, 3=SYSTEM
    mentions JSONB NOT NULL DEFAULT '[]',
    attachments JSONB NOT NULL DEFAULT '[]',       -- refs into the file table (S3-backed)
    thread_root_message_id UUID REFERENCES chat_message(id) ON DELETE CASCADE,  -- set for thread replies
    search_text TEXT NOT NULL DEFAULT ''           -- markdown-stripped plain text for search
);

CREATE INDEX idx_chat_message_conversation ON chat_message(conversation_id, created_at);
CREATE INDEX idx_chat_message_command ON chat_message(command_id) WHERE command_id IS NOT NULL;
CREATE INDEX idx_chat_message_room_version ON chat_message(conversation_id, room_version);
CREATE INDEX idx_chat_message_search_text_trgm ON chat_message USING GIN (search_text gin_trgm_ops);
```

| Column | Description |
|---|---|
| `sender_type` | 1=USER, 2=AGENT, 3=SYSTEM. Replaces the deprecated `CommandSource` enum at the message layer and drives unread counts, sender display, and search attribution. |
| `role` | Retained for compatibility (1=USER, 2=ASSISTANT); new reads key off `sender_type`. |
| `command_id` | Links assistant replies to their originating session command (set by `PostMessage`). Enables "View details" → command detail drill-down. |
| `room_version` | `conversation.version` at creation. The basis for agent cursors, user read cursors, unread counts, and long-poll deltas. |
| `mentions` / `attachments` | Denormalized JSONB lists (proto `Mention` / `Attachment`). Mentions drive thread subscription and wake routing; attachments reference S3-backed `file` rows. |
| `thread_root_message_id` | Non-NULL only for thread replies. Thread replies are excluded from the main-channel list and unread badges; the root carries `thread_reply_count`. |
| `search_text` | `markdownToPlainText(content)` written by every insert path; powers the GIN-trigram `SearchChatMessages`. |

`CreateChatMessageBumpVersion` (`store/chat_message.go`) is the single entry point for user (`SendMessage`) and assistant (`PostMessage`) messages: it bumps `conversation.version`, inserts the row, clears the per-member `closed` flags (main-channel messages only), and wakes roomhub long-pollers. A message insert that also creates a task row goes through `CreateTaskMessageBumpVersion`.

### 3.4 command table

```sql
-- Phase 3 removed the chat-era columns; commands are agent-initiated now:
ALTER TABLE command DROP COLUMN IF EXISTS executor_kind;
ALTER TABLE command DROP COLUMN IF EXISTS source_type;

-- The primary conversation link is retained for the command-detail view,
-- plus a many-to-many table for multi-channel turns:
ALTER TABLE command ADD COLUMN conversation_id UUID;
CREATE INDEX idx_command_chat_history ON command(agent_id, principal_id, created_at DESC)
  WHERE conversation_id IS NOT NULL;

CREATE TABLE command_conversation (
    command_id UUID NOT NULL REFERENCES command(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (command_id, conversation_id)
);
```

A session command is created by `Dispatcher.HandleBeginSession` with the system-bot principal and no instruction; it is linked to conversations when the agent reads (`ListConversationMessages`) or acks (`AckProcessedVersion` with `command_id`). `Dispatcher.HandleResult` finalizes status/summary only. Chat history is never queried from `command`; `SearchChatHistory` searches `chat_message` directly.

---

## 4. Proto Changes

All in `proto/v1/v1/command.proto` (package `laelia.v1`). There are no `google.api.http` REST bindings; the frontend talks ConnectRPC directly, and access control is declared with `laelia.v1.auth_method` (IAM/CUSTOM) plus `laelia.v1.permission` annotations resolved by the IAM interceptor.

### 4.1 Enums

`CommandSource` was removed entirely (`reserved "executor_kind", "source";` on `Command`). The message layer now uses:

```protobuf
enum SenderType {
  SENDER_TYPE_UNSPECIFIED = 0;
  SENDER_TYPE_USER = 1;
  SENDER_TYPE_AGENT = 2;
  SENDER_TYPE_SYSTEM = 3;
}
```

`TaskStatus` (TODO/IN_PROGRESS/IN_REVIEW/DONE) and `SearchScope` (MESSAGES/FILES) were added for the task board and chat search.

### 4.2 Chat Messages

`ChatMessage` (abridged; `name` is the bare message UUID, `conversation` the bare conversation UUID):

```protobuf
message ChatMessage {
  string name = 1;
  string conversation = 2;
  string principal_name = 3;
  int32 role = 4;                     // 1=USER, 2=ASSISTANT (legacy)
  string content = 5;
  string command_id = 6;              // set for AGENT replies
  google.protobuf.Timestamp created_at = 7;
  string sender_name = 8;
  SenderType sender_type = 9;
  int64 room_version = 10;
  repeated Mention mentions = 11;
  bool is_own = 12;                   // caller-relative, for agent readers
  repeated Attachment attachments = 13;
  string thread_root = 14;            // root message id when this is a thread reply
  int32 thread_reply_count = 15;      // populated on root messages
  TaskInfo task = 16;                 // set when the message is a task
  string agent_id = 17;               // "agents/{id}" of AGENT senders
  string principal_id = 18;           // author's mention handle ("system-bot" for SYSTEM)
  repeated Reaction reactions = 19;
}
```

### 4.3 Search

`SearchChatHistoryResponse` now returns `SearchChatHistoryEntry` (the legacy flat `ChatHistoryEntry` message is unused, kept only for wire compatibility):

```protobuf
message SearchChatHistoryEntry {
  ChatMessage message = 1;
  Conversation conversation = 2;
  string snippet = 3;                 // excerpt around the first match
  int32 match_field = 4;              // 1=message content, 2=attachment file name
  string matched_attachment_name = 5;
  SearchThreadContext thread_context = 6;  // set when the hit is a thread reply
}
```

`SearchChatHistoryRequest` carries `query`, `conversation` (optional scope), `since`/`until`, `from`, `scope`, `limit`, and `page_token`; `agent` is deprecated and ignored (identity comes from the auth context).

### 4.4 Conversation lifecycle RPCs

```protobuf
rpc GetOrCreateConversation(GetOrCreateConversationRequest) returns (GetOrCreateConversationResponse);
// user opens (or reuses) the type-1 DM with an agent; laelia.conversations.create

rpc GetOrCreateUserUserDM(GetOrCreateUserUserDMRequest) returns (GetOrCreateUserUserDMResponse);
// type-4 user↔user DM (peer by "users/<id>")

rpc GetOrCreateUserDM(GetOrCreateUserDMRequest) returns (GetOrCreateUserDMResponse);
// agent-callable twin: opens the type-1 DM with a user (peer by handle)

rpc GetOrCreateAgentDM(GetOrCreateAgentDMRequest) returns (GetOrCreateAgentDMResponse);
// type-3 agent↔agent DM (peer by "agents/<id>")

rpc ResolveChannelByTitle(ResolveChannelByTitleRequest) returns (ResolveChannelByTitleResponse);
rpc ListPeerAgents(ListPeerAgentsRequest) returns (ListPeerAgentsResponse);

rpc ListConversationMessages(ListConversationMessagesRequest) returns (ListConversationMessagesResponse);
rpc SendMessage(SendMessageRequest) returns (ChatMessage);       // user-facing
rpc PostMessage(PostMessageRequest) returns (PostMessageResponse); // agent-facing
```

Read cursors and optimistic concurrency are first-class protocol concepts:

```protobuf
message ListConversationMessagesRequest {
  string conversation = 1 [(google.api.field_behavior) = REQUIRED];
  int32 page_size = 2;
  string page_token = 3;
  int64 after_version = 4;   // delta: room_version > after_version (chronological)
  int64 before_version = 5;  // one page before a pivot (history window)
  int32 wait_ms = 6;         // long poll (≤30000ms) when no delta yet
}
message ListConversationMessagesResponse {
  repeated ChatMessage messages = 1;
  string next_page_token = 2;
  int64 current_version = 3;
}

message PostMessageRequest {
  string conversation = 1;      // "conversations/{id}"
  string content = 2;           // may be empty when attachments are provided
  int64 base_version = 3;       // optimistic concurrency: commit only if == current
  string command_id = 4;        // links the assistant reply to its session command
  repeated Attachment attachments = 5;
  string thread_root = 6;       // reply inside an existing thread
}
message PostMessageResponse {
  bool committed = 1;
  ChatMessage message = 2;
  int64 current_version = 3;
  repeated ChatMessage new_messages = 4;   // populated when !committed
  string conflict_description = 5;
}
```

Beyond messaging, the service now covers channel membership (`CreateChannel`, `ListChannels`, `JoinChannel`, `AddChannelMember`, `TransferChannelOwnership`, …), threads (`ListThreadMessages`, `ListChannelThreads`, `ListThreadUpdates`), tasks/claims (`ConvertMessageToTask`, `ClaimTask`, `UpdateTaskStatus`, …), reminders, reactions (`AddReaction`/`RemoveReaction`), files (`UploadFile`/`DownloadFile`/`ListFiles`), activities, the agent inbox (`ListChannelUpdates`, `ListAccessibleChannels`, `AckProcessedVersion`), and presence (`SyncPresence`, see `docs/plan/chat-presence-badge-design.md`).

---

## 5. Manager Backend

### 5.1 Store Layer

**`backend/manager/store/conversation.go`** — conversation rows and get-or-create:

```go
const (
    ConversationTypeDM      int32 = 1
    ConversationTypeChannel int32 = 2
    ConversationTypeAgentDM int32 = 3
    ConversationTypeUserDM  int32 = 4
)

type ConversationMessage struct {
    ID        uuid.UUID
    AgentID   sql.NullInt32   // nullable: channels/DMs without an owning agent
    Title     string
    Type      int32
    CreatedBy int
    OwnerID   int
    CreatedAt time.Time
    UpdatedAt time.Time
    Version   int64           // room version
    Archived  bool
}

func (s *Store) GetOrCreateDirectConversation(ctx, agentID, principalID int) (*ConversationMessage, error)
// race-free: ON CONFLICT (agent_id, created_by) WHERE type = 1 DO NOTHING,
// loser re-reads; seeds agent + user cursors on the create path.

func (s *Store) GetOrCreateAgentDM(ctx, agentAID, agentBID int) (*ConversationMessage, error)
func (s *Store) GetOrCreateUserUserDM(ctx, callerID, peerID int) (*ConversationMessage, error)
func (s *Store) CreateChannel / UpdateChannel / DeleteChannel / SetConversationArchived
func (s *Store) ListUserConversationsWithUnread(...)  // left-rail roster + unread counts + preview
func (s *Store) ListAgentConversations / ListAccessibleChannels
```

**`backend/manager/store/chat_message.go`** — messages, threads, tasks:

```go
func (s *Store) CreateChatMessageBumpVersion(ctx, msg *ChatMessage) (*ChatMessage, int64, error)
// single entry point for user + assistant messages: bump version, insert row
// with room_version, clear per-member close flags, wake roomNotifier.

func (s *Store) ListConversationMessages(ctx, conversationID, afterVersion, beforeVersion int64, limit, offset int) ([]*ChatMessage, int64, error)
// three read modes, all chronological: delta (after_version), history page
// (before_version), latest-N (default). Excludes thread replies; fills
// thread_reply_count, task info, and reactions.

func (s *Store) ListThreadMessages(...)          // root + replies
func (s *Store) ListChannelThreads(...)          // per-thread summaries + previews
func (s *Store) SetChatMessageCommandID(...)     // command link helper
func (s *Store) GetRecentChatMessages(...)       // no production caller anymore
```

**`backend/manager/store/chat_search.go`** — `SearchChatMessages` over `search_text` + attachment names, token-ranked, scoped by `ChatSearchCaller` (user handle / agent resource id with owner-follow / workspace-read admin).

**`backend/manager/store/conversation_member.go`** — membership index + roles; every membership write is dual-maintained with the conversation IAM policy (see `conversation_policy.go`).

**`backend/manager/store/agent_channel_cursor.go` / `user_channel_cursor.go`** — durable per-member cursors (`UpsertCursor` is monotonic via GREATEST).

The `command` store keeps execution artifacts (`command_output`, `command_event`, `command_token_usage`) and the `conversation_id` link; the old `GetRecentChatHistory`-from-command query is long gone.

### 5.2 API Layer (`backend/manager/api/v1`)

**`channel_message.go` — `SendMessage` (user-facing)**: validates content/attachments, resolves the conversation, rejects agent-DM and archived targets, validates `thread_root`, resolves attachment membership, parses `@mentions` server-side (merged with client-supplied mentions), then delegates to the shared pipeline.

**`message_create.go` — `createMessage` (shared pipeline)**:

```go
func (s *CommandService) createMessage(ctx, in createMessageInput) (*store.ChatMessage, int64, error)
// CreateChatMessageBumpVersion (or CreateTaskMessageBumpVersion for as_task)
// → subscribeAndNotifyThread (thread replies) or notifyConversationAgents
// → GenerateActivityForMessage.
```

**`command_message.go`** — `GetOrCreateConversation`, `GetOrCreateUserUserDM`, `ListConversationMessages` (three read modes + `longPollDelta`, which subscribes to `roomhub` before re-reading so no wake is missed), `ListThreadMessages`, and `PostMessage` (agent path with base_version conflict handling; commits advance the posting agent's cursor).

**`command.go`** — the agent inbox: `ListChannelUpdates` (`ListChannelsWithUpdates`), `ListThreadUpdates` (subscribed threads), `AckProcessedVersion` (monotonic cursor advance + `LinkCommandConversation`).

**`command_search.go`** — `SearchChatHistory` with caller-scoped authorization (per-conversation `conversations.read`, or workspace scope for admins).

**`command_presence.go`** — `SyncPresence` (human heartbeat + batched online query; agents answered offline — see `docs/plan/chat-presence-badge-design.md`).

### 5.3 Dispatcher: `backend/manager/component/dispatcher/`

- **`HandleBeginSession`** (`dispatcher.go`): the drain gate. Checks `HasUpdates` (any conversation's version beyond the agent's cursor) and `HasDueReminders`; otherwise replies idle. Creates the session's RUNNING command (system-bot principal, empty instruction — the agent client supplies the prompt) and records it as the session's `currentCmdID`.
- **`HandleResult`** (`command_handler.go`): finalizes the command — status, exit code, duration, `final_summary`, result struct, ack seq — and closes watchers. It does **not** create chat messages (the agent already posted its reply via `PostMessage`); it also no longer chains the next command (the drain loop decides).
- **Wake paths**: `NotifyNewMessages` (message landed in a member conversation) and `NotifyThreadMention` (thread reply to a subscribed thread) push `NewMessagesAvailable` over the agent stream; a missed wake is recovered by the durable cursor on the next drain poll.
- `roomhub` (`backend/manager/component/roomhub/`) fans out per-conversation wakeups to the frontend's long-polling `ListConversationMessages` requests; the store's `roomNotifier` notifies it inside `CreateChatMessageBumpVersion`.

---

## 6. Agent Executor Integration

Identical in spirit to the original goal — the agent's runtime can search chat history and recall execution context — but the transport changed completely:

- **Chat tools are CLI subcommands, not MCP tools.** The LLM invokes the agent binary from its shell: `laelia-machine message check|read|search|send|ack`, `message thread ...`, `task ...`, `reminder ...`, `channel ...`, `command context`, `file ...`, `team ...`. Each CLI call forwards over a unix socket (`~/.laelia/daemon.sock`) to the **daemon** (`backend/agent/daemon/`), which authenticates with a per-daemon session token (`LAELIA_SESSION_TOKEN`) and forwards to the manager over ConnectRPC using the live machine token plus the `X-Laelia-Agent` header. The former embedded HTTP MCP server was removed (see the package comment in `backend/agent/daemon/server.go`).
- **Tool implementations live in `backend/agent/chattools/`** (`chattools.go`, `chattools_channel.go`, `chattools_task.go`, `chattools_reminder.go`, …), which build the `v1pb` requests (`SearchChatHistory`, `GetCommandContext`, `PostMessage`, `ListChannelUpdates`, …) and render text responses with error codes / next-action hints.
- **ACP MCP servers are for managed MCP only.** The executor's `session/new` carries `mcpServers` built by the client runner (`backend/agent/client/runner.go` `buildMcpServers`): a single stdio entry `laelia-mcp` → `laelia-machine mcp-proxy` (`backend/agent/cmd/mcp_proxy.go`), which forwards `tools/list`/`tools/call` to the daemon's localhost MCP proxy and on to the manager's McpGateway. There is no chat-specific MCP server, and MCP unavailability degrades gracefully (empty catalog → no MCP servers).
- The `CommandRequest`-style "source / principal_id" fields are gone with `CommandSource`; the agent knows who it is from `BeginSession` (agent display name, owner display name, team context, prompt version) injected into its system prompt.

---

## 7. Frontend

### 7.1 Store Architecture

Chat state lives in dedicated Zustand slices (registered in `frontend/src/stores/index.ts` next to `command`, `channel`, `thread`, `user`, `members`, …):

**`stores/chat.ts`** (current):

```typescript
interface ChatSlice {
  chatMessages: Record<string, ChatMessageUI[]>;   // keyed by conversation name
  chatLoading: Record<string, boolean>;
  chatCurrentVersion: Record<string, bigint>;      // per-conversation delta cursor
  chatJumpByConv / chatJumpLoading / chatHasOlderByConv / chatHasNewerByConv;

  getOrCreateConversation(agent: string): Promise<string>;
  getOrCreateUserUserDM(peerUser: string): Promise<string>;
  loadMessages(conversation: string): Promise<void>;
  sendChatMessage(agent: string, instruction: string, conversationId?: string): Promise<ChatMessage>;
  toggleReaction(conversation, messageId, emoji): Promise<void>;
  jumpToMessage / loadOlderMessages / loadNewerMessages / clearJump;
  appendChatMessage / patchChatMessage / removeChatMessage;  // optimistic composer writes
}
```

`sendChatMessage` flow (current):
1. Optimistically appends the user message (local `crypto.randomUUID()` placeholder).
2. Calls `SendMessage` (there is no `SendCommand(source=CHAT)` anymore).
3. Reconciles: removes the placeholder and merges the server echo (id-deduped) so the watcher cannot duplicate it.
4. The agent's reply arrives asynchronously through the channel watcher — the UI no longer polls command status.

**`stores/chat-watcher.ts`** — the watcher cadences: the channel and thread watchers run a self-rescheduling long-poll loop (`LONG_POLL_MS = 25000` against the server's 30s `wait_ms` cap, 1s retry backoff, paused while the tab is hidden), plus a 5s badge poll (`ListChannelThreads`, tasks, agent activity).

**`stores/channel.ts`** holds the left-rail roster (conversations with unread counts, pin/close/mute state, last-message preview) and the open conversation; `stores/thread.ts` the open thread; `stores/command.ts` remains command-centric (detail views, outputs/events).

### 7.2 Chat Pages

```
┌──────────────────────────────────────────────────────┐
│ ChatLayout  /chat                                    │
│ ├─ conversation-list.tsx (DM rows + channel rows,    │
│ │  unread badges, pin, presence dot)                 │
│ └─ :conversationId → ChatConversationPage            │
│     ├─ chat-conversation header (peer avatar +       │
│     │  presence badge, agent activity, thread)       │
│     ├─ message-row.tsx (markdown, mentions,          │
│     │  reactions, task badges, attachments)          │
│     ├─ chat-composer.tsx (optimistic send, uploads)  │
│     └─ thread-panel.tsx (side conversation)          │
└──────────────────────────────────────────────────────┘
```

Key UX behaviors (current):
- **Initialization**: `ListChannels` builds the left rail; opening a conversation loads the latest page (`ListConversationMessages`, latest-N) and starts the long-poll watcher.
- **Send**: `SendMessage` with optimistic echo-reconcile; agent replies arrive via the watcher.
- **View Details**: assistant messages with `command_id` link to `/members/agents/{agentId}/commands/{commandId}` (debug-mode gated in `message-row.tsx`).
- **Threads**: reply counts and the 3-most-recent-replies preview come from `ListChannelThreads`; the thread panel reads `ListThreadMessages`.
- **Tasks/reactions/files/activities** are first-class message affordances, not chat add-ons.

### 7.3 Routes

- `/chat` → `ChatLayout` (`pages/dashboard/chat-layout.tsx`); index → `ChatEmptyState`; `/chat/:conversationId` → `ChatConversationPage` (`pages/dashboard/chat-conversation.tsx`). Both lazy loaded.
- `/agents/:agentId/**` is a legacy redirect to `/members/agents/:agentId/**`; the agent detail "Chat" tab (`pages/dashboard/agent-chat.tsx`, route `.../chat`) lists the agent's conversations via `ListChannelsForAgent` and deep-links into `/chat/:conversationId`.

---

## 8. Files Inventory (current)

| File | Status | Purpose |
|---|---|---|
| `backend/manager/migration/migration/LATEST.sql` | evolved | Unified conversation model, `chat_message` extensions (room_version, sender_type, mentions, attachments, threads, search_text), cursors, `command_conversation`; dropped `command.source_type`/`executor_kind` |
| `proto/v1/v1/command.proto` | evolved | `SenderType`, current `ChatMessage`, SendMessage/PostMessage, versioned reads + wait_ms, DM/channel RPCs, `SearchChatHistoryEntry`, task/reminder/reaction/file/presence RPCs |
| `backend/manager/store/conversation.go` | evolved | Four conversation types, race-free get-or-create DMs, roster/unread queries |
| `backend/manager/store/chat_message.go` | evolved | `CreateChatMessageBumpVersion`, versioned list reads, threads/tasks/reactions fills |
| `backend/manager/store/chat_search.go` | current | Trigram search over `search_text` + attachment names |
| `backend/manager/store/conversation_member.go` + `conversation_policy.go` | current | Membership index + IAM policy dual-writes |
| `backend/manager/store/agent_channel_cursor.go`, `user_channel_cursor.go` | current | Durable per-member cursors |
| `backend/manager/api/v1/command_message.go` | evolved | `GetOrCreateConversation` (+user twin), `ListConversationMessages` (long poll), `PostMessage` |
| `backend/manager/api/v1/channel_message.go` + `message_create.go` | current | `SendMessage` + shared `createMessage` pipeline (notify/thread/activity) |
| `backend/manager/api/v1/command.go` | evolved | Command CRUD/watch + agent inbox RPCs (`ListChannelUpdates`, `AckProcessedVersion`) |
| `backend/manager/api/v1/command_search.go` | current | Caller-scoped `SearchChatHistory` handler |
| `backend/manager/component/dispatcher/dispatcher.go` + `command_handler.go` | evolved | `HandleBeginSession` (drain gate + session command), `HandleResult` (finalize only), wake paths |
| `backend/manager/component/roomhub/roomhub.go` | current | Per-conversation wakeup hub for long polls |
| `backend/agent/daemon/server.go` + `handlers_chat.go` | current | Unix-socket daemon exposing the chat/task/file/channel tools (replaced the former MCP HTTP server) |
| `backend/agent/chattools/*.go` | current | Tool implementations calling the manager ConnectRPC APIs |
| `backend/agent/cmd/message.go`, `command.go`, `mcp_proxy.go`, … | current | `laelia-machine` CLI subcommands the LLM invokes from its shell |
| `backend/agent/client/runner.go` | current | `buildMcpServers`: stdio `laelia-mcp` proxy for managed MCP |
| `frontend/src/stores/chat.ts`, `chat-watcher.ts`, `channel.ts`, `thread.ts`, `command.ts` | current | Chat state, watcher long-poll, roster/thread/command state |
| `frontend/src/pages/dashboard/chat-layout.tsx`, `chat-conversation.tsx`, `agent-chat.tsx` | current | Chat pages (unified chat + agent detail chat tab) |
| `frontend/src/components/chat/*` (conversation-list, chat-composer, message-row, thread-panel, …) | current | Chat UI components |

Removed along the way (historical): the embedded agent MCP HTTP server (`backend/agent/mcp/server.go` never shipped in this form — superseded by the daemon + CLI), `SendCommand(source=CHAT)` frontend flow, send-time chat-context injection (`GetRecentChatMessages`/`buildLightChatContext` are caller-less today), and the Phase-1 inbox tables (`agent_inbox`, `agent_working_state`, `held_action`).