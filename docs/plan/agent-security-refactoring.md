# Laelia Agent-Manager 通信安全重构方案

## 设计前提

| 约束 | 说明 |
|------|------|
| 无需迁移/兼容 | 项目未上线，可直接破坏性变更 |
| 单实例部署 | 暂不考虑多实例，缓存用进程内方案 |
| 仅改 Agent Token | 用户侧认证保持现有机制不变，两套共存 |
| Token 持久化 | Agent 优先用文件中的 refresh token，fallback 到 `--token` |

---

## 一、Agent Token 体系设计

### 1.1 分层 Token 模型

```
┌─────────────────────────────────────────────────────────────┐
│  Bootstrap Token                                            │
│  来源: CreateAgent API 返回                                  │
│  有效期: 7天（可配置）                                        │
│  用途: 首次连接 或 refresh token 失效后 fallback              │
│  特性: 可重用，直到管理员吊销或轮换                             │
│  存储: 仅通过 --token 参数传入，不持久化                        │
├─────────────────────────────────────────────────────────────┤
│  Access Token                                               │
│  来源: ConnectAgent / RefreshAgentToken 返回                 │
│  有效期: 15分钟（可配置）                                     │
│  用途: Heartbeat 及后续所有 agent API 调用                    │
│  特性: 短期，心跳时可透明续期                                  │
│  存储: 仅内存                                                │
├─────────────────────────────────────────────────────────────┤
│  Refresh Token                                              │
│  来源: ConnectAgent / RefreshAgentToken 返回                 │
│  有效期: 24小时（可配置）                                     │
│  用途: access token 过期后换取新 access + refresh              │
│  特性: 单次使用轮换，重试窗口30秒                              │
│  存储: 内存 + 文件持久化 (~/.laelia/agent-token, 0600权限)     │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 连接生命周期

```
Agent 启动 (--token <bootstrap_token>)
    │
    ├─ 尝试从 ~/.laelia/agent-token 加载 refresh_token ──┐
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

### 1.3 Refresh Token 重试窗口

解决网络重试导致的假阳性吊销问题：

```go
// Refresh token 状态机
const (
    RefreshActive   = "ACTIVE"     // 可用
    RefreshConsumed = "CONSUMED"   // 已使用，保留明文30秒（重试窗口）
    RefreshRevoked  = "REVOKED"    // 已吊销
)

// 刷新逻辑：
// 1. 收到 RefreshAgentToken(R1)
// 2. 检查 R1 状态：
//    - ACTIVE → 正常处理，生成新的 access+refresh，标记 R1 为 CONSUMED，
//              启动30秒定时器后标记为 REVOKED
//    - CONSUMED → 返回上一次的响应（幂等），不生成新 token
//    - REVOKED → 拒绝，这可能是窃取重用，吊销整个 token family
// 3. 如果同一 family 中出现已被 REVOKED 的 refresh token → 吊销该 agent 所有 token
```

### 1.4 并发会话策略

```
同一 Agent 同时只允许一个活跃 session:
  1. 新 ConnectAgent 到达 → 查找该 agent 是否有活跃 session
  2. 如果有 → 标记旧 session 为 KICKED，旧连接下次心跳时收到 KICKED 错误
  3. 新 session 建立，记录 connected_at、source_ip 等
  4. 旧连接收到 KICKED → Agent 退出或重连

  配置项: agent.max_concurrent_sessions = 1 (默认)
```

---

## 二、Proto 协议重新设计

### 2.1 完整 agent.proto

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

```sql
-- agent_session 表: 追踪活跃会话
CREATE TABLE agent_session (
    id bigserial PRIMARY KEY,
    session_id text NOT NULL UNIQUE,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    token_family text NOT NULL,              -- token family 标识 (for rotation detection)
    state text NOT NULL DEFAULT 'ACTIVE',    -- ACTIVE, TERMINATED, KICKED
    source_ip text NOT NULL,
    fingerprint text NOT NULL,                -- hostname:os:arch
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

-- agent_refresh_token 表: 跟踪 refresh token 状态 (用于 reuse detection)
CREATE TABLE agent_refresh_token (
    id bigserial PRIMARY KEY,
    agent_id int NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    token_hash text NOT NULL,               -- SHA-256(bootstrap_token 或 refresh_token)
    token_type text NOT NULL DEFAULT 'BOOTSTRAP',  -- BOOTSTRAP / REFRESH
    token_family text NOT NULL,             -- family 标识 (同一 bootstrap 衍生的 token 属同一家族)
    state text NOT NULL DEFAULT 'ACTIVE',   -- ACTIVE, CONSUMED, REVOKED
    fingerprint text,                        -- 首次使用时的连接指纹
    source_ip text,
    issued_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_by text                          -- 哪个用户创建的
);

CREATE INDEX idx_agent_refresh_token_hash ON agent_refresh_token(token_hash);
CREATE INDEX idx_agent_refresh_token_family ON agent_refresh_token(token_family, state);
CREATE INDEX idx_agent_refresh_token_agent ON agent_refresh_token(agent_id, token_type, state);

-- 扩展 agent 表
ALTER TABLE agent ADD COLUMN last_token_rotated_at timestamptz;
-- token_version 字段已存在，无需新增
```

### 3.2 Agent Auth 拦截器变更

现有 `auth.go` 中的 `getUserOrAgentConnect` 需要扩展：

```go
// 新增: token 类型识别
//
// JWT claims 中增加 token_type 字段:
//   BOOTSTRAP: 仅用于 ConnectAgent 和 RefreshAgentToken
//   ACCESS:    用于 Heartbeat 等常规 API
//   REFRESH:   仅用于 RefreshAgentToken

type agentClaimsMessage struct {
    Name         string `json:"name"`
    TokenVersion int    `json:"token_version"`
    TokenType    string `json:"token_type"`    // 新增: "BOOTSTRAP" / "ACCESS"
    SessionID    string `json:"session_id"`    // 新增: ACCESS token 绑定的会话
    TokenFamily  string `json:"token_family"`  // 新增: token family 标识
    jwt.RegisteredClaims
}

// 拦截器逻辑:
// 1. ConnectAgent → 接受 BOOTSTRAP 类型
// 2. Heartbeat + DisconnectAgent → 仅接受 ACCESS 类型
// 3. RefreshAgentToken → 接受 BOOTSTRAP 和 REFRESH 类型
// 4. 其他 (ListAgents, CreateAgent 等) → 仅接受 IAM (用户 token)
```

---

## 四、核心逻辑实现

### 4.1 ConnectAgent 流程

```
1. 提取 bootstrap_token (从请求体) 或 access_token (从 Authorization header)
2. 验证 token:
   a. 如果是 bootstrap_token:
      - 查数据库 agent_refresh_token 表, 验证 hash 匹配且 state=ACTIVE
      - 验证 bootstrap_token 未过期 (expires_at > now)
      - 验证 agent 存在、未删除、token_version 匹配
   b. 如果是 access_token:
      - 标准 JWT 验证 (同现有逻辑)
3. 计算/验证 fingerprint:
   - fingerprint = SHA256(hostname + os + arch)
   - 存入 session 和 token 记录
4. 检查并发会话:
   - 查找该 agent 是否有 ACTIVE session
   - 如果有 → 标记旧 session.state = KICKED, 原因 "replaced"
5. 创建新 session (INSERT agent_session)
6. 生成 token:
   - access_token: 15min, type=ACCESS, 包含 session_id
   - refresh_token: 24h, type=REFRESH, 新 token_family (或沿用的)
7. 存储 refresh_token 到 agent_refresh_token 表
8. 标记 bootstrap_token 为 CONSUMED (如果使用的是 bootstrap)
9. 更新 agent.status = ONLINE
10. 返回 ConnectAgentResponse
```

### 4.2 AgentHeartbeat 流程

```
1. 从 Authorization header 提取 access_token
2. 验证 JWT (access_token, type=ACCESS)
3. 验证 session_id:
   - 查 agent_session 表, session 必须是 ACTIVE 状态
   - 如果 session.state = KICKED → 返回 CodePermissionDenied, 要求重连
   - 如果 session 不存在 → 返回 CodeUnauthenticated
4. 验证 previous_nonce:
   - 从 session.metadata 中取出上次签发的 nonce
   - 计算 HMAC-SHA256(nonce_string, server_key) 与客户端发送的比对
   - 验证通过 → 生成新 nonce, 存入 session.metadata
   - 验证失败 → 容忍一次 (可能是网络重传), 记录告警, 发出新 nonce
   - 连续失败 2 次 → 拒绝请求
5. (可选上限采样审计: 每100次心跳记录1次, 或仅在异常时记录)
6. 更新 agent_session.last_heartbeat_at = now()
7. 检查 access_token 是否剩余 < 5min:
   - 是 → 生成新 access_token, 放入 response
   - 否 → response.access_token 为空
8. 更新 agent.status.last_heartbeat_at (内存缓存, 批量写DB)
9. 返回 AgentHeartbeatResponse (next_nonce, 可选 access_token)
```

### 4.3 RefreshAgentToken 流程

```
1. 从请求体提取 refresh_token
2. 计算 SHA-256(refresh_token), 查 agent_refresh_token 表:
   a. 找到, state=ACTIVE:
      - 验证未过期
      - 验证 fingerprint 匹配 (如果请求中有)
      - 生成新 access_token (15min) + 新 refresh_token (24h, 同 family)
      - 标记旧 refresh_token 为 CONSUMED, 设置 consumed_at
      - 启动30秒定时器: CONSUMED → REVOKED
      - 存储新 refresh_token 到数据库
      - 更新 session.last_heartbeat_at
      - 返回新 token 对
   b. 找到, state=CONSUMED (在30秒重试窗口内):
      - 幂等返回: 重新生成一对 access+refresh, 但使用相同的 family
      - 不触发 family 吊销
      - 标记旧 token 为 REVOKED (立即)
      - 返回新 token 对
   c. 找到, state=REVOKED:
      - 安全事件: refresh token reuse detected!
      - 吊销该 token_family 下所有 token
      - 吊销 agent 的所有 session
      - bump agent.token_version
      - 返回 CodeUnauthenticated, 要求重新 bootstrap
   d. 找不到:
      - 返回 CodeUnauthenticated
```

### 4.4 Nonce 重放防护

```go
type NonceManager struct {
    secrets map[string][]byte  // agent_id → HMAC key
    mu      sync.RWMutex
}

// 生成 nonce:
// 1. 生成 24 字节随机数
// 2. 拼接: agent_id + session_id + random + timestamp
// 3. HMAC-SHA256 签名
// 4. 输出: base64url(random_bytes) + "." + hex(hmac_signature)
// 5. 存入 session metadata

// 验证 nonce:
// 1. 拆分 nonce → random_bytes + hmac
// 2. 重新计算 HMAC-SHA256(agent_id + session_id + random_bytes + expected_timestamp, key)
// 3. 比对签名
// 4. 比对 timestamp 在 [server_time - 35s, server_time + 5s] 范围内
//    (容忍时钟偏差 ±5s)
```

**容错设计**: 如果 Agent 未收到上次心跳的响应（网络超时），nonce 不匹配：

```
服务端保留 session 中最近2个 nonce (当前 + 上一个)
Agent 发送 previous_nonce 时:
  - 匹配当前 nonce → 正常
  - 匹配上一个 nonce → 容忍，发出新 nonce，记录告警
  - 都不匹配 → 拒绝，返回 CodeUnauthenticated
```

### 4.5 心跳写入优化

当前每个心跳直接写数据库。1000个 Agent × 120 beats/hour = 120k writes/hour。

**优化方案**: 内存缓存 + 批量刷写

```go
type HeartbeatBuffer struct {
    mu      sync.Mutex
    updates map[int]*AgentHeartbeatUpdate  // agent_id → 最新状态
    store   *store.Store
    interval time.Duration  // 刷写间隔，默认 10秒
}

type AgentHeartbeatUpdate struct {
    AgentID         int
    LastHeartbeatAt int64
    SessionID       string
    Metrics         *AgentMetrics
}

func (b *HeartbeatBuffer) Record(update *AgentHeartbeatUpdate) {
    b.mu.Lock()
    b.updates[update.AgentID] = update  // 只保留最新值
    b.mu.Unlock()
}

func (b *HeartbeatBuffer) FlushLoop(ctx context.Context) {
    ticker := time.NewTicker(b.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            b.flush() // 退出前刷写
            return
        case <-ticker.C:
            b.flush()
        }
    }
}

func (b *HeartbeatBuffer) flush() {
    b.mu.Lock()
    snapshot := b.updates
    b.updates = make(map[int]*AgentHeartbeatUpdate)
    b.mu.Unlock()

    if len(snapshot) == 0 {
        return
    }

    // 批量 UPDATE
    b.store.BatchUpdateAgentStatus(ctx, snapshot)
}
```

同时，读取 agent 状态时优先读内存缓存：

```go
// GetAgent 仍然从 DB 读 (或缓存)，但 LastHeartbeatAt 从 HeartbeatBuffer 读
func (s *AgentService) GetAgent(...) {
    agent := s.store.GetAgent(...)
    if latest := s.heartbeatBuffer.GetLatest(agent.ID); latest != nil {
        agent.Status.LastHeartbeatAt = latest.LastHeartbeatAt
    }
    return agent
}
```

### 4.6 时钟偏移处理

```go
// 在 Hello RPC 中，已返回 server 时间:
// HelloResponse { current_time: int64 }
// Agent 启动时调用 Hello 获取服务器时间，计算偏移:
//
//   clockOffset = serverTime - localTime
//
// JWT 库（golang-jwt/jwt/v5）已内置 leeway 支持：
parser := jwt.NewParser(jwt.WithLeeway(30*time.Second))
```

### 4.7 IP 校验

```go
type IPValidationPolicy int

const (
    IPValidationOff    IPValidationPolicy = 0  // 不校验
    IPValidationWarn   IPValidationPolicy = 1  // 仅告警
    IPValidationStrict IPValidationPolicy = 2  // 不匹配则拒绝
)

func extractSourceIP(r *http.Request, trustProxy bool) string {
    if trustProxy {
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
            ips := strings.Split(xff, ",")
            return strings.TrimSpace(ips[0])
        }
        if xri := r.Header.Get("X-Real-IP"); xri != "" {
            return strings.TrimSpace(xri)
        }
    }
    host, _, _ := net.SplitHostPort(r.RemoteAddr)
    return host
}

func validateAgentIP(reportedIP, sourceIP string, policy IPValidationPolicy) error {
    if policy == IPValidationOff || reportedIP == "" || sourceIP == "" {
        return nil
    }
    if reportedIP != sourceIP {
        switch policy {
        case IPValidationWarn:
            slog.Warn("agent IP mismatch", "reported", reportedIP, "source", sourceIP)
            return nil
        case IPValidationStrict:
            return connect.NewError(connect.CodePermissionDenied,
                fmt.Errorf("agent-reported IP %s doesn't match source IP %s", reportedIP, sourceIP))
        }
    }
    return nil
}
```

### 4.8 TLS 方案

```go
// 服务端 TLS 初始化
func initTLS(cfg *config.TLSConfig) (*tls.Config, error) {
    if cfg.Domain != "" {
        // 公有云: 自动 ACME (Let's Encrypt)
        return initAutoCert(cfg.Domain, cfg.Email, cfg.CertDir)
    }

    certDir := filepath.Join(cfg.DataDir, "certs")

    // 尝试加载已有证书
    cert, err := tls.LoadX509KeyPair(
        filepath.Join(certDir, "server.pem"),
        filepath.Join(certDir, "server.key"),
    )
    if err == nil {
        return &tls.Config{
            MinVersion:   tls.VersionTLS13,
            Certificates: []tls.Certificate{cert},
        }, nil
    }

    // 首次运行: 自动生成自签名 CA + Server 证书
    slog.Info("No TLS certificate found, generating self-signed CA and server certificate...")
    ca, serverCert, err := generateSelfSignedCert(certDir, cfg.Hosts)
    if err != nil {
        return nil, err
    }
    slog.Info("CA fingerprint", "sha256", ca.fingerprint)
    slog.Info("Save this fingerprint for agent verification (or use --insecure)")

    return &tls.Config{
        MinVersion:   tls.VersionTLS13,
        Certificates: []tls.Certificate{serverCert},
    }, nil
}
```

Agent 端 TOFU:

```go
type ManagerVerifier struct {
    knownHostsPath string
    insecure       bool
}

func (v *ManagerVerifier) Verify(host string, rawCerts [][]byte) error {
    if v.insecure {
        return nil // 开发模式跳过验证
    }

    fp := sha256Hex(rawCerts[0])
    saved, err := loadKnownHost(v.knownHostsPath, host)

    if err != nil || saved == "" {
        // 首次连接：打印指纹，等待确认
        fmt.Printf("The authenticity of manager %s can't be established.\n", host)
        fmt.Printf("CA fingerprint: SHA256:%s\n", fp)
        fmt.Printf("Continue connecting? (yes/no): ")
        if !askConfirmation() {
            return fmt.Errorf("connection rejected by user")
        }
        saveKnownHost(v.knownHostsPath, host, fp)
        return nil
    }

    if saved != fp {
        return fmt.Errorf("MANAGER FINGERPRINT CHANGED!\n"+
            "Expected: SHA256:%s\nGot: SHA256:%s\n"+
            "This may indicate a MITM attack.", saved, fp)
    }
    return nil
}
```

---

## 五、限流设计

### 5.1 分层限流

```go
type RateLimiterConfig struct {
    // 全局
    GlobalRate  float64 // 10000/min
    GlobalBurst int     // 5000

    // Per IP (连接相关)
    ConnectRate float64 // 10/min per IP
    ConnectBurst int    // 5
    LoginRate   float64 // 5/min per IP
    LoginBurst  int     // 3

    // Per Agent (心跳)
    HeartbeatRate float64 // 120/min per agent
    HeartbeatBurst int    // 10

    // Per User (管理 API)
    APIRate  float64 // 1000/min per user
    APIBurst int     // 100
}
```

实现基于 `golang.org/x/time/rate`:

```go
type APIRateLimiter struct {
    global        *rate.Limiter
    ipLimiters    *lru.Cache[string, *rate.Limiter]
    agentLimiters *lru.Cache[string, *rate.Limiter]
    userLimiters  *lru.Cache[string, *rate.Limiter]
    cfg           RateLimiterConfig
    mu            sync.Mutex
}

// 限流中间件 (ConnectRPC interceptor)
func (rl *APIRateLimiter) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
    return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
        // 1. 全局限流
        if !rl.global.Allow() {
            return nil, connect.NewError(connect.CodeResourceExhausted,
                errors.New("rate limit exceeded"))
        }

        // 2. 根据 RPC 类型选择限流策略
        switch {
        case isConnectRPC(req.Spec().Procedure):
            if !rl.getIPLimiter(sourceIP).Allow() {
                return nil, connect.NewError(connect.CodeResourceExhausted,
                    errors.New("connect rate limit exceeded"))
            }
        case isHeartbeatRPC(req.Spec().Procedure):
            if !rl.getAgentLimiter(agentID).Allow() {
                return nil, connect.NewError(connect.CodeResourceExhausted,
                    errors.New("heartbeat rate limit exceeded"))
            }
        // ...
        }

        return next(ctx, req)
    }
}
```

---

## 六、安全中间件

### 6.1 Echo HTTP 安全头中间件

```go
func SecurityHeaders() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            h := c.Response().Header()
            h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
            h.Set("X-Content-Type-Options", "nosniff")
            h.Set("X-Frame-Options", "DENY")
            h.Set("Content-Security-Policy", "default-src 'self'")
            h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
            return next(c)
        }
    }
}
```

### 6.2 审计拦截器（仅记录关键事件）

```go
// 心跳采样: 仅记录异常 (状态变化、认证失败、nonce 失败等)
// 其他 RPC: 全量审计
func AuditInterceptor(stores *store.Store) connect.UnaryFunc {
    return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
        resp, err := next(ctx, req)

        authCtx := common.GetAuthContextFromContext(ctx)
        if !authCtx.Audit {
            return resp, err
        }

        procedure := req.Spec().Procedure

        // 心跳采样: 仅在异常时记录
        if isHeartbeatRPC(procedure) && err == nil {
            if !shouldSampleHeartbeat(ctx) {
                return resp, err
            }
        }

        auditLog := &AuditLog{
            Method:    procedure,
            ActorType: getActorType(ctx),
            ActorID:   getActorID(ctx),
            SourceIP:  getSourceIP(ctx),
            Status:    statusFromError(err),
            Error:     errorFromError(err),
            Timestamp: time.Now(),
        }

        go stores.CreateAuditLog(context.Background(), auditLog)
        return resp, err
    }
}

func shouldSampleHeartbeat(ctx context.Context) bool {
    // 每100次心跳记录1次, 或者异常时总是记录
    return rand.Intn(100) == 0
}
```

---

## 七、Agent 客户端重构

### 7.1 状态机

```go
type AgentState int

const (
    StateDisconnected    AgentState = iota
    StateConnecting
    StateConnected
    StateDisconnecting
)

type AgentClient struct {
    managerURL     *url.URL
    bootstrapToken string         // --token 参数传入
    credentialMgr  *CredentialManager
    connState      AgentState
    sessionID     string
    serverNonce   string
    backoff        *ExponentialBackoff
    httpClient     *http.Client // 支持 TLS
    metricsCollector *MetricsCollector
}

func (c *AgentClient) Run(ctx context.Context) error {
    for {
        switch c.connState {
        case StateDisconnected:
            if err := c.connect(ctx); err != nil {
                slog.Error("connect failed", "error", err)
                c.backoff.Wait(ctx)
                continue
            }
            c.connState = StateConnected
            c.backoff.Reset()

        case StateConnected:
            select {
            case <-ctx.Done():
                c.connState = StateDisconnecting
            case <-c.heartbeatTicker.C:
                if err := c.heartbeat(ctx); err != nil {
                    slog.Error("heartbeat failed", "error", err)
                    c.connState = StateDisconnected
                }
            }

        case StateDisconnecting:
            c.sendDisconnect(ctx, "agent shutdown")
            c.credentialMgr.DeleteRefreshToken()
            return nil
        }
    }
}

func (c *AgentClient) connect(ctx context.Context) error {
    // 优先级: 文件中的 refresh_token > --token (bootstrap)
    token, tokenType := c.credentialMgr.GetBestToken()

    switch tokenType {
    case "refresh":
        resp, err := c.refreshToken(ctx, token)
        if err != nil {
            token = c.bootstrapToken
            tokenType = "bootstrap"
        } else {
            c.credentialMgr.SaveRefreshToken(resp.RefreshToken)
            c.accessToken = resp.AccessToken
            // 还需要 ConnectAgent 获取 session
        }
    }

    if tokenType == "bootstrap" {
        resp, err := c.connectAgent(ctx, token, c.collectInfo())
        if err != nil {
            return err
        }
        c.accessToken = resp.AccessToken
        c.credentialMgr.SaveRefreshToken(resp.RefreshToken)
        c.sessionID = resp.SessionId
        c.serverNonce = resp.NextNonce
    }

    c.backoff.Reset()
    return nil
}
```

### 7.2 凭证管理器

```go
type CredentialManager struct {
    tokenFilePath string  // ~/.laelia/agent-token
    refreshToken  string  // 内存中的 refresh token
}

func (cm *CredentialManager) GetBestToken() (string, string) {
    // 1. 尝试从文件加载 refresh token
    if cm.refreshToken == "" {
        cm.refreshToken = cm.loadFromFile()
    }
    if cm.refreshToken != "" {
        return cm.refreshToken, "refresh"
    }
    // 2. fallback 到 bootstrap token (--token 参数)
    return "", "bootstrap"
}

func (cm *CredentialManager) SaveRefreshToken(token string) {
    cm.refreshToken = token
    cm.writeToFile(token)
}

func (cm *CredentialManager) DeleteRefreshToken() {
    cm.refreshToken = ""
    os.Remove(cm.tokenFilePath)
}

func (cm *CredentialManager) writeToFile(token string) {
    dir := filepath.Dir(cm.tokenFilePath)
    os.MkdirAll(dir, 0700)
    os.WriteFile(cm.tokenFilePath, []byte(token), 0600)
}

func (cm *CredentialManager) loadFromFile() string {
    data, err := os.ReadFile(cm.tokenFilePath)
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(data))
}
```

---

## 八、配置化安全策略

所有安全相关阈值从 `setting` 表或配置文件读取：

```sql
-- 新增配置项 (存入 setting 表, key-value)
INSERT INTO setting (name, value) VALUES
('agent.heartbeat_interval_seconds', '30'),
('agent.offline_threshold_seconds', '60'),
('agent.access_token_duration', '15m'),
('agent.refresh_token_duration', '24h'),
('agent.bootstrap_token_duration', '168h'),  -- 7天
('agent.max_concurrent_sessions', '1'),
('agent.ip_validation_policy', 'WARN'),  -- OFF, WARN, STRICT
('agent.heartbeat_rate_limit_per_minute', '120'),
('agent.connect_rate_limit_per_minute', '10'),
('security.global_rate_limit_per_minute', '10000'),
('security.login_rate_limit_per_minute', '5');
```

运营时可通过管理 API 动态调整，无需重启服务。

---

## 九、文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `proto/v1/v1/agent.proto` | **重写** | 新增 RPC、消息、reserved token 字段 |
| `proto/v1/v1/annotation.proto` | 修改 | 无变更（已支持所需注解） |
| `proto/store/store/agent.proto` | 修改 | AgentStatus 新增 ConnectionState(KICKED)、active_session_id |
| `backend/manager/migration/latest.sql` | **新增迁移表** | agent_session 表、agent_refresh_token 表、agent 表扩展字段、setting 新增项 |
| `backend/manager/api/v1/agent.go` | **重写** | 所有 RPC 实现 |
| `backend/manager/api/auth/auth.go` | **大幅修改** | agent JWT claims 扩展、token 类型验证、HMAC key 管理 |
| `backend/manager/api/auth/nonce.go` | **新增** | Nonce 签发/验证 |
| `backend/manager/api/auth/ratelimit.go` | **新增** | 限流中间件 |
| `backend/manager/api/auth/tls.go` | **新增** | TLS 初始化、自签名 CA 生成 |
| `backend/manager/api/auth/iplist.go` | **新增** | IP 校验 |
| `backend/manager/store/agent.go` | 修改 | 新增 session/token CRUD、BatchUpdateAgentStatus |
| `backend/manager/store/agent_session.go` | **新增** | Session 存储 |
| `backend/manager/store/agent_token.go` | **新增** | Refresh Token 存储 |
| `backend/manager/store/setting.go` | 修改 | 新增安全配置项读取 |
| `backend/manager/server/grpc_routes.go` | 修改 | 注册新 RPC、新拦截器（限流、审计） |
| `backend/manager/server/echo_routes.go` | 修改 | 安全头中间件 |
| `backend/manager/server/server.go` | 修改 | TLS 配置 |
| `backend/manager/component/state/state.go` | 修改 | 扩展 TokenExpireCache、新增 NonceCache、HeartbeatBuffer |
| `backend/agent/client/client.go` | **重写** | 新增 Connect/Heartbeat/Disconnect/Refresh 方法、TLS |
| `backend/agent/cmd/run.go` | **重写** | 状态机、凭证管理、重连逻辑 |
| `backend/agent/credential/credential.go` | **新增** | 凭证文件管理 |
| `backend/common/context.go` | 修改 | 新增 SessionContextKey |

---

## 十、实施顺序

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

| 安全问题 | 改进前 | 改进后 |
|----------|--------|--------|
| 传输加密 | 纯 HTTP 明文 | TLS 1.3 (自签名 CA / ACME / TOFU) |
| Agent Token 有效期 | 365天 | Bootstrap 7天, Access 15分钟, Refresh 24小时 |
| Token 吊销 | 128 条目 LRU, 重启丢失 | 数据库持久化 + token_version + token family 吊销 |
| Token 轮换 | 无 | RotateAgentToken API + refresh token rotation |
| 重放攻击 | 心跳空 body, 无防护 | Nonce 链 + HMAC 签名 |
| 限流 | 无 | 分层限流 (全局/IP/Agent/User) |
| IP 校验 | 无 | 可配置 (OFF/WARN/STRICT) |
| 并发会话 | 无检测 | 单会话策略 + KICKED 机制 |
| 优雅断开 | 无 | AgentDisconnect + ForceDisconnectAgent |
| 审计日志 | 拦截器被注释 | 启用 + 心跳采样 |
| 密钥轮换 | 单一 kid="v1" | 支持 key rotation (多 kid) |
| 安全头 | 无 | HSTS/X-Frame-Options/CSP 等 |
| Agent 重连 | 无重试 | 指数退避 + 凭证 fallback |
| 会话追踪 | 无 | agent_session 表 |
| Token 泄露窗口 | 365天 | 15分钟 |
