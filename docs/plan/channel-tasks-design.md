# Channel Tasks 设计与实现方案

> 状态:2026-09-06 已对照当前代码核对更新。主要变化:功能已全部落地且与原设计有偏差——状态迁移放宽为任意频道成员可设任意状态(UI 为任务 thread 头部的状态下拉),新增 AssignTask/CloseTask/ListTaskCounts RPC 与团队指派,prompt/前端实现细节已按现状重写。

## Context

当前 channel/DM 聊天只有「消息」一种实体:用户发消息、agent 通过 drain loop 拉取并回复、thread 提供讨论上下文。多 agent 协作时缺少「工作单元」的概念——没有显式的认领、状态流转、人工审核环节,导致重复劳动、无法追踪进度、无法区分「需要行动的工作」与「普通对话」。

本方案在现有「消息即源」的模型上引入 **Task**:task 就是一条 top-level 消息附带任务元数据(per-channel 编号、状态、负责人),其 thread 作为讨论/审核通道。复用已有的 `chat_message` / `conversation.version` / `thread_participant` / `agent_channel_cursor` 基础设施,新增面很小:1 张表、1 个 store 文件、1 组 proto、1 组 agent CLI、1 个前端面板。

任务状态流:`TODO → IN_PROGRESS → IN_REVIEW → DONE`。用户以 As Task 发送/转换消息创建任务;agent 认领(自动转 IN_PROGRESS)、在 thread 内推进工作、转 IN_REVIEW 等人工审核、识别 thread 内审核通过后转 DONE。

## 关键设计决策(已与用户确认)

1. **创建路径**:发送时 As Task 开关 + 事后转换已有 top-level 消息 + agent 创建子任务,三者都支持。
2. **审核通过**:agent 识别 thread 内人类的 approval 语义("looks good" / "merge it")后自行 `task done`(CLI 把状态设为 DONE);同时任务 thread 头部提供状态/负责人下拉控件,任何频道成员可直接改状态(含 DONE),两者并存。
3. **事件呈现**:任务事件作为独立的 `sender_type=SYSTEM` 消息行插入聊天流(如 "📋 Alice converted a message to task #3")。
4. **认领语义**:认领自动 `TODO→IN_PROGRESS`;支持 unclaim 退回 TODO;DONE 终态不可 unclaim(`UpdateTaskStatus` 仍可把任务改回未完成态,清空 `completed_at`)。
5. **编号作用域**:按频道独立递增(`[task #3]`)。
6. **前端**:行内 `[task #N status=...]` 徽标 + 系统通知行 + 频道 Tasks 面板。
7. **转换权限**:任何频道成员(用户或 agent)可转换已有 top-level 消息为任务。
8. **claim 不主动通知其他 agent**:认领只产生系统通知行(系统消息不唤醒 agent,见下)。其他 agent 在下次自然 drain 时通过 `task list` / `message read` 看到任务已被认领,避免认领风暴。
9. **agent 任务发现**:drain 流程显式增加一步 `task list --status TODO`,因为 `message read` 只返回 cursor 之后的增量,已 ack 过的旧任务需要 `task list` 主动发现。(当前实现中 `task list` 是 init prompt 的固定 step 4。)
10. **用户可直接关闭任务**:任务 thread 标题栏(由 `ThreadTaskControls` 组件渲染)提供 **状态 + 负责人两个下拉**,任何频道成员可把任意任务置为任意状态(含 DONE,幂等语义由 `UpdateTaskStatus` 承担);后端另有独立的 `CloseTask` RPC(任意非 DONE → DONE、幂等、不重复发系统通知),当前前端未接线(UI 走 `updateTaskStatus`)。审核流(review→done)仍由 agent 自行 `task done`,两者并存。

## 数据模型

### `conversation` 表新增列
`backend/manager/migration/migration/LATEST.sql`(threads 块之后的 Tasks 块,现位于 LATEST.sql 的 task/reminder 区):

```sql
ALTER TABLE conversation ADD COLUMN IF NOT EXISTS next_task_number INTEGER NOT NULL DEFAULT 1;
```

per-channel 任务号原子递增:在任务创建事务内
`UPDATE conversation SET next_task_number = next_task_number + 1 WHERE id=$1 RETURNING next_task_number - 1`
返回分配到的编号(回滚时序列也回滚,编号连续)。该值只在任务事务内部使用,**没有**暴露到 `store.ConversationMessage` / 列举 conversation 的 SELECT 里(原设计中的这一步未采用)。

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

> 落地后表还增加了三列(`migration/migration/1.1/0022##task-assignee.sql` 与 `0026##agent-team.sql`):`assignee_type SMALLINT`(1=user、2=agent、3=team,NULL=未指派)、`assignee_user_id INTEGER REFERENCES principal(id)`(user 负责人,仅展示用途)、`assignee_team_id UUID REFERENCES agent_team(id)`,以及部分唯一索引 `idx_task_team_active`(同一团队同时只能被指派到一个未完成任务)。

## 系统通知不唤醒 agent

`backend/manager/store/agent_channel_cursor.go` 的 `agentRelevantMessageCondition` 带 `AND m.sender_type <> 3`(3=SenderTypeSystem)。系统通知仍 bump `conversation.version`(供用户 poll 拉取),但既不进入 agent 的 relevant 集,插入时也不调用 `notifyConversationAgents`。用户未读数仍计入系统通知(用户希望看到 "已转换为任务")。

## Proto (`proto/v1/v1/command.proto`)

沿用了 `SENDER_TYPE_*` / `TASK_STATUS_*` 前缀命名风格。

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
  string assignee_name = 3;       // 空=未指派
  string assignee_resource_id = 4;
  int32 assignee_type = 5;        // 1=user 2=agent 3=team,0=未指派(后续扩展)
  string assignee_team_id = 6;    // "agentTeams/{id}"
  string assignee_team_name = 7;
  string assignee_team_leader_name = 8;
}
```

- `ChatMessage` 的 `TaskInfo task = 16;`(只读 join 输出,由 ListConversationMessages/ListThreadMessages/GetChatMessage 对 root 消息填充)。
- `SendMessageRequest` 的 `bool as_task = 6;`(thread_root 必须为空)。
- 落地的 RPC(均注册在 `CommandService`):`ConvertMessageToTask`、`ListTasks`(分页 `page_size`/`page_token`)、`ListTaskCounts`、`CreateTask`、`ClaimTask`、`UnclaimTask`、`UpdateTaskStatus`、`AssignTask`(`member_type` 1=user/2=agent/3=team)、`CloseTask`。
- 权限注解:`ConvertMessageToTask`/`UpdateTaskStatus`/`AssignTask`/`CloseTask` 为 IAM + `laelia.conversations.send`(用户与 agent 都可调用);`ListTasks`/`ListTaskCounts` 为 IAM + `laelia.conversations.read`;`CreateTask`/`ClaimTask`/`UnclaimTask` 无 `auth_method`(agent-only,同 `PostMessage`)。
- proto 中没有 `google.api.http` 自定义路由注解——客户端走 ConnectRPC 默认路由(如 `POST /v1.CommandService/ClaimTask`),原设计中列出的 REST 风格路径未采用。

改完 `cd proto && buf format -w proto && buf lint proto && buf generate`。

## Store 层

**`backend/manager/store/task.go`**(已落地,函数名与原设计略有出入):`TaskStatus*` int16 常量、sentinel 错误(`ErrTaskNotFound`/`ErrTaskAlreadyExists`/`ErrTaskNotClaimable`/`ErrTaskNotOwner`/`ErrTaskInvalidTransition`/`ErrTaskAssigneeNotMember`/`ErrTaskTeamAlreadyAssigned`)、`TaskInfo` 结构(含 team 字段)、CRUD:

- `CreateTaskMessageBumpVersion(ctx, msg *ChatMessage) (*ChatMessage, int64, error)`(原设计名 `CreateTaskTx`)— 事务:bump `version` + `next_task_number` → 插 chat_message → 插 task 行(status=TODO)→ 返回带 TaskInfo 的消息与新 version,并 `roomNotifier.NotifyConversation`。`SendMessage(as_task)` 与 agent `CreateTask` 共用此函数,仅 principal/sender_type 不同。
- `ConvertMessageToTask(ctx, msgID, convID)`(原设计名 `ConvertMessageToTaskTx`)— 校验已有 task 行(`ErrTaskAlreadyExists`,唯一 PK 兜底并发)→ 事务 bump 编号 → 插 task 行(status=TODO)。chat_message 不变。
- `ClaimTask(ctx, msgID, convID, agentID)` — 原子 `UPDATE task SET status=IN_PROGRESS, assignee_agent_id=$, assignee_type=2 WHERE ... AND status=TODO AND assignee_agent_id IS NULL`;race-free,失败返回 `ErrTaskNotClaimable`。
- `ClaimTeamTask(ctx, msgID, convID, agentID)` — 团队指派任务的认领路径:仅指派团队的 **leader** 可认领(成功后 `assignee_type` 保持 3、leader 写入 `assignee_agent_id`)。
- `UnclaimTask(ctx, msgID, agentID)` — `WHERE assignee_agent_id=$agent AND status=IN_PROGRESS` → 置 TODO、清 assignee;DONE 不可 unclaim。
- `UpdateTaskStatus(ctx, msgID, target int16)` — **不校验 assignee、不限制迁移方向**(原设计仅允许 assignee 顺推);DONE 写 `completed_at`,移出 DONE 清空之。
- `AssignTask(ctx, msgID, convID, memberType, memberID)` — 指派给 user(仅展示)/agent/团队;团队路径校验 `TeamHasActiveTask`(重复指派返回 `ErrTaskTeamAlreadyAssigned`),user/agent 路径校验目标是会话成员。
- `CloseTask(ctx, msgID, convID) (msg, changed, err)` — 任意非 DONE → DONE(写 `completed_at`),幂等(`changed=false`),是用户侧关闭路径的兜底 RPC。
- `GetTaskMessage(ctx, msgID)`、`ListTasks(ctx, convID, statusFilter, pageSize, pageToken)`(OFFSET 分页,task_number 倒序)、`ListTaskCounts(ctx, convID)`(四状态计数,供面板汇总)。
- `fillTaskInfo(ctx, msgs)` — 仿 `fillThreadReplyCounts` 的分组查询,把 task 元数据(含团队 join)贴到 root 消息上;在 `store/chat_message.go` 的 `ListConversationMessages`、`ListThreadMessages`/`GetChatMessage`(root)末尾调用。

`conversation.next_task_number` 没有进入 `ConversationMessage` 结构或 conversation 列举查询(见"数据模型"一节的说明)。

## Service 层

**`backend/manager/api/v1/task.go`**(已落地,handler 集合比原设计多):

- `SendMessage`(`api/v1/channel_message.go`)扩展:`req.AsTask` 且 `thread_root` 非空时拒绝;走 `CreateTaskMessageBumpVersion` 创建 TODO 任务并发系统通知 "📋 {user} created task #N"。
- `ConvertMessageToTask`:校验 root + 成员(IAM 拦截器 + handler 内 `IsThreadRoot` 等)→ `ConvertMessageToTask` → 系统通知;**不** push 唤醒 agent(新任务靠 drain 时的 `task list` 发现)→ `GenerateActivityForMessage` 生成 TASK 活动。
- `ClaimTask`:agent context → 先读当前任务:团队指派任务走 `ClaimTeamTask`(仅 leader),否则 `ClaimTask`;成功后 `AddThreadParticipants(msgID, [agentID])` 订阅 thread;团队任务还会把团队指令消息重发进 thread;失败返回 `CodeFailedPrecondition`。
- `UnclaimTask`:agent context → `UnclaimTask` → 系统通知。
- `UpdateTaskStatus`:**任何频道成员**可调用(IAM `laelia.conversations.send`);无迁移限制;IN_REVIEW/DONE 时发系统通知("👀 ready for review"/"✅ completed")。
- `CreateTask`:agent context → `CreateTaskMessageBumpVersion`(sender_type=AGENT)→ **不**推进创建者 cursor,`notifyConversationAgents` 唤醒包括创建者在内的全部 agent 成员(创建者自己的子任务也要能被它发现/认领)→ 系统通知 → `GenerateActivityForMessage`。
- `AssignTask`:任意频道成员;团队指派时 `afterTeamAssign` 自动把团队全部 agent 加入会话、订阅 task thread、seed cursor、以 **leader 的 agent 身份**写一条 `sender_type=AGENT` 的团队指令消息(SYSTEM 行不唤醒 agent,故不用 SYSTEM)并 `notifyConversationAgents`。
- `ListTasks`/`ListTaskCounts`:成员校验(IAM read)→ 返回任务列表/计数。
- `CloseTask`:任意频道成员;幂等,仅在实际变更时发系统通知。
- **系统通知辅助** `postTaskSystemNotification(ctx, convID, content)`:用 `CreateChatMessageBumpVersion` 写 `sender_type=SYSTEM`、`principal_id=1`(系统 bot,已 seed)的 top-level 行,**不**调用 `notifyConversationAgents`。

## Agent CLI / Daemon / chattools / Prompt

- **CLI** `backend/agent/cmd/task.go`(已落地,地址形态为 `<address>:<message-id>` 消息句柄):
  ```
  laelia-machine task list <address> [--status S]... [--page-token T]
  laelia-machine task claim <message-handle>
  laelia-machine task unclaim <message-handle>
  laelia-machine task review <message-handle>     # → in_review
  laelia-machine task done <message-handle>       # → done
  laelia-machine task create <address> --content <text|->
  ```
- **Daemon** `backend/agent/daemon/server.go` 路由为 `/task/list`、`/task/claim`、`/task/unclaim`、`/task/update`(review/done 共用 update 路由,由参数区分)、`/task/create`。
- **chattools** `backend/agent/chattools/chattools_task.go` 提供 `ListTasks`/`ClaimTask`/`UnclaimTask`/`UpdateTaskStatus`/`CreateTask`,调用 `commandServiceClient` 对应方法。`task list` 每行:`<address>:<message-id>  #N  status=TODO|IN_PROGRESS|IN_REVIEW|DONE  assignee=<name|none>` + 内容。
- **Prompt**:
  - `backend/agent/executor/prompt/communication.md` 命令表含 `task *` 命令与 "Tasks" 小节:状态流、原子认领("claim 失败不要重试,转其他任务")、**HARD RULE:任务相关消息一律发进任务 thread**、决策规则(TODO 未指派 → 不要认领;指派给自己 → claim;指派给团队 → 仅 leader 认领并拆分子任务)、转 in_review 后在 thread 等人工 approval 再 `task done`、DONE 终态。
  - `AgentFirstPromptBody`(`executor/prompt.go`)已重构为 **turn batch 驱动**的 0–8 步流程(batch 头直接带各频道的 `<address>` 与 `processed_version`);step 4 为任务处理:对带 `[task #N status=TODO]` 的消息按上述规则 claim/推进,对 IN_REVIEW 的自身任务在 thread 找 approval 后 `task done`;`task list` 是每频道的固定步骤。

## 前端

- **Composer As Task 开关**:落地在 `frontend/src/hooks/use-chat-composer.ts`(`taskEnabled` 选项 + `asTask` 状态)+ `frontend/src/components/chat/chat-composer.tsx` 的切换按钮,仅在频道 composer 出现;发送时把 `asTask` 传入 `sendChannelMessage(channelId, text, ..., asTask)`。
- **Store**:`frontend/src/stores/channel.ts` 的 `sendChannelMessage` 带 `asTask?: boolean`,`create(SendMessageRequestSchema, { ..., asTask: asTask ?? false })`。
- **UI 类型**:`ChatMessageUI` 定义在 `frontend/src/stores/ui-models.ts`(原设计的 `stores/types.ts` 位置已迁移),`task?: { taskNumber, status, assigneeName?, assigneeResourceId?, assigneeType? }`;`frontend/src/stores/chat-helpers.ts` 的 `toUiMessage` 负责映射。
- **MessageRow**(`frontend/src/components/chat/message-row.tsx`):
  - 任务徽标:`<TaskStatusBadge task={msg.task} />`(组件在 `task-status-badge.tsx`,变体映射在 `frontend/src/lib/task-status.ts`),显示 `#N · 状态`。
  - 系统通知行:`msg.senderType === SenderType.SYSTEM` 时渲染为居中、低对比的文本行(无气泡/头像)。
- **Tasks 面板**:`frontend/src/components/chat/tasks-panel.tsx` + `frontend/src/stores/task.ts`(`TaskSlice`):分页加载(`loadTasks`/`loadMoreTasks`,task_number 倒序)、`ListTaskCounts` 计数(`taskCountsByConv`)、`convertMessageToTask`、`updateTaskStatus`、`assignTask`(支持指派给用户/agent/团队,团队选项来自 `hooks/use-agent-teams.ts`)。
- **Thread 标题栏任务控件**(`thread-header.tsx` + `thread-task-controls.tsx`):仅 `rootMsg.task` 存在且非 `readOnly` 时渲染 **状态下拉 + 负责人下拉**(负责人候选 = 频道成员 + 团队);状态变更走 store `updateTaskStatus`(任意状态,含 DONE 关闭),负责人变更走 `assignTask`;无二次确认弹窗。后端保留的 `CloseTask` RPC 当前未接前端。

## 状态迁移与权限

落地后的实际矩阵(与原设计的差异:`review`/`done` 不再限定 assignee,任意成员可设任意状态;新增指派与团队认领):

| 迁移 | 操作者 | SQL guard | 失败码 |
|---|---|---|---|
| (创建) → TODO | 任何成员(user/agent) | root 消息 + 本频道 + 无现存 task | FailedPrecondition |
| claim: TODO→IN_PROGRESS | 任何 agent 成员 | `status=TODO AND assignee_agent_id IS NULL`(race-free) | FailedPrecondition |
| claim(团队任务): TODO→IN_PROGRESS | 仅指派团队的 leader(`ClaimTeamTask`) | `status=TODO AND assignee_team_id` 的 leader=caller | FailedPrecondition |
| unclaim: IN_PROGRESS→TODO | 当前 assignee | `assignee_agent_id=caller AND status=IN_PROGRESS` | FailedPrecondition |
| 任意状态互转(`UpdateTaskStatus`) | 任何频道成员 | 无(状态合法即可;DONE 写 `completed_at`,移出清空) | InvalidArgument/NotFound |
| assign(指派 user/agent) | 任何频道成员 | 目标是会话成员 | InvalidArgument |
| assign(指派团队) | 任何频道成员 | 团队无未完成任务(`TeamHasActiveTask`) | FailedPrecondition |
| close(`CloseTask`) | 任何频道成员 | `status <> DONE`(幂等) | NotFound(无 task 行) |

`CreateTask`/`ClaimTask`/`UnclaimTask` 仅 agent(无 `auth_method` 注解,handler 用 `GetAgentFromContext` 取 caller);`UpdateTaskStatus`/`CloseTask`/`AssignTask`/`ConvertMessageToTask` 走 IAM 拦截器的 `laelia.conversations.send`,用户与 agent 均可调用。

## 实现顺序(已全部落地)

1. Proto:`command.proto` 的枚举/消息/RPC(Tasks + Reminders + TeamContext)→ `buf generate` 已重新生成 `backend/generated-go/v1/` 与 `frontend/src/types/proto-es/v1/`。
2. Migration:`migration/migration/LATEST.sql` 加 `conversation.next_task_number` 与 `task` 表(threads 块之后);增量文件含 `migration/migration/1.1/0022##task-assignee.sql`、`0026##agent-team.sql`。
3. Store:`store/task.go` + `store/agent_channel_cursor.go` 排除系统消息 + `store/chat_message.go` 接 `fillTaskInfo`。
4. Service:`api/v1/task.go` + `api/v1/channel_message.go`(`SendMessage.as_task`) + `postTaskSystemNotification` 辅助。
5. Agent:`cmd/task.go` + `daemon/server.go` 路由 + `chattools/chattools_task.go` + `prompt/communication.md` 与 `prompt.go`。
6. 前端:composer 开关(use-chat-composer)+ store 签名 + 类型/映射 + MessageRow 徽标与系统行 + tasks-panel + lib/task-status + thread 标题栏任务控件(`ThreadTaskControls` 状态/负责人下拉;`CloseTask` RPC 已实现但前端未接)。
7. 全流程格式化/lint:`gofmt`、`golangci-lint run --allow-parallel-runners`(反复至无 issue)、`pnpm --dir frontend biome:check && lint --fix && type-check`。

## 验证

- **Migration 测试**:`LAELIA_RUN_MIGRATION_TESTS=1 LAELIA_TEST_PG_URL=<url> go test ./backend/manager/migration -count=1`(该套件受环境变量门控)确认 schema 幂等应用通过。
- **后端构建**:`go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`。
- **Lint**:`golangci-lint run --allow-parallel-runners` 反复至 0 issue;前端 `pnpm --dir frontend biome:check`、`lint --fix`、`type-check`、`test`。
- **端到端手测**:
  1. 启动 manager(`go run ./backend/manager/bin/server/main.go --port 8181 --debug`)+ 前端 `pnpm --dir frontend dev` + 一个 agent(`laelia-machine daemon`)。
  2. 建频道、加入 agent;用户在 composer 勾选 As Task 发 "Fix the login bug" → 频道出现任务消息(行内 `[task #1 status=TODO]`)+ 系统通知行 "📋 ... created task #1"。
  3. Tasks 面板列出该任务;agent drain 后 `task list` 见任务,`task claim <msg>` → 行内变 `[task #1 status=IN_PROGRESS]` + 系统通知 "🙋 ... claimed task #1"。
  4. 在 task 的 thread 里 agent 推进工作,`task review` → `IN_REVIEW` + 系统通知("👀 ... ready for review")。
  5. 用户在 thread 回复 "looks good" → agent 被 thread 订阅唤醒,`thread read` 见 approval,`task done` → `DONE` + "✅ ... completed task #1"。
  6. 验证并发认领:两 agent 同时 `task claim` 同一 TODO 任务,只一个成功,另一个返回 `FailedPrecondition` 并按 prompt 转而处理其他任务。
  7. 验证系统通知不唤醒 agent:转换/认领产生的系统消息不让 agent 空转 `message check` 返回空。
  8. 事后转换:对一条已存在的普通 top-level 消息用 `convertToTask` → 得到编号,系统通知;thread 回复不受影响。
  9. agent 子任务:`task create` 发新任务消息,不自动认领,可被其他 agent claim。
  10. thread 头部任务控件:用状态下拉直接把任务置 DONE(或改回 TODO),行内徽标与面板计数同步;指派下拉可把任务给用户/agent/团队。