# Chat Feature Design

## 1. Overview

Add a real-time chat interface allowing users to converse directly with agents. Modeled after instant messaging UX (e.g. Feishu/Lark). The agent processes messages via its ACP-backed LLM and returns only the final answer in the chat UI. All intermediate reasoning, tool calls, and events are persisted for audit/debug in the existing command detail views.

This design introduces a dedicated **`chat_message`** table, decoupling chat messages from the `command` table. Each assistant message links back to its originating `command` for traceability. Custom MCP tools are injected into each ACP session so the LLM can actively search chat history and recall full execution context.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Dedicated `chat_message` table, not reused `command` | The `command` table carries 22+ fields (exit code, env, working dir, etc.) irrelevant to chat. A dedicated table provides clean separation: `chat_message` for conversation UI, `command` for execution artifacts. |
| `conversation` table with `agent_id` | One conversation per (agent, principal) pair for direct chat. Grows into group chat via `conversation_member` in the future. |
| `chat_message.command_id` nullable FK | Only ASSISTANT messages reference their originating command. USER messages have no command. This provides traceability without forcing every message through the command lifecycle. |
| Assistant message created in `Dispatcher.HandleResult` | The single point where command completion is processed. Uses `command.source_type` and `command.conversation_id` to decide whether to create a chat_message. |
| Lightweight context injection (3–5 recent rounds) + MCP tool hint | Gives the LLM enough context for continuity without overwhelming it. Older history is available on-demand via `search_chat_history` MCP tool. |
| `SearchChatHistory` queries `chat_message` via `conversation` JOIN | Clean role-based results; no more heuristic detection of user vs assistant from instruction/final_summary. |
| Embed MCP Server in Laelia Agent (localhost HTTP) | The ACP protocol natively supports `mcpServers` in `session/new`. Embedding avoids a separate binary; localhost-only needs no auth. |
| Two MCP tools: `search_chat_history` + `get_command_context` | Gives the LLM agency to dig into history and recall prior "thinking" on demand. |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ Frontend (React)                                                   │
│  /agents/:id/chat                                                  │
│  ┌─────────────┐   POST /v1/agents/*/conversations                │
│  │ Chat UI     │────────────────────────────────────────────────► │
│  │ (messages,  │   GET  /v1/conversations/*/messages              │
│  │  input box, │◄─────────────────────────────────────────────────│
│  │  queue      │                                                  │
│  │  indicator) │   POST /v1/agents/*/commands (source=CHAT)       │
│  └─────────────┘────────────────────────────────────────────────► │
│                  │  WatchCommand (streaming progress)              │
│                  ◄─────────────────────────────────────────────────│
└──────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ Laelia Manager                                                    │
│  ┌───────────────────┐  ┌────────────────────────────────────┐    │
│  │ SendCommand       │  │ GetOrCreateConversation            │    │
│  │  - creates conv   │  │ ListConversationMessages           │    │
│  │  - creates user   │  │ SearchChatHistory                  │    │
│  │    chat_message   │  │ GetCommandContext                  │    │
│  │  - light context  │  └──────────────┬─────────────────────┘    │
│  │    injection      │                 │                            │
│  └────────┬──────────┘                 │ gRPC                       │
│           │                            │                            │
│  ┌────────▼────────────────────────────▼──┐                         │
│  │ Dispatcher                             │                         │
│  │  HandleResult: creates assistant       │                         │
│  │  chat_message on CHAT completion       │                         │
│  └────────────────────┬───────────────────┘                         │
│                       │                                            │
│  ┌────────────────────▼───────────────────┐                         │
│  │ Store (PostgreSQL)                     │                         │
│  │  chat_message, conversation,           │                         │
│  │  command, command_output, command_event│                         │
│  └────────────────────────────────────────┘                         │
└──────────────────────────────────────────────────────────────────┘
         │ Bidirectional gRPC Stream (CommandChannel)
         ▼
┌──────────────────────────────────────────────────────────────────┐
│ Laelia Agent (per node)                                           │
│                                                                    │
│  ┌──────────────────────┐    ┌─────────────────────────────┐      │
│  │ command_stream       │    │ HTTP MCP Server (embedded)   │      │
│  │  buildRuntime(src)   │    │  127.0.0.1:{random_port}     │      │
│  │       │              │    │                              │      │
│  │       ▼              │    │  ┌─────────────────────────┐ │      │
│  │  ACPExecutor         │    │  │ Tools:                  │ │      │
│  │   session/new ───HTTP───►│  │  search_chat_history    │ │      │
│  │   mcpServers:       │    │  │  get_command_context    │ │      │
│  │   [{type:"http",    │    │  └───────────┬─────────────┘ │      │
│  │     url:"127.0.0.1  │    │              │ gRPC          │      │
│  │     :PORT/mcp       │    │     Manager API (token auth) │      │
│  │     ?agent=...&     │    └─────────────────────────────┘      │
│  │     principal=..."}]│                                          │
│  └──────────────────────┘                                         │
└──────────────────────────────────────────────────────────────────┘
         │ ACP (stdio)
         ▼
┌──────────────────────────────────────────────────────────────────┐
│ opencode (ACP binary)                                             │
│  LLM processes instruction, calls MCP tools for history/context   │
│  Returns final summary via ACP protocol                           │
└──────────────────────────────────────────────────────────────────┘
```

### Data Flow for a Chat Message

```
1. Frontend loads chat page → POST /v1/agents/{id}/conversations
   → Creates or finds direct conversation for (agent, principal)
   → GET /v1/conversations/{conv_id}/messages → renders chat history

2. User types message → POST /v1/agents/{id}/commands (source=CHAT)
   a. Manager gets/creates conversation via GetOrCreateDirectConversation
   b. Creates user chat_message (role=USER, conversation_id)
   c. Loads last 3–5 rounds from chat_message for light context injection
   d. Creates command row (status=PENDING, source_type=CHAT, conversation_id)
   e. Dispatches to agent via gRPC stream

3. Agent processes:
   a. buildRuntime sees source_type=CHAT
   b. ACPExecutor sets McpServers with HTTP URL pointing to local MCP server
   c. Starts ACP binary (opencode), creates session with mcpServers config
   d. Sends Prompt with the instruction (light context + user message)

4. opencode processes:
   a. LLM reads instruction including light conversation context
   b. LLM optionally calls MCP tools to search deeper history or recall context
   c. LLM executes tools, streams progress/events back to manager

5. Agent sends Result → Dispatcher.HandleResult:
   a. Loads command to check source_type and conversation_id
   b. If source_type=CHAT and conversation_id is set and final_summary is present:
      Creates assistant chat_message (role=ASSISTANT, content=final_summary, command_id)
   c. Updates command status, broadcasts final result

6. Frontend polls command status, on completion reloads messages from
   GET /v1/conversations/{conv_id}/messages → renders full updated history
```

---

## 3. Data Model

### 3.1 conversation table

```sql
CREATE TABLE conversation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id),
    title TEXT NOT NULL DEFAULT '',
    type SMALLINT NOT NULL DEFAULT 1,       -- 1=DIRECT, 2=GROUP (future)
    created_by INTEGER NOT NULL REFERENCES principal(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_conversation_agent_principal
  ON conversation(agent_id, created_by, type);
```

One conversation per (agent, principal) pair for direct (`type=1`) chats. The unique index enables `INSERT ... ON CONFLICT DO NOTHING` in `GetOrCreateDirectConversation`.

### 3.2 conversation_member table (future)

```sql
CREATE TABLE conversation_member (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,          -- 1=USER, 2=AGENT
    member_id TEXT NOT NULL,                -- principal_id or agent_resource_id
    PRIMARY KEY (conversation_id, member_type, member_id)
);
```

Schema-only; reserved for group chat. Not used in current direct-chat implementation.

### 3.3 chat_message table

```sql
CREATE TABLE chat_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    principal_id INTEGER NOT NULL REFERENCES principal(id),
    role SMALLINT NOT NULL DEFAULT 1,       -- 1=USER, 2=ASSISTANT
    content TEXT NOT NULL,
    command_id UUID REFERENCES command(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_message_conversation
  ON chat_message(conversation_id, created_at);

CREATE INDEX idx_chat_message_command
  ON chat_message(command_id) WHERE command_id IS NOT NULL;
```

| Column | Description |
|---|---|
| `role` | 1=USER (human message), 2=ASSISTANT (agent reply). Removes heuristic detection. |
| `content` | Full message text. Equivalent to `instruction` for user, `final_summary` for assistant. |
| `command_id` | Links assistant messages to their originating command. NULL for user messages. Enables "View details" → command detail drill-down. |

### 3.4 command table

```sql
-- Relevant columns for chat
ALTER TABLE command ADD COLUMN source_type SMALLINT NOT NULL DEFAULT 0;
-- 0=UNSPECIFIED, 1=MANUAL, 2=CHAT

ALTER TABLE command ADD COLUMN conversation_id UUID;
-- Set by SendCommand when source=CHAT; used by Dispatcher.HandleResult
-- to know which conversation to write the assistant message into.

CREATE INDEX idx_command_chat_history
  ON command(agent_id, principal_id, source_type, created_at DESC)
  WHERE source_type = 2;  -- CHAT
```

The `command` table retains its full execution detail (outputs, events, exit codes, etc.) but chat history is no longer queried from it. `SearchChatHistory` now joins `chat_message` ↔ `conversation` for role-aware, deduplicated results.

---

## 4. Proto Changes

All changes in `proto/v1/v1/command.proto`.

### 4.1 Enums

```protobuf
enum CommandSource {
  COMMAND_SOURCE_UNSPECIFIED = 0;
  MANUAL = 1;
  CHAT = 2;
}
```

### 4.2 Chat-specific Messages

```protobuf
message ChatMessage {
  string name = 1;                    // message ID (UUID)
  string conversation = 2;            // "conversations/{id}" (resource name)
  string principal_name = 3;
  int32 role = 4;                     // 1=USER, 2=ASSISTANT
  string content = 5;
  string command_id = 6;              // only set for ASSISTANT messages
  google.protobuf.Timestamp created_at = 7;
}

message ListConversationMessagesRequest {
  string conversation = 1 [(google.api.field_behavior) = REQUIRED];
  int32 page_size = 2;
  string page_token = 3;
}

message ListConversationMessagesResponse {
  repeated ChatMessage messages = 1;
  string next_page_token = 2;
}

message GetOrCreateConversationRequest {
  string agent = 1 [(google.api.field_behavior) = REQUIRED];
}

message GetOrCreateConversationResponse {
  string name = 1;                    // "conversations/{id}"
}
```

### 4.3 ChatHistoryEntry (revised)

```protobuf
message ChatHistoryEntry {
  string message_id = 1;              // chat_message.id
  string command_id = 2;              // set for assistant entries
  string role = 3;                    // "1" (USER) or "2" (ASSISTANT)
  string content = 4;                 // message text
  google.protobuf.Timestamp created_at = 5;
}
```

Previously used `instruction`/`final_summary` with heuristic role detection. Now uses direct `role`/`content` from `chat_message`.

### 4.4 RPCs

```protobuf
service CommandService {
  // ... existing: SendCommand, ListCommands, GetCommand, CancelCommand,
  //     WatchCommand, WatchCommandEvents, RespondPermission ...

  rpc SearchChatHistory(SearchChatHistoryRequest)
      returns (SearchChatHistoryResponse) {
    option (google.api.http) = {get: "/v1/{agent=agents/*}/chat-history"};
  }

  rpc GetCommandContext(GetCommandContextRequest)
      returns (GetCommandContextResponse) {
    option (google.api.http) = {get: "/v1/{name=agents/*/commands/*}/context"};
  }

  rpc GetOrCreateConversation(GetOrCreateConversationRequest)
      returns (GetOrCreateConversationResponse) {
    option (google.api.http) = {post: "/v1/{agent=agents/*}/conversations"};
  }

  rpc ListConversationMessages(ListConversationMessagesRequest)
      returns (ListConversationMessagesResponse) {
    option (google.api.http) = {get: "/v1/{conversation=conversations/*}/messages"};
  }
}
```

---

## 5. Manager Backend

### 5.1 Store Layer

**`store/conversation.go`** (new):

```go
type ConversationMessage struct {
    ID        uuid.UUID
    AgentID   int
    Title     string
    Type      int32
    CreatedBy int
    CreatedAt time.Time
}

func (s *Store) GetOrCreateDirectConversation(ctx context.Context, agentID, principalID int) (*ConversationMessage, error)
// INSERT ... ON CONFLICT DO NOTHING for (agent_id, created_by, type=1).
// Falls back to SELECT if conflict (concurrent creation).

func (s *Store) GetConversation(ctx context.Context, id uuid.UUID) (*ConversationMessage, error)
```

**`store/chat_message.go`** (new):

```go
type ChatMessage struct {
    ID             uuid.UUID
    ConversationID uuid.UUID
    PrincipalID    int
    PrincipalName  string
    Role           int32     // 1=USER, 2=ASSISTANT
    Content        string
    CommandID      uuid.NullUUID
    CreatedAt      time.Time
}

func (s *Store) CreateChatMessage(ctx context.Context, msg *ChatMessage) (*ChatMessage, error)

func (s *Store) ListConversationMessages(ctx context.Context, conversationID uuid.UUID, limit, offset int) ([]*ChatMessage, error)
// Returns messages in chronological order (ASC created_at) for chat UI rendering.

func (s *Store) GetRecentChatMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]*ChatMessage, error)
// Returns most recent N messages (DESC created_at) for light context building.
```

**`store/command.go`** (modified):

- `CommandMessage` gains `ConversationID *uuid.UUID` field.
- `CreateCommand` writes `conversation_id`.
- All SELECT queries scan `c.conversation_id`.
- `ChatHistoryEntry` struct updated to hold `MessageID`, `CommandID`, `Role`, `Content`.
- Removed `GetRecentChatHistory` (replaced by chat_message queries).
- `SearchChatHistory` rewritten with SQL:

```sql
SELECT cm.id, cm.command_id, cm.role, cm.content, cm.created_at
FROM chat_message cm
JOIN conversation c ON c.id = cm.conversation_id
WHERE c.agent_id = $1 AND cm.principal_id = $2
  AND ($3 = '' OR cm.content ILIKE '%' || $3 || '%')
  AND ($4::timestamptz IS NULL OR cm.created_at >= $4)
  AND ($5::timestamptz IS NULL OR cm.created_at <= $5)
ORDER BY cm.created_at DESC
LIMIT $6
```

### 5.2 API Layer: `api/v1/command.go`

**`SendCommand` (modified — CHAT path)**:

```go
if req.Msg.Source == v1pb.CommandSource_CHAT && instruction != "" {
    // 1. Get or create conversation
    conv, _ := s.store.GetOrCreateDirectConversation(ctx, agent.ID, principalID)
    conversationID = &conv.ID

    // 2. Create user chat_message
    s.store.CreateChatMessage(ctx, &store.ChatMessage{
        ConversationID: conv.ID,
        PrincipalID:    principalID,
        Role:           1, // USER
        Content:        instruction,
    })

    // 3. Light context injection (last 3–5 rounds from chat_message)
    if recent, _ := s.store.GetRecentChatMessages(ctx, conv.ID, 6); len(recent) > 0 {
        instruction = buildLightChatContext(recent) + "\n---\n" + instruction
    }
}
```

`buildLightChatContext` outputs:

```
## Recent conversation (use search_chat_history for older messages)
- User: What's the status of server X?
- Assistant: Server X is running, CPU at 45%.
- User: Check the logs.
---
```

Replaces the old `buildChatContext` which injected 20 rounds from `command` table with 2000-char truncation.

**`GetOrCreateConversation`** (new):

```go
func (s *CommandService) GetOrCreateConversation(ctx context.Context, req *connect.Request[...]) (*connect.Response[...], error) {
    agent := // lookup agent
    user, _ := GetUserFromContext(ctx)
    conv, _ := s.store.GetOrCreateDirectConversation(ctx, agent.ID, user.ID)
    return connect.NewResponse(&v1pb.GetOrCreateConversationResponse{
        Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
    }), nil
}
```

**`ListConversationMessages`** (new):

```go
func (s *CommandService) ListConversationMessages(ctx context.Context, req *connect.Request[...]) (*connect.Response[...], error) {
    convID := parseConversationID(req.Msg.Conversation) // "conversations/{id}" → uuid
    msgs, _ := s.store.ListConversationMessages(ctx, convID, limit, offset)
    // convert to proto ChatMessage, return with pagination
}
```

**`SearchChatHistory`** (modified):

Now uses new `ChatHistoryEntry` fields (`MessageId`, `CommandId`, `Role`, `Content`) instead of old `Instruction`/`FinalSummary`.

### 5.3 Dispatcher: `component/dispatcher/dispatcher.go`

**`HandleResult`** (modified):

```go
func (d *Dispatcher) HandleResult(ctx context.Context, agentID int, result *v1pb.CommandResult) error {
    cmdID := uuid.Parse(result.CommandId)

    // NEW: load command to check if chat message creation is needed
    cmd, _ := d.store.GetCommand(ctx, cmdID)

    // ... existing status update, ack seq, result summary ...

    // NEW: create assistant chat_message for CHAT commands
    if cmd != nil && cmd.SourceType == 2 && cmd.ConversationID != nil && result.FinalSummary != "" {
        d.store.CreateChatMessage(ctx, &store.ChatMessage{
            ConversationID: *cmd.ConversationID,
            PrincipalID:    cmd.PrincipalID,
            Role:           2, // ASSISTANT
            Content:        result.FinalSummary,
            CommandID:      uuid.NullUUID{UUID: cmdID, Valid: true},
        })
    }
    // ...
}
```

Uses `command.source_type` (set during `SendCommand`) and `command.conversation_id` to decide. One extra `GetCommand` call per result — negligible overhead.

### 5.4 MCP Server: `agent/mcp/server.go`

**`handleSearchChatHistory`** (modified):

```go
for _, e := range resp.Msg.Entries {
    results = append(results, chatHistoryResult{
        MessageID: e.MessageId,
        Role:      e.Role,        // directly from server — no heuristics
        Content:   e.Content,
        Timestamp: e.CreatedAt.AsTime().Format(...),
    })
}
```

Removed the old heuristic:
```go
// OLD:
role := "user"
if e.FinalSummary != "" && e.Instruction == "" {
    role = "assistant"
}
```

---

## 6. Agent Executor Integration

Identical to the original design. The agent's `ACPExecutor` injects the MCP server URL when `source_type == CHAT`. The `CommandRequest` message on the agent stream carries `source` and `principal_id` so the agent knows when to enable MCP tools.

---

## 7. Frontend

### 7.1 Store Architecture

Chat state is split from `commandSlice` into a dedicated `chatSlice`:

**`stores/chat.ts`** (new):

```typescript
interface ChatSlice {
  conversations: Record<string, string>;  // agentName → "conversations/{id}"
  chatMessages: ChatMessageUI[];
  chatLoading: boolean;

  getOrCreateConversation(agent: string): Promise<string>;
  loadMessages(conversation: string): Promise<void>;
  sendChatMessage(agent: string, instruction: string): Promise<void>;
}
```

`sendChatMessage` flow:
1. Optimistically inserts user message into local state (with `crypto.randomUUID()`)
2. Calls `SendCommand(source=CHAT)` via commandServiceClient
3. Frontend polls command status, on completion reloads via `loadMessages`
4. Server-validated assistant messages replace any stale local state

**`stores/command.ts`** (cleaned):

Removed `chatMessages`, `chatLoading`, `sendChatMessage`, `loadChatHistory`. Now contains only command-centric state and methods.

**`stores/index.ts`**:

```typescript
export const useAppStore = create<AppStoreState>()((...args) => ({
  ...createAuthSlice(...args),
  ...createAgentSlice(...args),
  ...createCommandSlice(...args),
  ...createChatSlice(...args),
}));
```

### 7.2 Chat Page: `pages/dashboard/chat.tsx`

```
┌──────────────────────────────────────────────────────┐
│ Agent Name / Chat                      [Tasks]        │
├──────────────────────────────────────────────────────┤
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:30  You: What's the status of server X?  │    │
│  └──────────────────────────────────────────────┘    │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:31  Agent: Server X is running, CPU 45%, │    │
│  │         memory 60%. 3 active connections.   │    │
│  │         [View details →]                     │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
├──────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────┐  [Send]      │
│  │ Type a message...                  │  (disabled   │
│  └────────────────────────────────────┘   when busy) │
└──────────────────────────────────────────────────────┘
```

Key UX behaviors:
- **Initialization**: Loads conversation (get-or-create), then loads messages from `ListConversationMessages`.
- **Send**: Creates a command, polls every 1s until completion, then reloads messages.
- **View Details**: Each assistant message links to `/agents/:agentId/commands/:cmdId` using `command_id` from `chat_message`.
- **Queue indicator**: Input disabled + "Agent is thinking..." when a command is in-flight.
- **Auto-scroll**: New messages scroll into view.

### 7.3 Route

`/agents/:agentId/chat` → `ChatPage` component (lazy loaded).

---

## 8. Files Inventory

| File | Status | Purpose |
|---|---|---|
| `backend/manager/migration/latest.sql` | modified | `chat_message` table, `conversation.agent_id`, unique index, fixed chat-history index |
| `proto/v1/v1/command.proto` | modified | `ChatMessage`, `GetOrCreateConversation`, `ListConversationMessages`, revised `ChatHistoryEntry` |
| `backend/manager/store/conversation.go` | **new** | `ConversationMessage` + `GetOrCreateDirectConversation` |
| `backend/manager/store/chat_message.go` | **new** | `ChatMessage` + `CreateChatMessage`/`ListConversationMessages`/`GetRecentChatMessages` |
| `backend/manager/store/command.go` | modified | Added `ConversationID`; removed `GetRecentChatHistory`; rewrote `SearchChatHistory` |
| `backend/manager/api/v1/command.go` | modified | `SendCommand` chat flow; new `GetOrCreateConversation`/`ListConversationMessages` handlers; `buildLightChatContext` |
| `backend/manager/component/dispatcher/dispatcher.go` | modified | `HandleResult` creates assistant `chat_message` |
| `backend/agent/mcp/server.go` | modified | Direct `role`/`content` access, no heuristic detection |
| `frontend/src/stores/types.ts` | modified | Added `ChatSlice`, `ChatMessageUI`; removed chat from `CommandSlice` |
| `frontend/src/stores/chat.ts` | **new** | Independent `chatSlice` |
| `frontend/src/stores/command.ts` | modified | Removed chat state/methods |
| `frontend/src/stores/index.ts` | modified | Registered `chatSlice` |
| `frontend/src/pages/dashboard/chat.tsx` | modified | Uses new store APIs; loads from `ListConversationMessages` |
