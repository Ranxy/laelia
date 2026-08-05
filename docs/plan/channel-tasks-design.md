# Channel Tasks 设计与实现方案

## Context

当前 channel/DM 聊天只有「消息」一种实体:用户发消息、agent 通过 drain loop 拉取并回复、thread 提供讨论上下文。多 agent 协作时缺少「工作单元」的概念——没有显式的认领、状态流转、人工审核环节,导致重复劳动、无法追踪进度、无法区分「需要行动的工作」与「普通对话」。

本方案在现有「消息即源」的模型上引入 **Task**:task 就是一条 top-level 消息附带任务元数据(per-channel 编号、状态、负责人),其 thread 作为讨论/审核通道。复用已有的 `chat_message` / `conversation.version` / `thread_participant` / `agent_channel_cursor` 基础设施,新增面很小:1 张表、1 个 store 文件、1 组 proto、1 组 agent CLI、1 个前端面板。

任务状态流:`TODO → IN_PROGRESS → IN_REVIEW → DONE`。用户以 As Task 发送/转换消息创建任务;agent 认领(自动转 IN_PROGRESS)、在 thread 内推进工作、转 IN_REVIEW 等人工审核、识别 thread 内审核通过后转 DONE。

## 关键设计决策(已与用户确认)

1. **创建路径**:发送时 As Task 开关 + 事后转换已有 top-level 消息 + agent 创建子任务,三者都支持。
2. **审核通过**:agent 识别 thread 内人类的 approval 语义("looks good" / "merge it")后自行 `task done`,不提供 UI 按钮。
3. **事件呈现**:任务事件作为独立的 `sender_type=SYSTEM` 消息行插入聊天流(如 "📋 Alice converted a message to task #3")。
4. **认领语义**:认领自动 `TODO→IN_PROGRESS`;支持 unclaim 退回 TODO;DONE 终态不可取消。
5. **编号作用域**:按频道独立递增(`[task #3]`)。
6. **前端**:行内 `[task #N status=...]` 徽标 + 系统通知行 + 频道 Tasks 面板。
7. **转换权限**:任何频道成员(用户或 agent)可转换已有 top-level 消息为任务。
8. **claim 不主动通知其他 agent**:认领只产生系统通知行(系统消息不唤醒 agent,见下)。其他 agent 在下次自然 drain 时通过 `task list` / `message read` 看到任务已被认领,避免认领风暴。
9. **agent 任务发现**:drain 流程显式增加一步 `task list --status TODO`,因为 `message read` 只返回 cursor 之后的增量,已 ack 过的旧任务需要 `task list` 主动发现。

## 数据模型

### `conversation` 表新增列
`backend/manager/migration/latest.sql`(在 threads 块之后、文件末尾新增 Tasks 块):

```sql
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS next_task_number INTEGER NOT NULL DEFAULT 1;
```

per-channel 任务号原子递增:在任务创建事务内
`UPDATE conversation SET next_task_number = next_task_number + 1 WHERE id=$1 RETURNING next_task_number - 1`
返回分配到的编号(回滚时序列也回滚,编号连续)。

### 新增 `task` 表

```sql
CREATE TABLE IF NOT EXISTS task (
    message_id UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
    task_number INTEGER NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1,            -- 1=TODO 2=IN_PROGRESS 3=IN_REVIEW 4=DONE
    assignee_agent_id INTEGER REFERENCES agent(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT task_status_check CHECK (status IN (1,2,3,4))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_conversation_number ON task(conversation_id, task_number);
CREATE INDEX IF NOT EXISTS idx_task_conversation_status ON task(conversation_id, status);
```

`message_id` 即 PK 又是 FK——task 就是这条消息,删除消息级联清理 task。`conversation_id` 冗余便于按频道列举。

### 系统通知不唤醒 agent
`backend/manager/store/agent_channel_cursor.go:43-47` 的 `agentRelevantMessageCondition` 增加 `AND m.sender_type <> 3`(3=SenderTypeSystem)。系统通知仍 bump `conversation.version`(供用户 poll 拉取),但既不进入 agent 的 relevant 集,插入时也不调用 `notifyConversationAgents`。用户未读数仍计入系统通知(用户希望看到 "已转换为任务")。

## Proto (`proto/v1/v1/command.proto`)

沿用现有前缀命名风格(`SENDER_TYPE_*` / `COMMAND_STATUS_*`),buf lint 要求枚举值前缀匹配枚举名。

```proto
enum TaskStatus {
  TASK_STATUS_UNSPECIFIED = 0;
  TASK_STATUS_TODO = 1;
  TASK_STATUS_IN_PROGRESS = 2;
  TASK_STATUS_IN_REVIEW = 3;
  TASK_STATUS_DONE = 4;
}
message TaskInfo {
  int32 task_number = 1;
  TaskStatus status = 2;
  string assignee_name = 3;       // 空=未认领
  string assignee_resource_id = 4;
}
```

- `ChatMessage` 新增 `TaskInfo task = 16;`(只读 join 输出,由 ListConversationMessages/ListThreadMessages 填充)。
- `SendMessageRequest` 新增 `bool as_task = 6;`(thread_root 必须为空)。
- 新增 `ConvertMessageToTaskRequest/Response`、`CreateTaskRequest/Response`、`ListTasksRequest/Response`、`ClaimTaskRequest/Response`、`UnclaimTaskRequest/Response`、`UpdateTaskStatusRequest/Response`。请求统一以 `message`("conversations/{c}/messages/{m}")或 `conversation` 作为资源名参数;`ListTasks` 带 `repeated TaskStatus status_filter`。
- `CommandService` 新增 6 个 RPC:
  - `ConvertMessageToTask`、`ListTasks`:IAM + `laelia.conversations.send`/`.read` 权限(用户与 agent 都可调用)。
  - `CreateTask`、`ClaimTask`、`UnclaimTask`、`UpdateTaskStatus`:agent-only,无 `auth_method`(同 `PostMessage`)。
- HTTP 路径:`POST /v1/{message=conversations/*/messages/*}:convertToTask`、`GET /v1/{conversation=conversations/*}/tasks`、`POST /v1/{conversation=conversations/*}:createTask`、`POST /v1/{message=...}:claimTask` / `:unclaimTask` / `:updateTaskStatus`。

改完 `cd proto && buf format -w proto && buf lint proto && buf generate`。

## Store 层

**新增 `backend/manager/store/task.go`**:`Task` 结构、`TaskStatus*` int16 常量(对照 `store/command.go` 的风格)、`TaskInfo` 结构、CRUD:

- `CreateTaskTx(ctx, msg *ChatMessage, agentID *int)` — 事务:bump `next_task_number` → 插 chat_message(复用 `CreateChatMessageBumpVersion`)→ 插 task 行(status=TODO)→ 返回带 TaskInfo 的消息。`SendMessage(as_task)` 与 agent `CreateTask` 共用此函数,仅 principal/sender_type 不同。
- `ConvertMessageToTaskTx(ctx, msgID, convID)` — 校验 `IsThreadRoot` 且无现存 task 行;事务 bump 编号 → 插 task 行(status=TODO)。chat_message 不变。
- `ClaimTask(ctx, msgID, convID, agentID)` — 原子 `UPDATE task SET status=2,assignee_agent_id=$2,updated_at=now() WHERE message_id=$3 AND conversation_id=$4 AND status=1 AND assignee_agent_id IS NULL RETURNING ...`;`sql.ErrNoRows` → `ErrTaskNotClaimable`(已被人认领)。
- `UnclaimTask(ctx, msgID, agentID)` — `WHERE status=2 AND assignee_agent_id=$agent` → 置 status=1、assignee=NULL。
- `UpdateTaskStatus(ctx, msgID, agentID, target)` — 校验 assignee=caller 且当前状态允许迁移;`in_progress→in_review`、`in_review→done`(done 时写 `completed_at`)。
- `ListTasks(ctx, convID, statusFilter)` — join agent 取 assignee 名,返回带 TaskInfo 的 root 消息列表。
- `fillTaskInfo(ctx, msgs)` — 仿 `fillThreadReplyCounts` 的分组查询,把 task 元数据贴到 root 消息上。在 `store/chat_message.go` 的 `ListConversationMessages` 与 `ListThreadMessages`(root)末尾调用。

`ConversationMessage` 结构(`store/conversation.go:13-23`)新增 `NextTaskNumber int32`;所有列举 conversation 列的 SELECT(约 5 处:`GetConversation`、`CreateChannel`、`GetOrCreateDirectConversation`、`ListUserConversations`、`ListUserConversationsWithUnread`)补上该列。

## Service 层

**新增 `backend/manager/api/v1/task.go`**:6 个 RPC handler。

- `SendMessage`(`channel.go:266`)扩展:若 `req.AsTask`,创建消息后调用 `CreateTaskTx` 置 TODO,发系统通知 "📋 {user} created task #N"。`thread_root` 非空时拒绝 `as_task`。
- `ConvertMessageToTask`:校验成员身份(用户用 `requireConversationMember`;agent 用 `IsConversationMember(MemberTypeAgent, ...)`,建议提取 `requireConversationMemberAny` 复用)→ `ConvertMessageToTaskTx` → 系统通知 → 唤醒 agent(转换出的新任务是有待认领的工作,需 `notifyConversationAgents`)。
- `ClaimTask`:agent context → `ClaimTask` 原子 UPDATE。成功后 `AddThreadParticipants(msgID, [agentID])`(订阅 task 的 thread,审核回复才能唤醒该 agent)→ 系统通知 "🙋 {agent} claimed task #N"。失败返回 `CodeFailedPrecondition`。
- `UnclaimTask`:校验 assignee=caller、非 DONE → `UnclaimTask` → 系统通知。
- `UpdateTaskStatus`:agent context;按 `task review`(→in_review)/ `task done`(→done)分支;校验 assignee=caller 与迁移合法性;done 时系统通知 "✅ Task #N done"。
- `CreateTask`:agent context → `CreateTaskTx`(sender_type=AGENT)→ 推进 agent 自身 cursor(同 PostMessage)→ `notifyConversationAgents` 唤醒其他 agent → 系统通知 "📋 {agent} created task #N"。
- `ListTasks`:成员校验 → 返回任务列表。
- **系统通知辅助** `postTaskSystemNotification(ctx, convID, content)`:用 `CreateChatMessageBumpVersion` 写 `sender_type=SYSTEM`、`principal_id=1`(系统 bot,`latest.sql:111` 已 seed)的 top-level 行,**不**调用 `notifyConversationAgents`。

## Agent CLI / Daemon / chattools / Prompt

- **CLI** 新增 `backend/agent/cmd/task.go`(仿 `thread.go`):
  ```
  laelia-machine task list <conversation> [--status S]...
  laelia-machine task claim <message-name>
  laelia-machine task unclaim <message-name>
  laelia-machine task review <message-name>      # → in_review
  laelia-machine task done <message-name>        # → done
  laelia-machine task create <conversation> --content <text|-> [--attach <file-id>...]
  ```
- **Daemon** `backend/agent/daemon/server.go:152-163` 新增 `/task/{list,claim,unclaim,review,done,create}` 路由,`Request` 结构补 `Status`、`Message` 字段。
- **chattools** `backend/agent/chattools/chattools.go` 新增 `ListTasks`/`ClaimTask`/`UnclaimTask`/`UpdateTaskStatus`/`CreateTask`,调用对应 `commandServiceClient` 方法,按 `PostMessage`/`ListThreadUpdates` 的风格格式化输出。`ListTasks` 每行:`<message-name>  #N  status=TODO|IN_PROGRESS|IN_REVIEW|DONE  assignee=<name|none>` + 内容首行。
- **Prompt**:
  - `backend/agent/executor/prompt/communication.md` 命令表加 6 行 `task *` 命令,并新增 "### Tasks" 小节:状态流、认领/取消、决策规则("需 agent 采取回复外行动则先 claim;仅对话则不 claim")、在 task 的 **thread** 内推进工作、转 in_review 后盯 thread 等人工 approval 再 `task done`、DONE 终态。
  - `AgentFirstPromptBody`(`prompt.go:46-69`)在 step 3(`message read`)后插入一步:对带 `[task #N status=TODO]` 的消息按需 `task claim`;对带 IN_REVIEW 的自身任务 `thread read` 找 approval 后 `task done`;并提示每频道可 `task list --status TODO` 发现已 ack 过的旧任务。更新 step 2 注明订阅的 thread 可能是某 task 的讨论 thread。

## 前端

- **Composer As Task 开关**:`frontend/src/pages/dashboard/chat-conversation.tsx`(及 thread-panel 的 composer 不需要,只 top-level)加 `asTask` state + 切换按钮(复用附件/mention 控件风格),传入 `sendChannelMessage(channelId, text, mentions, attachments, asTask)`。
- **Store**:`frontend/src/stores/channel.ts:72-100` `sendChannelMessage` 增 `asTask?: boolean`,`create(SendMessageRequestSchema, { ..., as_task: asTask ?? false })`。
- **UI 类型**:`frontend/src/stores/types.ts` `ChatMessageUI` 加 `task?: { taskNumber: number; status: number; assigneeName?: string }`;`frontend/src/stores/chat-helpers.ts:10-24` `toUiMessage` 映射。
- **MessageRow**(`frontend/src/components/chat/message-row.tsx`):
  - 任务徽标:在 `CommandStatusBadge`(205-210)旁加 `<TaskStatusBadge task={msg.task} />`(新组件,仿 `command-status-badge.tsx` + 新 `frontend/src/lib/task-status.ts` 做 `taskStatusToVariant`/i18n 映射),显示 `#N · IN_PROGRESS`。
  - 系统通知行:`msg.senderType === 3` 时渲染为居中、低对比的文本行(无气泡/头像)。
- **Tasks 面板**:新 `frontend/src/components/chat/tasks-panel.tsx`(仿 `thread-panel.tsx` 的开合与 store 接线),调 `commandServiceClient.listTasks`,列出任务(徽标 + 负责人),提供对选中消息的 "Convert to Task" 动作(`convertMessageToTask`)。新增 `frontend/src/stores/task.ts` + `TaskSlice`。

## 状态迁移与权限

| 迁移 | 操作者 | SQL guard | 失败码 |
|---|---|---|---|
| (创建) → TODO | 任何成员(user/agent) | root 消息 + 本频道 + 无现存 task | FailedPrecondition |
| claim: TODO→IN_PROGRESS | 任何 agent 成员 | `status=1 AND assignee IS NULL` | FailedPrecondition |
| unclaim: IN_PROGRESS→TODO | 当前 assignee | `status=2 AND assignee=caller` | FailedPrecondition |
| review: IN_PROGRESS→IN_REVIEW | 当前 assignee | `status=2 AND assignee=caller` | FailedPrecondition |
| done: IN_REVIEW→DONE | 当前 assignee | `status=3 AND assignee=caller` | FailedPrecondition |

`UpdateTaskStatus`/`ClaimTask`/`UnclaimTask`/`CreateTask` 仅 agent;handler 用 `GetAgentFromContext` 取 caller,`assignee_agent_id` 不匹配返回 `CodePermissionDenied`。无面向用户的 done 按钮。

## 实现顺序

1. Proto:`command.proto` 加枚举/消息/RPC → `buf format && buf lint && buf generate`,确认 `backend/generated-go/v1/` 与 `frontend/src/types/proto-es/v1/` 重新生成。
2. Migration:`latest.sql` 加 `conversation.next_task_number` 与 `task` 表(置于 threads 块之后)。
3. Store:`store/task.go` 新文件 + `store/conversation.go` 补列 + `store/agent_channel_cursor.go` 排除系统消息 + `store/chat_message.go` 接 `fillTaskInfo`。
4. Service:`api/v1/task.go` 新文件 + `channel.go`(`SendMessage.as_task`)与 `command.go`(`storeToV1ChatMessage` 透出 `task`、`ListTasks` 的 `is_own`)微调 + `postTaskSystemNotification` 辅助。
5. Agent:`cmd/task.go` + `daemon/server.go` 路由 + `chattools/chattools.go` 函数 + `prompt/communication.md` 与 `prompt.go`。
6. 前端:composer 开关 + store 签名 + 类型/映射 + MessageRow 徽标与系统行 + tasks-panel + lib/task-status。
7. 全流程格式化/lint:`gofmt`、`golangci-lint run --allow-parallel-runners`(反复至无 issue)、`pnpm --dir frontend biome:check && lint --fix && type-check`。

## 验证

- **Migration 测试**:`go test ./backend/manager/migration -count=1` 确认 `latest.sql` 幂等应用通过。
- **后端构建**:`go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`。
- **Lint**:`golangci-lint run --allow-parallel-runners` 反复至 0 issue;前端 `pnpm --dir frontend biome:check`、`lint --fix`、`type-check`、`test`。
- **ACP 集成**(改动触及 agent 执行/CLI prompt):在装有本地 `opencode acp` 的机器跑 `LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`。
- **端到端手测**:
  1. 启动 manager(`go run ./backend/manager/bin/server/main.go --port 8181 --debug`)+ 前端 `pnpm --dir frontend dev` + 一个 agent(`laelia-machine daemon`)。
  2. 建频道、加入 agent;用户在 composer 勾选 As Task 发 "Fix the login bug" → 频道出现任务消息(行内 `[task #1 status=TODO]`)+ 系统通知行 "📋 ... created task #1"。
  3. Tasks 面板列出该任务;agent drain 后 `message read` 见任务,`task claim <msg>` → 行内变 `[task #1 status=IN_PROGRESS]` + 系统通知 "🙋 ... claimed task #1"。
  4. 在 task 的 thread 里 agent 推进工作,`task review` → `IN_REVIEW` + 系统通知。
  5. 用户在 thread 回复 "looks good" → agent 被 thread 订阅唤醒,`thread read` 见 approval,`task done` → `DONE` + "✅ task #1 done"。
  6. 验证并发认领:两 agent 同时 `task claim` 同一 TODO 任务,只一个成功,另一个返回 `FailedPrecondition` 并按 prompt 转而处理其他任务。
  7. 验证系统通知不唤醒 agent:转换/认领产生的系统消息不让 agent 空转 `message check` 返回空。
  8. 事后转换:对一条已存在的普通 top-level 消息用 `convertToTask` → 得到编号,系统通知;thread 回复不受影响。
  9. agent 子任务:`task create` 发新任务消息,不自动认领,可被其他 agent claim。