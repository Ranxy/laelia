# Laelia Agent-Manager 通信安全重构方案

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：方案核心已落地（三层 token、轮换/吊销、会话表、nonce、TLS 自签、审计采样），但连接主体已演进为 machine-token 体系（设备码登录 + machine.json），bootstrap token 改为单次使用，分层限流与 setting 化安全配置未实现。

## 现状概述（2026-09-06）

本方案写于重构之前，现已基本落地并发生了一次重要架构演进：

- **machine-token 体系**：机器端守护进程 `laelia-machine` 不再用 `--token` bootstrap，而是通过**设备码登录**（`laelia-machine setup` → StartDeviceLogin/PollDeviceLogin）取得 refresh token，持久化在 `~/.laelia/machine.json`（0600）。一台机器托管多个 agent，所有 agent 复用机器的 access token，通过 `AgentChannel`（in-stream `AgentReady.agent_name`）或机器调用时的 `X-Laelia-Agent` 头声明 agent 身份（见 `backend/manager/api/auth/auth.go` 的 `resolveDeclaredAgent`）。
- **agent-token 连接 API 保留**：`ConnectAgent` / `RefreshAgentToken` / `AgentHeartbeat` / `AgentDisconnect` 及 token 轮换/吊销/会话管理 RPC 均已在 manager 侧实现（`backend/manager/api/v1/agent_token.go`、`agent_connection.go`），但 `backend/agent` 中没有任何调用方 —— daemon 走 machine 通道。agent-token 体系作为独立 agent 直连 manager 的机制保留。
- **machine 侧对偶实现**：`machine_session` / `machine_token` 表与 `RefreshMachineToken` / `ConnectMachine` / `MachineHeartbeat` / `MachineDisconnect` / `RevokeMachineToken` 与 agent 侧一一对应，但 refresh token 语义不同（多用途滚动续期，见 §七）。

## 设计前提

| 约束 | 说明 |
|------|------|
| 无需迁移/兼容 | 项目未上线，可直接破坏性变更 |
| 单实例部署 | 暂不考虑多实例，缓存用进程内方案（nonce 重放缓存、TokenExpireCache 均为进程内，多实例需共享，代码中有 TODO 注释） |
| ~~仅改 Agent Token~~ | **已变更**：实际演进为 machine-token 为主、agent-token 为辅的双体系；用户侧认证机制不变 |
| ~~Token 持久化：~/.laelia/agent-token~~ | **已变更**：机器凭证持久化在 `~/.laelia/machine.json`（`backend/agent/state/state.go`，0600 原子写）；agent-token 路径的客户端持久化未实现（无调用方） |

---

## 一、Agent Token 体系设计

### 1.1 分层 Token 模型

```
┌─────────────────────────────────────────────────────────────┐
│  Bootstrap Token                                            │
│  来源: CreateAgent / RotateAgentToken API 返回               │
│  有效期: 7天 (bootstrapTokenDuration)                        │
│  用途: ConnectAgent 请求体传入（首次连接）                     │
│  特性: 【已变更】单次使用 —— ConnectAgent 成功后即标记        │
│        CONSUMED（防止泄露的 bootstrap token 重放踢掉合法      │
│        agent），见 agent_connection.go                        │
│  存储: 不持久化；DB 中按 SHA-256 hash 存一份用于校验           │
├─────────────────────────────────────────────────────────────┤
│  Access Token                                               │
│  来源: ConnectAgent / RefreshAgentToken / 心跳续期 返回       │
│  有效期: 15分钟 (accessTokenDuration)                        │
│  用途: 后续所有 agent API 调用 (Bearer)                       │
│  特性: 短期，心跳时剩余 < 1/3（即 <5min）时透明续期            │
│  存储: 仅内存                                                │
├─────────────────────────────────────────────────────────────┤
│  Refresh Token                                              │
│  来源: ConnectAgent（仅 bootstrap 路径）/ RefreshAgentToken   │
│  有效期: 24小时 (refreshTokenDuration)                       │
│  用途: access token 过期后换取新 access + refresh             │
│  特性: 单次使用轮换；重放 CONSUMED/REVOKED → 吊销整个 family  │
│  存储: manager 侧按 SHA-256 hash 存 agent_token 表；          │
│        【已变更】agent 端文件持久化 (~/.laelia/agent-token)    │
│        未实现 —— 当前 daemon 不走 agent-token 路径             │
└─────────────────────────────────────────────────────────────┘
```

JWT claims（`backend/manager/api/auth/auth.go`）：`token_type`（BOOTSTRAP/ACCESS/REFRESH）、`session_id`、`token_family`、`token_version`，audience 为 `ll.agent.access.<mode>`（machine/provisioner/user 各有独立 audience，拦截器按 audience 分支解析）。

### 1.2 连接生命周期

> 已实现（manager 侧 `agent_connection.go`）。注意两点与原设计的差异：
> ① ConnectAgent 仅在 bootstrap 路径上签发 access+refresh；用 access token 重连时不再重复签发（避免 hash 碰撞与 refresh 表无限增长）。
> ② 当前 daemon 实际走 §七 的 machine 通道，下面是 agent-token 路径的设计/实现流程。

```
Agent 启动 (--token <bootstrap_token>)
    │                                      （以下为 agent-token 路径设计流程；
    ├─ 尝试从 ~/.laelia/agent-token 加载 refresh_token ──┐   客户端文件持久化未实现）
    │                                                     │
    ├─ 有 refresh_token?                                  │
    │   ├─ YES → RefreshAgentToken(refresh_token)         │
    │   │         ├─ 成功 → 拿到新 access+refresh → 心跳循环
    │   │         └─ 失败（过期/吊销）→ 用 --token fallback ──┐
    │   │                                                     │
    │   └─ NO → 用 --token (bootstrap) ─────────────────────┤
    │                                                       │
    ├─ ConnectAgent(bootstrap_token, info, fingerprint)    │
    │   └─ 成功 → 拿到 access_token + refresh_token         │
    │            + session_id + server_nonce                │
    │            持久化 refresh_token 到文件                  │
    │            进入心跳循环                                │
    │                                                       │
    ├─ 心跳循环 (每30秒):                                   │
    │   ├─ Heartbeat(session_id, previous_nonce, metrics)  │
    │   ├─ 成功 → 更新 nonce, 检查 response 中的续期 token  │
    │   │   └─ 如果 access_token 剩余 < 5min → response 包含新 access_token
    │   └─ 失败 → 指数退避重试（最多1分钟间隔）              │
    │                                                       │
    ├─ Access Token 过期:                                   │
    │   └─ RefreshAgentToken(refresh_token, fingerprint)   │
    │       ├─ 成功 → 拿到新 access + refresh → 继续心跳    │
    │       └─ 失败 → 用 --token 重新 ConnectAgent          │
    │                                                       │
    └─ 优雅退出 (SIGTERM/SIGINT):                           │
        └─ AgentDisconnect(session_id, reason="shutdown")  │
            → 删除 refresh_token 文件                       │
            → 退出                                         │
```

### 1.3 Refresh Token 重用检测

> **已变更**：最终实现没有采用"幂等重放窗口"，而是更保守的"重放即盗窃"策略（`token_refresh.go` 的 `validateRefreshToken` + `agent_token.go` 的 `refreshReuseAction`，与 machine 侧共用）：

```go
// Refresh token 状态机（storepb.AgentTokenState）
//   ACTIVE   → 正常轮换：标记旧 token 为 CONSUMED（记录 consumed_at），
//              30 秒后定时器将其置为 REVOKED（scheduleTokenRevoke）
//   CONSUMED → 重用！吊销整个 token family，返回 PermissionDenied
//   REVOKED  → 重用！同上
//
// 另有两条防线：
//   - fingerprint 绑定：请求携带的 fingerprint 与存储的不一致 → 拒绝
//   - token_version 绑定：JWT 中的版本 ≠ agent 当前 TokenVersion → 吊销 family 并拒绝
```

即 30 秒窗口只是 CONSUMED→REVOKED 的延迟状态转移；窗口内的二次使用同样触发 family 吊销，不做幂等补偿。刷新成功时新 refresh token 与旧 token 同 family（`{resourceID}:v{version}`，RotateAgentToken 时 bump 版本并换新 family）。

### 1.4 并发会话策略

> 已实现，但形式比原设计更简单：无 `agent.max_concurrent_sessions` 配置项，新连接直接 `TerminateAllAgentSessions(agent.ID, "replaced")` 终结该 agent 全部旧会话（machine 侧对偶为 `TerminateAllMachineSessions`）。旧会话下次心跳携带 session_id 时收到 `CodePermissionDenied`（"session has been replaced by a new connection"）。

---

## 二、Proto 协议重新设计

> **已实现并扩展**：现行协议见 `proto/v1/v1/agent.proto`（与 `proto/store/store/agent.proto`）。下列代码块是设计快照，与现行 proto 的主要差异：
> - 管理端 RPC 远多于设计：新增 `UpdateAgent`、`TransferAgentOwnership`、`StopAgent`/`StartAgent`/`RestartAgent`、`UpdateAgentACPConfig`、`UpdateAgentMcpConfig`、`RefreshAgentProviders`/`RefreshAgentModels`、`ListAgentWorkspace`/`ReadAgentWorkspaceFile`、`ListPiModels`、头像三 RPC 等。权限不是"仅管理员"一刀切：大部分 RPC 无 permission 注解、由 handler 按"owner 或 `laelia.agents.edit`"判定（`canEditAgent`）；`ListAgents`/`GetAgent` 要求 `laelia.agents.get`，`ListAgentSessions` 要求 `laelia.agents.listSessions`（见 `backend/common/permission/permission_gen.go`）。
> - `CreateAgent` 是 machine-scoped：handler 用 `laelia.machines.createAgent` 对机器的 IAM policy 判定，agent 创建时必须绑定 machine（`agent.machine`）。
> - `ConnectAgentResponse` 额外返回 `acp_config`（服务端解析后的 ACP 配置）；`AgentHeartbeatResponse` 额外返回 `command_stream_required` + `pending_command_hint`（bidi 命令流不可用时的兜底命令提示）。
> - `AgentStatus.ConnectionState` 增加 `STOPPED = 5`（StopAgent 停用态），共 6 个状态。
> - `AgentSession.state` 直接复用 `AgentStatus.ConnectionState`。
> - agent-token 连接 RPC（ConnectAgent 等）的调用方已由 §七 的 machine 客户端取代，proto 保留。

### 2.1 完整 agent.proto（设计快照）

```protobuf
syntax = "proto3";

package laelia.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/api/resource.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/timestamp.proto";
import "v1/annotation.proto";
import "v1/common.proto";

option go_package = "github.com/Ranxy/laelia/backend/generated-go/v1";

service AgentService {
  // ========== 管理 API (IAM 认证, 仅管理员) ==========

  rpc CreateAgent(CreateAgentRequest) returns (CreateAgentResponse) {
    option (google.api.http) = {
      post: "/v1/agents"
      body: "agent"
    };
    option (laelia.v1.audit) = true;
  }

  rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse) {
    option (google.api.http) = {get: "/v1/agents"};
  }

  rpc GetAgent(GetAgentRequest) returns (Agent) {
    option (google.api.http) = {get: "/v1/{name=agents/*}"};
    option (google.api.method_signature) = "name";
  }

  rpc DeleteAgent(DeleteAgentRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {delete: "/v1/{name=agents/*}"};
    option (laelia.v1.audit) = true;
  }

  // Token 轮换: 生成新的 bootstrap token，旧 token 在宽限期后失效
  rpc RotateAgentToken(RotateAgentTokenRequest) returns (RotateAgentTokenResponse) {
    option (google.api.http) = {
      post: "/v1/{name=agents/*}:rotateToken"
      body: "*"
    };
    option (laelia.v1.audit) = true;
  }

  // Token 吊销: 吊销 agent 的所有 token（包括 bootstrap, access, refresh）
  rpc RevokeAgentToken(RevokeAgentTokenRequest) returns (RevokeAgentTokenResponse) {
    option (google.api.http) = {
      post: "/v1/{name=agents/*}:revokeToken"
      body: "*"
    };
    option (laelia.v1.audit) = true;
  }

  // 管理员强制断开 agent 连接
  rpc ForceDisconnectAgent(ForceDisconnectAgentRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      post: "/v1/{name=agents/*}:forceDisconnect"
      body: "*"
    };
    option (laelia.v1.audit) = true;
  }

  // 查询 agent 的活跃会话
  rpc ListAgentSessions(ListAgentSessionsRequest) returns (ListAgentSessionsResponse) {
    option (google.api.http) = {get: "/v1/{name=agents/*}/sessions"};
  }

  // ========== Agent 连接 API (CUSTOM 认证, agent token) ==========

  // Agent 首次连接或使用 bootstrap token 连接
  rpc ConnectAgent(ConnectAgentRequest) returns (ConnectAgentResponse) {
    option (google.api.http) = {
      post: "/v1/agents:connect"
      body: "*"
    };
    option (laelia.v1.auth_method) = CUSTOM;
    option (laelia.v1.audit) = true;
  }

  // Agent 心跳
  rpc AgentHeartbeat(AgentHeartbeatRequest) returns (AgentHeartbeatResponse) {
    option (google.api.http) = {
      post: "/v1/agents:heartbeat"
      body: "*"
    };
    option (laelia.v1.auth_method) = CUSTOM;
    // 心跳不走全量审计，仅记录异常（采样审计）
  }

  // Agent 主动断开连接
  rpc AgentDisconnect(AgentDisconnectRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      post: "/v1/agents:disconnect"
      body: "*"
    };
    option (laelia.v1.auth_method) = CUSTOM;
    option (laelia.v1.audit) = true;
  }

  // Agent 刷新 access token
  rpc RefreshAgentToken(RefreshAgentTokenRequest) returns (RefreshAgentTokenResponse) {
    option (google.api.http) = {
      post: "/v1/agents:refreshToken"
      body: "*"
    };
    option (laelia.v1.auth_method) = CUSTOM;
    option (laelia.v1.audit) = true;
  }

  // 健康检查（无需认证）
  rpc Hello(HelloRequest) returns (HelloResponse) {
    option (google.api.http) = {
      post: "/v1/agent/hello"
      body: "*"
    };
    option (laelia.v1.allow_without_credential) = true;
    option (laelia.v1.audit) = false;
  }
}

// ========== 请求/响应消息 ==========

// --- 创建 Agent ---

message CreateAgentRequest {
  Agent agent = 1 [(google.api.field_behavior) = REQUIRED];
}

// 改为返回独立响应（不再直接返回 Agent + token）
message CreateAgentResponse {
  Agent agent = 1;
  string bootstrap_token = 2;  // 7天有效，可重用直到轮换/吊销
}

// --- Token 管理 ---

message RotateAgentTokenRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
  string reason = 2;  // 审计用途
}

message RotateAgentTokenResponse {
  string bootstrap_token = 1;  // 新的 bootstrap token
}

message RevokeAgentTokenRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
  string reason = 2;
}

message RevokeAgentTokenResponse {}

// --- 强制断开 ---

message ForceDisconnectAgentRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
  string reason = 2;
}

// --- 会话管理 ---

message ListAgentSessionsRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
  int32 page_size = 2;
  string page_token = 3;
  bool include_terminated = 4;
}

message ListAgentSessionsResponse {
  repeated AgentSession sessions = 1;
  string next_page_token = 2;
}

message AgentSession {
  string session_id = 1;
  string agent_name = 2;
  string source_ip = 3;
  string agent_version = 4;
  string fingerprint = 5;
  google.protobuf.Timestamp connected_at = 6;
  google.protobuf.Timestamp last_heartbeat_at = 7;
  google.protobuf.Timestamp disconnected_at = 8;
  string disconnect_reason = 9;
  ConnectionState state = 10;
}

// --- Agent 连接 ---

message ConnectAgentRequest {
  string bootstrap_token = 1;   // 首次连接或在 refresh 失效后使用
  AgentInfo info = 2;
  string fingerprint = 3;        // agent 生成的连接指纹 (hostname:os:arch)
}

message ConnectAgentResponse {
  string access_token = 1;           // 15分钟有效
  string refresh_token = 2;          // 24小时有效，单次使用轮换
  string session_id = 3;             // 会话标识
  string next_nonce = 4;             // 服务端签名 nonce，下次心跳必须携带
  google.protobuf.Timestamp access_token_expires_at = 5;
  AgentStatus initial_status = 6;
}

// --- Agent 心跳 ---

message AgentHeartbeatRequest {
  string session_id = 1;
  string previous_nonce = 2;         // 上次 heartbeat 返回的 nonce（重放防护）
  AgentMetrics metrics = 3;          // 可选：agent 运行指标
}

message AgentHeartbeatResponse {
  string next_nonce = 1;              // 下次心跳使用的 nonce
  google.protobuf.Timestamp next_heartbeat_at = 2;  // 期望下次心跳时间
  string access_token = 3;            // 仅在当前 access token 剩余 < 5分钟时返回
  google.protobuf.Timestamp access_token_expires_at = 4;
}

// --- Agent 主动断开 ---

message AgentDisconnectRequest {
  string session_id = 1;
  string reason = 2;  // "shutdown", "upgrade" 等
}

// --- Token 刷新 ---

message RefreshAgentTokenRequest {
  string refresh_token = 1;
  string fingerprint = 2;            // 验证连接指纹
}

message RefreshAgentTokenResponse {
  string access_token = 1;
  string refresh_token = 2;          // 新的 refresh token（轮换）
  google.protobuf.Timestamp access_token_expires_at = 3;
}

// --- 列表/查询 ---

message ListAgentsRequest {
  int32 page_size = 1;
  string page_token = 2;
  bool show_deleted = 3;
}

message ListAgentsResponse {
  repeated Agent agents = 1;
  string next_page_token = 2;
}

message GetAgentRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
}

message DeleteAgentRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference) = {type: "laelia/Agent"}
  ];
}

// --- Hello ---

message HelloRequest {}

message HelloResponse {
  int64 current_time = 1;
  string server_version = 2;
}

// --- Agent 定义 ---

message Agent {
  option (google.api.resource) = {
    type: "laelia/Agent"
    pattern: "agents/{agent}"
  };

  string name = 1;
  State state = 2;
  string title = 3;
  reserved 4;
  reserved "token";
  AgentInfo info = 5;
  AgentStatus status = 6;
  google.protobuf.Timestamp created_at = 7;
  map<string, string> labels = 8;
  google.protobuf.Timestamp last_token_rotated_at = 9;
  int32 token_version = 10;
}

message AgentInfo {
  string agent_type = 1;
  string hostname = 2;
  string os = 3;
  string arch = 4;
  string ip = 5;
  string version = 6;
  map<string, string> labels = 7;
}

message AgentStatus {
  enum ConnectionState {
    CONNECTION_STATE_UNSPECIFIED = 0;
    ONLINE = 1;
    OFFLINE = 2;
    ERROR = 3;
    KICKED = 4;
  }
  ConnectionState state = 1;
  google.protobuf.Timestamp last_heartbeat_time = 2;
  google.protobuf.Timestamp connected_time = 3;
  string error_message = 4;
  string active_session_id = 5;
}

// --- Agent 指标 ---

message AgentMetrics {
  double cpu_percent = 1;
  uint64 memory_used_bytes = 2;
  uint64 memory_total_bytes = 3;
  uint64 disk_used_bytes = 4;
  uint64 disk_total_bytes = 5;
  uint32 uptime_seconds = 6;
  uint32 goroutine_count = 7;
}
```

### 2.2 与现有 proto 的变更对比

| 变更 | 原有 | 新增/修改 | 说明 |
|------|------|----------|------|
| `CreateAgent` 返回 | `Agent` (含 token) | `CreateAgentResponse` (agent + bootstrap_token) | token 不再嵌在 Agent 里 |
| `Agent.token` | 字段 4 | `reserved 4` | 不再通过 API 返回 |
| `ConnectAgentRequest` | `{AgentInfo info}` | 新增 `bootstrap_token`, `fingerprint` | |
| `ConnectAgentResponse` | `空` | 返回 access_token, refresh_token, session_id, nonce | |
| `AgentHeartbeatRequest` | `空` | 新增 `session_id`, `previous_nonce`, `metrics` | |
| `AgentHeartbeatResponse` | `空` | 返回 `next_nonce`, 续期 `access_token` | |
| `AgentDisconnect` | 无 | 新增 RPC (agent 主动断开) | |
| `ForceDisconnectAgent` | 无 | 新增 RPC (管理员强制断开) | |
| `RefreshAgentToken` | 无 | 新增 RPC (token 刷新) | |
| `RotateAgentToken` | 无 | 新增 RPC (管理员轮换 token) | |
| `RevokeAgentToken` | 无 | 新增 RPC (管理员吊销 token) | |
| `ListAgentSessions` | 无 | 新增 RPC | |
| `AgentStatus` | 3 种状态 | 新增 `KICKED` 状态 | |
| `Hello` | 无 auth 注解 | `allow_without_credential = true` | 明确无需认证 |

---

## 三、数据库 Schema 设计

### 3.1 迁移脚本

> **已实现**：以下为 `backend/manager/migration/migration/LATEST.sql` 中的实际表结构（增量迁移在各 `{MAJOR.MINOR}/` 目录）。machine 侧另有对偶的 `machine_session` / `machine_token` 表（含 `idx_machine_token_hash` 唯一索引）。

```sql
-- agent_session 表: 追踪活跃会话
CREATE TABLE agent_session (
    id bigserial PRIMARY KEY,
    session_id text NOT NULL UNIQUE,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    token_family text NOT NULL,
    state text NOT NULL DEFAULT 'ACTIVE',    -- ACTIVE / KICKED / TERMINATED...
    source_ip text NOT NULL DEFAULT '',
    fingerprint text NOT NULL DEFAULT '',
    agent_version text NOT NULL DEFAULT '',
    connected_at timestamptz NOT NULL DEFAULT now(),
    disconnected_at timestamptz,
    last_heartbeat_at timestamptz NOT NULL DEFAULT now(),
    disconnect_reason text,
    metadata jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_agent_session_agent ON agent_session(agent_id, state);
CREATE INDEX idx_agent_session_session ON agent_session(session_id);
CREATE INDEX idx_agent_session_active ON agent_session(state, last_heartbeat_at);

-- agent_token 表: 跟踪 token 状态 (reuse detection)
CREATE TABLE agent_token (
    id bigserial PRIMARY KEY,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    token_hash text NOT NULL,               -- SHA-256(bootstrap_token 或 refresh_token)
    token_type text NOT NULL DEFAULT 'BOOTSTRAP',  -- BOOTSTRAP / ACCESS / REFRESH
    token_family text NOT NULL,
    state text NOT NULL DEFAULT 'ACTIVE',   -- ACTIVE, CONSUMED, REVOKED
    fingerprint text NOT NULL DEFAULT '',
    source_ip text NOT NULL DEFAULT '',
    issued_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_by text NOT NULL DEFAULT ''
);

CREATE INDEX idx_agent_token_hash ON agent_token(token_hash);  -- 迁移后追加 UNIQUE 约束
CREATE INDEX idx_agent_token_family ON agent_token(token_family, state);
CREATE INDEX idx_agent_token_agent ON agent_token(agent_id, token_type, state);

-- 扩展 agent 表
ALTER TABLE agent ADD COLUMN last_token_rotated_at timestamptz;
-- token_version 字段已存在，无需新增
```

注意：`idx_agent_token_hash` 在后续增量迁移中升级为 **UNIQUE** 索引 —— 这正是"ConnectAgent 仅在 bootstrap 路径签发 token"的原因（同秒重复签发同一 token 会撞唯一约束，见 `agent_connection.go` 注释）。

### 3.2 Agent Auth 拦截器变更

> **已实现**：`backend/manager/api/auth/auth.go`。claims 结构与设计一致并已落地：

```go
type agentClaimsMessage struct {
    Name         string `json:"name"`
    TokenVersion int    `json:"token_version"`
    TokenType    string `json:"token_type"`    // "BOOTSTRAP" / "ACCESS" / "REFRESH"
    SessionID    string `json:"session_id,omitempty"`
    TokenFamily  string `json:"token_family,omitempty"`
    jwt.RegisteredClaims
}
```

实际实现要点（与原设计的差异）：
- 拦截器先 `peekTokenAudience` 读未签名 payload 的 `aud` 选分支（user / agent / machine / provisioner 四种 audience：`ll.user.access.<mode>` 等），再做完整签名校验并复查 audience，避免四次解析。
- **token_type 的强制不在拦截器里统一做**：拦截器只认 audience + 签名 + token_version；`ACCESS` 类型约束在 machine 侧于 `authenticateMachineByClaims` 中强制（非 ACCESS 拒绝）。agent 侧的 bootstrap token 不走 Authorization 头 —— ConnectAgent handler 从请求体取出后自行校验（`authenticateBootstrapToken`：JWT 签名 + `TokenType==BOOTSTRAP` + 版本匹配 + DB hash 匹配 + ACTIVE + 未过期）；REFRESH 只被 `RefreshAgentToken`/`RefreshMachineToken` 的请求体接受（`ParseAgentToken`/`ParseMachineToken`）。
- CUSTOM auth_method 的 RPC（ConnectAgent / RefreshAgentToken 等）在无凭证时放行到 handler（`IsAuthenticationAllowed`），由 handler 完成认证。
- 拦截器链（`backend/manager/server/grpc_routes.go`）：DebugInterceptor → IPValidator → APIAuthInterceptor → IAMInterceptor → AuditInterceptor。

---

## 四、核心逻辑实现

### 4.1 ConnectAgent 流程

> **已实现**（`agent_connection.go`），实际流程：

```
1. 优先从 Authorization header 提取 access token（拦截器已解析出 agent）；
   若无（或解析不到 agent）→ 从请求体取 bootstrap_token，走 authenticateBootstrapToken:
   - JWT 验签（HS256, kid=v1）+ TokenType==BOOTSTRAP
   - agent 存在、未删除、token_version 匹配
   - SHA-256 hash 在 agent_token 表中存在、state=ACTIVE、未过期
2. 计算 token_family（bootstrap claims 的 token_family，缺省为 resourceID）
3. 生成 session_id（32 位随机 hex）+ 用 NonceManager 生成首个 nonce
4. 更新 agent.status = ONLINE（含 ConnectedAt/ActiveSessionId），写入 ACP 配置
5. TerminateAllAgentSessions(agent.ID, "replaced") —— 旧会话全部 KICKED
6. IP 校验：ValidateAgentIP(reportedIP, sourceIP, IPValidationWarn) —— 当前固定 WARN
7. INSERT agent_session（source_ip / fingerprint / token_family / ACTIVE）
8. 仅 bootstrap 路径：
   - 签发 access_token（15min, type=ACCESS, 绑定 session_id）
   - 签发 refresh_token（24h, type=REFRESH），SHA-256 入 agent_token 表（同 family）
   - bootstrap token 标记 CONSUMED（单次使用，防重放）
9. 解析 ACP 配置（global_provider 引用解析为 api_provider/api_key/model）后返回
   ConnectAgentResponse（含 next_nonce、initial_status、acp_config）
```

### 4.2 AgentHeartbeat 流程

> **已实现**（`agent_connection.go`），实际流程：

```
1. 从 Authorization header 提取 access_token（拦截器验证 JWT）
2. 验证 session_id（可选字段，传入即校验）:
   - 查 agent_session 表；不存在 → CodeUnauthenticated
   - session.state = KICKED → CodePermissionDenied（要求重连）
   - session.AgentID 不匹配 → CodePermissionDenied
3. 验证 previous_nonce（见 4.4）:
   - VerifyNonce 失败且 previous_nonce 非空 → 直接拒绝 CodeUnauthenticated
   - previous_nonce 为空 → 跳过校验（容忍空值）
   【已变更】原设计的"容忍一次/保留2个 nonce"未实现 —— 无宽限期
4. TouchAgentSession 更新 session.last_heartbeat_at（请求路径即时）
5. HeartbeatBuffer.Record(...) —— 内存缓冲，10s 批量刷写（见 4.5）
6. 生成新 nonce 放入 response.next_nonce
7. access token 剩余 < 1/3（15min 的 1/3 = 5min）→ 生成新 access_token 放入 response
8. 若 dispatcher 中 agent 无活跃连接，查询下一条 pending 命令放入
   response.command_stream_required / pending_command_hint（命令流兜底通道）
9. 返回 AgentHeartbeatResponse（next_heartbeat_at = now+30s）
```

### 4.3 RefreshAgentToken 流程

> **已实现**（`agent_token.go` + 共享的 `token_refresh.go`），实际流程：

```
1. 从请求体提取 refresh_token
2. JWT 验签（ParseAgentToken，不强制 token_type/version —— 由流程绑定）
3. 计算 SHA-256(refresh_token), 查 agent_token 表:
   a. 找到, state=ACTIVE:
      - 验证未过期
      - 验证 fingerprint（双方都非空时必须一致，否则视为窃取 → CodePermissionDenied）
      - 验证 principal 存在且未删除；JWT 的 token_version 必须 == agent.TokenVersion
        （不匹配 → 吊销该 family，CodeUnauthenticated）
      - 生成新 access_token (15min) + 新 refresh_token (24h, 同 family)
      - 标记旧 refresh_token 为 CONSUMED（记录 consumed_at）
      - 启动 30 秒定时器: CONSUMED → REVOKED（scheduleTokenRevoke）
      - 存储新 refresh_token 到数据库
      - 返回新 token 对
   b. 找到, state=CONSUMED 或 REVOKED:
      - 【已变更】无幂等重放窗口 —— 一律视为重用攻击:
        吊销该 token_family 全部 token，返回 CodePermissionDenied
        ("refresh token reuse detected, token family revoked")
   c. 找不到:
      - 返回 CodeUnauthenticated
```

注意：b 分支不再 bump agent.token_version、不吊销 session —— family 吊销 + 版本绑定已覆盖。该验证逻辑与 machine 侧共用（`validateRefreshToken` 注入各自查询单元）。

### 4.4 Nonce 重放防护

> **已实现**：`backend/manager/component/state/nonce.go`（不是设计稿中的 `api/auth/nonce.go`）。实际实现：

```go
// NonceManager（进程内，per-agent 对称密钥）
// 生成 nonce:
// 1. 生成 24 字节随机数
// 2. 拼接: agentResourceID + sessionID + base64url(random) + timestamp（秒）
// 3. HMAC-SHA256 签名（密钥为 per-agent 32 字节随机密钥，getOrCreateKey 惰性创建）
// 4. 输出: base64url(random) + "." + timestamp + "." + hex(hmac)
//    【已变更】时间戳显式嵌在 nonce 中（非设计稿的 random+sig 两段式）
//
// 验证 nonce:
// 1. 拆分三段，解析时间戳
// 2. 时间戳必须在 [now-35s, now+5s] 窗口内
// 3. 重新计算 HMAC 并恒时比对
// 4. recordAndCheckReplay: 一次性使用 —— 已验证过的 nonce 在 45s TTL 内
//    再次出现即为重放，拒绝（进程内 map，惰性清扫；多实例需共享，代码有 TODO(T14)）
```

**容错设计【已变更】**: 原设计的"保留最近2个 nonce / 容忍一次不匹配"未实现。实际行为（`AgentHeartbeat`）：

```
Agent 发送 previous_nonce 时:
  - 为空 → 跳过校验（容忍）
  - 非空且校验失败 → 直接返回 CodeUnauthenticated，无宽限
```

### 4.5 心跳写入优化

> **已实现**：`backend/manager/component/state/heartbeat.go` 的 `HeartbeatBuffer`，随 `state.NewWithStore` 创建。实际实现与设计稿的差异：缓冲的是 `AgentHeartbeatUpdate{AgentID, LastHeartbeatAt, SessionID}`（无 Metrics），默认 10 秒刷写（`Start`/`Stop` 管理生命周期，退出前 final flush，单飞防重复启动），刷写调用 `store.TouchAgentHeartbeats` 做**多行批量 UPDATE**（agent_session touch + agent.status 的 jsonb_set），单次刷写带 10s 超时防挂死。`GetLatest(agentID)` 也在（供读路径合并最新心跳）。

另注意：该优化只用于 **AgentHeartbeat** 路径；`MachineHeartbeat` 仍逐次 `UpdateMachine` 直接写 DB（machine 数量远小于 agent）。

### 4.6 时钟偏移处理

```go
// Hello RPC 已实现（AgentService.Hello，allow_without_credential）:
// HelloResponse { current_time: int64; server_version: string }
// Agent 启动时调用 Hello 获取服务器时间，计算偏移:
//
//   clockOffset = serverTime - localTime
//
// 【未实现】JWT 库 leeway（WithLeeway）未使用 —— 代码库中无调用；
// 时钟容错实际由 nonce 的 [-35s, +5s] 窗口承担。
parser := jwt.NewParser(jwt.WithLeeway(30*time.Second))
```

### 4.7 IP 校验

> **已实现**：`backend/manager/api/auth/iplist.go`。`IPValidationPolicy`（Off/Warn/Strict）三种策略与设计一致。与原设计的差异：
> - `extractSourceIP(header, remoteAddr, trustProxy)`：trustProxy 时取 X-Forwarded-For 最左项或 X-Real-IP，否则用 TCP peer 地址（去端口）—— 客户端伪造头在 trustProxy=false 时被完全忽略。
> - `ValidateAgentIP` 的空 sourceIP 处理是 fail-closed 的：Strict 下拒绝，Warn 下仅告警。
> - **策略当前为硬编码 `IPValidationWarn`**（`grpc_routes.go` 的 IPValidator 拦截器 + `ConnectAgent`/`ConnectMachine` 里的连接时校验），未接入 setting 表，无 STRICT 运行态。
> - IPValidator 拦截器只负责把 sourceIP 注入 context；实际的不匹配校验发生在 Connect 时（比对 agent 上报的 info.ip）。

### 4.8 TLS 方案

> **部分实现**：`backend/manager/api/auth/tls.go`。自签名 CA + 服务器证书路径已落地（`InitTLS`：加载 `certs/server.pem`/`server.key`，否则自动生成自签名 CA + 服务器证书并落盘，TLS 1.3，日志打印 CA 指纹）。与原设计的差异：

- **ACME/Let's Encrypt 自动证书未实现**：`initAutoCert` 直接返回 "not yet implemented" 错误，`Domain` 模式不可用（用手动证书或自签模式）。
- **TOFU 交互确认未实现**：`ManagerVerifier.Verify` 存在但**没有任何调用方**（未接入机器端 TLS 客户端），且行为是"首次连接直接报错并提示 --insecure 或保存指纹"，不做交互式 yes/no 确认；`SaveKnownHost` 亦无调用方。
- 机器端实际做法：`https://` 时 `tls.Config{MinVersion: TLS 1.3, InsecureSkipVerify: --insecure}`（`backend/agent/client/client.go`）；`http://` 需要 `--allow-http` 显式放行（h2c）。
- 供给 pod 场景：`LAELIA_FINGERPRINT` 环境变量可覆盖 fingerprint（provisioner 播种，见 `client.ComputeFingerprint`）。

---

## 五、限流设计

> **未实现（已变更）**：原设计的分层限流（全局/IP/Agent/User 的 `RateLimiterConfig` + LRU limiter 中间件）没有落地，代码库中不存在 `api/auth/ratelimit.go`，拦截器链中也没有限流器。当前只有 `backend/manager/api/v1/auth_service.go` 里的**针对性限流**：
> - 验证邮件重发：全局 30 次/60 秒（`resendGlobalRate`/`resendGlobalBurst`）+ 每地址 1 次/分钟（LRU 10000 个 `rate.Limiter`）。
>
> 拦截器顺序上有一条相关注释：限流器（若将来加入）必须在 auth 之后运行，否则按匿名 IP 桶分类会误伤已认证调用（见 `grpc_routes.go`）。

---

## 六、安全中间件

### 6.1 Echo HTTP 安全头中间件

> **已实现**：`backend/manager/server/echo_routes.go` 的 `securityHeadersMiddleware`。实际发送的头：`Strict-Transport-Security`（max-age=31536000; includeSubDomains）、`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`X-XSS-Protection: 1; mode=block`、`Referrer-Policy: strict-origin-when-cross-origin`。**Content-Security-Policy 未设置**（原设计中有）。

### 6.2 审计拦截器（仅记录关键事件）

> **已实现**：`backend/manager/api/v1/audit_interceptor.go`（配套 `audit_buffer.go`）。与原设计的差异：
> - 审计写经由 `AuditBuffer`（2 秒窗口批量入库），不是每条 `go stores.CreateAuditLog`。
> - 心跳采样：仅 `/laelia.v1.AgentService/AgentHeartbeat` 成功时按 1/100 采样（`heartbeatSamplingRate = 100`），失败必记录；MachineHeartbeat 不在 proto 审计注解内，不产生审计。
> - 其他 RPC 按 proto 的 `laelia.v1.audit` 注解决定是否记录（成功与失败都记）。
> - 记录字段：method、actor_type（user/agent）、actor_id、source_ip、status、error，以及 handler 通过 `SetServiceData` 附加的 resource/payload（如 IAM policy 变更详情）。

---

## 七、Agent 客户端重构

> **已变更（架构演进）**：本节原设计的"AgentClient + CredentialManager + ~/.laelia/agent-token"没有实现 —— `backend/agent/client/` 中是 **`MachineClient`**（机器端客户端），一台机器一个进程托管多个 agent。原设计的 `backend/agent/credential/` 目录不存在，凭证持久化由 `backend/agent/state/state.go` 承担（`~/.laelia/machine.json`，0600 原子写，存 manager_url / machine_id / refresh_token / hostname）。

### 7.1 当前机器端认证模型（machine-token）

```
laelia-machine setup（首次）:
  1. StartDeviceLogin → 打印审批 URL + user code，浏览器内登录用户批准
  2. PollDeviceLogin 轮询 → APPROVED 后取得 machine_id + refresh_token
  3. 持久化到 ~/.laelia/machine.json
  （--provisioned 供给 pod 模式：凭证由 provisioner 播种，死凭证直接 fail fast，
    绝不启动交互式登录；recovery = 删除并重新供给）

laelia-machine run（主循环，backend/agent/client/client.go）:
  1. Connect: 无 access token → RefreshMachineToken(refresh_token, fingerprint)
     - access token = 本次会话 bearer 凭证
     - refresh token 为【多用途】滚动续期：仅当距过期 < 10 天
       (machineRefreshRotateWindow) 时签发替换 token 并持久化；
       常规重连复用同一 token，不消费 —— 丢失响应可安全重试
       （与 agent-token 的单次轮换语义不同！）
  2. ConnectMachine(access token) → session_id + 分配的 agent 花名册
     （TerminateAllMachineSessions("replaced") 保证单活跃会话）
  3. MachineChannel 控制流（bidi）+ 每 agent 一条 AgentChannel drain 流，
     均复用机器 access token；agent 身份由流内 AgentReady.agent_name 声明，
     机器侧主动调用 RPC 时用 X-Laelia-Agent 头声明
  4. 心跳循环: MachineHeartbeat 每 30s（session_id 必填，KICKED/TERMINATED
     → PermissionDenied 强制重连）；access token 剩余 < 1/3 时响应内续期
  5. 失败重试: 指数退避 2s → 1min (defaultRetryBaseWait/MaxWait)；
     永久性认证失败（Unauthenticated/PermissionDenied）→ 退出并提示重新 setup
  6. 优雅退出: MachineDisconnect(session_id, "shutdown")；
     保留持久化 refresh token（重启后凭它重连；彻底退役用 RevokeMachineToken）
```

要点对照原设计：
- 心跳 nonce 机制保留（MachineHeartbeatRequest.previous_nonce / NextNonce），机器端在心跳时更新 `serverNonce`。
- 心跳超时 10s/次（heartbeatTimeout），控制流死亡也会触发整机重连。
- agent 级 liveness 不再由 agent 心跳推导：agent 在线 = 其 AgentChannel 存活 或 所挂机器已连接（`agentReachable`）。
- 机器指纹 `ComputeFingerprint = SHA256(hostname:os:arch)` 前 16 hex；`LAELIA_FINGERPRINT` 可覆盖（供给 pod）。

### 7.2 状态机

`MachineClient` 的 `ConnState`（Disconnected/Connecting/Connected/Disconnecting）与原设计一致，但主循环为"connect → 心跳/控制流 → 失败退避重连"结构（`Run`），不是原设计稿中的四态 switch 状态机；凭证管理收敛为 `refreshToken` 字段 + `saveRefreshToken` 回调（run.go 注入持久化）。

---

## 八、配置化安全策略

> **未实现（已变更）**：setting 表中没有新增任何 `agent.*` / `security.*` 安全配置项（LATEST.sql 的 setting 种子中无此类 key），运营时动态调整安全阈值的能力不存在。当前所有阈值都是 Go 常量：

| 阈值 | 值 | 位置 |
|------|-----|------|
| accessTokenDuration | 15 min | `backend/manager/api/v1/agent.go` |
| refreshTokenDuration | 24 h | 同上 |
| bootstrapTokenDuration | 7 天 | 同上 |
| refreshTokenReuseWindow | 30 s | 同上 |
| machineRefreshTokenDuration | 90 天 | `backend/manager/api/v1/machine.go` |
| machineRefreshRotateWindow | 10 天 | 同上 |
| 心跳间隔 / NextHeartbeatAt | 30 s | `agent_connection.go` / `client.go`（机器端 `defaultHeartbeatInterval`） |
| HeartbeatBuffer 刷写 | 10 s | `component/state/heartbeat.go` |
| IP 校验策略 | 硬编码 WARN | `grpc_routes.go` / `agent_connection.go` |
| 审计心跳采样 | 1/100 | `audit_interceptor.go` |

---

## 九、涉及文件现状

> 原清单是设计稿的变更计划；下表按当前代码核对（✓ 存在，✗ 不存在/未实现）：

| 文件 | 现状 | 说明 |
|------|------|------|
| `proto/v1/v1/agent.proto` | ✓ 已实现并大幅扩展 | RPC 远多于设计稿（见 §二） |
| `proto/store/store/agent.proto` | ✓ | AgentTokenType/AgentTokenState 枚举 + AgentInfo JSONB 形状 |
| `backend/manager/migration/migration/LATEST.sql` | ✓ | agent_session / agent_token（原稿表名 agent_refresh_token 未采用）、machine_session / machine_token；setting 安全项未加 |
| `backend/manager/api/v1/agent.go` | ✓ | AgentService 主体 + 时长常量 |
| `backend/manager/api/v1/agent_token.go` | ✓ | Rotate/Revoke/Refresh/Hello |
| `backend/manager/api/v1/agent_connection.go` | ✓ | Connect/Heartbeat/Disconnect/ForceDisconnect/ListSessions |
| `backend/manager/api/v1/token_refresh.go` | ✓ | agent/machine 共用的 refresh 验证 |
| `backend/manager/api/v1/machine_token.go` / `machine_connection.go` | ✓ | machine 对偶实现 |
| `backend/manager/api/auth/auth.go` | ✓ | JWT 签发/解析、四类 audience 分支、claims |
| `backend/manager/api/auth/tls.go` | ✓ | InitTLS + 自签 CA；ACME 未实现；ManagerVerifier 无调用方 |
| `backend/manager/api/auth/iplist.go` | ✓ | IPValidationPolicy + ValidateAgentIP |
| `backend/manager/api/auth/nonce.go` | ✗ | nonce 在 `component/state/nonce.go` |
| `backend/manager/api/auth/ratelimit.go` | ✗ 未实现 | 限流未做（仅 auth_service.go 针对性限流） |
| `backend/manager/store/agent_session.go` / `agent_token.go` | ✓ | 会话/token CRUD、TerminateAll/Revoke family |
| `backend/manager/server/grpc_routes.go` | ✓ | 拦截器链：Debug→IPValidator→Auth→IAM→Audit |
| `backend/manager/server/echo_routes.go` | ✓ | securityHeadersMiddleware |
| `backend/manager/component/state/` | ✓ | state.go (TokenExpireCache LRU 128) + nonce.go + heartbeat.go |
| `backend/agent/client/client.go` | ✓ | MachineClient（refresh/Connect/Heartbeat/Disconnect/退避/TLS） |
| `backend/agent/cmd/run.go` / `setup.go` | ✓ | run 主循环 + 设备码登录/供给模式 |
| `backend/agent/state/state.go` | ✓ | machine.json 持久化（原稿的 credential/ 目录不存在） |
| `backend/common/permission/` | ✓ | `laelia.agents.create/get/edit/listSessions` 等（permission.json 单一来源生成） |
| `backend/common/context.go` | ✓ | SourceIPContextKey / AuthContextKey / AgentContextKey 等 |

---

## 十、实施顺序

> **历史计划，已执行完毕**（含偏离）：第 1-4 周的 P0/P1 主体均已落地；其中"限流中间件"未实施（见 §五），"Agent TOFU" 缩水为自签 CA + `--insecure`（见 §4.8），其余按 §七 的 machine-token 演进形态实现。下表保留为原始排期记录。

```
第1周: 基础安全 (P0)
├── Day 1-2: TLS (自签名CA + 自动生成 + Agent TOFU)
├── Day 2-3: Token 有效期缩短 (agent: 365d → bootstrap 7d)
├── Day 3-4: 限流中间件
└── Day 4-5: 安全头中间件 + IP 校验

第2周: Token 体系 (P0)
├── Day 1-2: 数据库迁移 (agent_session, agent_refresh_token)
├── Day 2-3: JWT claims 扩展 (token_type, session_id, token_family)
├── Day 3-4: ConnectAgent 新流程 (bootstrap → access + refresh)
└── Day 4-5: RefreshAgentToken + reuse detection

第3周: 协议完善 (P1)
├── Day 1-2: Heartbeat nonce 实现
├── Day 2-3: AgentDisconnect + ForceDisconnectAgent
├── Day 3-4: RotateAgentToken + RevokeAgentToken
└── Day 4-5: 心跳数据库优化 (批量写入) + 采样审计

第4周: Agent 重构 (P1)
├── Day 1-2: Agent 状态机 + 重连逻辑
├── Day 2-3: 凭证管理器 (文件持久化 + fallback)
├── Day 3-4: 并发会话检测 + KICKED 状态
└── Day 4-5: 测试 + 集成验证
```

---

## 十一、安全改进对照表

> 按当前代码核对后的实际状态：

| 安全问题 | 改进前 | 改进后（实际） |
|----------|--------|--------|
| 传输加密 | 纯 HTTP 明文 | TLS 1.3 自签名 CA（✓）；ACME 自动证书（✗ 未实现）；TOFU 指纹校验（✗ 未接线，--insecure 跳过） |
| Agent/Machine Token 有效期 | 365天 | Bootstrap 7天（单次使用）, Access 15分钟, Agent Refresh 24小时, Machine Refresh 90天滚动续期 |
| Token 吊销 | 128 条目 LRU, 重启丢失 | 数据库持久化 + token_version + token family 吊销（LRU TokenExpireCache 仍在，仅作过期加速） |
| Token 轮换 | 无 | RotateAgentToken / RevokeAgentToken API + refresh rotation（agent 单次 / machine 滚动） |
| 重放攻击 | 心跳空 body, 无防护 | Nonce 链 + HMAC 签名 + 服务端一次性重放缓存（[-35s,+5s] 窗口） |
| 限流 | 无 | 仅针对性限流（验证邮件重发）；分层限流 ✗ 未实现 |
| IP 校验 | 无 | 已实现 OFF/WARN/STRICT，当前固定 WARN（不可配置） |
| 并发会话 | 无检测 | 单活跃会话（新连接终结旧会话 "replaced"）+ KICKED 机制 |
| 优雅断开 | 无 | AgentDisconnect / MachineDisconnect + ForceDisconnect(Agent|Machine) |
| 审计日志 | 拦截器被注释 | 已启用 + AgentHeartbeat 1/100 采样 + AuditBuffer 批量入库 |
| 密钥轮换 | 单一 kid="v1" | 仍为单一 kid="v1"，多 kid rotation ✗ 未实现 |
| 安全头 | 无 | HSTS/X-Frame-Options/nosniff/X-XSS-Protection/Referrer-Policy（CSP ✗） |
| Agent/机器重连 | 无重试 | 指数退避 2s→1min；永久认证失败退出提示重新 setup（machine）/ 重新 bootstrap（agent） |
| 会话追踪 | 无 | agent_session / machine_session 表 |
| Token 泄露窗口 | 365天 | 15分钟（access token） |
