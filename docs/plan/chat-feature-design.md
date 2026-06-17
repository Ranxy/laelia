# Chat Feature Design

## 1. Overview

Add a real-time chat interface allowing users to converse directly with agents. Modeled after instant messaging UX (e.g. Feishu/Lark). The agent processes messages via its ACP-backed LLM and returns only the final answer in the chat UI. All intermediate reasoning, tool calls, and events are persisted for audit/debug in the existing command detail views.

This design reuses the existing **command** model, extending it with a `source_type` discriminator (`MANUAL` vs `CHAT`), and injects custom MCP tools into each ACP session so the LLM can actively search chat history and recall full execution context.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Reuse `command` table as chat message store | Commands already capture instruction, final_summary, outputs, events, and are keyed by agent+principal — a natural fit |
| `source_type` enum on `command` | Cleanly separates manual CLI commands from chat messages; all unified in command history views |
| Inject N recent messages as prompt context | Provides LLM with conversation continuity without modifying the ACP protocol |
| Embed MCP Server in Laelia Agent (localhost HTTP) | The ACP protocol natively supports `mcpServers` in `session/new`. Embedding avoids a separate binary; localhost-only needs no auth |
| Two MCP tools: `search_chat_history` + `get_command_context` | Gives the LLM agency to dig into history and recall prior "thinking" on demand |
| Group chat reserved but not implemented | `conversation` + `conversation_member` tables created as schema-only; `command.conversation_id` nullable FK |

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ Frontend (Vue.js / React)                                         │
│  /agents/:id/chat                                                 │
│  ┌─────────────┐   POST /v1/agents/*/commands (source=CHAT)       │
│  │ Chat UI     │────────────────────────────────────────────────► │
│  │ (messages,  │                                                  │
│  │  input box, │  WatchCommand (streaming final reply)            │
│  │  queue      │◄─────────────────────────────────────────────────│
│  │  indicator) │                                                  │
│  └─────────────┘                                                  │
└──────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────┐
│ Laelia Manager                                                    │
│  ┌──────────────┐   ┌─────────────────────────┐                   │
│  │ SendCommand  │   │ SearchChatHistory       │  (new RPCs)       │
│  │  - injects   │   │ GetCommandContext        │                   │
│  │    recent N  │   └───────────┬─────────────┘                   │
│  │    history   │               │                                  │
│  └──────┬───────┘               │ gRPC                             │
│         │                       │                                  │
│  ┌──────▼───────────────────────▼──┐                               │
│  │ Store (PostgreSQL)              │                               │
│  │  command, command_output,       │                               │
│  │  command_event, conversation    │                               │
│  └─────────────────────────────────┘                               │
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
1. User types message in chat UI → POST /v1/agents/{id}/commands (source=CHAT)
2. Manager receives request:
   a. Queries recent 20 chat messages for (agent, principal)
   b. Formats conversation history, appends to instruction
   c. Creates command row (status=PENDING, source_type=CHAT)
   d. Dispatches to agent via gRPC stream
3. Agent receives command:
   a. buildRuntime sees source_type=CHAT
   b. ACPExecutor sets McpServers with HTTP URL pointing to local MCP server
   c. Starts ACP binary (opencode), creates session with mcpServers config
   d. Sends Prompt with the enriched instruction (history + user message)
4. opencode processes:
   a. LLM reads instruction including conversation history
   b. LLM optionally calls MCP tools to search deeper history or recall context
   c. LLM executes tools, streams progress/events back to manager
5. Manager persists outputs + events, broadcasts final result
6. Chat UI receives final_summary via streaming, renders as agent reply
```

---

## 3. Data Model Changes

### 3.1 command table

```sql
ALTER TABLE command ADD COLUMN source_type SMALLINT NOT NULL DEFAULT 0;
-- 0=MANUAL, 1=CHAT

ALTER TABLE command ADD COLUMN conversation_id UUID REFERENCES conversation(id);
-- nullable; NULL for direct (1:1) chat, set for group conversations in future

CREATE INDEX idx_command_chat_history
  ON command(agent_id, principal_id, source_type, created_at DESC)
  WHERE source_type = 1;
```

### 3.2 conversation tables (v1: schema only, not used)

```sql
CREATE TABLE conversation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL DEFAULT '',
    type SMALLINT NOT NULL DEFAULT 1,  -- 1=DIRECT, 2=GROUP
    created_by INTEGER NOT NULL REFERENCES principal(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE conversation_member (
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    member_type SMALLINT NOT NULL,  -- 1=USER, 2=AGENT
    member_id TEXT NOT NULL,         -- principal_id or agent_resource_id
    PRIMARY KEY (conversation_id, member_type, member_id)
);
```

---

## 4. Proto Changes

All changes in `proto/v1/v1/command.proto`.

### 4.1 New Enum: CommandSource

```protobuf
enum CommandSource {
  COMMAND_SOURCE_UNSPECIFIED = 0;
  MANUAL = 1;
  CHAT = 2;
}
```

### 4.2 Modified Messages

```protobuf
message Command {
  // ... existing fields 1-20 ...
  CommandSource source = 21;
  string conversation_id = 22;  // reserved for group chat
}

message SendCommandRequest {
  // ... existing fields 1-8 ...
  CommandSource source = 9;
}
```

### 4.3 New RPCs

```protobuf
// Search chat history for a given agent+principal pair.
// Returns matching command records (instruction + final_summary).
rpc SearchChatHistory(SearchChatHistoryRequest) returns (SearchChatHistoryResponse) {
    option (google.api.http) = {get: "/v1/{agent=agents/*}/chat-history"};
}

// Get the full execution context for a specific command.
// Returns command metadata + all outputs + all events.
rpc GetCommandContext(GetCommandContextRequest) returns (GetCommandContextResponse) {
    option (google.api.http) = {get: "/v1/{name=agents/*/commands/*}/context"};
}
```

```protobuf
message SearchChatHistoryRequest {
  string agent = 1 [(google.api.field_behavior) = REQUIRED];
  string query = 2;                       // keyword search (matches command + final_summary)
  google.protobuf.Timestamp since = 3;    // optional: earliest message time
  google.protobuf.Timestamp until = 4;    // optional: latest message time
  string principal_id = 5;               // filter by user
  int32 limit = 6;                        // max results (default 10, max 50)
}

message SearchChatHistoryResponse {
  repeated ChatHistoryEntry entries = 1;
}

message ChatHistoryEntry {
  string command_id = 1;
  string instruction = 2;       // user message
  string final_summary = 3;     // agent reply
  google.protobuf.Timestamp created_at = 4;
}

message GetCommandContextRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}

message GetCommandContextResponse {
  Command command = 1;
  repeated CommandOutput outputs = 2;
  repeated CommandEvent events = 3;
}
```

---

## 5. Manager Backend Changes

### 5.1 Store: `store/command.go`

New methods:

```go
// GetRecentChatHistory returns the most recent N chat messages for a given
// agent+principal pair, ordered by created_at DESC (most recent first).
// Returns (instruction, final_summary, created_at) for each row.
func (s *Store) GetRecentChatHistory(ctx context.Context, agentID, principalID int, limit int) ([]*ChatHistoryEntry, error)

// SearchChatHistory searches chat messages by keyword across instruction
// and final_summary fields, optionally filtered by time range.
func (s *Store) SearchChatHistory(ctx context.Context, agentID, principalID int, query string, since, until *time.Time, limit int) ([]*ChatHistoryEntry, error)

// GetCommandContext returns the full details of a command including its
// outputs and events — the complete "thinking" context behind a reply.
func (s *Store) GetCommandContext(ctx context.Context, commandID uuid.UUID) (*CommandContext, error)
```

**SQL for search:**
```sql
SELECT c.id, c.instruction, c.final_summary, c.created_at
FROM command c
WHERE c.agent_id = $1
  AND c.principal_id = $2
  AND c.source_type = 1  -- CHAT only
  AND ($3 = '' OR c.instruction ILIKE '%' || $3 || '%'
               OR c.final_summary ILIKE '%' || $3 || '%')
  AND ($4::timestamptz IS NULL OR c.created_at >= $4)
  AND ($5::timestamptz IS NULL OR c.created_at <= $5)
ORDER BY c.created_at DESC
LIMIT $6
```

### 5.2 API: `api/v1/command.go`

**Modify `SendCommand`**: when `source == CHAT`, inject recent history into instruction before creating the command:

```go
if req.Msg.Source == v1pb.CommandSource_CHAT {
    history, err := s.store.GetRecentChatHistory(ctx, agent.ID, principalID, 20)
    if err != nil {
        slog.Warn("failed to load chat history", "error", err)
    }
    contextBlock := buildChatPromptContext(history)
    if contextBlock != "" {
        instruction = contextBlock + "\n---\n" + instruction
    }
}
```

Where `buildChatPromptContext` formats entries like:

```
## Recent conversation history with this user:
- User: {instruction}
- Assistant: {final_summary}
- User: {instruction}
- Assistant: {final_summary}
## Current message:
{instruction}
```

**Implement `SearchChatHistory`**:
- Validates agent exists
- Optional ACL check (only return messages for authenticated principal)
- Delegates to `store.SearchChatHistory`

**Implement `GetCommandContext`**:
- Loads command, outputs, events from store
- Returns as unified response

### 5.3 Command Message: `store/command.go`

Implement `CommandMessage` to include `SourceType int32` and `ConversationID` fields.

### 5.4 CommandResult: Pass Source Info to Agent

The `CommandRequest` message in the agent stream must carry the `source_type` so the agent knows whether to inject MCP servers. Add `source` field:

```protobuf
message CommandRequest {
  // ... existing fields 1-9 ...
  CommandSource source = 10;
}
```

---

## 6. Agent MCP Server

### 6.1 SDK: `github.com/modelcontextprotocol/go-sdk` v1.6.1

The official MCP Go SDK provides:
- `mcp.NewServer` — create an MCP server instance
- `mcp.AddTool[In, Out]` — register typed tools with JSON Schema from struct tags
- `mcp.NewStreamableHTTPHandler` — create a `net/http.Handler` for streamable HTTP transport
- Tool handlers receive `context.Context`, `*CallToolRequest`, and typed input

### 6.2 Implementation: `backend/agent/mcp/server.go`

```go
package mcp

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "log/slog"

    "github.com/modelcontextprotocol/go-sdk/mcp"
    v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
    "github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

type Server struct {
    server  *mcp.Server
    handler http.Handler
    port    int

    managerClient v1connect.CommandServiceClient
    agentResourceID string
}

func New(managerAddr string, agentResourceID string) (*Server, error) {
    client := v1connect.NewCommandServiceClient(
        // gRPC connection to manager, authenticated with agent token
    )

    srv := mcp.NewServer(
        &mcp.Implementation{Name: "laelia-chat", Version: "1.0.0"},
        &mcp.ServerOptions{},
    )

    ms := &Server{
        server:         srv,
        managerClient:  client,
        agentResourceID: agentResourceID,
    }

    // Register tools
    mcp.AddTool(srv,
        &mcp.Tool{
            Name:        "search_chat_history",
            Description: "Search past chat messages by keyword and optional time range. Returns matching user messages and agent replies.",
        },
        ms.handleSearchChatHistory,
    )
    mcp.AddTool(srv,
        &mcp.Tool{
            Name:        "get_command_context",
            Description: "Get the full execution context (thinking process, tool calls, outputs) behind a specific agent reply, by its command/message ID.",
        },
        ms.handleGetCommandContext,
    )

    // Create HTTP handler with stateless sessions on random port
    handler := mcp.NewStreamableHTTPHandler(
        func(r *http.Request) *mcp.Server { return srv },
        &mcp.StreamableHTTPOptions{
            Stateless:  true,  // stateless: LLaMA context is per ACP session
        },
    )

    // Wrap with middleware to capture query params (agent, principal)
    ms.handler = ms.contextMiddleware(handler)

    return ms, nil
}

// contextMiddleware parses agent/principal from URL query and stores in
// request context for tool handlers to access.
func (s *Server) contextMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()
        if aid := r.URL.Query().Get("agent"); aid != "" {
            ctx = context.WithValue(ctx, ctxKeyAgentID, aid)
        }
        if pid := r.URL.Query().Get("principal"); pid != "" {
            ctx = context.WithValue(ctx, ctxKeyPrincipalID, pid)
        }
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Start binds to 127.0.0.1:0 (random port) and begins serving.
func (s *Server) Start() error {
    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        return err
    }
    s.port = listener.Addr().(*net.TCPAddr).Port
    slog.Info("MCP server started", "port", s.port)

    go func() {
        if err := http.Serve(listener, s.handler); err != nil {
            slog.Error("MCP server stopped", "error", err)
        }
    }()
    return nil
}

func (s *Server) Port() int { return s.port }

func (s *Server) Stop() {
    // Graceful shutdown (implement with server.Shutdown)
}
```

### 6.3 Tool Input/Output Types

```go
type SearchChatHistoryInput struct {
    Query     string `json:"query" jsonschema_description:"keywords to search for"`
    Since     string `json:"since,omitempty" jsonschema_description:"ISO8601 timestamp; include messages after this time"`
    Limit     int    `json:"limit,omitempty" jsonschema_description:"max results, between 1–50 (default 10)"`
}

type SearchChatHistoryOutput struct {
    Results []ChatHistoryResult `json:"results" jsonschema_description:"matching chat messages"`
}

type ChatHistoryResult struct {
    MessageID    string `json:"message_id" jsonschema_description:"unique ID of this message"`
    Role         string `json:"role" jsonschema_description:"'user' or 'assistant'"`
    Content      string `json:"content" jsonschema_description:"message text"`
    Timestamp    string `json:"timestamp" jsonschema_description:"ISO8601 timestamp"`
}

type GetCommandContextInput struct {
    CommandID string `json:"command_id" jsonschema_description:"the command/message ID to fetch full context for"`
}

type GetCommandContextOutput struct {
    Instruction  string       `json:"instruction" jsonschema_description:"the original user message"`
    FinalSummary string       `json:"final_summary" jsonschema_description:"the agent's reply"`
    Events       []EventEntry `json:"events" jsonschema_description:"structured events during execution (tool calls, thinking, etc.)"`
}

type EventEntry struct {
    SeqNo    int32  `json:"seq_no" jsonschema_description:"sequence number"`
    Type     string `json:"type" jsonschema_description:"event type"`
    Summary  string `json:"summary" jsonschema_description:"event summary"`
    Payload  string `json:"payload" jsonschema_description:"JSON payload"`
}
```

### 6.4 Context Passing Strategy

The MCP server is shared across all chat sessions for a given agent. Per-session context (principal_id) is passed via URL query parameters on the `mcpServers[].url` field in the ACP session config:

```
http://127.0.0.1:{port}/mcp?agent=agents/abc123&principal=101
```

A middleware extracts these values into `context.Context` before the MCP handler processes the request. The MCP SDK's `Stateless: true` mode ensures each opencode connection creates a clean MCP session.

Tool handlers access context values from `ctx`:

```go
func (s *Server) handleSearchChatHistory(ctx context.Context, req *mcp.CallToolRequest, input SearchChatHistoryInput) (*mcp.CallToolResult, SearchChatHistoryOutput, error) {
    principalID := ctx.Value(ctxKeyPrincipalID).(string)
    // Call manager RPC with principalID filter
}
```

**Fallback**: If context values are not available (e.g., protocol version mismatch), the tool returns an error instructing the LLM to pass context parameters directly.

---

## 7. Agent Executor Integration

### 7.1 Modified Files

**`backend/agent/client/command_stream.go`**

- `commandStream` struct gets a `mcpPort int` field
- `runCommand` passes MCP context to `buildRuntime`
- On agent startup, MCP server is started and port recorded

**`backend/agent/executor/acp_executor.go`**

- `ACPExecutor` struct gets new fields: `sourceType int32`, `mcpPort int`, `agentResourceID string`, `principalID string`
- `NewACP` accepts additional parameters for MCP configuration
- In `run()`, when `sourceType == CHAT`:

```go
var mcpServers []acp.McpServer
if e.sourceType == 2 { // CHAT
    mcpServers = []acp.McpServer{{
        Http: &acp.McpServerHttpInline{
            Type: "http",
            Name: "laelia-chat",
            Url:  fmt.Sprintf("http://127.0.0.1:%d/mcp?agent=%s&principal=%s",
                    e.mcpPort, e.agentResourceID, e.principalID),
        },
    }}
}

sessionResp, err := e.conn.NewSession(e.ctx, acp.NewSessionRequest{
    Cwd:        e.workingDir,
    AdditionalDirectories: additionalRoots(e.allowedRoots, e.workingDir),
    McpServers: mcpServers,
})
```

**`backend/agent/executor/runtime.go`**

- `Request` struct extended with `SourceType int32`, `AgentResourceID string`, `PrincipalID string`

---

## 8. Frontend Changes

### 8.1 New Files

```
src/pages/dashboard/chat.tsx          # Chat page component
src/components/chat-message-list.tsx   # Message list (user + agent bubbles)
src/components/chat-input.tsx          # Message input with send button
```

### 8.2 Router Changes

`src/router/routes/dashboard.tsx`:
```tsx
{
  path: "agents/:agentId/chat",
  handle: { name: "chat" },
  Component: lazy(() => import("@/pages/dashboard/chat")),
}
```

### 8.3 Store Changes

`src/stores/command.ts`:
- `sendCommand` extended to accept `source?: CommandSource`
- New state: `chatHistory: Map<string, ChatEntry[]>` keyed by agent

### 8.4 Chat Page Behavior

```
┌──────────────────────────────────────────────────────┐
│ Agent Name / Chat                      [Command List] │
├──────────────────────────────────────────────────────┤
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:30  User: What's the status of server X? │    │
│  └──────────────────────────────────────────────┘    │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:31  Agent: Server X is running, CPU 45%, │    │
│  │         memory 60%. 3 active connections.   │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:32  User: Can you check the logs?        │    │
│  └──────────────────────────────────────────────┘    │
│  ┌──────────────────────────────────────────────┐    │
│  │ 12:34  Agent: I found 2 errors in the last  │    │
│  │         hour: ... [View Details →]           │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
├──────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────┐  [Send]      │
│  │ Type a message...                  │  (disabled   │
│  └────────────────────────────────────┘   when busy) │
│  Agent is processing... (message queued)              │
└──────────────────────────────────────────────────────┘
```

Key UX behaviors:
- **Send button disable**: Locked while a command is in-flight (state != COMPLETED/FAILED/CANCELLED)
- **Queue indicator**: When agent is busy and user sends, show "Your message is queued"
- **Auto-scroll**: New messages scroll into view
- **History loading**: Scroll up to load older messages (cursor-based pagination from ListCommands)
- **View Details**: Each agent message links to `/agents/:agentId/commands/:cmdId` for full execution context
- **Real-time**: Subscribe to `WatchCommand` stream for live final_summary

---

## 9. Implementation Plan

### Phase 1 — Data Model & Proto (1-2 days)

| Step | Description |
|---|---|
| 1.1 | Add `source_type` column to `command` table (migration SQL) |
| 1.2 | Create `conversation` + `conversation_member` tables (migration SQL) |
| 1.3 | Add `CommandSource` enum to `command.proto` |
| 1.4 | Add `source` field to `Command`, `SendCommandRequest`, `CommandRequest` |
| 1.5 | Define `SearchChatHistoryRequest/Response`, `GetCommandContextRequest/Response` |
| 1.6 | Run `buf generate` in `proto/` |

### Phase 2 — Manager Backend (2-3 days)

| Step | Description |
|---|---|
| 2.1 | `store/command.go`: Add `GetRecentChatHistory`, `SearchChatHistory`, `GetCommandContext` |
| 2.2 | `store/command.go`: Update `CreateCommand` to accept `source_type` |
| 2.3 | `api/v1/command.go`: Implement `SearchChatHistory` RPC |
| 2.4 | `api/v1/command.go`: Implement `GetCommandContext` RPC |
| 2.5 | `api/v1/command.go`: Modify `SendCommand` to inject chat history for CHAT source |
| 2.6 | `component/dispatcher/`: Pass `source` through `CommandRequest` to agent |

### Phase 3 — Agent MCP Server + Executor (2-3 days)

| Step | Description |
|---|---|
| 3.1 | Add `github.com/modelcontextprotocol/go-sdk` to `go.mod` (`go get`) |
| 3.2 | Create `backend/agent/mcp/server.go` — MCP Server with two tools |
| 3.3 | Wire MCP Server start/stop in agent entrypoint (`cmd/run.go`) |
| 3.4 | `executor/runtime.go`: Extend `Request` with SourceType, AgentResourceID, PrincipalID |
| 3.5 | `executor/acp_executor.go`: Construct `McpServers` when source_type == CHAT |
| 3.6 | `client/command_stream.go`: Pass MCP port + source info to executor |

### Phase 4 — Frontend (2-3 days)

| Step | Description |
|---|---|
| 4.1 | Create chat page component (`pages/dashboard/chat.tsx`) |
| 4.2 | Create message list and input components |
| 4.3 | Add route `/agents/:agentId/chat` |
| 4.4 | Extend store with chat-specific methods (send chat, load history) |
| 4.5 | Implement send button lock + queue indicator |
| 4.6 | Wire up real-time streaming via `WatchCommand` |

### Phase 5 — Testing & Polish (1-2 days)

| Step | Description |
|---|---|
| 5.1 | Unit tests for new store methods |
| 5.2 | Integration test: send chat message → agent processes → MCP tool can query |
| 5.3 | Frontend manual test: chat UI, history scrolling, queue behavior |
| 5.4 | Go lint + format + test suite pass |

---

## 10. Open Questions & Risks

| # | Question | Status |
|---|---|---|
| 1 | **Context flow through MCP SDK**: Does `ctx` in tool handlers carry the HTTP request context (with our middleware values)? If not, fall back to a `sync.Map` keyed by session ID, populated during `getServer` callback. | **To verify during Phase 3** |
| 2 | **MCP stateless mode**: `StreamableHTTPOptions.Stateless: true` means no session state is retained between requests. This simplifies our implementation but may limit future features (e.g., multi-turn within one ACP session). Acceptable for v1. | Accepted |
| 3 | **ACP session reuse**: Each chat message creates a new ACP session. The LLM's internal state is reset each time. Long-term, session reuse (ACP `session/load`) would improve efficiency. | v2 consideration |
| 4 | **Large command_event payloads**: `get_command_context` returning all events for a long-running command could produce large payloads. Consider truncation or summarizing. | Mitigation: serializing only TEXT_DELTA and TOOL_CALL events, skipping RAW_ACP batches |
| 5 | **Port exhaustion**: Random ports, agent lifecycle binding. One port per agent process — negligible risk for hundreds of agents. | Non-issue for typical deployments |
| 6 | **open `mcpCapabilities.http`**: The ACP binary (opencode) must support `mcpCapabilities.http` transport. Per the ACP spec, new agents SHOULD support HTTP transport. The current `@zed-industries/claude-code-acp` supports it. | Required — verify with current opencode version |
