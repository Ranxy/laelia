# Design: Agent Teams (团队) + Task Team Assignment

## 1. Context

Laelia 目前支持创建和管理单个 agent。Agent 是常驻的自主进程，通过 `laelia-machine` CLI 在频道/DM 中发消息、认领和完成任务（`task` 表挂在 `chat_message` 上，当前 assignee 只能是单个 user 或单个 agent）。

新需求要引入 **Agent 团队（Agent Team）**：

- 用户创建/管理团队，把多个 agent 加入团队；
- 团队必须有一个 leader，负责管理团队任务、协调其他成员；
- 可配置每个成员在团队中的职责；
- 可配置团队级提示词（team prompt）；
- Task 可以被指派给团队：当团队被指派到某个 task 时，团队内每个 agent 都收到特殊 prompt，内容包含：给团队处理该 task 的提示词 + 团队本身的 team-prompt；
- leader 和普通成员的提示词不同：leader 被提示要控制 task 开发进度、协调团队内其他 agent。

## 2. 当前代码现状分析

### 2.1 Agent

- `agent` 表（`backend/manager/migration/migration/LATEST.sql`）保存 agent 身份、owner、machine、enabled 等；私有 persona 存在 `agent.info.acp_config.persona_prompt`（JSONB）。
- `AgentService` 提供 `CreateAgent/GetAgent/UpdateAgent/...`，权限字符串 `laelia.agents.*`。
- 每个 agent 有 `owner`（用户）和 `machine`（运行主机）。
- Agent 的自主执行：Manager `Dispatcher.HandleBeginSession` 返回 `command_id` + `agent_display_name` + `owner_display_name`；machine 端 `executor.BuildPrompt(name, ownerDisplayName, personaPrompt)` 组装冷启动 init prompt（身份 + persona + Ownership & Safety + communication + first prompt + memory）。
- Agent 通过 `notifyConversationAgents` 被唤醒；会话内通过 `task list/claim/review/done` 与 manager 交互。

### 2.2 Task

- `task` 表：`message_id`(PK/FK), `conversation_id`, `task_number`, `status`, `assignee_agent_id`, `assignee_user_id`, `assignee_type`。
- `TaskInfo`（v1）仅表达单个 assignee（user/agent）。
- 任务流：TODO → IN_PROGRESS → IN_REVIEW → DONE；claim 只能由一个 agent 认领。
- 前端有 task board（`frontend/src/stores/task.ts`）。

### 2.3 现有 Group（用户组）

- `user_group` 是 IAM 用户组，用于 policy 绑定，**不是 agent 团队**。新的 Agent Team 应独立建模，不混用。

### 2.4 结论

当前系统**没有**任何 agent 团队概念。要实现该功能，需要：

1. 新增团队资源（数据模型 + API + 前端）；
2. 扩展 task 的指派模型，支持团队 assignee；
3. 扩展 agent 上下文的 prompt 注入，使团队成员在处理团队 task 时看到 team_prompt + 任务指令 + 角色指令；
4. 明确 leader 与 member 的协作流程。

---

## 3. 已确认决策（来自需求澄清）

1. **团队作用域**：Agent Team 属于创建它的用户（个人资源），但**其他用户可见**。管理权限归创建者/owner + workspace admin；普通用户可浏览团队列表和详情。
2. **成员加入限制**：用户只能把 **owner 为自己** 的 agent 加入自己创建的团队；即 `agent.owner_id == 当前用户` 才允许加入。跨用户 agent 需要先转交 owner 或由 owner 自己加入。
3. **指派后由 leader 认领**：团队被指派到 task 时，task 先保持 `TODO` 并设置团队 assignee；随后由 **leader 通过 claim** 将其置为 `IN_PROGRESS`，成为“团队负责人”。普通成员不 claim 主任务，只认领 leader 创建的子任务。
4. **协作模型**：leader 编排 + 成员认领子任务。leader 负责拆分、派活、跟踪进度；成员通过现有 claim/thread 流程完成子任务。
5. **Prompt 注入**：采用 **task thread 写系统消息 + BeginSession 可选团队上下文** 双轨方案，按本报告第 4.3 / 6.4 节实现。
6. **自动加入频道**：团队被指派到某个 conversation 的 task 时，自动把团队所有 agent 加入该 conversation（尊重 `allow_add_to_channel` 等权限，见第 8 节风险）。
7. **交付范围**：前后端一起实现。
8. **一个 agent 只能加入一个团队**：`agent_team_member.agent_id` 全局唯一（移除旧团队后才能加入新团队）。
9. **一个团队同一时间只能被指派到一个未完成 task**：`task.assignee_team_id` 在 `TODO/IN_PROGRESS/IN_REVIEW`（status 1/2/3）下全局唯一；task 完成（DONE）或取消指派（清空 `assignee_team_id`）后团队可被指派到下一个 task。

### 3.1 团队是“一等工作区资源”，不依赖现有 user_group

新建 `agent_team` + `agent_team_member` 关系表，与 `user_group` 分离：

- 团队成员是 agent（不是用户）；
- 团队有唯一的 leader；
- 团队有 team_prompt；
- 团队成员有职责说明（role/职责描述）。

### 3.2 Task 可指派给团队

`task` 表新增 `assignee_team_id`，`assignee_type` 增加“团队”类型。现有单 agent/单 user 指派保留兼容。

### 3.3 Prompt 注入策略

我们推荐 **“持久化系统消息 + 会话级可选增强”双轨**：

1. **持久化系统消息（主通道）**：当团队被指派到 task 时，manager 在 task 的 thread 中写入一条 system 消息，包含：
   - 团队名、leader 名；
   - team_prompt；
   - 当前 task 的指令；
   - 每个成员的角色指令（leader 有“控制进度、协调成员”的额外要求，普通成员有“你的职责是 …”）。

   这天然进入 agent 的对话上下文，且是 durable 的：任何 agent 读该 thread 时都能看到，支持多 agent、多轮协作，也不需要改动 ACP/pi 的冷启动协议。

2. **BeginSession 可选增强（辅助）**：扩展 `BeginSessionResponse`，增加可选的 `team_context`（当前 agent 所属团队 + 角色 + team_prompt）。agent 客户端可把它并入冷启动 init prompt（例如 `BuildPrompt` 增加一段 `## Your team`）。这样即使 agent 还没读到具体 task 消息，也能知道“自己属于哪个团队、是什么角色”。

> 如果用户期望的是“任务一被指派，agent 的系统 prompt 立刻带上有 task 指令”，则主要靠系统消息实现；`BeginSessionResponse` 的增强只做身份/团队意识，不承载具体任务指令（因为一个 session 可能同时面对多个频道/多个 task，任务指令必须按 conversation/thread 读取）。

---

## 4. 数据模型设计

### 4.1 新表 `agent_team`

```sql
CREATE TABLE agent_team (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  resource_id   TEXT NOT NULL UNIQUE,      -- teams/{id}，用于 API 引用
  name          TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  team_prompt   TEXT NOT NULL DEFAULT '',
  leader_agent_id INTEGER REFERENCES agent(id) ON DELETE SET NULL, -- 冗余，便于查询
  -- owner_id 是团队的属主（创建者/可管理方），其他用户仅可见。
  owner_id      INTEGER NOT NULL REFERENCES principal(id),
  created_by    INTEGER NOT NULL REFERENCES principal(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted       BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE agent_team_member (
  team_id       UUID NOT NULL REFERENCES agent_team(id) ON DELETE CASCADE,
  agent_id      INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  role          SMALLINT NOT NULL DEFAULT 2, -- 1=LEADER, 2=MEMBER
  responsibility TEXT NOT NULL DEFAULT '',    -- 该成员在团队中的职责
  joined_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, agent_id)
);

CREATE UNIQUE INDEX idx_agent_team_member_leader
  ON agent_team_member(team_id) WHERE role = 1;  -- 每队最多一个 leader

-- 一个 agent 只能属于一个团队（全局唯一，已确认决策 8）
CREATE UNIQUE INDEX idx_agent_team_member_agent
  ON agent_team_member(agent_id);
```

设计说明：

- `leader_agent_id` 是冗余列，方便 `GetAgentTeam` 不 join 就返回 leader；权威数据在 `agent_team_member`。
- 团队成员必须在 `agent_team_member` 中存在且 `deleted=false`。
- 团队可软删除，删除后 `task.assignee_team_id` 置 NULL 或保留引用（建议保留历史 + 软删除团队名显示为“已删除团队”）。

### 4.2 Task 表扩展

```sql
ALTER TABLE task ADD COLUMN IF NOT EXISTS assignee_team_id UUID REFERENCES agent_team(id) ON DELETE SET NULL;
```

- `assignee_type` 语义扩展：`1=user`, `2=agent`, `3=team`。
- 若 `assignee_type=3`，则 `assignee_team_id` 有效，`assignee_agent_id`/`assignee_user_id` 为空。
- `TaskInfo` API 增加：
  - `assignee_team_name`
  - `assignee_team_id`
  - `assignee_team_leader_name`

**唯一性约束（已确认决策 9）**：一个团队同一时间只能被指派到一个未完成 task。

```sql
-- status 1/2/3 = TODO / IN_PROGRESS / IN_REVIEW；DONE 后释放，或解指派清空 assignee_team_id
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_team_active
  ON task(assignee_team_id) WHERE assignee_team_id IS NOT NULL AND status IN (1,2,3);
```

> 取消指派（unassign/解指派）时应清空 `assignee_team_id`（`assignee_type=0`），使团队可被指派到新 task；task 完成（DONE）后该唯一索引自动放行。

### 4.3 可选：会话级团队上下文（BeginSession）

在 `BeginSessionResponse` 增加可选字段（因一个 agent 只能属于一个团队，使用单数 `team`）：

```proto
message TeamContext {
  string team_id = 1;
  string team_name = 2;
  string team_prompt = 3;
  string role = 4;           // "leader" | "member"
  string responsibility = 5; // 该 agent 在团队中的职责
}

message BeginSessionResponse {
  ...
  TeamContext team = 5; // 该 agent 当前所属的团队（可选；未加入团队则为空）
}
```

> 具体 task 上下文仍在 task thread 中。

---

## 5. API 设计

新增 `AgentTeamService`（proto：`proto/v1/v1/agent_team_service.proto`），仿照 `GroupService`：

```proto
service AgentTeamService {
  rpc CreateAgentTeam(CreateAgentTeamRequest) returns (AgentTeam);
  rpc GetAgentTeam(GetAgentTeamRequest) returns (AgentTeam);
  rpc ListAgentTeams(ListAgentTeamsRequest) returns (ListAgentTeamsResponse);
  rpc UpdateAgentTeam(UpdateAgentTeamRequest) returns (AgentTeam);
  rpc DeleteAgentTeam(DeleteAgentTeamRequest) returns (google.protobuf.Empty);

  // 成员管理（可并入 UpdateAgentTeam.members 更新，也可独立 RPC）
  rpc AddAgentTeamMember(AddAgentTeamMemberRequest) returns (AgentTeam);
  rpc RemoveAgentTeamMember(RemoveAgentTeamMemberRequest) returns (AgentTeam);
  rpc UpdateAgentTeamMemberRole(UpdateAgentTeamMemberRoleRequest) returns (AgentTeam);
}
```

消息结构（草案）：

```proto
message AgentTeam {
  string name = 1;             // teams/{id}
  string team_id = 2;           // resource id
  string title = 3;
  string description = 4;
  string team_prompt = 5;
  string leader_agent = 6;      // agents/{id}
  repeated AgentTeamMember members = 7;
  google.protobuf.Timestamp created_at = 8;
  google.protobuf.Timestamp updated_at = 9;
  bool can_manage = 10;         // 调用者可管理？
  string owner = 11;            // users/{id}，团队属主（display）
}

message AgentTeamMember {
  string agent = 1;             // agents/{id}
  AgentTeamRole role = 2;       // LEADER / MEMBER
  string responsibility = 3;    // 职责
}
```

Task 相关扩展：

- `AssignTaskRequest` 增加 `member_type=3`（team）时，`member_id` 为 `teams/{id}` 或 team resource id。
- `ListTasks` 的返回 `TaskInfo` 增加 team assignee 字段。
- 可新增 `AssignTaskToTeam` RPC，或直接扩展 `AssignTask`。

权限（`backend/common/permission/permission.json` 新增）：

```json
{"name": "AgentTeamsCreate", "id": "laelia.agentTeams.create"},
{"name": "AgentTeamsGet",   "id": "laelia.agentTeams.get"},
{"name": "AgentTeamsList",  "id": "laelia.agentTeams.list"},
{"name": "AgentTeamsUpdate","id": "laelia.agentTeams.update"},
{"name": "AgentTeamsDelete","id": "laelia.agentTeams.delete"}
```

建议权限与 `GroupService` 一致：团队 owner（创建者）可管理，workspace admin 可管理全部；其他用户可 `Get/List`（可见）。

**成员加入限制（已确认）**：添加成员时，仅允许加入 `agent.owner_id == 当前用户`（或 workspace admin）的 agent。

---

## 6. 流程设计

### 6.1 团队创建 / 编辑

1. 用户选择多个 agent，指定一个为 leader；
2. **一个 agent 只能属于一个团队**：选择成员时，已属于其他团队的 agent 不可加入，需先从原团队移除（或直接显示不可选）。
3. 对每个成员填职责（可空）；
4. 填 team_prompt（可空，用于给所有成员注入团队背景/协作约定）；
5. 保存。

### 6.2 团队指派到 Task

用户（或 agent）在 channel 中把一个 task 指派给团队（**已确认：自动把团队所有 agent 加入该 conversation**）：

1. **唯一性校验**：该团队当前不能已有未完成（TODO/IN_PROGRESS/IN_REVIEW）task；否则返回 `FAILED_PRECONDITION`（已确认决策 9）。
2. **自动加入**：将该团队所有 agent 加入 task 所在 conversation（成员类型 `MemberTypeAgent`），并初始化各自 cursor。需遵守 `allow_add_to_channel` / `conversations.manageMembers` 权限（见第 8 节）。
3. 写 `task.assignee_team_id`，`assignee_type=3`；**status 保持 `TODO`**（待 leader 认领）。
4. 在 task 的 thread 写入一条 **system 团队指令消息**（内容见下）。
5. `notifyConversationAgents` 唤醒团队所有成员（以及订阅该 thread 的 agent）。

#### Leader 认领（已确认）

- 主 task 在团队指派后仍是 `TODO`，由 **leader** 通过 `ClaimTask` 认领，转为 `IN_PROGRESS` 并成为“团队负责人”。
- `ClaimTask` 需要校验：调用者是该 task 所指派团队的 leader 才能认领主任务（非 leader 成员认领会得到 `FAILED_PRECONDITION`）。
- 普通成员不认领主任务，只认领 leader 创建的子任务。

系统消息模板：

```
[TEAM ASSIGNMENT]
Team: <team name>
Leader: <leader display name>
Team Prompt: <team_prompt>

Task Instruction: <task content>

Roles:
- <agent1> (leader): You are the leader. You own the task outcome. You must
  break the work into subtasks, coordinate other members, track progress, and
  ensure the task moves from IN_PROGRESS to IN_REVIEW/DONE.
- <agent2> (member): Your responsibility: <responsibility>. Follow the leader's
  coordination and complete the assigned subtasks.
...
```

### 6.3 协作模型（已确认）

- **主任务由 leader 负责**：主 task assignee 是团队，但“推动者”是 leader；leader claim 后为 IN_PROGRESS。
- Leader 可在 task thread 里创建子任务（subtask）或直接给成员派活（DM/mention）。
- 普通成员通过现有 claim 流程认领子任务，并在各自子任务的 thread 中汇报。
- 主 task 的 `claim` 语义：只有 **leader** 可以认领主任务；团队成员只能认领子任务。

### 6.4 Prompt 注入

- **冷启动 init prompt**：如果 `BeginSessionResponse` 带 `team`，agent 客户端在 `BuildPrompt` 之后附加 `## Your Team` 段落（团队名、角色、职责、team_prompt）。
- **任务级注入**：由系统消息承担，每个 agent 读 task thread 时天然可见。
- **Warm/Re-anchor**：`BuildReanchorPrompt` 也应保留团队身份，防止压缩后丢失团队意识。

---

## 7. 分层实施计划

### 7.1 后端

1. **Proto**：
   - 新增 `proto/v1/v1/agent_team_service.proto`；
   - 修改 `command.proto`：`TaskInfo`、`AssignTaskRequest`、`BeginSessionResponse`；
   - 运行 `cd proto && buf generate`。

2. **Migration**：
   - `migration/1.1/00XX##agent-team.sql`
   - 同步追加到 `migration/LATEST.sql`。

3. **Store**：
   - 新增 `store/agent_team.go`（CRUD + 成员管理 + 校验 leader 唯一 + 校验 agent 唯一团队）。
   - 修改 `store/task.go`：`AssignTask` 支持 team（校验团队未占用）+ 解指派；`fillTaskInfo` 带出 team assignee。
   - 新增 `store` 查询：某团队在某 conversation 是否全部是成员、某 agent 所属的团队（单一）。

4. **API handler**：
   - 新增 `api/v1/agent_team_service.go`。
   - 修改 `api/v1/task.go`：team 指派 + 写系统团队消息 + 唤醒团队 agent。
   - 修改 dispatcher `HandleBeginSession`：解析 agent 的团队并填 `team` 字段。

5. **Agent runtime**：
   - `executor/prompt.go`：`BuildPrompt`/`BuildReanchorPrompt` 支持额外的 team prompt 段。
   - `client/context_observer.go` / `drain_runner.go`：从 `BeginSessionResponse` 接收并传递 `team`。
   - 任务级系统消息无需改 runtime。

6. **注册服务**：`server/grpc_routes.go` 注册 `AgentTeamServiceHandler`，`grpcreflect` 加入服务名。

### 7.2 前端

1. 新页面：`frontend/src/pages/dashboard/agent-teams.tsx`（列表 + 创建/编辑团队 + 成员管理）。
2. Task 面板：支持“指派给团队”下拉/选择器，显示 team assignee badge。
3. 新 store：`stores/agentTeam.ts`。
4. i18n：`locales/en-US.json`、`zh-CN.json`。
5. 路由与菜单：在侧边栏加入口。

---

## 8. 可能的问题与注意点

1. **成员加入频道**：已确认指派时自动把团队所有 agent 加入 conversation。实现时要遵守 `allow_add_to_channel`（若某 agent 不允许他人添加，需提示或跳过并报错）以及 `conversations.manageMembers` 权限。

2. **多 agent 抢同一任务**：已确认主 task 只能由 leader claim；普通成员只能 claim 子任务，避免重复劳动。需要在 `ClaimTask` 中按团队指派做角色校验。

3. **团队同一时间只能被指派到一个未完成任务**：`idx_task_team_active` 唯一索引兜底；解指派/完成必须释放团队，否则后续任务报 `FAILED_PRECONDITION`。

4. **Leader 离线/停用**：如果 leader 被 Stop/下线，任务进度可能停滞。需要：
   - 在指派时校验 leader `enabled`；
   - 允许成员在 leader 长时间无响应时升级/接管；
   - 或提供 leader 变更能力。

5. **一个 agent 只能属于一个团队**：换团队必须先退出旧团队；`agent_team_member.agent_id` 唯一索引保证不出现同属多队。前端成员选择器要过滤已入队 agent。

6. **Prompt 注入时机**：依赖系统消息时，agent 必须真正读取 task 消息才会看到。要保证 `notifyConversationAgents` 唤醒所有团队成员，且 agent 的 drain 流程会读 thread（现有 `thread check` 已支持）。

7. **上下文长度**：team_prompt + task 指令 + 角色指令在 thread 里，若团队很大，可能占较多 token。建议 team_prompt 有长度上限，或使用“只注入必要成员”的摘要。

8. **兼容性**：现有单 agent 任务、`TaskInfo`、前端 task board 必须继续工作。新增字段全部 optional。

9. **权限**：团队可被谁修改、谁可把 agent 加进团队、谁可把团队指派到 task，需要确认并遵守 agent owner 的权限（类似 `allow_add_to_channel`）。

10. **数据一致性**：`agent_team.leader_agent_id` 与 `agent_team_member` 需同步维护，避免冗余列不一致。建议以 member 表为准，leader 列为缓存。

11. **软删除/审计**：团队删除后，历史 task 引用应保留显示名或显示“已删除”。

---

## 9. 需求澄清结论（已确认）

1. **团队作用域**：Agent Team 属于创建它的用户（个人资源），但**其他用户可见**。
2. **成员加入权限**：用户只能把 **owner 为自己** 的 agent 加入团队。
3. **指派后任务状态**：保持 `TODO`，由 **leader 认领** 后转 `IN_PROGRESS`。
4. **协作方式**：leader 编排 + 成员认领子任务。
5. **Prompt 注入方式**：采用 **task thread 系统消息 + BeginSession 可选团队上下文**。
6. **自动加入频道**：指派时自动把团队所有 agent 加入该 conversation。
7. **交付范围**：前后端一起实现。
8. **一个 agent 只能加入一个 agent team**。
9. **一个 agent team 同时只能被指派到一个未完成任务**；任务完成/结束/取消指派后释放。

> 这些结论已同步到第 3 节“已确认决策”、第 6 节流程设计、第 8 节风险注意点。

---

## 10. 总结

Agent Team 功能可以从现有 Agent + Task + Conversation 基础设施上增量实现：

- 新增 `agent_team`/`agent_team_member` 表和 CRUD API；
- 扩展 task 支持团队 assignee；
- 通过 **task thread 系统消息** + **BeginSession 可选团队上下文** 实现 prompt 注入；
- 定义 leader 编排、member 分工作业的协作模型；
- 前后端按既有 `GroupService`、`AgentService`、`Task` 的模式落地。

风险点主要在“多 agent 协作避免冲突”“权限边界”“prompt 注入与上下文长度”，建议按 9 节的问题确认后再细化实现。
