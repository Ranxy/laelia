# Laelia 架构重构：从 Command-Driven 到 Message-Driven

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：消息驱动骨架已落地并被进一步演进——`SendCommand`/`ExecutorKind`/inbox 已彻底删除，agent 改为 `BeginSession` 会话模型 + per-channel 游标发现（原方案的 `PullMessages`/`SubmitAction`/服务端 Held Draft 未实现，版本校验以 `PostMessage.base_version` 冲突检测在客户端落地），权限决策流已移除（自动授予）。下文按当前实现重写。

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

> 状态：本节为**重构前的历史诊断**，描述的是旧 Command-Driven 模型。其中的 `SendCommand`、`ExecutorKind`、`CommandSource`、agent_inbox/agent_working_state、权限二元模型等在当前代码中均已移除（实施结果见"实施结果"一节）；保留本节是为了记录重构动机。

### 现状（重构前）：Command-Driven 模型

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

## 新架构：Message-Driven 模型（当前实际实现）

### 核心思想（已落地，形态有调整）

```
User → SendMessage("帮我检查服务器")            （用户唯一发送入口）
     → 创建 chat_message, conversation.version++（同事务）
     → dispatcher.NotifyNewMessages 唤醒成员 Agent（NewMessagesAvailable）
     → roomhub 唤醒前端长轮询
     → Agent drain 循环 → BeginSession（Manager 校验游标/提醒/启用状态）
     → 有待处理工作 → Manager 创建 RUNNING 会话 command（会话锚点）
     → Agent 拉取未读（ListChannelUpdates → message check / message read）
     → Agent 自主判断：回复？执行工具？追问？沉默？
     → 回复 → PostMessage(base_version=N)
         → 版本一致 → committed=true，消息创建，发送者游标前移
         → 版本冲突 → committed=false + new_messages（Agent 客户端自行决议）
```

**本质变化**：`message` 是主，`command` 退化为"会话锚点"——`BeginSession` 时创建的 RUNNING command 只用于承载执行/事件/审计关联，不再承载指令内容（`instruction` 为空，prompt 由 agent 侧组装）。

> 设计偏差记录：原方案中的 `SubmitAction` / `PullMessages` / `MessageSnapshot` / `ResolveHeldAction` / 服务端 `held_action` 表**均未实现**。版本校验改为轻量的 `PostMessage.base_version` 冲突检测（决议在客户端完成），消息拉取改为 `ListChannelUpdates` 发现 + 既有的 `ListConversationMessages(after_version)` 增量读取（见下文）。

### 实际落地：BeginSession 会话模型（取代 SubmitAction）

Agent 侧由 drain 循环驱动（`backend/agent/client/drain_runner.go`），Manager 侧入口在 `backend/manager/component/dispatcher/dispatcher.go` 的 `HandleBeginSession`：

| 维度 | 旧 `SendCommand` | 当前实现 |
|------|------------------|----------|
| 触发者 | 用户（API 调用方） | Agent 自主（drain 循环；唤醒只是加速器） |
| 会话开启 | 无 | `BeginSession` → Manager 校验游标（`HasUpdates`）、到期提醒（`HasDueReminders`）、agent 启用状态与运行时能力 → 无工作回 `BeginSessionResponse{idle=true}`，有工作则创建 RUNNING command 并回 `BeginSessionResponse{command_id, agent_display_name, owner_display_name, team, prompt_version, ...}` |
| 版本校验 | 无 | Agent 回复携带 `PostMessage.base_version`，Manager 比对 `conversation.version`（一致才提交） |
| 执行器选择 | 用户指定 `ExecutorKind` | ACP（stdio `acp` + acp2 v2 thread 路径）或内置 pi 运行时，由 agent 能力（`supports_acp` / `supports_pi`）决定，对用户不可见 |
| 上下文注入 | Manager 静态注入最近 6 条 | Agent 用 `message check` / `message read` 主动拉取；`GetCommandContext`（CLI `command context`）仅用于执行历史恢复/详情 |

会话 command 与 conversation 的关联在 Agent 读取频道/提交 `AckProcessedVersion` 时补全；结果回写由 Agent 主动 `PostMessage`（带 `command_id` 关联），不再由 Manager 代发 assistant 消息。

### 实际数据流

```
┌─ 1. 用户发送消息 ──────────────────────────────────────────┐
│  SendMessage(conversation, content)                         │
│  → store.CreateChatMessageBumpVersion: chat_message 创建,   │
│    conversation.version++（同一事务）                        │
│  → dispatcher.NotifyNewMessages 唤醒成员 Agent               │
│  → roomhub.NotifyConversation 唤醒前端长轮询                 │
│  → 线程订阅 / activity 生成（旁路，best-effort）             │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 2. Agent 发现工作 ─────────────────────────────────────────┐
│  Agent ← NewMessagesAvailable（best-effort 唤醒，非真相源）  │
│  Agent → BeginSession                                        │
│  Manager: agent_channel_cursor vs conversation.version、     │
│  到期提醒、agent 启用/运行时能力                              │
│    ✓ 有工作 → 创建 RUNNING command → BeginSessionResponse    │
│    ✗ 无工作 → BeginSessionResponse{idle=true}（保持空闲）    │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 3. Agent 拉取消息并决策 ────────────────────────────────────┐
│  Agent → ListChannelUpdates（游标后的频道/线程未读清单）      │
│  Agent → message check / message read（按 after_version）    │
│  Agent 自主判断：回复 / 执行工具 / 追问 / 沉默               │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 4. Agent 本地执行 ─────────────────────────────────────────┐
│  ACP（opencode/codex）或内置 pi 运行时，同一 drain turn       │
│  → bidi stream 上报 CommandProgress / CommandEvent           │
│  → 权限已自动授予（permission_decision 流已移除）            │
│  → 用户可 CancelCommand（→ CancelMessage）中断，             │
│    或 SteerCommand（→ SteerMessage）向在途 turn 注入消息     │
└─────────────────────────────────────────────────────────────┘
                          ↓
┌─ 5. Agent 回写结果 ─────────────────────────────────────────┐
│  Agent → PostMessage(conversation, content, base_version,    │
│                       command_id)                            │
│  base_version == version → committed=true：消息创建，        │
│                             发送者游标前移（UpsertCursor）    │
│  base_version != version → committed=false：响应携带         │
│    new_messages（≤50 条）+ conflict_description，            │
│    由 Agent 决定改写重发 / 照发 / 放弃（沉默）                │
└─────────────────────────────────────────────────────────────┘
```

### 架构对比

```
                    旧架构 (Command-Driven, 已移除)

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

                新架构 (Message-Driven, 已落地)

  ┌──────────────────────────────────────────────────┐
  │                                                  │
  │  User ──SendMessage──→ Conversation              │
  │                         │                        │
  │                      message (主, 唯一)           │
  │                         │                        │
  │                         ↓                        │
  │        NewMessagesAvailable 唤醒 + 游标发现       │
  │                         │                        │
  │                    Agent drain turn              │
  │              (BeginSession → command 锚点)       │
  │                 /      |       \                 │
  │             回复     执行工具    沉默              │
  │               │        │                         │
  │               │  CommandProgress/Event/Result    │
  │               ↓                                  │
  │        PostMessage(base_version)                 │
  │          ├ 一致 → committed (消息+游标前移)       │
  │          └ 冲突 → new_messages (自行决议)         │
  │                                                  │
  │  沉默是有效行为；唤醒只是 hint，游标是真相源        │
  └──────────────────────────────────────────────────┘
```

---

## 非对话类触发路径

`SenderType` 枚举已实现（`SENDER_TYPE_USER=1 / SENDER_TYPE_AGENT=2 / SENDER_TYPE_SYSTEM=3`，见 `proto/v1/v1/command.proto`）；`chat_message.sender_type` 列 1=USER、2=AGENT、3=SYSTEM。

- 系统消息由**内置后台功能**以系统机器人身份（`principal_id=1`）直接写入会话：提醒到期胶囊与错过补发（`backend/manager/store/reminder.go`）、任务状态胶囊（`backend/manager/api/v1/task.go`、`backend/manager/api/v1/reminder.go`）等。这些写入统一走 bump-version 管线，会唤醒成员 Agent。
- 原方案中的 `GetOrCreateSystemConversation` **未实现**；当前也没有通用的 webhook/CI 注入 API——程序化触发依赖上述内置功能或未来的专用入口。
- Agent 拉取后按 `sender_type=SYSTEM` 识别系统消息（chattools 输出行带 sender 类型），按引导语处理。

这样"一切源于消息"成立：用户消息、系统消息、Agent 回复都是 `chat_message`，差异仅在 `sender_type`。

---

## Proto 设计（当前实际形态，`proto/v1/v1/command.proto`）

### 消息服务 (CommandService)

`SendCommand` RPC、`SendCommandRequest`、`ExecutorKind`、`CommandSource` 枚举已**全部删除**（没有保留弃用窗口）。`SendMessage` 是用户唯一发送入口（handler 强制要求用户身份；agent 调用被拒，必须走 `PostMessage`）。保留的命令相关 RPC：`ListCommands` / `GetCommand` / `CancelCommand` / `SteerCommand` / `WatchCommand` / `WatchCommandEvents` / `GetCommandContext`。

频道/会话管理 RPC 大幅扩展：threads、archive/mute、channel members、tasks、reminders、files、activities、presence、reactions、`SearchChatHistory` 等（完整清单见 `proto/v1/v1/command.proto` 的 `service CommandService`）。

### Agent 流服务 (AgentStreamService)（重命名已按方案落地）

```protobuf
service AgentStreamService {
  rpc AgentChannel(stream AgentStreamMessage) returns (stream ManagerStreamMessage);
}

message AgentStreamMessage {
  oneof message {
    AgentReady agent_ready = 1;
    BeginSession begin_session = 2;
    CommandProgress progress = 5;
    CommandResult result = 6;
    CommandEvent event = 7;
    Ping ping = 8;
    ProvidersDiscovered providers_discovered = 9;      // 响应 discover_providers
    WorkspaceListResponse workspace_list_response = 10;
    WorkspaceReadResponse workspace_read_response = 11;
    PromptReleaseNoticeAck prompt_release_notice_ack = 12;
  }
}

message ManagerStreamMessage {
  oneof message {
    NewMessagesAvailable new_messages = 4;             // best-effort 唤醒
    BeginSessionResponse begin_session_response = 8;
    CancelMessage cancel = 5;
    Pong pong = 6;
    // 7 曾是 permission_decision；权限现已自动授予，该分支已删除
    DiscoverProviders discover_providers = 9;          // 要求 daemon 重新探测 LLM provider
    WorkspaceListRequest workspace_list_request = 10;
    WorkspaceReadRequest workspace_read_request = 11;
    SteerMessage steer = 12;                           // 向在途 turn 注入后续消息
    PromptReleaseNotice prompt_release_notice = 13;    // 系统提示词变更推送
  }
}
```

### 关键消息类型（实际实现）

- **`SenderType`**：`SENDER_TYPE_UNSPECIFIED=0 / SENDER_TYPE_USER=1 / SENDER_TYPE_AGENT=2 / SENDER_TYPE_SYSTEM=3`（取代 `CommandSource`）。
- **`AgentReady`**：`session_id`、`last_command_id`、`last_ack_seq`、`last_event_seq`、`agent_name`（声明本连接服务的 agent；重连语义见"崩溃恢复"）。
- **`NewMessagesAvailable`**：`conversation_ids[]`、`versions[]`、`thread_root_message_id`（线程唤醒提示）。仅是唤醒信号；**真相源是 agent 的持久化 per-channel 游标**（`agent_channel_cursor` 表），掉线漏唤醒靠重连后 `ListChannelUpdates` 对比 `conversation.version` 与游标重新发现。
- **`BeginSession` / `BeginSessionResponse`**：会话开启协商；响应携带 `command_id`、`idle`、`agent_display_name`、`owner_display_name`、`team`（TeamContext）、`prompt_version`（"<static_expected>.<dynamic_hash>" 提示词指纹）、`prompt_release_notice`。
- **`PostMessageRequest` / `PostMessageResponse`**：agent 回复入口；`base_version` REQUIRED；冲突时响应 `committed=false` + `current_version` + `new_messages[]`（≤50 条，含 `is_own` 标记）+ `conflict_description`。
- **`SendMessageRequest`**：用户发送；支持 `content`（可空，允许纯附件消息）、服务端解析的 `mentions`、`attachments`、`thread_root`、`as_task`。

原方案中的 `PullMessages` / `MessageSnapshot` / `SubmitAction` / `ActionResponse` / `ResolveHeldAction` / `ActionResolution` **从未加入 proto**——对应能力由 `ListChannelUpdates` + `ListConversationMessages(after_version)` + `PostMessage(base_version)` 承接。

### ChatMessage 字段（实际编号）

`ChatMessage`（`proto/v1/v1/command.proto`）：`sender_type = 9`、`room_version = 10`、`mentions = 11`、`is_own = 12`、`attachments = 13`、`thread_root = 14`、`thread_reply_count = 15`、`task = 16`、`agent_id = 17`、`principal_id = 18`、`reactions = 19`。原方案设想的 `room_version=10 / sender_type=11` 编号中，`sender_type` 实际落在 9（中间插入了 mentions 等字段）。

### CommandRequest（实际字段）

```protobuf
message CommandRequest {
  string command_id = 1;
  string instruction = 2;
  string profile = 3;
  map<string, string> env = 4;
  string working_dir = 5;
  int32 timeout_seconds = 6;
  bool allow_diff = 7;
  string principal_id = 8;
  string conversation_id = 9;
  string reply_to_message_id = 10;
  string agent_display_name = 11;
}
```

`executor_kind` / `source` 字段已删除（枚举本身也已删除）。会话型 command 的 `instruction` 为空（指令由 agent 侧 prompt 组装）。

### 重命名对照（已全部落地）

| 旧名称 | 新名称 | 状态 |
|--------|--------|------|
| `AgentCommandMessage` | `AgentStreamMessage` | 已落地（Go 侧构造函数名 `NewAgentCommandService` 保留，返回的服务类型为 `AgentStreamService`） |
| `ManagerCommandMessage` | `ManagerStreamMessage` | 已落地 |
| `AgentCommandService` | `AgentStreamService` | 已落地（`backend/manager/api/v1/agent_command.go`） |
| `CommandChannel` | `AgentChannel` | 已落地 |

### 已删除的 Proto 元素（全部直接删除，无弃用窗口）

| 元素 | 处理 |
|------|------|
| `ExecutorKind` enum | 已删除；仅在 Lifecycle 事件 payload 中保留字符串形式（`"ACP"` / `"THREAD"`） |
| `CommandSource` enum | 已删除（由 `SenderType` 取代） |
| `SendCommand` RPC / `SendCommandRequest` | 已删除 |
| `PullInbox` / `SelectInboxItem` / `DeferInboxItem` | 已删除（inbox 模型移除） |
| `InboxSnapshot` / `InboxItemSelected` / `InboxItem` | 已删除 |
| `RespondPermission` / `permission_decision` | 已删除（权限自动授予） |

---

## 数据库变更（实际落地）

### 已实现（消息驱动骨架 + 演进）

`backend/manager/migration/migration/LATEST.sql` 与增量迁移（`backend/manager/migration/migration/1.1/`）：

- `conversation.version BIGINT NOT NULL DEFAULT 1`（房间版本号）。
- `chat_message.room_version BIGINT NOT NULL DEFAULT 0` + `idx_chat_message_room_version (conversation_id, room_version)`。
- `chat_message.sender_type SMALLINT NOT NULL DEFAULT 1`（1=USER, 2=AGENT, 3=SYSTEM）+ 历史数据回填（role=2 且 sender_agent_id 非空 → AGENT；principal_id=1 → SYSTEM）。
- `DROP TABLE IF EXISTS agent_inbox / agent_working_state CASCADE`（inbox 模型移除）。
- 新增游标表 `agent_channel_cursor` / `user_channel_cursor`（`processed_version`），作为 agent/前端"已处理到哪"的真相源——取代 inbox 的"下一条工作"派发。
- 线程模型：`chat_message.thread_root_message_id`、`thread_participant`、`user_thread_participant` 等。

### 与原方案的偏差

- **`held_action` 表未创建**：LATEST.sql 中显式 `DROP TABLE IF EXISTS held_action CASCADE`（清理历史实验），Held Draft 的服务端实现被客户端 `PostMessage.base_version` 冲突检测取代（见"崩溃恢复"一节）。
- **`command.executor_kind` / `command.source_type` 已删除**（原方案 Phase 1 "保留不删" → 实际在后续迁移中直接 DROP）。`command` 表现有列：`id / agent_id / machine_id / principal_id / command / instruction / profile / allow_diff / status / exit_code / duration_ms / created_at / started_at / completed_at / result_json / env / working_dir / timeout_seconds / error_message / final_summary / last_ack_seq / conversation_id`。
- store 层提供 `IncrementConversationVersion` / `GetConversationVersion` / `GetMessagesAfterVersion`（`backend/manager/store/conversation_version.go`）；但消息写入主路径使用 `CreateChatMessageBumpVersion` / `CreateTaskMessageBumpVersion`（消息创建与 bump 同事务，见 `backend/manager/store/chat_message.go`、`backend/manager/store/task.go`）。

---

## 崩溃恢复（原"Held Draft 超时"一节随 held_action 方案废弃）

### Agent 重连恢复（实际行为）

`AgentReady` 携带 `session_id` / `last_command_id` / `last_ack_seq` / `last_event_seq` / `agent_name`（处理逻辑在 `backend/manager/api/v1/agent_command.go` 的 `handleAgentReady`）：

1. 若 `last_command_id` 对应的 command 仍为 RUNNING（断线前在途）——**不恢复执行**，直接标记 FAILED（"agent disconnected during execution"）并清空会话当前 command；agent 的 drain 循环将开启全新会话。原方案的 grace period 恢复流程已不存在。
2. 断线期间漏掉的消息不靠补推：Manager 发送 best-effort `NotifyWake`，agent 自 kick 后由下一次 `BeginSession` 通过持久化游标（`agent_channel_cursor`）重新发现全部未处理工作——游标是真相源，唤醒丢失只是多等一个周期。
3. 执行历史（output/event）可通过 `GetCommandContext`（CLI `command context`）恢复阅读。

### PostMessage 版本冲突（取代 Held Draft）

原方案的 held_action 表 / 四决议 RPC / 超时扫描**未实现**。等价能力内联在 `PostMessage`（`backend/manager/api/v1/command_message.go`）：

- `base_version == conversation.version` → `committed=true`，消息创建，发送者游标前移（`UpsertCursor`，单调 `GREATEST`，不会被显式 ack 回退——保证自己的回复不会被误认为新工作）。
- `base_version != current` → `committed=false`，响应携带 `new_messages`（最多 50 条，含 `is_own` 标记）与 `conflict_description`；由 **agent 客户端**决定改写重发 / 照发 / 放弃（沉默）。没有服务端暂存、没有超时、没有 FORCE_SEND——REVISE/SEND_AS_IS/DISCARD 都在客户端完成，"FORCE_SEND"即无视冲突再次提交。

### Permission 模型

权限已**自动授予**：`PERMISSION_REQUESTED` 事件、`RespondPermission` API 与 `ManagerStreamMessage.permission_decision`（原字段 7）均已删除。高风险操作的把关改为引导语约束（owner DM 确认等，见 `backend/agent/executor/prompt/communication.md`）。用户中断/干预走 `CancelCommand`（→ `CancelMessage` 下发）与 `SteerCommand`（→ `SteerMessage` 在途注入）。

---

## 前端执行进度可见性（当前实现）

- `WatchCommand` / `WatchCommandEvents` RPC 保留；`frontend/src/stores/command.ts` 提供 `watchCommand` / `watchCommandEvents` / `getCommand` / `cancelCommand` / `steerCommand`。
- 聊天视图渲染**已提交的消息**（token 级流式渲染管线已退役）；需要看执行过程时从消息行跳转 command-detail 页（`/members/agents/{agent}/commands/{command_id}`），该页（`frontend/src/pages/dashboard/command-detail.tsx`）用 `watchCommand` + `watchCommandEvents` 订阅输出与事件流；`frontend/src/components/chat-events/`（tool-call、diff、warning）与 `frontend/src/components/command-events/` 复用同一事件模型（`frontend/src/lib/command-events-model.ts`）。
- **`WatchConversationEvents` 未实现**（仍以 command 为粒度订阅）。

---

## 后端代码变更（实际结果）

### 删除的文件

| 文件 | 说明 |
|------|------|
| `backend/agent/executor/executor.go` | BashExecutor 已删除（连同 SHELL 执行器）；现为 ACP executor（`backend/agent/executor/acp_executor.go`）+ thread executor（`backend/agent/executor/thread_executor.go`，acp2 v2 thread 路径） |
| `backend/manager/store/inbox.go` | inbox 模型移除 |

### 新建的关键文件

| 文件 | 职责 |
|------|------|
| `backend/manager/store/conversation_version.go` | `IncrementConversationVersion` / `GetConversationVersion` / `GetMessagesAfterVersion` |
| `backend/manager/api/v1/message_create.go` | SendMessage/PostMessage 共享的消息创建管线（bump 版本 + 唤醒 + activity） |
| `backend/manager/component/dispatcher/session_lifecycle.go`、`session_registry.go`、`command_bus.go` | agent 会话注册、BeginSession 生命周期、流管理 |
| `backend/agent/client/drain_runner.go`、`message_router.go`、`stream_connector.go` | agent 侧 drain 循环、Manager 消息路由、连接管理 |
| `backend/agent/acp2/` | acp2 v2 thread 传输层 |

（原方案的 `backend/manager/store/held_action.go` 未创建。）

### 重写/新增的核心文件

| 文件 | 变更 |
|------|------|
| `proto/v1/v1/command.proto` | 按上文 Proto 设计重写（SendCommand/枚举/inbox RPC 删除；BeginSession/PostMessage/SenderType/threads/reactions 等加入） |
| `backend/manager/component/dispatcher/dispatcher.go` | `HandleBeginSession`（游标/提醒/启用/运行时能力校验 → 创建 RUNNING command）、`NotifyNewMessages` / `NotifyWake` / `NotifyThreadMention`、`HandleResult` 等 |
| `backend/manager/api/v1/agent_command.go` | `AgentChannel` bidi：AgentReady（在途 command 标记 FAILED + wake）、BeginSession、Progress/Result/Event、Ping、workspace/providers、prompt notice ack |
| `backend/manager/api/v1/command_message.go` | `PostMessage`（成员门禁、归档只读、base_version 冲突检测、mentions 服务端解析、发送者游标前移） |
| `backend/manager/api/v1/channel_message.go` | `SendMessage`（用户专用；agent 调用被拒）+ `notifyConversationAgents` / 线程订阅唤醒 |
| `backend/manager/api/v1/command.go` | `ListChannelUpdates` / `ListThreadUpdates` / `AckProcessedVersion`（AX Agent Inbox 发现与游标推进） |
| `backend/manager/store/chat_message.go` | `RoomVersion` / `SenderType` 字段；`CreateChatMessageBumpVersion` |
| `backend/manager/store/agent_channel_cursor.go` | 持久化 per-channel 游标（取代 agent_inbox 的"下一条工作"派发） |
| `backend/agent/client/command_stream.go` | 连接循环 + 唤醒 + BeginSession 请求编排（配套 `drain_runner.go` / `message_router.go`） |
| `backend/agent/executor/runtime.go` | `Request` 无 `ExecutorKind`/`SourceType`；携带 `ConversationID`、`AgentID`、`MachineID`、`TurnPrompt`、`ReanchorPrompt`、owner/team 注入字段等 |
| `backend/manager/server/grpc_routes.go` | 注册 `AgentStreamService` |

---

## 前端变更（当前状态）

- `frontend/src/stores/chat.ts`：`sendChatMessage()` 纯 `SendMessage`（乐观占位 + 服务端回显按 id 去重）；无任何 `SendCommand` 调用链。
- `frontend/src/stores/command.ts`：无 `sendCommand`；保留 `cancelCommand`、`steerCommand`、`getCommand`、`watchCommand`、`watchCommandEvents`（执行监控/详情页用）。
- `frontend/src` 中已无 `ExecutorKind` / `sendCommand` 引用（仅生成物 `src/types/proto-es/` 含枚举历史命名，无调用）；`frontend/src/lib/command-status.ts` 只含状态→i18n key/徽章 variant 映射；`frontend/src/locales/` 无 `executor-shell` 文案。
- 执行详情：`frontend/src/pages/dashboard/command-detail.tsx` 通过 `watchCommand` + `watchCommandEvents` 订阅；`frontend/src/pages/dashboard/command-list.tsx` 的发送走 `sendChatMessage`（SendMessage）。

---

## 实施结果（截至 2026-09-06）

### Phase 1（消息驱动骨架）——已实现，且被进一步演进

- `SendMessage` 成为用户唯一发送入口；inbox 模型移除（表与 RPC 全删）；SHELL 执行器移除；`conversation.version` / `chat_message.room_version` / `sender_type` 落库；`NewMessagesAvailable` 唤醒。
- **超出原方案**：线程（thread）模型、per-channel 游标（`agent_channel_cursor`）、`ListChannelUpdates` / `ListThreadUpdates` / `AckProcessedVersion`、`BeginSession` 会话模型、系统提示词版本与推送（`prompt_version` / `PromptReleaseNotice`）、workspace/provisioner 流、团队上下文注入（`TeamContext`）、`SteerCommand`、任务/提醒/反应等会话内功能。

### Phase 2（Held Draft）——按简化形态实现

- 服务端 held_action 表 / `ResolveHeldAction` / 超时扫描**未实现**（LATEST.sql 显式 DROP）。
- 等价物：`PostMessage.base_version` 冲突检测 + `new_messages` 回传，决议在 agent 客户端完成（见"崩溃恢复"一节）。

### Phase 3（清理废弃 API）——大部分完成

- `SendCommand` / `SendCommandRequest` / `ExecutorKind` / `CommandSource` / inbox RPC 已删除（直接删除，未走弃用期）。
- `command.executor_kind` / `command.source_type` 列已 DROP。
- `WatchConversationEvents` **未实现**。

---

## 对 AX 四问的回应（按当前实现）

| AX 问题 | 我们的回应 |
|---------|-----------|
| What does the agent see? | `ListChannelUpdates` 发现 + `message check` / `message read` 增量拉取（`after_version`）；`ChatMessage` 携带 `sender_type` / `room_version` / `mentions` / thread / reactions / task 上下文 |
| What state does it carry? | 持久化 per-channel 游标（`agent_channel_cursor`）+ `AckProcessedVersion`；唤醒仅是 hint，游标是真相源 |
| What can it recover from? | 断线重连：在途 command 标记 FAILED、游标重发现；回复冲突：`PostMessage` 返回 `new_messages` 供改写/照发/放弃；`GetCommandContext` 恢复执行历史 |
| What is it allowed to decide? | 回复 / 执行工具 / 追问 / 沉默（"silence is valid" 引导语 + drain 会话最小间隔 `minSessionGap` 硬刹车）；`sender_type=SYSTEM` 让程序化消息走同一管线 |

---

## 设计决策记录（最终态）

| 决策 | 选择 | 理由/结果 |
|------|------|-----------|
| 是否新建 `RoomMessage` 类型 | 否，扩展 `ChatMessage` | 已按此落地（`sender_type=9`、`room_version=10`、…） |
| 消息拉取机制 | `ListChannelUpdates` 发现 + `ListConversationMessages(after_version)` 增量，而非流上专用 `PullMessages` | 复用用户端同一读取管线；agent 与前端共享语义 |
| `command.executor_kind`/`source_type` 列 | 已直接 DROP（未按 Phase 1 保留） | 历史审计价值有限，迁移更干净 |
| `SendCommand` RPC 处理 | 已删除（未走 deprecated 保留期） | 前端调用链同期移除，无需兼容窗口 |
| Held Draft 何时引入 | 未建 held_action 表；以 `PostMessage.base_version` 客户端决议实现 | 会话场景以 1:1/DM 为主，服务端暂存/超时收益低 |
| SHELL executor 删除时机 | 已删除 | 仅保留 ACP（stdio + acp2 thread）与内置 pi 运行时 |
| `conversation.version` vs `room_version` 命名 | DB 列名 `version`，proto 字段名 `room_version` | 已按此落地 |
| 权限决策流 | 移除 `permission_decision`/`RespondPermission`，改为自动授予 + 引导语约束 | 降低在途 turn 的阻塞面 |