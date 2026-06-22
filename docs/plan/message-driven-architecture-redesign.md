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

### 适用场景说明

本设计面向**多参与方频道（multi-party channel）**场景：一个频道内可能有多个用户、多个 Agent，消息并发到达。在此场景下，Held Draft、自主沉默、主动让序等行为是避免混乱的关键。

对于**1:1 直接对话（direct conversation）**场景，用户发送后通常等待回复再发下一条，并发概率低。Held Draft 在 1:1 场景中更多是"保险机制"而非高频路径。因此本设计分阶段实施：先建立消息驱动骨架（Phase 1），再在多参与方场景中引入 Held Draft（Phase 2）。

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
   - Agent 没有选择性——Dispatcher 的 `HandleSelectInboxItem` 按 FIFO 自动推送任务

2. **Agent 被当成工具调用**
   - `ExecutorKind` 区分 SHELL 和 ACP，暴露了执行机制给用户 API
   - `CommandSource` 区分 MANUAL 和 CHAT，但本质都是"发送命令"
   - Shell Executor 本质上就是一个远程 bash 执行器，与 AX 理念冲突

3. **缺少房间状态概念**
   - 没有 `room_version` —— Agent 无法知道其决策是否基于过期状态
   - 没有 Held Draft 机制 —— Agent 的回复可能在推理期间已过时
   - 对话上下文是静态注入的（最近 6 条历史），Agent 不能主动拉取

4. **Agent 的选项空间被压缩**
   - Agent 只能"执行命令并返回结果"——不能沉默、不能追问、不能拒绝
   - 权限模型是二元的（auto-approve 或 ask），没有层次感

5. **非对话类触发路径被强行套用对话模型**
   - CI/CD、定时任务、webhook 等程序化触发目前只能通过 `CommandSource=MANUAL` 的 `SendCommand` 进入，没有对应的"对话"语义

---

## 新架构：Message-Driven 模型

### 核心思想

```
User → SendMessage("帮我检查服务器")
     → 创建 chat_message (唯一用户入口)
     → room_version++
     → 通知连接中的 Agent
     → Agent 拉取消息
     → Agent 自主判断：回复？执行工具？追问？沉默？
     → 若需执行 → Agent 发起 SubmitAction（新的执行触发）
     → Manager 校验版本 → 内部创建 command（不可见）
     → Agent 本地执行 ACP
     → 结果 → 创建 assistant chat_message → room_version++
```

**本质变化**：`message` 是主，`command` 退化为内部实现细节。`SubmitAction` 取代 `SendCommand` 成为执行触发点，但触发者从"用户"变为"Agent"。

### SubmitAction 的角色澄清

`SubmitAction` **就是**新的执行触发器，等价于旧架构中 `SendCommand` + inbox 派发的合并。区别在于：

| 维度 | 旧 `SendCommand` | 新 `SubmitAction` |
|------|------------------|-------------------|
| 触发者 | 用户（API 调用方） | Agent（拉取消息后自主发起） |
| 版本校验 | 无 | 携带 `base_version`，Manager 做 Held Draft 校验 |
| 执行器选择 | 用户指定 `ExecutorKind` | 固定 ACP，对用户不可见 |
| 上下文注入 | Manager 静态注入最近 6 条 | Agent 主动 `PullMessages` 拉取所需范围 |

提交成功（`committed=true`）后，Manager 内部创建 `command` 记录并通过 bidi stream 下发 `CommandRequest` 给 Agent，后续执行流程（Progress/Event/Result）保持不变。

### 新数据流

```
┌─ 1. 用户发送消息 ──────────────────────────────────────────┐
│  SendMessage(conversation, "帮我检查服务器")                  │
│  → chat_message 创建, conversation.version++                  │
│  → push NewMessagesAvailable 通知给该房间的连接中 Agent        │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 2. Agent 拉取消息 ─────────────────────────────────────────┐
│  Agent ← NewMessagesAvailable({conversation_id})              │
│  Agent → PullMessages(conversation_id, after_version)         │
│  Agent ← MessageSnapshot(messages)                            │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 3. Agent 决定行动 + Held Draft 校验 ────────────────────────┐
│  Agent → SubmitAction(conversation_id, reply_to_message_id,  │
│                       base_version=N)                        │
│                                                               │
│  Manager 比较 base_version vs conversation.version:           │
│    ✓ 一致 → ActionResponse(committed=true, action_id,         │
│                            command_id)                        │
│    ✗ 不一致 → ActionResponse(held=true, action_id,            │
│                              new_messages=[...])              │
│                                                               │
│  若 held: Agent 有四种选择:                                    │
│    ResolveHeldAction(REVISE) - 放弃原稿, 重新决策              │
│    ResolveHeldAction(SEND_AS_IS) - 坚持原回复                  │
│    ResolveHeldAction(DISCARD) - 沉默是有效行为                  │
│    ResolveHeldAction(FORCE_SEND) - 我确定要发, 绕过版本检查    │
│                                                               │
│  committed=true 后:                                           │
│    Manager 内部创建 command 记录                               │
│    Manager → CommandRequest 下发到 Agent bidi stream          │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 4. Agent 本地执行 ACP ─────────────────────────────────────┐
│  Agent 启动 opencode ACP session                             │
│  用已拉取的消息构建对话上下文                                   │
│  opencode 决定工具调用、生成回复                                │
│  → 通过 bidi stream 报告 CommandProgress/CommandEvent        │
│  → 若有权限请求 → PERMISSION_REQUESTED event                  │
│    → 用户 RespondPermission API → Manager bidi → Agent       │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 5. Agent 报告结果 ─────────────────────────────────────────┐
│  Agent → CommandResult(command_id, final_summary)            │
│  Manager → 创建 assistant chat_message (link command_id)     │
│  Manager → conversation.version++                             │
│  Manager → 关闭 command 的 output/event watchers            │
│  Agent → PullMessages (检查是否有新消息积压)                  │
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
  │                │  SubmitAction (held draft 校验)  │
  │                │  → 内部创建 command               │
  │                │  (实现细节, 不可见)              │
  │                ↓      ↓                          │
  │         assistant message  ←── 结果              │
  │                                                  │
  │              + Held Draft 版本校验 (Phase 2)     │
  │              + Action Explicitness               │
  │              + Agent 自主决策                     │
  └──────────────────────────────────────────────────┘
```

---

## 非对话类触发路径

删除 `SendCommand` 后，程序化触发（CI/CD、定时任务、webhook）通过 **系统消息** 进入对话：

- 在 `chat_message` 表中，`sender_type` 引入 `SYSTEM=3`（见下文 proto 枚举）
- 系统消息与用户消息走同一流程：`SendMessage` 创建 → `room_version++` → 通知 Agent
- 对于无明确对话上下文的程序化触发，Manager 可通过 `GetOrCreateSystemConversation(agent, principal)` 创建一个 `type=2`（系统）会话
- Agent 拉取后可识别 `sender_type=SYSTEM`，按系统指令处理（例如执行服务器检查并直接回写 summary）

这样"一切源于消息"不是口号：用户消息、系统消息、Agent 回复都是 `chat_message`，差异仅在 `sender_type`。

---

## Proto 设计

### 消息服务 (CommandService)

保留现有的消息/频道/会话管理 RPC。`SendCommand` RPC 在 Phase 1 中**标记弃用**（保留实现但不再被前端调用），Phase 2 中删除。`SendMessage` 成为主要入口，新增 `room_version` 递增逻辑。

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
    ResolveHeldAction resolve_held_action = 4;   // Phase 2
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
    CommandRequest command_request = 3;        // SubmitAction 提交后内部下发
    NewMessagesAvailable new_messages = 4;     // 新消息推送通知
    CancelMessage cancel = 5;
    Pong pong = 6;
    PermissionDecision permission_decision = 7;
  }
}
```

### 新增消息类型

```protobuf
// 消息发送者类型（复用于 chat_message.sender_type 与 RoomMessage）
enum SenderType {
  SENDER_TYPE_UNSPECIFIED = 0;
  USER = 1;
  AGENT = 2;
  SYSTEM = 3;   // 程序化触发（CI/CD、定时任务、webhook）
}

// Agent 拉取房间未读消息。
// 采用单会话拉取（而非 map），因为 Agent 通常一次只关注一个会话。
// 服务端通过 session 维护游标也可，但显式 after_version 让 Agent
// 在重连/崩溃恢复时能自描述其进度。
message PullMessages {
  string conversation_id = 1;
  int64 after_version = 2;   // 返回 room_version > after_version 的消息
}

message MessageSnapshot {
  repeated ChatMessage messages = 1;   // 复用现有 ChatMessage，新增 room_version 字段
  int64 current_version = 2;            // 当前房间版本（供 Agent 记录为下次 base_version）
}

// ChatMessage 新增字段（不新建 RoomMessage，避免重复类型）:
//   int64 room_version = 10;
//   SenderType sender_type = 11;
// 现有 sender_name/role/content/created_at/command_id 保持不变。

// Agent 提交行动（新的执行触发器）
message SubmitAction {
  string conversation_id = 1;
  string reply_to_message_id = 2;  // 触发此行动的消息
  int64 base_version = 3;          // Agent 决策时的房间版本
  string instruction = 4;          // Agent 提取后的执行指令（可选，默认用 reply_to_message 内容）
  string profile = 5;              // 执行 profile
  map<string, string> env = 6;
  string working_dir = 7;
  int32 timeout_seconds = 8;
  bool allow_diff = 9;
}

// Manager 对行动的校验结果（Held Draft 检查）
message ActionResponse {
  string action_id = 1;
  bool committed = 2;               // true: 提交成功, command 已创建
  string command_id = 3;            // committed=true 时返回新建 command 的 ID
  int64 current_version = 4;        // 当前房间版本
  repeated ChatMessage new_messages = 5;  // 若 held, 判定后到达的新消息
}

// Agent 决议暂存的草稿（Phase 2）
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
  repeated int64 versions = 2;       // 每个 conversation 的当前版本号
}
```

### CommandRequest 字段简化

`CommandRequest` 不再暴露 `executor_kind` 和 `source`，因为执行器固定为 ACP、来源固定为消息驱动。保留 Agent 执行所需的核心字段：

```protobuf
message CommandRequest {
  string command_id = 1;
  string instruction = 2;          // 合并原 command + instruction（ACP 模式下 instruction 即指令）
  string profile = 3;
  map<string, string> env = 4;
  string working_dir = 5;
  int32 timeout_seconds = 6;
  bool allow_diff = 7;
  string principal_id = 8;
  string conversation_id = 9;      // 新增：Agent 回写 assistant message 时需要
  string reply_to_message_id = 10;  // 新增：关联触发消息
}
```

### 重命名对照

| 旧名称 | 新名称 | 原因 |
|--------|--------|------|
| `AgentCommandMessage` | `AgentStreamMessage` | 流不再只承载命令 |
| `ManagerCommandMessage` | `ManagerStreamMessage` | 同上 |
| `AgentCommandService` | `AgentStreamService` | 同上 |
| `CommandChannel` | `AgentChannel` | 同上 |

### 删除/弃用的 Proto 元素

| 元素 | 处理 | 原因 |
|------|------|------|
| `ExecutorKind` enum | Phase 1 弃用，Phase 2 删除 | 不再需要区分执行器类型 |
| `CommandSource` enum | Phase 1 弃用，Phase 2 删除 | 一切源于消息（含 SYSTEM sender_type） |
| `SendCommand` RPC | Phase 1 弃用（保留兼容），Phase 2 删除 | `SendMessage` 成为唯一入口 |
| `SendCommandRequest` | 随 `SendCommand` 删除 | 同上 |
| `PullInbox` / `SelectInboxItem` / `DeferInboxItem` | Phase 1 删除 | inbox 模型被消息拉取取代 |
| `InboxSnapshot` / `InboxItemSelected` / `InboxItem` | Phase 1 删除 | 同上 |

---

## 数据库变更

### Phase 1（建立消息驱动骨架）

```sql
-- === 新增：房间版本控制 ===
ALTER TABLE conversation ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
COMMENT ON COLUMN conversation.version IS '房间版本号，每次新增 chat_message 时 +1';

ALTER TABLE chat_message ADD COLUMN room_version BIGINT NOT NULL DEFAULT 0;
COMMENT ON COLUMN chat_message.room_version IS '该消息诞生时的 conversation.version';

-- === 新增：sender_type 枚举值 ===
-- 现有 chat_message 无 sender_type 列；conversation_member.member_type 已有。
-- 为支持系统消息，新增列而非复用 conversation_member：
ALTER TABLE chat_message ADD COLUMN sender_type SMALLINT NOT NULL DEFAULT 1;
-- 1=USER, 2=AGENT, 3=SYSTEM
COMMENT ON COLUMN chat_message.sender_type IS '1=USER, 2=AGENT, 3=SYSTEM';

-- 为历史数据回填 AGENT（role=2 且 sender_agent_id 非空的为 AGENT 发送）
UPDATE chat_message SET sender_type = 2 WHERE role = 2 AND sender_agent_id IS NOT NULL;
UPDATE chat_message SET sender_type = 3 WHERE role = 1 AND principal_id = 1;
-- 剩余 role=1 且 principal_id>1 的保持 sender_type=1 (USER)

CREATE INDEX idx_chat_message_room_version ON chat_message(conversation_id, room_version);

-- === 删除：Phase 1 inbox 表 ===
DROP TABLE IF EXISTS agent_inbox CASCADE;
DROP TABLE IF EXISTS agent_working_state CASCADE;

-- === command 表：保留历史字段（不 DROP，避免丢失审计数据） ===
-- executor_kind 和 source_type 列保留不再写入，历史 command 仍可查询。
-- 新建 command 时 executor_kind 固定写 2 (ACP)、source_type 固定写 2 (CHAT)。
-- Phase 2 再考虑废弃。
```

### Phase 2（Held Draft 机制）

```sql
-- state: 1=HELD, 2=RESOLVED, 3=EXPIRED
CREATE TABLE held_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    action_json JSONB NOT NULL,          -- 原始 SubmitAction proto
    base_version BIGINT NOT NULL,        -- Agent 决策时的房间版本
    current_version BIGINT NOT NULL,     -- 暂存时的房间版本
    state SMALLINT NOT NULL DEFAULT 1,   -- 1=HELD, 2=RESOLVED, 3=EXPIRED
    resolution SMALLINT,                 -- 1=REVISE, 2=SEND_AS_IS, 3=DISCARD, 4=FORCE_SEND
    command_id UUID REFERENCES command(id) ON DELETE SET NULL,  -- resolved=FORCE_SEND/SEND_AS_IS 后创建
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '10 minutes')
);

CREATE INDEX idx_held_action_agent_state ON held_action(agent_id, state);
CREATE INDEX idx_held_action_conversation ON held_action(conversation_id, state);
CREATE INDEX idx_held_action_expires ON held_action(state, expires_at) WHERE state = 1;

-- 后台定期清理过期 held_action (由 Dispatcher 扫描 expires_at)
```

---

## 崩溃恢复与 Held Draft 超时

### Agent 重连恢复

当前 `AgentReady` 携带 `last_command_id` / `last_ack_seq` / `last_event_seq` 用于恢复 in-flight command。新架构保留此机制，并扩展：

1. **Agent 重连时**发送 `AgentReady`：
   - 若有未完成的 `command_id` → 走现有 grace period 恢复流程
   - 并检查该 agent 是否有 `held_action` 处于 `HELD` 状态：
     - 存在 → 发送 `ActionResponse(held=true, ...)` 重新提示 Agent 决议
     - 不存在 → 发送 `NewMessagesAvailable` 让 Agent 拉取最新消息

2. **Held Action 超时**：
   - `expires_at` 默认 10 分钟
   - Dispatcher 后台 goroutine 每 1 分钟扫描 `state=HELD AND expires_at < now()`
   - 超时 → `state=EXPIRED`，释放占用，记录审计
   - 不自动 FORCE_SEND（避免发送过期内容）

### Permission 模型与 Held Draft 的交互

当一个 ACP command 处于 `PERMISSION_REQUESTED` 等待用户决策时：
- 若期间该 conversation 有新消息到达，**不触发** Held Draft（command 已在执行中）
- Permission 决策仍通过 `RespondPermission` API → bidi stream → Agent
- 若用户取消 command（`CancelCommand`），Manager 取消 ACP session 并将任何未决议的 held_action 标记为 `DISCARD`

---

## 前端执行进度可见性

### 问题

删除 `SendCommand` 后，`command` 变成内部细节。但前端仍需展示 ACP 执行过程（工具调用、diff、文本输出等）。

### 方案：对话级事件流

保留 `WatchCommand` / `WatchCommandEvents` RPC（以 `command_id` 为准），但前端通过 `chat_message.command_id` 关联找到对应 command，再订阅其事件流。

新增 **`WatchConversationEvents`** RPC（Phase 2 考虑），以 `conversation` 为维度聚合该会话内所有 command 的事件，前端无需逐个 command 订阅：

```protobuf
rpc WatchConversationEvents(WatchConversationEventsRequest) returns (stream CommandEvent) {
  option (google.api.http) = {get: "/v1/{conversation=conversations/*}:watchEvents"};
}

message WatchConversationEventsRequest {
  string conversation = 1;
  int32 after_seq_no = 2;  // 可选，沿用现有语义
}
```

Phase 1 阶段前端继续用 `chat_message.command_id` → `WatchCommandEvents`，保证兼容。

---

## 后端代码变更

### 删除的文件

| 文件 | 阶段 | 原因 |
|------|------|------|
| `backend/agent/executor/executor.go` | Phase 1 | BashExecutor 废弃，只保留 ACP（需先确认无生产 Agent 依赖 SHELL） |
| `backend/manager/store/inbox.go` | Phase 1 | inbox 模型被消息拉取取代 |

### 新建的文件

| 文件 | 阶段 | 职责 |
|------|------|------|
| `backend/manager/store/held_action.go` | Phase 2 | `held_action` 表的 CRUD + 超时扫描 |
| `backend/manager/store/conversation_version.go` | Phase 1 | `IncrementConversationVersion`、`GetMessagesAfterVersion` |

### 重度改写的文件

| 文件 | 阶段 | 变更说明 |
|------|------|----------|
| `backend/manager/component/dispatcher/dispatcher.go` | Phase 1+2 | Phase 1：删除 inbox 方法，新增 `HandlePullMessages`、`NotifyNewMessages`；`HandleResult` 改为创建 assistant message + 递增 conversation.version。Phase 2：新增 `HandleSubmitAction`（Held Draft 校验 + 内部创建 command + 下发 `CommandRequest`）、`HandleResolveHeldAction`、held_action 超时扫描 goroutine |
| `backend/agent/client/command_stream.go` | Phase 1+2 | Phase 1：`PullMessages` 流程、`NewMessagesAvailable` 处理、删除 shell executor 分支。Phase 2：`SubmitAction` 流程、Held Draft 处理 |
| `proto/v1/v1/command.proto` | Phase 1+2 | 按上文 Proto 设计部分分阶段改写 |

### 中度改写的文件

| 文件 | 阶段 | 变更说明 |
|------|------|----------|
| `backend/manager/api/v1/command.go` | Phase 1 | 删除/弃用 `SendCommand` handler；`SendMessage` handler 增加 `room_version` 递增 + `NewMessagesAvailable` 通知；`buildInboxSummary` 删除 shell 分支 |
| `backend/manager/api/v1/agent_command.go` | Phase 1+2 | `CommandChannel` 改为 `AgentChannel`；处理 `PullMessages`/`SubmitAction`/`ResolveHeldAction` |
| `backend/manager/store/command.go` | Phase 1 | `CommandMessage` 保留 `ExecutorKind`/`SourceType`/`ConversationID` 字段（历史兼容），`CreateCommand` 固定写入 `ExecutorKind=ACP`、`SourceType=CHAT` |
| `backend/manager/store/chat_message.go` | Phase 1 | `ChatMessage` 新增 `RoomVersion`、`SenderType` 字段；`CreateChatMessage` 写入这些字段 |
| `backend/manager/store/conversation.go` | Phase 1 | 新增 `IncrementVersion` 方法 |
| `backend/manager/migration/latest.sql` | Phase 1+2 | 按上表分阶段新增表和列 |
| `backend/manager/server/grpc_routes.go` | Phase 1 | 更新服务名绑定（`AgentStreamService`） |

### 轻微改写的文件

| 文件 | 阶段 | 变更说明 |
|------|------|----------|
| `backend/agent/executor/runtime.go` | Phase 1 | `Request` 结构体删除 `ExecutorKind`、`SourceType` 字段，新增 `ConversationID`、`ReplyToMessageID` |
| `backend/agent/executor/acp_executor.go` | Phase 1 | 适配新的 Request 结构 |

---

## 前端变更

### Store 变更

| 文件 | 阶段 | 变更 |
|------|------|------|
| `stores/chat.ts` | Phase 1 | `sendChatMessage()` 改为纯 `SendMessage`，不再调用 `SendCommand`；`streamChatCommand()` 适配：通过 `chat_message.command_id` 关联 `WatchCommandEvents` |
| `stores/command.ts` | Phase 1 | 删除 `sendCommand()`；保留 `listCommands()`、`getCommand()`、`watchCommand()`、`watchCommandEvents()` 用于执行监控 |
| `stores/types.ts` | Phase 1 | 删除 `sendCommand`、`executorKind` 等类型定义 |

### 组件和页面变更

| 文件 | 阶段 | 变更 |
|------|------|------|
| `pages/dashboard/command-list.tsx` | Phase 1 | 删除 `handleSend()` 中的 `SendCommand` 调用和 `ExecutorKind` 引用 |
| `pages/dashboard/command-detail.tsx` | Phase 1 | 删除 `isACP` 条件分支（全部是 ACP） |
| `lib/command-status.ts` | Phase 1 | 删除 `executorKindToI18nKey` 中的 SHELL 条目 |

### 文案变更

| 文件 | 阶段 | 删除内容 |
|------|------|----------|
| `locales/en-US.json` | Phase 1 | `"executor-shell": "Shell"` |
| `locales/zh-CN.json` | Phase 1 | `"executor-shell": "Shell"` |

### Proto 生成物变更

```
frontend/src/types/proto-es/v1/command_pb.d.ts
frontend/src/types/proto-es/v1/command_pb.js
```
自动重新生成，随 proto 变更更新。

---

## 实施步骤（分阶段）

### Phase 1：消息驱动骨架（不含 Held Draft）

目标：`SendMessage` 成为唯一用户入口，inbox 模型替换为消息拉取，SHELL 执行器移除。

#### Step 1.1: Proto 改写（Phase 1 部分）
- 新增 `PullMessages`、`MessageSnapshot`、`NewMessagesAvailable`、`SenderType` enum
- `ChatMessage` 新增 `room_version`、`sender_type` 字段
- `CommandRequest` 简化字段（删除 `executor_kind`、`source`，新增 `conversation_id`、`reply_to_message_id`）
- 重命名 `AgentCommandService` → `AgentStreamService`、`*CommandMessage` → `*StreamMessage`、`CommandChannel` → `AgentChannel`
- 删除 `PullInbox`/`SelectInboxItem`/`DeferInboxItem`/`InboxSnapshot`/`InboxItemSelected`/`InboxItem`
- `SendCommand` RPC 标记 `deprecated`（proto option `deprecated = true`），不删除
- `ExecutorKind`/`CommandSource` enum 保留但标记 deprecated
- 运行 `buf format -w proto && buf lint proto && cd proto && buf generate`

#### Step 1.2: 数据库迁移（Phase 1 部分）
- 新增 `conversation.version`、`chat_message.room_version`、`chat_message.sender_type` 列
- 回填历史 `sender_type`
- 新增 `idx_chat_message_room_version` 索引
- 删除 `agent_inbox`、`agent_working_state` 表
- `command.executor_kind`、`command.source_type` 列**保留**（历史兼容），新建 command 固定写 ACP/CHAT

#### Step 1.3: Store 层变更
- 删除 `inbox.go`
- 新建 `conversation_version.go`（`IncrementConversationVersion`、`GetMessagesAfterVersion`）
- 更新 `chat_message.go`（新增 `RoomVersion`、`SenderType` 字段读写）
- 更新 `command.go`（`CreateCommand` 固定 `ExecutorKind=ACP`、`SourceType=CHAT`）
- 更新 `conversation.go`（新增 `IncrementVersion`）

#### Step 1.4: Dispatcher 改写（Phase 1）
- 删除所有 inbox 方法（`HandlePullInbox`、`HandleSelectInboxItem`、`HandleDeferInboxItem`、`SendInboxSnapshot`、`NotifyInboxUpdated`）
- 新增 `HandlePullMessages(agentID, conversationID, afterVersion)` → 返回 `MessageSnapshot`
- 新增 `NotifyNewMessages(agentID, conversationID, version)` → 推送 `NewMessagesAvailable`
- 修改 `HandleResult`：创建 assistant `chat_message`（带 `room_version`）+ `IncrementConversationVersion`
- `RegisterAgent` 不再 upsert working_state（该表已删）

#### Step 1.5: API Handler 更新
- `command.go`：`SendMessage` handler 增加 `IncrementConversationVersion` + `NotifyNewMessages`；`SendCommand` 保留但内部委托给 `SendMessage` 流程（兼容期）
- `agent_command.go`：`CommandChannel` → `AgentChannel`；处理 `PullMessages`
- `grpc_routes.go`：更新服务名绑定

#### Step 1.6: Agent Client 改写（Phase 1）
- 删除 `buildRuntime` 中的 SHELL 分支
- `handleInboxSnapshot` → `handleNewMessagesAvailable` + `pullMessages` 流程
- `CommandRequest` 适配新字段
- `runCommand` 中 LIFECYCLE event 不再写 `executor_kind`（或固定写 `"ACP"`）

#### Step 1.7: Agent Executor 适配
- `runtime.go` `Request` 删除 `ExecutorKind`、`SourceType`，新增 `ConversationID`、`ReplyToMessageID`
- `acp_executor.go` 适配
- 删除 `executor.go`（BashExecutor）

#### Step 1.8: 前端适配
- 删除前端 `SendCommand` 调用链
- `sendChatMessage()` 改为纯 `SendMessage`
- 执行进度通过 `chat_message.command_id` → `WatchCommandEvents`（保持兼容）
- 删除 `ExecutorKind` 引用与 shell 文案

#### Step 1.9: 构建验证
- `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`
- `golangci-lint run --allow-parallel-runners`
- `go test ./backend/... -count=1`
- `LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`（若 stdio/runtime 集成有变）
- `pnpm --dir frontend type-check`

---

### Phase 2：Held Draft 机制（多参与方场景）

目标：引入版本校验与草稿决议，应对并发消息场景。

#### Step 2.1: Proto 增量
- 新增 `SubmitAction`、`ActionResponse`、`ResolveHeldAction`、`ActionResolution` enum
- `AgentStreamMessage`/`ManagerStreamMessage` 新增对应 oneof 分支

#### Step 2.2: 数据库迁移
- 新建 `held_action` 表（含 `expires_at`、`command_id` 关联）

#### Step 2.3: Store 层
- 新建 `held_action.go`（CRUD + `ExpireHeldActions` 扫描）

#### Step 2.4: Dispatcher 增量
- 新增 `HandleSubmitAction`：
  1. 比较 `base_version` 与 `conversation.version`
  2. 一致 → 内部创建 command + 下发 `CommandRequest` → `ActionResponse(committed=true, command_id)`
  3. 不一致 → 创建 `held_action` 记录 → `ActionResponse(held=true, new_messages)`
- 新增 `HandleResolveHeldAction`：
  - `REVISE` → 删除 held_action，Agent 重新 `PullMessages` + 重提 `SubmitAction`
  - `SEND_AS_IS` → 直接创建 command + 下发
  - `DISCARD` → 标记 `RESOLVED`，不发 command
  - `FORCE_SEND` → 直接创建 command + 下发
- 新增 held_action 超时 goroutine（每 1 分钟扫描 `state=HELD AND expires_at < now()` → `state=EXPIRED`）
- `AgentReady` 重连流程扩展：检查并重新提示 held_action

#### Step 2.5: Agent Client 增量
- `PullMessages` 后发起 `SubmitAction`
- 处理 `ActionResponse(held=true)` → 读 `new_messages` → 发 `ResolveHeldAction`
- `AgentReady` 崩溃恢复时检查并决议遗留 held_action

#### Step 2.6: 构建验证
- 同 Phase 1 Step 1.9

---

### Phase 3：清理废弃 API（可选，视兼容需求）

- 删除 `SendCommand` RPC、`SendCommandRequest`
- 删除 `ExecutorKind`、`CommandSource` enum
- 删除 `command` 表的 `executor_kind`、`source_type` 列（确认无历史查询需求后）
- 新增 `WatchConversationEvents` RPC（前端逐 command 订阅 → 会话级订阅）

---

## 对 AX 四问的回应

本次重构直接回应了 Raft 团队提出的 AX 四大问题：

| AX 问题 | 我们的回应 |
|---------|-----------|
| What does the agent see? | `MessageSnapshot`（复用 `ChatMessage` + `room_version`）提供结构化的房间状态，包含版本标记和发送者上下文 |
| What state does it carry? | `PullMessages(conversation_id, after_version)` 让 Agent 携带上次看到的版本，只拉增量；重连时 `AgentReady` 自描述进度 |
| What can it recover from? | Held Draft 四种决议路径（Revise/SendAsIs/Discard/ForceSend）+ `expires_at` 超时 + 崩溃重连重新提示覆盖全部恢复场景 |
| What is it allowed to decide? | `SubmitAction` 的选项空间显式化；Agent 自主决定回复/执行/沉默/追问；`sender_type=SYSTEM` 让程序化触发也走同一决策路径 |

---

## 设计决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 是否新建 `RoomMessage` 类型 | 否，复用 `ChatMessage` + 新增字段 | 避免双类型表示同一数据；`ChatMessage` 已有 `sender_name`/`role`/`content`/`created_at` |
| `PullMessages` 用 map 还是单值 | 单值 `conversation_id + after_version` | Agent 通常一次关注一个会话；map 浪费带宽且游标应由 Agent 自维护 |
| `command.executor_kind`/`source_type` 列 | Phase 1 保留不删，Phase 3 视情况删 | 保留历史审计数据；新建 command 固定写默认值 |
| `SendCommand` RPC 处理 | Phase 1 deprecated 保留，Phase 3 删除 | 给前端/外部调用方迁移窗口 |
| Held Draft 何时引入 | Phase 2，与骨架分离 | 1:1 场景并发概率低；Held Draft 主要服务多参与方频道 |
| SHELL executor 删除时机 | Phase 1 确认无生产依赖后删除 | 若有 CI 依赖 SHELL，转为 SYSTEM sender_type 消息 |
| `conversation.version` vs `room_version` 命名 | DB 列名 `version`，proto 字段名 `room_version` | DB 内部用简名，跨服务接口用语义明确名；文档统一用 `room_version` 描述概念 |
| `held_action` 超时策略 | `expires_at` 默认 10 分钟，不自动 FORCE_SEND | 避免发送过期内容；让 Agent 重连后重新决策更安全 |