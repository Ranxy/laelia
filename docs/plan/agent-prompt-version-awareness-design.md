# Agent 系统提示词版本感知与发布通知设计

> 状态：2026-09-06 已对照当前代码核对更新。本文档与实现一致（阶段 1–3 已落地）：静态版本注入（`build-embedded-machines.sh` 的 `PROMPT_HASH` + manifest `prompt_bundle_version`）、复合 `buildPromptVersion`、proto `prompt_release_notice=13`/`prompt_release_notice_ack=12`、四个推送触发点（persona/owner/team/机器版本过旧）与文中列出的 5 个测试均逐一验证存在。第 9 节三项可选仍未实施：无服务端未确认巡检、pi `LaunchFingerprint` 仍只含 persona 不含 team/owner、前端未展示已确认版本。

---

## 1. 背景与问题

Laelia 的 Agent 系统提示词（init prompt）由两部分组成：

1. **机器二进制内嵌的静态提示词**：`communication.md`、`agent_memory.md`、`reanchor.md`、`AgentFirstPromptBody`，通过 `go:embed` 编译进 `laelia-machine` 二进制。
2. **manager 侧动态提示词**：`persona_prompt`、`team_prompt`、`owner_display_name`，存在 manager 数据库，每次 `BeginSession` 下发。

原实现中，init prompt **只在冷启动时发送一次**，warm turn 通过 ACP session resume / pi 会话继承历史，不再重发。因此存在两个问题：

- **动态提示词变化不感知**：管理员改了 persona / team / owner 后，已经 warm 的 Agent 会话看不到变化，仍按旧提示词工作。
- **静态提示词升级不感知**：机器二进制升级（内嵌提示词变化）后，已存在的 warm 会话仍沿用旧提示词；且 manager 无法判断哪些机器/Agent 用的提示词已过时。

目标：**让 Agent 在系统提示词变化后，最迟下一个 turn（理想情况当前 turn）感知到变化，并让 manager 可靠知道哪些 Agent 已确认。**

---

## 2. 设计目标

1. **版本可追踪**：静态提示词和动态提示词都有可比较的版本号。
2. **下个 turn 生效**：无论 Agent 是否在线、是否 warm，变化最迟在下一个 drain turn 被感知。
3. **运行中即时感知**：对支持 steer 的运行时（pi、ACP v2 thread），变化可注入当前 turn。
4. **可靠确认**：Agent 注入后向 manager 上报 ack，manager 持久化已确认版本；未确认的 notice 随 BeginSession 反复附带，直到确认。
5. **不热更新 system prompt**：不尝试在会话中途修改 system prompt（多数运行时不支持），而是把“提示词已更新”作为一条高优先级用户消息注入。

---

## 3. 核心概念

### 3.1 静态提示词版本（PromptBundleVersion）

- 构建 `laelia-machine` 时，对 `backend/agent/executor/prompt/*.md` 和 `prompt.go` 计算内容哈希（SHA-256 前 16 位），作为 `version.PromptBundleVersion` 注入二进制。
- 同时写入 manager 的 `embedded_machine/manifest.json` 的 `prompt_bundle_version` 字段，作为 manager 的“期望静态提示词版本”。
- 开发环境不 embed machine 时，该字段为空，不会误报。

### 3.2 动态提示词版本

- 对 `persona_prompt + team_prompt + owner_display_name` 计算内容哈希（SHA-256 前 16 位），作为动态部分。

### 3.3 复合提示词版本（prompt_version）

`BeginSessionResponse` 下发一个复合指纹：

```
prompt_version = "<static_expected>.<dynamic_hash>"
```

- `static_expected`：manager 期望的静态提示词版本（来自 manifest）。
- `dynamic_hash`：persona/team/owner 的内容哈希。

Agent 侧解析后，可区分“静态变化”和“动态变化”，分别采取不同动作。

### 3.4 提示词发布通知（PromptReleaseNotice）

一条带唯一 `notice_key` 的通知，包含 `message` 和 `prompt_version`。用于：

- manager → agent 主动推送（运行中即时感知）；
- 离线恢复时随 `BeginSessionResponse` 反复附带；
- Agent 注入后回传 `PromptReleaseNoticeAck`。

---

## 4. 架构与数据流

```
[构建时]
  prompt/*.md + prompt.go ──哈希──> version.PromptBundleVersion（烧入 machine 二进制）
                                    └─> manifest.json.prompt_bundle_version（manager 期望）

[manager]
  persona/team/owner 更新 ──> 计算 prompt_version ──> PushPromptReleaseNotice(agent)
                                                          │ 成功：走 AgentChannel 推送
                                                          └ 失败：持久化 pending notice
  机器连接上报 PromptBundleVersion ≠ 期望 ──> PushPromptReleaseNoticeToMachine(所有 agent)

[BeginSession]
  manager 返回 { prompt_version, prompt_release_notice(pending) }

[agent]
  runSession：
    1. 消费 pending notice（steer 失败入队 / BeginSession 附带）
    2. applyPromptVersion 对比本地 ContextState.PromptVersion
       - 动态变化 → 注入 notice + NeedsReanchor
       - 静态过旧 → 注入“请升级”notice
       - 二进制升级 → fingerprint 自动冷启动
    3. 发送 PromptReleaseNoticeAck

[manager]
  收到 ack ──> 持久化已确认 prompt_version + 清空 pending notice
```

---

## 5. 实现细节

### 5.1 机器二进制侧

**`backend/agent/version/version.go`**
- 新增 `PromptBundleVersion = "dev"`，构建时由 `-ldflags -X` 注入。

**`scripts/build-embedded-machines.sh` / `scripts/docker/Dockerfile.machine`**
- 构建时计算 `PROMPT_HASH`，注入 `version.PromptBundleVersion`，并写入 manifest 的 `prompt_bundle_version`。

**`backend/agent/client/client.go`**
- `collectMachineInfo()` 在 `MachineInfo` 中上报 `PromptBundleVersion`。

### 5.2 静态提示词升级 → 自动冷启动

**`backend/agent/executor/acp_session.go`**
- `sessionFingerprint` 加入 `version.PromptBundleVersion`：二进制升级 → ACP session 指纹变化 → 下个 turn 冷启动，重发完整新 init prompt。

**`backend/agent/pi/config.go`**
- `LaunchFingerprint` 加入 `version.PromptBundleVersion` 和 `PersonaPrompt`：
  - 二进制升级 → pi 重启 → 冷启动；
  - persona 变更 → pi 重启 → 冷启动（阶段 3）。

### 5.3 动态提示词版本计算（manager）

**`backend/manager/component/dispatcher/dispatcher.go`**
- `buildPromptVersion(owner, team, agent)`：返回 `<static_expected>.<dynamic_hash>`。
- `HandleBeginSession` 在响应中返回 `prompt_version` 和 pending `prompt_release_notice`。

### 5.4 主动推送（阶段 2）

**Proto（`proto/v1/v1/command.proto`）**
- `ManagerStreamMessage` 增加 `PromptReleaseNotice prompt_release_notice = 13`。
- `AgentStreamMessage` 增加 `PromptReleaseNoticeAck prompt_release_notice_ack = 12`。
- 新增 `PromptReleaseNotice` / `PromptReleaseNoticeAck` 消息。

**Dispatcher**
- `SendPromptReleaseNotice(agentID, notice)`：向 AgentChannel 推送。
- `PushPromptReleaseNotice(ctx, agentID)`：计算当前版本并推送（供动态变更调用）。
- `PushPromptReleaseNoticeToMachine(ctx, machineID, notice)`：给机器上所有启用 agent 推送（供静态升级调用）。

**触发点**
- `UpdateAgentConfig`（persona 变更）→ 推送。
- `UpdateAgentTeam`（team_prompt 变更）→ 给所有成员推送。
- `TransferAgentOwnership`（owner 变更）→ 推送。
- `ConnectMachine`（机器连接，上报版本 ≠ 期望）→ 给该机器所有 agent 推送“请升级”。

### 5.5 未确认重试闭环（阶段 3）

**Proto（`proto/store/store/agent.proto`）**
- `AgentInfo` 增加 `pending_prompt_notice`（JSONB，无需 SQL 迁移）。
- 新增 `PendingPromptNotice` 消息。

**Store（`backend/manager/store/agent.go`）**
- `UpdateAgentPromptVersion`：确认时清空 pending notice。
- `SetPendingPromptNotice`：持久化推送失败的 notice。

**Dispatcher**
- 推送失败时持久化 pending notice。
- `HandleBeginSession` 把 pending notice 随响应下发，直到 ack。

### 5.6 Agent 侧注入

**`backend/agent/client/command_stream.go`**
- 新增线程安全的 `pendingPromptNotice` 字段 + `set/takePendingPromptNotice()`。

**`backend/agent/client/message_router.go`**
- 处理 `ManagerStreamMessage_PromptReleaseNotice`：
  - 支持 steer（pi / ACP v2）→ 直接注入当前 turn + ack；
  - 否则 → 入队 + wake。
- 新增 `sendPromptReleaseNoticeAck()`。

**`backend/agent/client/drain_runner.go`**
- `runSession` 消费两类 notice（本地 pending / BeginSession 附带），前置到 turn、更新 `ContextState.PromptVersion`、设 `NeedsReanchor`、发送 ack。
- `applyPromptVersion` 对比本地已确认版本，动态变化注入 notice + re-anchor，静态过旧注入“请升级”notice。

**`backend/agent/executor/context_state.go`**
- `ContextState` 新增 `PromptVersion`，用于本地去重。

---

## 6. 行为场景

| 场景 | 行为 |
|---|---|
| 管理员改 persona（Agent 在线、支持 steer） | 立即注入当前 turn + ack |
| 管理员改 persona（Agent 在线、不支持 steer） | 入队，下个 turn 注入 + ack |
| 管理员改 persona（Agent 离线） | 推送失败 → 持久化 pending；重连后随 BeginSession 附带 |
| 管理员改 team / owner | 同上（team 给所有成员推送） |
| 机器二进制升级（新静态提示词） | ACP/pi fingerprint 变化 → 下个 turn 冷启动拿新提示词 |
| 机器二进制过旧（上报版本 ≠ 期望） | 机器连接时给所有 agent 推送“请升级”notice |
| 首次部署（无历史版本） | 静默确认，不打扰 |

---

## 7. 测试

- `TestApplyPromptVersion`：首次静默、动态变化、机器过旧、二进制升级、已确认无变化。
- `TestMessageRouterPromptReleaseNoticeQueuesWhenNotSteerable`：非 steerable 入队 + 不立即 ack。
- `TestSendPromptReleaseNoticeAck`：ack 消息格式。
- `TestBuildPromptVersion`：相同输入稳定、team/owner 变化敏感、复合格式。
- `TestLatestPromptBundleVersion`：manifest 有/无该字段、无 manifest。
- 全量 `go test ./backend/agent/... ./backend/manager/...` 通过。

---

## 8. 改动文件清单

**Proto**
- `proto/v1/v1/command.proto`
- `proto/v1/v1/machine.proto`
- `proto/store/store/agent.proto`
- `proto/store/store/machine.proto`
- 对应 `backend/generated-go/...` 生成文件

**构建**
- `scripts/build-embedded-machines.sh`
- `scripts/docker/Dockerfile.machine`

**机器二进制**
- `backend/agent/version/version.go`
- `backend/agent/client/client.go`
- `backend/agent/executor/acp_session.go`
- `backend/agent/pi/config.go`
- `backend/agent/executor/context_state.go`
- `backend/agent/client/command_stream.go`
- `backend/agent/client/message_router.go`
- `backend/agent/client/drain_runner.go`
- `backend/agent/client/context_observer.go`

**Manager**
- `backend/manager/component/dispatcher/dispatcher.go`
- `backend/manager/component/machinebuild/machinebuild.go`
- `backend/manager/store/agent.go`
- `backend/manager/api/v1/agent.go`
- `backend/manager/api/v1/agent_config.go`
- `backend/manager/api/v1/agent_team_service.go`
- `backend/manager/api/v1/agent_command.go`
- `backend/manager/api/v1/machine_connection.go`
- `backend/manager/api/v1/machine_convert.go`
- `backend/manager/server/grpc_routes.go`

**测试**
- `backend/agent/client/command_stream_context_test.go`
- `backend/agent/client/command_stream_test.go`
- `backend/manager/component/dispatcher/dispatcher_test.go`
- `backend/manager/component/machinebuild/machinebuild_test.go`

---

## 9. 后续可选项

- 服务端“未确认 Agent 列表”的主动巡检/重试（当前靠 BeginSession 附带已能覆盖离线恢复）。
- 把 team_prompt / owner 也纳入 pi 的 launch fingerprint（当前 persona 已纳入，team/owner 走 re-anchor）。
- 前端展示每个 Agent 的已确认提示词版本。
