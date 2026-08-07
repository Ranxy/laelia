# Agent 工作区文件浏览器(Workspace Browser)设计与实现

> 本文档描述 laelia 的 agent 工作区文件浏览器功能:需求、安全边界、协议、manager/机器侧/前端实现,以及验证方式。内容与当前代码实现保持一致(实现提交 `5af25f3 feat: add agent workspace file browser`)。

## 背景与需求

管理页面需要查看 agent 在宿主机上的工作目录(文件树)并预览文本/图片内容,便于 owner/管理员巡检与排障;同时需要机器级的工作区管理(查看各 agent 工作目录占用、删除不再需要的目录)。

### 需求确认(用户决策)

1. **UI 位置**:agent 详情页新增"工作区"tab
2. **预览**:文本 + 图片预览
3. **隐藏文件**:提供"显示隐藏文件"开关
4. **权限**:仅 owner/管理员可查看(内容敏感);无权限则工作区 tab 不显示
5. **机器级工作区管理**:一并接入(scan 列表 + delete),权限同样适配

### 实现决策(用户补充确认)

1. **机器 token 不可暴露**:已调研机器凭据落盘位置,见下文"机器凭据落盘盘点",并增加针对性的过滤规则(防御纵深)
2. **前端双栏布局**:左侧树状文件目录,右侧文件内容;点击左侧文件直接在右侧展示
3. **markdown 渲染**:复用前端既有的 markstream-react 渲染方式(参考 `components/preview/markdown-preview-overlay.tsx`)

## 机器凭据落盘盘点(调研结论)

`~/.laelia/` 目录布局与凭据存放(已核实代码):

```
~/.laelia/
├── machine-token-<machineID>          ← 机器持久化 refresh token(0600,client.go:151)
├── bin/pi-<hash>-<os>-<arch>          ← pi 二进制缓存(pi/binary_release.go:42)
└── <machineID>/
    ├── daemon.sock                    ← daemon unix socket(daemon/server.go:99)
    └── <agentID>/
        ├── command-state.json         ← 命令执行状态(executor/state.go)
        ├── acp-session.json           ← ACP session 状态 + fingerprint(executor/acp_session.go)
        ├── context-state.json         ← 上下文状态(executor/context_state.go)
        ├── pi-session.json            ← pi 会话路径 + fingerprint(pi/session.go)
        └── …                          ← LLM agent 自身在 cwd 内产生的项目文件/状态
```

**凭据盘点结论**:

| 凭据 | 落盘位置 | 是否在可浏览区域内 | 说明 |
| --- | --- | --- | --- |
| 机器 refresh token | `~/.laelia/machine-token-<machineID>` | **否** | 是机器工作区根 `~/.laelia/<machineID>/` 的**兄弟文件**,不在 scan/agent 任何浏览根内 |
| 机器 access token | 仅内存(`client.go:85` `accessToken`) | — | 每次 refresh 更新,不落盘 |
| agent 通道鉴权 | 复用机器 access token(`runner.go:cs.getToken`) | — | 不落盘 |
| daemon session token | 仅内存(`daemon/server.go:106`) | — | 进程启动随机生成,不落盘 |
| pi LLM API key | 仅注入子进程 env(`pi/config.go:236`) | — | 落盘只有 sha256 fingerprint(截断 16 hex),非原文 |
| 状态文件(command/acp-session/context/pi-session.json) | agent 工作目录内 | **是** | 只含 session/命令/上下文状态与 fingerprint,**不含 token/密钥** |
| LLM agent 全局凭据(如 opencode 的 auth.json) | LLM 工具自身全局目录(`~/.config`、`~/.local/share` 等) | **否** | 在 cwd(agent 工作目录)之外,浏览不到 |

**结论与过滤策略**:

1. 机器 token 文件与两个浏览根(`~/.laelia/<machineID>/`、`~/.laelia/<machineID>/<agentID>/`)均不相交,天然不可见;机器级 scan 只列子目录(`IsDir` 过滤),`daemon.sock`(socket 文件)与 `~/.laelia/bin/`(兄弟目录)也不会出现
2. **防御纵深**:即使未来布局变化(如 token 移入机器目录),也需要保证列表层不出现凭据文件名。将 `machine-token-` 前缀加入"永不显示"规则;读层仍由 secret 正则兜底(`machine-token-<id>` 命中 `/(?:^|[._-])token(?:s)?(?:[._-]|$)/i`,内容读取会被拒绝)
3. secret 文件名(用户自己起的 `.env` 等)**可以出现在列表**,但**内容读取一律拒绝**——避免过度隐藏导致用户困惑,同时保证敏感内容不泄露

## 总体设计

### 架构流程

```
前端(workspace tab)
  │ ListAgentWorkspace / ReadAgentWorkspaceFile(unary Connect RPC,handler-gated)
  ▼
Manager AgentService
  │ dispatcher: RegisterPending → SendWorkspaceListRequest(request_id)(bidi)
  ▼
Machine app AgentChannel 接收泵(command_stream.go)
  │ goroutine: workspace 包读 ~/.laelia/<machineID>/<agentID>/
  ▼
WorkspaceListResponse(request_id) ──→ dispatcher CompletePending ──→ unary RPC 返回
```

Machine 级同构:`MachineService.ListMachineWorkspaces / DeleteMachineWorkspace` → `ManagerMachineStreamMessage` 下发 → `machine_control.go` 接收泵 → `~/.laelia/<machineID>/` 扫描/删除 → `MachineStreamMessage` 回包。

### 协议扩展(当前实现)

#### `proto/v1/v1/command.proto`(AgentChannel)

`ManagerStreamMessage` oneof 新增(字段号 10/11,当前最大 9):

```proto
message ManagerStreamMessage {
  oneof message {
    // ...
    WorkspaceListRequest workspace_list_request = 10; // ask the agent daemon to list one level of its workspace
    WorkspaceReadRequest workspace_read_request = 11; // ask the agent daemon to read a workspace file
  }
}

message WorkspaceListRequest {
  string request_id = 1;   // 关联 unary RPC 的 pending 请求
  string dir_path = 2;     // 相对 agent 工作区根,空 = 根目录
  bool include_hidden = 3; // 显示点文件(仍受永不显示策略过滤)
}

message WorkspaceReadRequest {
  string request_id = 1;
  string path = 2;         // 相对 agent 工作区根
}
```

`AgentStreamMessage` oneof 新增(字段号 10/11,当前最大 9):

```proto
message AgentStreamMessage {
  oneof message {
    // ...
    WorkspaceListResponse workspace_list_response = 10;
    WorkspaceReadResponse workspace_read_response = 11;
  }
}

message WorkspaceListResponse {
  string request_id = 1;
  repeated WorkspaceEntry entries = 2;  // 服务端已排序:目录优先 + 名称比较
}
```

> `WorkspaceEntry` 与 `WorkspaceReadResponse` 定义在 `v1/agent.proto`(同一 `laelia.v1` 包),由 per-agent 流(unary `ListAgentWorkspace`/`ReadAgentWorkspaceFile`)与 unary RPC 共享。

#### `proto/v1/v1/machine.proto`(MachineChannel)

`ManagerMachineStreamMessage` oneof 新增(字段号 7/8,当前最大 6):

```proto
message ManagerMachineStreamMessage {
  oneof message {
    // ...
    MachineWorkspaceScanRequest machine_workspace_scan_request = 7;   // scan per-agent workspace directories
    MachineWorkspaceDeleteRequest machine_workspace_delete_request = 8; // delete one agent workspace directory
  }
}

message MachineWorkspaceScanRequest {
  string request_id = 1;
}

message MachineWorkspaceDeleteRequest {
  string request_id = 1;
  string directory_name = 2;  // 仅目录名,不含路径分隔符(机器侧校验)
}
```

`MachineStreamMessage` oneof 新增(字段号 5/6,当前最大 4):

```proto
message MachineStreamMessage {
  oneof message {
    // ...
    MachineWorkspaceScanResponse machine_workspace_scan_response = 5;
    MachineWorkspaceDeleteResponse machine_workspace_delete_response = 6;
  }
}

message MachineWorkspaceSummary {
  string directory_name = 1;
  int64 total_size_bytes = 2;
  google.protobuf.Timestamp last_modified = 3;
  int64 file_count = 4;
}

message MachineWorkspaceScanResponse {
  string request_id = 1;
  repeated MachineWorkspaceSummary workspaces = 2;
}

message MachineWorkspaceDeleteResponse {
  string request_id = 1;
  string directory_name = 2;
  bool success = 3;
}
```

#### `proto/v1/v1/agent.proto` / `machine.proto`(新 unary RPC)

AgentService(handler-gated,不加 IAM annotation,同 `UpdateAgent` 模式):

```proto
rpc ListAgentWorkspace(ListAgentWorkspaceRequest) returns (ListAgentWorkspaceResponse) {
  option (google.api.http) = { post: "/v1/{name=agents/*}:listWorkspace" };
}
rpc ReadAgentWorkspaceFile(ReadAgentWorkspaceFileRequest) returns (ReadAgentWorkspaceFileResponse) {
  option (google.api.http) = { post: "/v1/{name=agents/*}:readWorkspaceFile" };
}

message WorkspaceEntry {
  string name = 1;
  string path = 2;                 // 相对根,目录/文件均有效
  bool is_directory = 3;
  int64 size = 4;                  // 文件字节数,目录为 0
  google.protobuf.Timestamp modified_at = 5;
  bool is_hidden = 6;              // 点文件
}

message WorkspaceReadResponse {
  string request_id = 1;
  string content = 2;   // 文本:utf-8 原文;图片:base64;其他:空
  bool binary = 3;      // true = 图片/其他二进制
  int64 size = 4;
  string mime_type = 5; // 图片才有
  string encoding = 6;  // "utf-8" / "base64" / 空
  string error = 7;     // 预览被拒绝的原因(敏感文件/超限/不存在),前端展示,不代表传输错误
}

message ListAgentWorkspaceRequest {
  string name = 1;            // agents/{agent}
  string dir_path = 2;        // 空 = 根
  bool include_hidden = 3;
}
message ListAgentWorkspaceResponse {
  repeated WorkspaceEntry entries = 1;
}

message ReadAgentWorkspaceFileRequest {
  string name = 1;            // agents/{agent}
  string path = 2;
}
message ReadAgentWorkspaceFileResponse {
  WorkspaceReadResponse file = 1;  // 复用流内消息结构
}
```

MachineService:

```proto
rpc ListMachineWorkspaces(ListMachineWorkspacesRequest) returns (ListMachineWorkspacesResponse) {
  option (google.api.http) = { post: "/v1/{name=machines/*}:listWorkspaces" };
}
rpc DeleteMachineWorkspace(DeleteMachineWorkspaceRequest) returns (DeleteMachineWorkspaceResponse) {
  option (google.api.http) = { post: "/v1/{name=machines/*}:deleteWorkspace" };
}

message ListMachineWorkspacesRequest {
  string name = 1;            // machines/{machine}
}
message ListMachineWorkspacesResponse {
  repeated MachineWorkspaceSummary workspaces = 1;
}
message DeleteMachineWorkspaceRequest {
  string name = 1;            // machines/{machine}
  string directory_name = 2;
}
message DeleteMachineWorkspaceResponse {
  bool success = 1;
}
```

**权限门控(handler 内)**:

| RPC | 门控 | 说明 |
| --- | --- | --- |
| `ListAgentWorkspace` / `ReadAgentWorkspaceFile` | `canEditAgent(ctx, user, agent)` | owner 或 workspace admin(`laelia.agents.edit`) |
| `ListMachineWorkspaces` / `DeleteMachineWorkspace` | `isMachineAdmin(ctx, iam, user, machine)` | 机器创建者或 workspace admin(与 `Machine.can_manage` 一致) |

不新增 `laelia.workspace.*` 权限——owner/管理员语义已由现有权限精确覆盖,避免权限矩阵膨胀。`user` 通过现有 auth interceptor 从 ctx 取(参考 `RefreshAgentProviders` 的取法)。

### Dispatcher 扩展(`backend/manager/component/dispatcher/dispatcher.go`)

新增泛型 pending-reply 辅助(新代码,与既有 `pendingDiscovers` 并存):

```go
// pendingReplies 以 request_id 为 key 关联 bidi 回包与 unary RPC 等待者。
type pendingReplies[T proto.Message] struct {
	mu sync.Mutex
	m  map[string]chan T
}

func (p *pendingReplies[T]) register(requestID string) chan T
func (p *pendingReplies[T]) cancel(requestID string)
func (p *pendingReplies[T]) complete(msg T) // 从 map 取出 channel 投递并删除
```

Dispatcher 上挂 4 个实例 + 对应 Send/Register/Cancel/Complete 方法(镜像 `SendDiscoverProviders`/`RegisterPendingDiscover`/`CompletePendingDiscover` 签名):

- `pendingWorkspaceLists` + `SendWorkspaceListRequest(agentID int, requestID, dirPath string, includeHidden bool) error`(发给 `agentID` 的 AgentChannel,经 `agent_channels` 定位)
- `pendingWorkspaceReads` + `SendWorkspaceReadRequest(agentID int, requestID, path string) error`
- `pendingMachineScans` + `SendMachineWorkspaceScan(machineID int, requestID string) error`(经机器控制通道)
- `pendingMachineDeletes` + `SendMachineWorkspaceDelete(machineID int, requestID, directoryName string) error`

**Manager 接收泵接线**:

- `backend/manager/api/v1/agent_command.go` AgentChannel 接收循环新增两个 case:
  - `case *v1pb.AgentStreamMessage_WorkspaceListResponse: s.dispatcher.CompletePendingWorkspaceList(m.WorkspaceListResponse)`
  - `case *v1pb.AgentStreamMessage_WorkspaceReadResponse: s.dispatcher.CompletePendingWorkspaceRead(m.WorkspaceReadResponse)`
- `backend/manager/api/v1/machine_command.go` 接收循环新增两个 case:
  - `MachineStreamMessage_MachineWorkspaceScanResponse` → `CompletePendingMachineWorkspaceScan`
  - `MachineStreamMessage_MachineWorkspaceDeleteResponse` → `CompletePendingMachineWorkspaceDelete`

**Unary handler 范式**(`backend/manager/api/v1/agent.go`):

```go
func (s *AgentService) ListAgentWorkspace(ctx context.Context, req *connect.Request[v1pb.ListAgentWorkspaceRequest]) (*connect.Response[v1pb.ListAgentWorkspaceResponse], error) {
	// 1. GetAgentResourceID + store.GetAgentByResourceID + canEditAgent 门控
	// 2. if !s.dispatcher.IsAgentConnected(agent.ID) → CodeFailedPrecondition
	// 3. requestID := uuid.NewString(); replyCh := s.dispatcher.RegisterPendingWorkspaceList(requestID)
	//    defer s.dispatcher.CancelPendingWorkspaceList(requestID)
	// 4. SendWorkspaceListRequest(...)
	// 5. select { case msg := <-replyCh / 60s → CodeDeadlineExceeded / ctx.Done() }
}
```

`ReadAgentWorkspaceFile` 同构;`ListMachineWorkspaces`/`DeleteMachineWorkspace` 在 `machine.go`,用 `IsMachineConnected(machineID)` 做在线检查;Delete 回包 `success=false` 时映射为 `CodeFailedPrecondition`。

### 机器侧实现

#### 新包 `backend/agent/workspace/`

纯文件系统逻辑,不依赖 proto/网络,便于单测。包含 `policy.go` / `tree.go` / `file.go` / `scan.go` 及对应 `*_test.go`。

- `policy.go` — 常量与判定函数:

  ```go
  var textExtensions = map[string]bool{".md": true, ".txt": true, ".json": true, ".js": true,
      ".ts": true, ".jsx": true, ".tsx": true, ".yaml": true, ".yml": true, ".toml": true,
      ".log": true, ".csv": true, ".xml": true, ".html": true, ".css": true, ".sh": true, ".py": true}
  var imageMimeByExt = map[string]string{".apng": "image/apng", ".avif": "image/avif",
      ".gif": "image/gif", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
      ".png": "image/png", ".webp": "image/webp"}
  const (
      textFileMaxBytes     = 1 << 20 // 1MB:更大的文本文件不预览
      imagePreviewMaxBytes = 5 << 20 // 5MB:更大的图片不预览
  )

  var secretFilePatterns = []*regexp.Regexp{
      regexp.MustCompile(`(?i)^\.env(?:\.|$)`),
      regexp.MustCompile(`(?i)(?:^|[._-])secret(?:s)?(?:[._-]|$)`),
      regexp.MustCompile(`(?i)(?:^|[._-])credential(?:s)?(?:[._-]|$)`),
      regexp.MustCompile(`(?i)(?:^|[._-])token(?:s)?(?:[._-]|$)`),
  }

  // 永不显示:通用高敏凭据目录 + laelia 机器凭据文件前缀(防御纵深,
  // 见"机器凭据落盘盘点")。点文件仍受 includeHidden 开关控制。
  var neverVisibleHiddenNames = map[string]bool{".aws": true, ".gnupg": true, ".ssh": true}

  func isNeverVisibleEntry(name string) bool // 集合命中 || strings.HasPrefix(name, "machine-token-")
  func isHiddenPath(rel string) bool         // 任一 part 以 "." 开头
  func isNeverVisiblePath(rel string) bool   // 任一 part 为永不显示
  func isSecretFilePath(rel string) bool     // 任一 part 命中 secretFilePatterns
  ```

  > 说明:secret 正则编译为**大小写不敏感**(`(?i)`),`TOKEN.json`、`.ENV`、`Credential.json` 等一律拒绝读取;`.aws/.gnupg/.ssh` 是 LLM agent 可能在 workdir 内创建的高敏凭据目录,列表层永不显示;`machine-token-` 是机器 refresh token 文件名前缀,列表层直接隐藏(内容读取另有 secret 正则兜底)。

- `tree.go` — `List(root, dirPath string, includeHidden bool) ([]Entry, error)`:
  - 根不存在 → 返回空列表(不报错)
  - `resolveInRoot(root, dirPath) (resolved, rootReal string, err error)`:
    1. `filepath.Abs(filepath.Join(rootAbs, dirPath))` 后先做**词法校验**(`resolved == rootAbs || strings.HasPrefix(resolved, rootAbs + sep)`)
    2. 用 `filepath.EvalSymlinks` 解析符号链接,解析后的真实路径必须仍位于解析后的根目录(`rootReal`)内,否则返回 `ErrAccessDenied`
    3. 根目录自身为符号链接时以解析后的根为准(兼容 `/tmp`、家目录等场景);根不存在时以词法根为基准,交由下游 `EvalSymlinks` 失败自然处理(List 容忍为空目录,Read 上报 OS 错误)
    4. 策略判定基于**解析后的相对路径**——工作区内的符号链接既无法逃逸根目录,也无法指向敏感路径绕过 secret 过滤
  - 目标目录本身为 never-visible / hidden(未开 includeHidden)→ 空
  - 读目录 → 排序(**目录优先**,再 `strings.Compare` 名称比较,不引入额外依赖)→ 跳过 `node_modules`、never-visible、隐藏(未开 includeHidden)→ **符号链接条目一律不展示**(`DirEntry.Info()` 不跟随链接,`ModeSymlink` 跳过)→ `os.Stat` 失败跳过 → 组装 `Entry{Name, Path(rel, 斜杠分隔), IsDir, Size, ModifiedAt, IsHidden}`
  - 目录不可读 → 返回空列表(不报错)
- `file.go` — `Read(root, path string) (ReadResult, error)`:
  - 同样 `resolveInRoot` 校验;never-visible 或 secret 路径 → `ReadResult{Error: "preview is disabled for sensitive workspace files"}`(拒绝)
  - 目录 → `Error: "cannot read a directory"`
  - 文本扩展名或无扩展名 → size ≤ 1MB 才读 utf-8,超限 → `Error: "file too large to preview"`
  - 图片扩展名 → size ≤ 5MB 才 base64(`Encoding: "base64"`),超限 → `{Binary: true, Size, MimeType, Error: "image too large to preview"}`
  - 其他二进制 → 仅元信息(`{Binary: true, Size}`)
  - OS 级失败(不存在/权限)以 error 返回;`ReadResult.Error` 由调用方映射进 `WorkspaceReadResponse.error`
- `scan.go` — `Scan(root) ([]Summary, error)`、`Delete(root, directoryName string) error`:
  - `Scan`:读一级目录,仅 `IsDir()` 项(socket/普通文件如 `daemon.sock` 天然跳过),递归统计 `totalSizeBytes`/`fileCount`/`latestMtime`(`summarizeWorkspaceDirectory`,单项失败容错跳过)
  - `Delete`:校验 `directoryName` 为**裸目录名**——非空、非 `.`/`..`、不含 `/` `\`、不含 `..`(`isValidWorkspaceDirectoryName`,比仅拒绝分隔符更严格,杜绝删除工作区根自身),`filepath.Join(root, directoryName)` 后 `os.RemoveAll` 递归删除

#### 接收泵接线

`backend/agent/client/command_stream.go` `mainLoop` 接收泵 switch 新增两个 case(异步处理 + 回包):

```go
case *v1pb.ManagerStreamMessage_WorkspaceListRequest:
    go c.handleWorkspaceList(ctx, sender, m.WorkspaceListRequest)
case *v1pb.ManagerStreamMessage_WorkspaceReadRequest:
    go c.handleWorkspaceRead(ctx, sender, m.WorkspaceReadRequest)
```

- **发送串行化**:`connect-go` 的 `Send` 不允许并发调用,而新增的异步回包 goroutine 与 ping ticker、drain loop 并发发送,因此接收泵将原始 stream 包进 `serializedSender`(内部 mutex 串行化 `Send`),所有发送点统一经它
- `handleWorkspaceList`:root = `executor.AgentWorkingDir(c.machineID, c.agentID)`(即 `~/.laelia/<machineID>/<agentID>/`,同时覆盖 pi 布局)→ `workspace.List` → 回 `AgentStreamMessage_WorkspaceListResponse`
- `handleWorkspaceRead`:同 root → `workspace.Read` → 回 `WorkspaceReadResponse`

`backend/agent/client/machine_control.go` 接收泵 switch 新增两个 case(复用现有 `sendStream`):

```go
case *v1pb.ManagerMachineStreamMessage_MachineWorkspaceScanRequest:
    go c.handleMachineWorkspaceScan(ctx, sendStream, m.MachineWorkspaceScanRequest)
case *v1pb.ManagerMachineStreamMessage_MachineWorkspaceDeleteRequest:
    go c.handleMachineWorkspaceDelete(ctx, sendStream, m.MachineWorkspaceDeleteRequest)
```

- `handleMachineWorkspaceScan`:root = `~/.laelia/<c.machineID>`(`os.Getenv("HOME")`;该根内只有 `daemon.sock` + `<agentID>/` 子目录,机器 token 文件 `machine-token-<id>` 在 `~/.laelia/` 根下、不在本根内)→ `workspace.Scan` → 回 `MachineWorkspaceScanResponse`
- `handleMachineWorkspaceDelete`:→ `workspace.Delete` → 回 `MachineWorkspaceDeleteResponse`

### 前端设计

#### Agent:工作区 tab(双栏布局)

- `frontend/src/router/handles.ts`:`AGENT_ROUTE_WORKSPACE = "agent.workspace"`
- `frontend/src/router/routes/dashboard.tsx` agent 子路由新增:
  ```ts
  { path: "workspace", handle: { name: AGENT_ROUTE_WORKSPACE },
    lazy: () => import("@/pages/dashboard/agent-workspace").then(m => ({ Component: m.AgentWorkspacePage })) }
  ```
- `agent-detail-layout.tsx`:
  - `TabKey` 增加 `"workspace"` 分支与 `TabsTrigger`,`activeTab` 推导加 `if (afterId === "workspace") return "workspace"`
  - **tab 显隐**:layout 挂载时经 store `getAgent(agentId)` 拉取完整详情(`canEdit` 是 per-caller 字段、不可依赖缓存),`agent.canEdit === true` 才渲染工作区 tab;直接访问 `/workspace` 路由时页面内做同样门控并重定向到 profile
- 新页面 `frontend/src/pages/dashboard/agent-workspace.tsx`:**双栏布局**——左 `w-72` 树、右内容面板,点击左侧文件直接在右侧展示:
  - `frontend/src/components/workspace/workspace-tree.tsx`:递归树,目录节点展开时**按层调用** `ListAgentWorkspace(dirPath)`(懒加载,不预展开);顶部"显示隐藏文件"复选框(切换后从根重新拉取)与刷新按钮;服务端已完成过滤(`node_modules`/永不显示/secret)与排序;隐藏条目以 60% 透明度展示;加载/空/错误态(错误态含重试)
  - `frontend/src/components/workspace/workspace-file-panel.tsx`:文件头(名称、大小、关闭);加载态;`error` 非空 → danger 文案(敏感文件/超限);图片 → `data:<mime>;base64,` 内联展示;其他二进制 → 仅大小元信息;文本 → 等宽 `pre` 展示
  - **markdown 渲染**:`.md`/`.markdown` 且非 binary 的文件经 `MarkdownRender`(`markstream-react`)渲染,容器类 `markstream-chat`、`customId="workspace-md-preview"`、`final fade batchRendering deferNodesUntilVisible={false}`,与 `components/preview/markdown-preview-overlay.tsx` 的渲染方式一致
- 新 store slice `frontend/src/stores/workspace.ts`(`createWorkspaceSlice`):`listAgentWorkspaceDir(name, dirPath, includeHidden)`、`readAgentWorkspaceFile(name, path)`、`listMachineWorkspaces(name)`、`deleteMachineWorkspace(name, dirName)`;经 `@/connect` 的 `agentServiceClient`/`machineServiceClient` 调用,`create(RequestSchema, ...)` 编解码

#### Machine:工作区管理

- `frontend/src/router/handles.ts`:`MACHINE_ROUTE_WORKSPACE = "machine.workspace"`
- `dashboard.tsx` machine 子路由新增 `{ path: "workspace", ... }` → `machine-workspace` 页面
- `machine-detail-layout.tsx` 改为与 agent 一致的 tab 布局(profile / 工作区),工作区 tab 仅当 `machine.canManage` 为 true 时渲染(`GetMachine` 已填充 `canManage`);页面内再校验并重定向
- 新页面 `frontend/src/pages/dashboard/machine-workspace.tsx`:表格列出 `directoryName`/`totalSizeBytes`(`formatBytes`)/`fileCount`/`lastModified`(`formatTimestamp`)+ "删除"按钮(**AlertDialog 二次确认**,删除 agent 工作目录为破坏性操作),删除成功 toast,失败展示错误;空态/加载/错误态齐全

#### i18n

`zh-CN.json` / `en-US.json` 新增 `workspace.*` keys(show-hidden / preview / empty / load-error / sensitive-file / file-too-large / binary-file / delete-confirm / deleted / loading / refresh / directory / file / size / file-count / last-modified / delete / delete-error / no-workspaces / close / select-file),以及 `agent.tab-workspace` / `machine.tab-workspace`。

### 错误处理与边界

| 场景 | 行为 |
| --- | --- |
| agent 离线 | unary RPC 返回 `CodeFailedPrecondition`(镜像 `RefreshAgentProviders`) |
| 等待回包超时 | `CodeDeadlineExceeded`(60s,与 provider discovery 一致) |
| ctx 取消 | `CodeDeadlineExceeded`,defer cancel pending |
| 目标目录不存在 | 返回空 `entries`(不报错) |
| 路径越界 / never-visible / secret | 列表静默空或 `WorkspaceReadResponse.error` 提示(不泄露目录结构) |
| 读取单文件失败 | `error` 字段返回,unary RPC 仍成功(前端可展示具体原因) |
| 文件超限 | 文本:error;图片:仅元信息 + error |
| 机器在线检查 | `IsMachineConnected` |

### 安全说明

- 敏感文件拦截在**机器侧文件读取层**强制执行,manager 与前端均不可绕过
- **符号链接双重防护**:`resolveInRoot` 词法校验 + `EvalSymlinks` 真实路径校验,链接无法逃逸工作区根;符号链接条目在列表中一律不展示;策略判定基于解析后路径,链接指向敏感文件也无法绕过 secret 过滤
- secret 正则大小写不敏感,`TOKEN.json`/`.ENV`/`Credential.json` 等一律拒绝读取
- 机器 token(`~/.laelia/machine-token-<machineID>`)与两个浏览根不相交,天然不可见;`machine-token-` 前缀进入永不显示规则,即使未来布局变化也不会在列表中暴露凭据文件名(详见"机器凭据落盘盘点")
- agentID/machineID 均为服务端生成的 UUID(`common.GetAgentResourceID`),天然安全,不依赖路径消毒;但 Delete 仍校验 `directoryName` 为裸目录名(防脏数据)
- 预览内容经 bidi 流返回,受既有 agent token / machine token 通道鉴权保护,不新增暴露面

## 实现状态与验证

### 实现状态

- 全部功能已实现并提交:`5af25f3 feat: add agent workspace file browser`
- 机器侧 `backend/agent/workspace/` 含单元测试:`policy_test.go` / `tree_test.go` / `file_test.go` / `scan_test.go`(覆盖路径穿越与符号链接逃逸、never-visible(含 `machine-token-` 前缀)、secret 正则大小写、大小限制、node_modules、排序、scan 跳过非目录、Delete 目录名校验)

### 验证清单

1. Go:`gofmt -w` → `golangci-lint run --allow-parallel-runners`(反复至干净)→ `go test ./backend/agent/workspace/... -count=1` → `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`
2. 前端:`pnpm --dir frontend biome:check` → `pnpm --dir frontend type-check` → `pnpm --dir frontend test`
3. 端到端手动验证:owner/管理员可见工作区 tab、普通成员不可见(直接访问 URL 重定向);懒加载展开、隐藏文件开关、刷新;文本/markdown/图片预览、二进制仅元信息;敏感文件(`.env`/`TOKEN.json` 等)拒绝;`.aws/.gnupg/.ssh` 与 `machine-token-*` 不出现;机器级扫描/删除(含二次确认);agent 离线报错

## 风险与注意事项

- **不触碰 executor 命令执行路径**:本功能只读/删除工作目录文件,与 `backend/agent/executor/` 的会话/命令状态机无交集;`workspace` 包不依赖 executor,避免测试耦合。删除目录前确认对应 agent runner 已停止(`DeleteMachineWorkspace` 只删目录,不负责停 agent,前端文案需提示)
- **发送并发**:`command_stream.go` 的异步回包与 ping/drain 并发发送,已通过 `serializedSender` 串行化;后续新增异步回包必须复用该发送路径
- **大目录性能**:scan 是递归全量统计,只用于机器级列表(低频、一次性),agent 级浏览保持按层懒加载,不做预展开
- **不引入新权限**:owner/管理员语义复用现有 `canEditAgent` / `isMachineAdmin`,权限矩阵不变
- **token 文件位置为安全边界**:机器 token 位于浏览根之外是当前布局的安全前提,后续若改动 `~/.laelia/` 布局(如把 token 移入机器目录),必须同步更新 `neverVisibleHiddenNames` / 正则规则
