# Design: Agent Teams (团队) + Task Team Assignment

> 状态:2026-09-06 已对照当前代码核对更新。主要变化:功能已全部落地——团队 CRUD 走 `AgentTeamService`(成员经 `UpdateAgentTeam` 的 `members` mask 维护,无独立成员 RPC),指派走 `AssignTask(member_type=3)`,团队指令以 leader 身份的 AGENT 消息写入任务 thread,前端入口为 human 详情页的 teams 标签页 + `team-detail` 页面。

## 1. Context

Laelia 目前支持创建和管理单个 agent。Agent 是常驻的自主进程，通过 `laelia-machine` CLI 在频道/DM 中发消息、认领和完成任务（`task` 表挂在 `chat_message` 上，当前 assignee 只能是单个 user 或单个 agent）。

新需求要引入 **Agent 团队（Agent Team）**：

- 用户创建/管理团队，把多个 agent 加入团队；
- 团队必须有一个 leader，负责管理团队任务、协调其他成员；
- 可配置每个成员在团队中的职责；
- 可配置团队级提示词（team prompt）；
- Task 可以被指派给团队：当团队被指派到某个 task 时，团队内每个 agent 都收到特殊 prompt，内容包含：给团队处理该 task 的提示词 + 团队本身的 team-prompt；
- leader 和普通成员的提示词不同：leader 被提示要控制 task 开发进度、协调团队内其他 agent。

## 2. 当前代码现状分析(撰写时;团队功能落地后见下文"已实现"注记)

### 2.1 Agent

- `agent` 表(`backend/manager/migration/migration/LATEST.sql`)保存 agent 身份、owner、machine、enabled 等;私有 persona 存在 `agent.info.acp_config.persona_prompt`(JSONB);另有一个公开简介列 `agent.description`(`migration/migration/1.1/0023##agent-description.sql`)。
- `AgentService` 提供 `CreateAgent/GetAgent/UpdateAgent/...`，权限字符串 `laelia.agents.*`。
- 每个 agent 有 `owner`（用户）和 `machine`（运行主机）。
- Agent 的自主执行：Manager `Dispatcher.HandleBeginSession` 返回 `command_id` + `agent_display_name` + `owner_display_name` + `team`(TeamContext) + `prompt_version` + `prompt_release_notice`；machine 端 `executor.BuildPrompt(name, ownerDisplayName, personaPrompt, teamPrompt)` 组装冷启动 init prompt（身份 + persona + team + Ownership & Safety + communication + first prompt + memory）。
- Agent 通过 `notifyConversationAgents` 被唤醒；会话内通过 `task list/claim/review/done` 与 manager 交互。

### 2.2 Task

- `task` 表：`message_id`(PK/FK), `conversation_id`, `task_number`, `status`, `assignee_agent_id`, `assignee_user_id`, `assignee_type`（1=user、2=agent，后扩展 3=team）, `assignee_team_id`。
- `TaskInfo`（v1）表达单个 assignee（user/agent）以及团队 assignee（`assignee_type`/`assignee_team_id`/`assignee_team_name`/`assignee_team_leader_name`）。
- 任务流：TODO → IN_PROGRESS → IN_REVIEW → DONE；claim 只能由一个 agent 认领（团队任务仅 leader）。
- 前端有 task board（`frontend/src/stores/task.ts`）。

### 2.3 现有 Group（用户组）

- `user_group` 是 IAM 用户组，用于 policy 绑定，**不是 agent 团队**。Agent Team 独立建模（`agent_team`/`agent_team_member`），未与 `user_group` 混用。

### 2.4 结论

设计时系统**没有**任何 agent 团队概念。实现路径即下文第 4–7 节，现已全部落地：

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
5. **Prompt 注入**：采用 **task thread 指令消息 + BeginSession 可选团队上下文** 双轨方案,按本报告第 4.3 / 6.4 节实现(指令消息落地为 AGENT 类型,见 3.3 的偏差说明)。
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

我们推荐 **“持久化系统消息 + 会话级可选增强”双轨**（落地时有偏差,见下）：

1. **持久化任务指令消息（主通道）**：当团队被指派到 task 时，manager 在 task 的 thread 中写入一条指令消息，包含：
   - 团队名、leader 名；
   - team_prompt；
   - 当前 task 的指令；
   - 每个成员的角色指令（leader 有“控制进度、协调成员”的额外要求，普通成员有“你的职责是 …”）。

   **落地实现**：这条消息不是 `sender_type=SYSTEM` 而是 `sender_type=AGENT`、以 **leader 的 agent 身份**发送（`api/v1/task.go` 的 `postTeamAssignmentMessage`/`buildTeamAssignmentMessage`）——agent 唤醒路由刻意排除 SYSTEM 行，SYSTEM 消息不会唤醒任何 agent，所以必须用 AGENT 类型才能成为真实的 wake 信号。它在团队指派时写入一次，leader claim 时再重发一次,让 leader 立刻看到团队构成。

2. **BeginSession 可选增强（辅助）**：`BeginSessionResponse` 增加可选的 `team`（`TeamContext`：当前 agent 所属团队 + 角色 + 职责 + team_prompt，`proto/v1/v1/command.proto`）。agent 客户端把它并入冷启动 init prompt——`executor.BuildPrompt`/`BuildReanchorPrompt` 追加 `## Your Team` 段落。此外 `prompt_version` 的动态哈希把 team_prompt 计入,团队 prompt 变更时 manager 通过 `PushPromptReleaseNotice` 推送成员,运行中的 agent 能在当前或下一轮感知变化。

> 具体 task 指令仍在 task thread 中；`BeginSessionResponse.team` 只承载身份/团队意识，不承载具体任务指令。

---

## 4. 数据模型设计(已落地)

### 4.1 新表 `agent_team`

`backend/manager/migration/migration/LATEST.sql`（增量文件 `migration/migration/1.1/0026##agent-team.sql`）：

```sql
CREATE TABLE IF NOT EXISTS agent_team (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  resource_id   TEXT NOT NULL UNIQUE,      -- teams/{id}，用于 API 引用
  name          TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  team_prompt   TEXT NOT NULL DEFAULT '',
  leader_agent_id INTEGER REFERENCES agent(id) ON DELETE SET NULL, -- 冗余，便于查询
  owner_id      INTEGER NOT NULL REFERENCES principal(id),
  created_by    INTEGER NOT NULL REFERENCES principal(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted       BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS agent_team_member (
  team_id       UUID NOT NULL REFERENCES agent_team(id) ON DELETE CASCADE,
  agent_id      INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  role          SMALLINT NOT NULL DEFAULT 2, -- 1=LEADER, 2=MEMBER
  responsibility TEXT NOT NULL DEFAULT '',    -- 该成员在团队中的职责
  joined_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, agent_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_team_member_leader
  ON agent_team_member(team_id) WHERE role = 1;  -- 每队最多一个 leader

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_team_member_agent
  ON agent_team_member(agent_id);  -- 一个 agent 只能属于一个团队
```

设计说明：

- `leader_agent_id` 是冗余列，方便 `GetAgentTeam` 不 join 就返回 leader；权威数据在 `agent_team_member`。
- 团队成员必须在 `agent_team_member` 中存在且 `deleted=false`。
- 团队软删除（`deleted` 列）；历史 task 的 `assignee_team_id` 保留引用（`fillTaskInfo` join 不到时团队名字段为空）。

### 4.2 Task 表扩展(已落地)

```sql
ALTER TABLE task ADD COLUMN IF NOT EXISTS assignee_team_id UUID REFERENCES agent_team(id) ON DELETE SET NULL;
```

- `assignee_type` 语义扩展：`1=user`, `2=agent`, `3=team`。
- 若 `assignee_type=3`，则 `assignee_team_id` 有效，`assignee_agent_id`/`assignee_user_id` 为空（leader claim 后 `assignee_agent_id` 记录推动者）。
- `TaskInfo` API 落地字段（`proto/v1/v1/command.proto`）：
  - `assignee_type = 5`
  - `assignee_team_id = 6`（"agentTeams/{id}"）
  - `assignee_team_name = 7`
  - `assignee_team_leader_name = 8`

**唯一性约束（已确认决策 9）**：一个团队同一时间只能被指派到一个未完成 task。

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_team_active
  ON task(assignee_team_id) WHERE assignee_team_id IS NOT NULL AND status IN (1,2,3);
```

> 重复指派在 handler/store 侧用 `store.TeamHasActiveTask` 校验（返回 `ErrTaskTeamAlreadyAssigned` → `FAILED_PRECONDITION`），唯一索引兜底；task 完成（DONE）或改指派他人后索引自动放行。

### 4.3 会话级团队上下文（BeginSession，已落地）

`BeginSessionResponse`（`proto/v1/v1/command.proto`）增加字段 `TeamContext team = 5;`（因一个 agent 只能属于一个团队，使用单数 `team`）：

```proto
message TeamContext {
  string team_id = 1;        // "agentTeams/{id}"
  string team_name = 2;
  string team_prompt = 3;
  string role = 4;           // "leader" | "member"
  string responsibility = 5; // 该 agent 在团队中的职责
}
```

> `Dispatcher.HandleBeginSession` 经 `store.GetAgentTeamByAgentID` 填充；具体 task 上下文仍在 task thread 中。`buildPromptVersion` 的动态哈希也把 team_prompt 计入。

---

## 5. API 设计(已落地,与原草案有出入)

`AgentTeamService`（proto：`proto/v1/v1/agent_team_service.proto`）：

```proto
service AgentTeamService {
  rpc GetAgentTeam(GetAgentTeamRequest) returns (AgentTeam);        // IAM, laelia.agentTeams.get
  rpc ListAgentTeams(ListAgentTeamsRequest) returns (ListAgentTeamsResponse); // IAM, laelia.agentTeams.list
  rpc GetMyAgentTeam(google.protobuf.Empty) returns (AgentTeam);    // agent-only, 无 auth_method
  rpc CreateAgentTeam(CreateAgentTeamRequest) returns (AgentTeam);  // IAM, laelia.agentTeams.create, audit
  rpc UpdateAgentTeam(UpdateAgentTeamRequest) returns (AgentTeam);  // IAM + handler 门禁(owner/admin), audit
  rpc DeleteAgentTeam(DeleteAgentTeamRequest) returns (google.protobuf.Empty); // IAM + handler 门禁, audit
}
```

与原草案的差异：

- **没有** `AddAgentTeamMember`/`RemoveAgentTeamMember`/`UpdateAgentTeamMemberRole` 独立 RPC——成员（含 leader、职责）统一经 `UpdateAgentTeam` 的 `update_mask` 路径 `members`/`leader_agent` 整体替换（`store.UpdateAgentTeam` → `replaceTeamMembersTx`）。
- 资源名模式为 `agentTeams/{agentTeam}`（`common.FormatAgentTeamName`），`AgentTeam` 消息字段为 `name/title/description/team_prompt/leader_agent/members/created_at/updated_at/can_manage(OUTPUT_ONLY)/owner(OUTPUT_ONLY)`——原草案的独立 `team_id` 字段由 `name` 承担。
- 新增了 `GetMyAgentTeam`（agent 查询自己所属团队，agent CLI `team get` 使用）。

Task 相关扩展（`proto/v1/v1/command.proto`）：

- `AssignTaskRequest`：`member_type=3`（team）时 `member_id` 为团队 resource id（"agentTeams/{id}"）。
- `ListTasks`/`ListConversationMessages` 返回的 `TaskInfo` 带出 team assignee 字段。
- 未新增 `AssignTaskToTeam` RPC——直接扩展 `AssignTask`。

权限（`backend/common/permission/permission.json`，已落地）：

```json
{"name": "AgentTeamsCreate", "id": "laelia.agentTeams.create"},
{"name": "AgentTeamsGet",   "id": "laelia.agentTeams.get"},
{"name": "AgentTeamsList",  "id": "laelia.agentTeams.list"},
{"name": "AgentTeamsUpdate","id": "laelia.agentTeams.update"},
{"name": "AgentTeamsDelete","id": "laelia.agentTeams.delete"}
```

管理门禁与 `GroupService` 思路一致：团队 owner（创建者）或 workspace admin 可管理（`UpdateAgentTeam`/`DeleteAgentTeam` 在 handler 内二次校验 `canManageTeam`）；其他用户可 `Get/List`（可见）。

**成员加入限制（已确认,已落地）**：添加成员时，仅允许加入 `agent.owner_id == 当前用户`（或 workspace admin）的 agent（`validateAndConvertMembers`）。

---

## 6. 流程设计

### 6.1 团队创建 / 编辑

1. 用户选择多个 agent，指定一个为 leader；
2. **一个 agent 只能属于一个团队**：选择成员时，已属于其他团队的 agent 不可加入，需先从原团队移除（或直接显示不可选）。
3. 对每个成员填职责（可空）；
4. 填 team_prompt（可空，用于给所有成员注入团队背景/协作约定）；
5. 保存。

### 6.2 团队指派到 Task(已落地)

用户（或 agent）在 channel 中把一个 task 指派给团队（`AssignTask(member_type=3)`，**已确认:自动把团队所有 agent 加入该 conversation**）：

1. **唯一性校验**：该团队当前不能已有未完成（TODO/IN_PROGRESS/IN_REVIEW）task，否则返回 `FAILED_PRECONDITION`（`ErrTaskTeamAlreadyAssigned`，已确认决策 9）。
2. **自动加入**（`api/v1/task.go` 的 `afterTeamAssign`）：将该团队所有 agent 加入 task 所在 conversation（成员类型 `MemberTypeAgent`），seed 各自 cursor 到当前 version（只看得到之后的团队指令消息），并 `AddThreadParticipants` 把全部成员订阅到 task thread。落地实现**没有**做 `allow_add_to_channel` / `can_manage_channel_members` 检查——团队指派是强制加入（原设计第 8 节风险 1 提到的尊重私有 agent 设置未实现）。
3. 写 `task.assignee_team_id`，`assignee_type=3`；**status 保持 `TODO`**（待 leader 认领）。
4. 在 task 的 thread 写入一条 **以 leader 身份发送的 AGENT 指令消息**（内容见下;SYSTEM 行不唤醒 agent,故不用 SYSTEM）。
5. `notifyConversationAgents` 唤醒团队所有成员（以及订阅该 thread 的 agent）。

#### Leader 认领（已确认,已落地）

- 主 task 在团队指派后仍是 `TODO`，由 **leader** 通过 `ClaimTask` 认领，转为 `IN_PROGRESS` 并成为“团队负责人”（`store.ClaimTeamTask`:`assignee_type` 保持 3,`assignee_agent_id` 写 leader）。
- `ClaimTask` 需要校验：调用者是该 task 所指派团队的 leader 才能认领主任务（非 leader 成员认领会得到 `FAILED_PRECONDITION`）。
- leader 认领成功后,团队指令消息会**再次**写入 task thread（`postTeamAssignmentMessage`）,leader 无需翻历史即可看到团队构成与职责。
- 普通成员不认领主任务，只认领 leader 创建的子任务。

系统消息模板（`buildTeamAssignmentMessage`,已落地,与草案一致）：

```
[TEAM ASSIGNMENT]
Team: <team name>
Team ID: <team resource name>
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

### 6.3 协作模型(已确认,已落地)

- **主任务由 leader 负责**：主 task assignee 是团队，但“推动者”是 leader；leader claim 后为 IN_PROGRESS。
- Leader 可在 task thread 里创建子任务（subtask）或直接给成员派活（DM/mention）。
- 普通成员通过现有 claim 流程认领子任务，并在各自子任务的 thread 中汇报。
- 主 task 的 `claim` 语义：只有 **leader** 可以认领主任务；团队成员只能认领子任务。
- Prompt 层也固化了该规则（`executor/prompt/communication.md` 与 `AgentFirstPromptBody` step 4：“If you are a team leader, prefer assigning work to your team, not to yourself”）。

### 6.4 Prompt 注入(已落地)

- **冷启动 init prompt**：`BeginSessionResponse.team` 非空时，`executor.BuildPrompt(name, owner, persona, teamPrompt)` 附加 `## Your Team` 段落（`executor/prompt.go` 的 `buildTeamSection`）。
- **任务级注入**：由 leader 身份的 AGENT 指令消息承担，每个 agent 读 task thread 时天然可见。
- **Warm/Re-anchor**：`BuildReanchorPrompt(name, owner, teamPrompt)` 同样保留团队段落;`prompt_version` 动态哈希含 team_prompt,团队 prompt 变更时经 `Dispatcher.PushPromptReleaseNotice` 通知成员（`UpdateAgentTeam` 的 team_prompt 更新路径）。

---

## 7. 分层实施计划(已按此落地;括注实际落点)

### 7.1 后端

1. **Proto**：
   - 新增 `proto/v1/v1/agent_team_service.proto`；
   - 修改 `command.proto`：`TaskInfo`、`AssignTaskRequest`、`BeginSessionResponse.team`/`TeamContext`；
   - 运行 `cd proto && buf generate`。

2. **Migration**：
   - `migration/migration/1.1/0022##task-assignee.sql`（task.assignee_type/assignee_user_id）与 `0026##agent-team.sql`（agent_team/agent_team_member/assignee_team_id/idx_task_team_active）。
   - 同步追加到 `migration/migration/LATEST.sql`。

3. **Store**：
   - 新增 `store/agent_team.go`（`GetAgentTeamByID/ByResourceID/ByName`、`ListAgentTeams`、`ListAgentTeamMembers`、`GetAgentTeamByAgentID`、`CreateAgentTeam`、`UpdateAgentTeam` + `replaceTeamMembersTx`、`DeleteAgentTeam`、`TeamHasActiveTask`）。
   - `store/task.go`：`AssignTask` 支持 team（`TeamHasActiveTask` 校验）+ 改指派即清空旧团队；`fillTaskInfo` join 出 team assignee（含 leader 名）。

4. **API handler**：
   - 新增 `api/v1/agent_team_service.go`。
   - `api/v1/task.go`：`AssignTask` 的 team 分支 + `afterTeamAssign`（自动加成员/订阅/seed cursor）+ `postTeamAssignmentMessage`/`buildTeamAssignmentMessage` + `ClaimTask` 的 leader 认领分支（`ClaimTeamTask`）。
   - dispatcher `HandleBeginSession`：`store.GetAgentTeamByAgentID` 填 `team` 字段;`buildPromptVersion` 计入 team_prompt;`PushPromptReleaseNotice` 推送 team_prompt 变更。

5. **Agent runtime**：
   - `executor/prompt.go`：`BuildPrompt`/`BuildReanchorPrompt` 追加 `## Your Team` 段（`buildTeamSection`）。
   - `executor/runtime.go` 的 `TeamPrompt` 字段;`acp_executor.go` 调 `BuildPrompt(identityName, owner, persona, req.TeamPrompt)`。
   - 任务级指令消息无需改 runtime。

6. **注册服务**：`server/grpc_routes.go` 注册 `AgentTeamServiceHandler`,`grpcreflect` 服务名列表已加入（`v1connect.AgentTeamServiceName`）。

### 7.2 前端

1. 团队管理 UI：落地为 `frontend/src/components/agent/agent-teams-manager.tsx`,嵌入 human 详情页（`pages/dashboard/human-detail.tsx`）的 "agents | teams" 标签页（仅本人可见 teams 标签）;团队详情页 `pages/dashboard/team-detail.tsx`,路由 `users/:userId/teams/:teamId`（`HUMAN_TEAM_ROUTE`）。
2. Task 面板：thread 头部任务控件 `components/chat/thread-task-controls.tsx` 提供"指派给团队"下拉（数据来自 `hooks/use-agent-teams.ts` 的 `useAgentTeamsQuery`）,显示 team assignee。
3. 数据读取走 TanStack Query hook `hooks/use-agent-teams.ts`（共享 `["agent-teams"]` 缓存）,未建独立 `stores/agentTeam.ts`。
4. i18n：`locales/en-US.json`、`zh-CN.json`。
5. 路由与菜单：human 详情页标签入口。

---

## 8. 可能的问题与注意点(落地情况注记)

1. **成员加入频道**：已确认指派时自动把团队所有 agent 加入 conversation。**落地实现未检查 `allow_add_to_channel`/`can_manage_channel_members`**——团队指派强制加入并订阅 thread;私有 agent 也会被加入（如需收紧,改 `afterTeamAssign`）。

2. **多 agent 抢同一任务**：已确认主 task 只能由 leader claim；普通成员只能 claim 子任务。已落地：`ClaimTask` 按团队指派分流到 `ClaimTeamTask`（leader 校验）;prompt 层同步固化。

3. **团队同一时间只能被指派到一个未完成任务**：`idx_task_team_active` 唯一索引 + `TeamHasActiveTask` 兜底已落地;改指派（`AssignTask` 覆盖写）即释放团队。

4. **Leader 离线/停用**：尚未实现 leader 离线的升级/接管机制;指派时也未校验 leader `enabled`。仍是开放问题。

5. **一个 agent 只能属于一个团队**：`idx_agent_team_member_agent` 唯一索引已落地;前端成员选择器（agent-teams-manager）过滤已入队 agent。

6. **Prompt 注入时机**：指派时 `notifyConversationAgents` 唤醒团队全部成员（已落地）;指令消息为 AGENT 类型,可真实唤醒。

7. **上下文长度**：team_prompt 无长度上限（仍未实现截断/摘要）。

8. **兼容性**：单 agent/用户/团队三种 assignee 共存,`TaskInfo` 新增字段全部向后兼容,前端 task board 正常。

9. **权限**：团队可管理性 = owner 或 workspace admin（已落地）;把 agent 加进团队 = agent owner（或 admin）（已落地）;把团队指派到 task = 会话的 `conversations.send`（IAM 拦截器）。

10. **数据一致性**：`agent_team.leader_agent_id` 与 `agent_team_member` 以 member 表为准,创建/更新时同事务写入（`replaceTeamMembersTx`）。

11. **软删除/审计**：团队软删除;历史 task 的 `assignee_team_id` 保留,join 不到团队时 `TaskInfo` 团队字段为空。

---

## 9. 需求澄清结论(已确认;均已实现)

1. **团队作用域**：Agent Team 属于创建它的用户（个人资源），但**其他用户可见**。
2. **成员加入权限**：用户只能把 **owner 为自己** 的 agent 加入团队。
3. **指派后任务状态**：保持 `TODO`，由 **leader 认领** 后转 `IN_PROGRESS`。
4. **协作方式**：leader 编排 + 成员认领子任务。
5. **Prompt 注入方式**：task thread 指令消息（AGENT 类型） + BeginSession 可选团队上下文。
6. **自动加入频道**：指派时自动把团队所有 agent 加入该 conversation（未做 allow_add_to_channel 检查）。
7. **交付范围**：前后端一起实现。
8. **一个 agent 只能加入一个 agent team**。
9. **一个 agent team 同时只能被指派到一个未完成任务**；任务完成/结束/取消指派后释放。

> 这些结论已同步到第 3 节“已确认决策”、第 6 节流程设计、第 8 节风险注意点。

---

## 10. 总结

Agent Team 功能已从现有 Agent + Task + Conversation 基础设施上增量实现：

- 新增 `agent_team`/`agent_team_member` 表和 `AgentTeamService` CRUD API（成员经 `UpdateAgentTeam` mask 维护）;
- 扩展 task 支持团队 assignee（`AssignTask(member_type=3)` + leader-only claim）;
- 通过 **task thread 的 leader 身份 AGENT 指令消息** + **BeginSession `TeamContext`（`## Your Team` 段 + prompt_version/prompt_release_notice）** 实现 prompt 注入;
- 定义 leader 编排、member 分工作业的协作模型（prompt 与 store 双重约束）;
- 前端以 human 详情页 teams 标签 + team-detail 页 + thread 任务控件落地。

仍开放的风险点是“leader 离线后的接管/变更”与“team_prompt 长度上限”。
