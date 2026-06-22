# Laelia 架构重构：从 Command-Driven 到 Message-Driven

## 背景：Raft 博客《Is Having Agents in the Room Meant to Be Chaotic?》的核心论点

Raft 团队在他们的博客中提出了 **AX（Agent Experience Design）** 的设计理念。关键论点如下：

### 核心问题：回合制 vs 持续感知

> Humans coordinate gracefully in shared spaces because we have **continuous perception**. We sense the rhythm of a conversation without consciously reading every message; we feel the pause before stepping in. None of that has to be designed. It is what being continuously present means.
>
> Agents don't inhabit the room the way humans do. Their interaction is **turn-based**: each invocation, the agent reads a snapshot of the room, reasons, commits an action, and then waits for the next invocation.

传统协作工具（Slack、Discord 等）为人类的"持续感知"设计，但 Agent 是"回合制"的——每次调用读取快照、推理、提交，然后等待。快照与提交之间的"间隙"导致了 Agent 协作中的混乱（重复响应、过期回复等）。

### 关键设计原则

1. **Agent Inbox（收件箱）**
   > The agent decides what is worth its context, instead of the room deciding for it.

   不应该把所有消息推给 Agent，而应由 Agent 按需拉取。Agent 自己决定什么值得占用它的上下文窗口。

2. **Held Draft（暂存草稿）**
   > The room informs the agent that something arrived; the agent decides what to do with that information.

   Agent 回复带上"房间版本标记"。如果提交时房间已变化，草稿被暂存并告知 Agent 发生了什么变化。Agent 有四种选择：
   - **REVISE**：基于新上下文重新决策
   - **SEND_AS_IS**：坚持原回复
   - **DISCARD**：放弃（沉默是有效行为）
   - **FORCE_SEND**：绕过检查强制提交

3. **Perception Empathy（感知同理心）**
   > Sit where the agent sits and look around the room. What does it actually see at the moment it acts?

   站在 Agent 的视角看——它实际看到什么？缺少什么人类能自动感知的信息？

4. **Action Explicitness（行动显式化）**
   > Agents need those internal options made external. Action explicitness means surfacing the option-space, not assuming the agent will derive it.

   Agent 需要把人类内化的决策选项显式地呈现出来，不应假设 Agent 会自行推导。

### Raft 的核心洞见

> An agent that can only respond when @mentioned can no longer notice something problematic in a thread, can no longer decide whether to defer or give way. **Rules-based filtering doesn't reduce noise; it turns the agent back into a tool waiting to be invoked.**

Agent 应该是"房间里的人"，而不是"等待被调用的工具"。

---

## 当前架构问题诊断

### 现状：Command-Driven 模型

```
User → SendCommand(instruction="帮我检查服务器")
     → 创建 command 记录 (这是"任务")
     → 同时可选创建 chat_message (这是"对话")
     → 放入 agent_inbox (这是"任务队列")
     → Agent 被通知
     → Agent 执行
     → 结果变成 assistant chat_message
```

**问题本质**：`command` 是主，`message` 是副。实际上是"发送命令附带聊天记录"。

### 具体问题

1. **`SendCommand` 是核心 API 入口**
   - 用户显式"发号施令"，Agent 被动执行
   - Agent 没有选择性——Dispathcer 的 `TryDispatchNext` 按 FIFO 自动推送任务

2. **Agent 被当成工具调用**
   - `ExcutorKind` 区分 SHELL 和 ACP，暴露了执行机制给用户
   - `CommandSource` 区分 MANUAL 和 CHAT，但本质都是"发送命令"
   - Shell Executor 本质上就是一个远程 bash 执行器，与 AX 理念冲突

3. **缺少房间状态概念**
   - 没有 `room_version` —— Agent 无法知道其决策是否基于过期状态
   - 没有 Held Draft 机制 —— Agent 的回复可能在推理期间已过时
   - 对话上下文是静态注入的（3-5 轮历史），Agent 不能主动拉取

4. **Agent 的选项空间被压缩**
   - Agent 只能"执行命令并返回结果"——不能沉默、不能追问、不能拒绝
   - 权限模型是二元的（auto-approve 或 ask），没有层次感

---

## 新架构：Message-Driven 模型

### 核心思想

```
User → SendMessage("帮我检查服务器")
     → 创建 chat_message (唯一入口)
     → Agent 拉取消息
     → Agent 自己判断：回复？执行工具？追问？沉默？
     → 如果需要执行工具 → 内部创建 command（不可见）
     → 结果 → 创建 assistant chat_message
```

**本质变化**：`message` 是主，`command` 退化为内部实现细节。

### 新数据流

```
┌─ 1. 用户发送消息 ──────────────────────────────────────────┐
│  SendMessage(conversation, "帮我检查服务器")                  │
│  → chat_message 创建, room_version++                          │
│  → push NewMessagesAvailable 通知给该房间的连接中 Agent        │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 2. Agent 拉取消息 ─────────────────────────────────────────┐
│  Agent ← NewMessagesAvailable({conversation_id})              │
│  Agent → PullMessages({conv_id: last_version})                │
│  Agent ← MessageSnapshot(messages)                            │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 3. Agent 决定行动 + Held Draft 校验 ────────────────────────┐
│  Agent → SubmitAction(conv_id, reply_to_msg_id,               │
│                       base_version=N)                         │
│                                                               │
│  Manager 比较 base_version vs current_version:                │
│    ✓ 一致 → ActionResponse(committed=true)                    │
│    ✗ 不一致 → ActionResponse(held=true, new_messages=[...])   │
│                                                               │
│  若 held: Agent 有四种选择:                                    │
│    ResolveHeldAction(REVISE) - 放弃原稿, 重新决策              │
│    ResolveHeldAction(SEND_AS_IS) - 坚持原回复                  │
│    ResolveHeldAction(DISCARD) - 沉默是有效行为                  │
│    ResolveHeldAction(FORCE_SEND) - 我确定要发, 绕过版本检查    │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 4. Agent 本地执行 ACP ─────────────────────────────────────┐
│  Agent 启动 opencode ACP session                             │
│  用已拉取的消息构建对话上下文                                   │
│  opencode 决定工具调用、生成回复                                │
│  → 通过 bidi stream 报告 execution events/progress            │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 5. Agent 报告结果 ─────────────────────────────────────────┐
│  Agent → ExecutionResult(action_id, final_summary)            │
│  Manager → 创建 assistant chat_message                        │
│  Manager → room_version++                                     │
│  Agent → PullMessages (检查是否有新消息)                       │
└─────────────────────────────────────────────────────────────┘
```

### 架构对比

```
                    旧架构 (Command-Driven)

  ┌──────────────────────────────────────────────────┐
  │                                                  │
  │  User ──SendCommand──→ CommandService            │
  │                         │                        │
  │                    ┌────┴────┐                    │
  │                    │ command │ (主)               │
  │                    │ message │ (副, 可选)          │
  │                    └────┬────┘                    │
  │                         ↓                        │
  │              Dispatcher.push()                   │
  │                         ↓                        │
  │                  Agent 执行                       │
  │                                                  │
  └──────────────────────────────────────────────────┘

                    新架构 (Message-Driven)

  ┌──────────────────────────────────────────────────┐
  │                                                  │
  │  User ──SendMessage──→ Conversation              │
  │                         │                        │
  │                      message (主, 唯一)           │
  │                         │                        │
  │                         ↓                        │
  │               Agent 拉取消息                      │
  │                         │                        │
  │                    Agent 判断                     │
  │                  /    |     \                    │
  │              回复   执行工具   沉默                │
  │                │      │                          │
  │                │  内部创建 command                │
  │                │  (实现细节, 不可见)              │
  │                ↓      ↓                          │
  │         assistant message  ←── 结果              │
  │                                                  │
  │              + Held Draft 版本校验                │
  │              + Action Explicitness               │
  │              + Agent 自主决策                     │
  └──────────────────────────────────────────────────┘
```

---

## Proto 设计

### 消息服务 (MessageService)

保留现有的消息/频道/会话管理 RPC，`SendMessage` 成为主要入口。

### Agent 流服务 (AgentStreamService)

bidi stream 从命令通道演化为通用的 Agent 通信通道。

```protobuf
service AgentStreamService {
  rpc AgentChannel(stream AgentStreamMessage) returns (stream ManagerStreamMessage);
}

message AgentStreamMessage {
  oneof message {
    AgentReady agent_ready = 1;
    PullMessages pull_messages = 2;
    SubmitAction submit_action = 3;
    ResolveHeldAction resolve_held_action = 4;
    CommandProgress progress = 5;
    CommandResult result = 6;
    CommandEvent event = 7;
    Ping ping = 8;
  }
}

message ManagerStreamMessage {
  oneof message {
    MessageSnapshot message_snapshot = 1;
    ActionResponse action_response = 2;
    CommandRequest command_request = 3;        // 内部执行派发
    NewMessagesAvailable new_messages = 4;     // 新消息推送通知
    CancelMessage cancel = 5;
    Pong pong = 6;
    PermissionDecision permission_decision = 7;
  }
}
```

### 新增消息类型

```protobuf
// Agent 拉取房间未读消息
message PullMessages {
  map<string, int64> last_versions = 1;  // conversation_id -> 最后看到的版本
}

message MessageSnapshot {
  repeated RoomMessage messages = 1;
}

message RoomMessage {
  string message_id = 1;
  string conversation_id = 2;
  string sender_name = 3;
  int32 sender_type = 4;      // 1=USER, 2=AGENT
  int32 role = 5;             // 1=USER, 2=ASSISTANT
  string content = 6;
  int64 room_version = 7;     // 消息诞生时的房间版本
  google.protobuf.Timestamp created_at = 8;
}

// Agent 提交行动
message SubmitAction {
  string conversation_id = 1;
  string reply_to_message_id = 2;  // 触发此行动的消息
  int64 base_version = 3;          // Agent 决策时的房间版本
}

// Manager 对行动的校验结果（Held Draft 检查）
message ActionResponse {
  string action_id = 1;
  bool committed = 2;               // true: 提交成功, false: 暂存
  int64 current_version = 3;        // 当前房间版本
  repeated RoomMessage new_messages = 4;  // 若 held, 变化了什么
}

// Agent 决议暂存的草稿
message ResolveHeldAction {
  string action_id = 1;
  ActionResolution resolution = 2;
}

enum ActionResolution {
  ACTION_RESOLUTION_UNSPECIFIED = 0;
  REVISE = 1;       // 放弃原稿，重新决策
  SEND_AS_IS = 2;   // 坚持原回复
  DISCARD = 3;      // 沉默
  FORCE_SEND = 4;   // 绕过检查强制提交
}

// Manager 推送新消息通知给连接中的 Agent
message NewMessagesAvailable {
  repeated string conversation_ids = 1;
}
```

### 重命名对照

| 旧名称 | 新名称 | 原因 |
|--------|--------|------|
| `AgentCommandMessage` | `AgentStreamMessage` | 流不再只承载命令 |
| `ManagerCommandMessage` | `ManagerStreamMessage` | 同上 |
| `AgentCommandService` | `AgentStreamService` | 同上 |
| `CommandChannel` | `AgentChannel` | 同上 |

### 删除的 Proto 元素

| 元素 | 原因 |
|------|------|
| `ExecutorKind` enum (SHELL, ACP) | 不再需要区分执行器类型 |
| `CommandSource` enum (MANUAL, CHAT) | 一切源于消息 |
| `SendCommand` RPC | 消息成为唯一入口 |
| `SendCommandRequest` | 随 RPC 删除 |
| `PullInbox` / `SelectInboxItem` / `DeferInboxItem` | Phase 1 的 inbox 模型被消息拉取取代 |
| `InboxSnapshot` / `InboxItemSelected` / `InboxItem` | 同上 |

---

## 数据库变更

```sql
-- === 新增：房间版本控制（Held Draft 机制核心） ===
ALTER TABLE conversation ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

-- 每条消息记录其诞生时的房间版本
ALTER TABLE chat_message ADD COLUMN room_version BIGINT NOT NULL DEFAULT 0;

-- === 新增：暂存草稿 ===
-- state: 1=HELD, 2=RESOLVED, 3=EXPIRED
CREATE TABLE held_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id),
    conversation_id UUID NOT NULL REFERENCES conversation(id),
    action_json JSONB NOT NULL,          -- 原始 SubmitAction proto
    base_version BIGINT NOT NULL,        -- Agent 决策时的房间版本
    current_version BIGINT NOT NULL,     -- 暂存时的房间版本
    state SMALLINT NOT NULL DEFAULT 1,
    resolution SMALLINT,                 -- 最终决议
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

-- === 删除：Phase 1 inbox 表 ===
DROP TABLE IF EXISTS agent_inbox CASCADE;
DROP TABLE IF EXISTS agent_working_state CASCADE;

-- === 简化：command 表 ===
-- command 退化为纯内部执行追踪，不再暴露执行器类型和来源
ALTER TABLE command DROP COLUMN IF EXISTS executor_kind;
ALTER TABLE command DROP COLUMN IF EXISTS source_type;
-- conversation 关联通过 chat_message.command_id 维护即可
-- 保留核心字段: id, agent_id, principal_id, instruction, status, exit_code, 
--              duration_ms, result_json, final_summary, 时间戳等
```

---

## 后端代码变更

### 删除的文件

| 文件 | 原因 |
|------|------|
| `backend/agent/executor/executor.go` | BashExecutor 完全废弃，只保留 ACP |
| `backend/manager/store/inbox.go` | Phase 1 inbox 模型被消息拉取取代 |

### 新建的文件

| 文件 | 职责 |
|------|------|
| `backend/manager/store/held_action.go` | held_action 表的 CRUD 操作 |

### 重度改写的文件

| 文件 | 变更说明 |
|------|----------|
| `backend/manager/component/dispatcher/dispatcher.go` | 删除所有 inbox 方法 → 新增 held-draft 校验、`HandleSubmitAction`、`HandleResolveHeldAction`；`HandleResult` 改为创建 assistant message |
| `backend/agent/client/command_stream.go` | `PullMessages` 流程 → `SubmitAction` 流程 → Held Draft 处理；删除 shell executor 分支 |
| `proto/v1/v1/command.proto` | 按上文 Proto 设计部分全面改写 |

### 中度改写的文件

| 文件 | 变更说明 |
|------|----------|
| `backend/manager/api/v1/command.go` | 删除 `SendCommand` handler；`SendMessage` handler 增加 room_version 递增；`buildInboxSummary` 删除 shell 分支 |
| `backend/manager/api/v1/agent_command.go` | `CommandChannel` 改为 `AgentChannel`；处理新的消息类型 |
| `backend/manager/store/command.go` | `CommandMessage` 删除 `ExecutorKind`、`SourceType`、`ConversationID` 字段 |
| `backend/manager/migration/latest.sql` | 按上表新增表和列 |
| `backend/manager/server/grpc_routes.go` | 更新服务名绑定 |

### 轻微改写的文件

| 文件 | 变更说明 |
|------|----------|
| `backend/agent/executor/runtime.go` | `Request` 结构体删除不必要字段 |
| `backend/agent/executor/acp_executor.go` | 适配新的 Request 结构 |

---

## 前端变更

### Store 变更

| 文件 | 变更 |
|------|------|
| `stores/chat.ts` | `sendChatMessage()` 改为纯 `SendMessage`，不再调用 `SendCommand`；`streamChatCommand()` 适配新的事件流 |
| `stores/command.ts` | 删除 `sendCommand()`；保留 `listCommands()`、`getCommand()`、`watchCommand()` 用于执行监控 |
| `stores/types.ts` | 删除 `sendCommand`、`executorKind` 等类型定义 |

### 组件和页面变更

| 文件 | 变更 |
|------|------|
| `pages/dashboard/command-list.tsx` | 删除 `handleSend()` 中的 SendCommand 调用和 ExecutorKind 引用 |
| `pages/dashboard/command-detail.tsx` | 删除 `isACP` 条件分支（全部是 ACP） |
| `lib/command-status.ts` | 删除 `executorKindToI18nKey` 中的 SHELL 条目 |

### 文案变更

| 文件 | 删除内容 |
|------|----------|
| `locales/en-US.json` | `"executor-shell": "Shell"` |
| `locales/zh-CN.json` | `"executor-shell": "Shell"` |

### Proto 生成物变更

```
frontend/src/types/proto-es/v1/command_pb.d.ts
frontend/src/types/proto-es/v1/command_pb.js
```
自动重新生成，随 proto 变更更新。

---

## 实施步骤

### Step 1: Proto 全面改写
- 新增消息类型（PullMessages、MessageSnapshot、RoomMessage、SubmitAction、ActionResponse、ResolveHeldAction、NewMessagesAvailable）
- 重命名服务和消息（AgentStreamService、AgentStreamMessage、ManagerStreamMessage）
- 删除废弃元素（ExecutorKind、CommandSource、SendCommand、Phase 1 inbox）
- 运行 `buf format -w proto && buf lint proto && cd proto && buf generate`

### Step 2: 数据库迁移
- 新增 `conversation.version`、`chat_message.room_version` 列
- 新增 `held_action` 表
- 删除 `agent_inbox`、`agent_working_state` 表
- 删除 `command.executor_kind`、`command.source_type` 列

### Step 3: Store 层变更
- 删除 `inbox.go`
- 新建 `held_action.go`
- 更新 `command.go`（删除废弃字段）
- 更新 `command.go` 中 `CreateCommand` 等方法签名

### Step 4: Dispatcher 重写
- 删除所有 inbox 方法和消息类型处理
- 新增 `HandleSubmitAction`（Held Draft 校验 + 执行派发）
- 新增 `HandleResolveHeldAction`（草稿决议）
- 修改 `HandleResult`（自动创建 assistant message + 递增 room_version）

### Step 5: API Handler 更新
- `command.go`：删除 SendCommand、更新 SendMessage
- `agent_command.go`：处理 PullMessages/SubmitAction/ResolveHeldAction
- `grpc_routes.go`：更新服务绑定

### Step 6: Agent Client 重写
- 删除 shell executor 分支
- PullMessages 流程实现
- SubmitAction 流程实现
- Held Draft 处理

### Step 7: 前端适配
- 删除 SendCommand 调用链
- 更新 chat message 发送流程
- 删除 ExecutorKind 引用

### Step 8: 构建验证
- `go build ./...` 全量编译
- `go vet ./...` 静态分析
- `golangci-lint run --allow-parallel-runners` 代码规范
- `go test ./...` 单元测试
- `pnpm --dir frontend type-check` 前端类型检查

---

## 对 AX 四问的回应

本次重构直接回应了 Raft 团队提出的 AX 四大问题：

| AX 问题 | 我们的回应 |
|---------|-----------|
| What does the agent see? | `MessageSnapshot` 提供结构化的房间状态，包含版本标记和发送者上下文 |
| What state does it carry? | `PullMessages(last_versions)` 让 Agent 能携带上次看到的版本，只拉增量 |
| What can it recover from? | Held Draft 四种决议路径（Revise/SendAsIs/Discard/ForceSend）覆盖全部恢复场景 |
| What is it allowed to decide? | `SubmitAction` 的选项空间显式化；Agent 自主决定回复/执行/沉默/追问 |
