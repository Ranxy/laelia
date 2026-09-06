# Agent 工作区文件浏览器(Workspace Browser)设计与实现

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：机器级"删除工作区"已重构为 `DeleteAgentWorkspace` 控制消息（随 DeleteAgent 触发，独立 RPC/页面删除按钮已移除）；机器凭据落盘盘点更新为 `~/.laelia/machine.json` 与新数据根布局；机器侧工作区接收泵移至 `message_router.go`；前端新增 HTML 沙箱预览、改用共享 `MarkdownRenderer`（streamdown），布局文件移至 `src/app/layouts/`。

> 本文档描述 laelia 的 agent 工作区文件浏览器功能:需求、安全边界、协议、manager/机器侧/前端实现,以及验证方式。内容与当前代码实现保持一致(实现提交 `5af25f3 feat: add agent workspace file browser`,及其后的演进,见上文状态行)。

## 背景与需求

管理页面需要查看 agent 在宿主机上的工作目录(文件树)并预览文本/图片内容,便于 owner/管理员巡检与排障;同时需要机器级的工作区管理(查看各 agent 工作目录占用、删除不再需要的目录)。

### 需求确认(用户决策)

1. **UI 位置**:agent 详情页新增"工作区"tab
2. **预览**:文本 + 图片预览
3. **隐藏文件**:提供"显示隐藏文件"开关
4. **权限**:仅 owner/管理员可查看(内容敏感);无权限则工作区 tab 不显示
5. **机器级工作区管理**:接入 scan 列表;删除已重构为 `DeleteAgentWorkspace` 控制消息(随 `DeleteAgent` 触发,先停 runner 再删目录),不再提供独立的"删除工作区"RPC/页面按钮(见"已变更"说明)

### 实现决策(用户补充确认)

1. **机器 token 不可暴露**:已调研机器凭据落盘位置,见下文"机器凭据落盘盘点"(**已更新**:凭据文件现为数据根下的 `machine.json`,不再有 `machine-token-<id>` 文件与对应的列表过滤前缀)
2. **前端双栏布局**:左侧树状文件目录,右侧文件内容;点击左侧文件直接在右侧展示
3. **markdown 渲染**:经共享的 `MarkdownRenderer`(`frontend/src/lib/markdown.tsx`,基于 streamdown)渲染;另新增 HTML 文件的沙箱 iframe 预览(`components/workspace/html-file-view.tsx`)

## 机器凭据落盘盘点(调研结论,已按当前代码更新)

数据根默认 `~/.laelia/`,可用 `LAELIA_HOME` 覆盖(`backend/agent/home/home.go`)。当前布局:

```
<数据根>/  (~/.laelia/,LAELIA_HOME 可覆盖)
├── machine.json          ← 机器注册状态 + refresh token(0600 原子写,state/state.go)
├── daemon.sock           ← daemon unix socket(数据根下,daemon/server.go)
├── bin/
│   └── pi-<hash>-<os>-<arch>/pi   ← pi 二进制缓存(pi/binary_release.go)
└── <machineID>/
    └── <agentID>/        ← agent 工作区(executor.AgentWorkingDir)
        ├── command-state.json    ← 命令执行状态(executor/state.go)
        ├── acp-session.json      ← ACP session 状态 + fingerprint(executor/acp_session.go)
        ├── context-state.json    ← 上下文状态(executor/context_state.go)
        ├── pi-session.json       ← pi 会话路径 + fingerprint(pi/session.go)
        └── …                     ← LLM agent 自身在 cwd 内产生的项目文件/状态
```

> 已变更:bootstrap-token 时代的 `machine-token-<machineID>` 逐机 token 文件已移除(`state/state.go`:"the bootstrap-token era's per-machine token files are gone");机器 id 与 refresh token 合并进 `machine.json` 一个文件,`daemon.sock` 也移到数据根下(不再位于 `<machineID>/` 内)。

**凭据盘点结论**:

| 凭据 | 落盘位置 | 是否在可浏览区域内 | 说明 |
| --- | --- | --- | --- |
| 机器 refresh token | `<数据根>/machine.json` | **否** | 位于数据根顶层,是机器级 scan 根 `<数据根>/<machineID>/` 的**兄弟文件**,不在任何浏览根内;0600 权限,原子写 |
| 机器 access token | 仅内存(`client.go` 的 `accessToken` 字段) | — | 每次 refresh 更新,不落盘 |
| agent 通道鉴权 | 复用机器 access token(`runner.go` 的 `cs.getToken`) | — | 不落盘 |
| daemon session token | 仅内存(daemon 启动随机生成) | — | 进程启动随机生成,不落盘 |
| pi LLM API key | 仅注入子进程 env(`pi/config.go`) | — | 落盘只有 sha256 fingerprint(截断 hex),非原文 |
| 状态文件(command/acp-session/context/pi-session.json) | agent 工作目录内 | **是** | 只含 session/命令/上下文状态与 fingerprint,**不含 token/密钥** |
| LLM agent 全局凭据(如 opencode 的 auth.json) | LLM 工具自身全局目录(`~/.config`、`~/.local/share` 等) | **否** | 在 cwd(agent 工作目录)之外,浏览不到 |

**结论与过滤策略**:

1. 机器凭据文件 `machine.json` 与两个浏览根(`<数据根>/<machineID>/`、`<数据根>/<machineID>/<agentID>/`)均不相交,天然不可见;机器级 scan 只列子目录(`IsDir` 过滤),`daemon.sock`(socket 文件)与 `<数据根>/bin/`(兄弟目录)也不会出现
2. **已变更**:`machine-token-` 前缀的"永不显示"过滤规则随旧 token 文件一并移除(`workspace/policy.go` 的 `isNeverVisibleEntry` 现在只匹配 `.aws/.gnupg/.ssh`);防暴露改由"凭据在浏览根之外"这一布局前提保证,读层仍由 secret 正则兜底(文件名含 `token` 段即拒绝读取)
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

Machine 级同构:`MachineService.ListMachineWorkspaces` → `ManagerMachineStreamMessage`(scan 请求)下发 → `machine_control.go` 接收泵 → `<数据根>/<machineID>/` 扫描 → `MachineStreamMessage` 回包。**已变更**:机器级"删除工作区"不再是 scan/delete 对称的 pending-reply RPC,而是单向控制消息 `DeleteAgentWorkspace`(先停 runner 再删目录,无回包),由 manager 侧 `DeleteAgent` 触发(见下文协议与"已变更"说明)。

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
    MachineWorkspaceScanRequest machine_workspace_scan_request = 7; // scan per-agent workspace directories
    DeleteAgentWorkspace delete_agent_workspace = 8; // stop the runner and delete an agent's workspace directory
  }
}

message MachineWorkspaceScanRequest {
  string request_id = 1;
}

// DeleteAgentWorkspace tells the machine to tear down an agent's runner and
// permanently remove its workspace directory under the machine data root.
message DeleteAgentWorkspace {
  string agent_name = 1; // "agents/<id>";机器侧据此定位 <agentID> 目录
}
```

`MachineStreamMessage` oneof 新增(字段号 5,当前另有 upgrade/models 复用其它号):

```proto
message MachineStreamMessage {
  oneof message {
    // ...
    MachineWorkspaceScanResponse machine_workspace_scan_response = 5;
  }
}
```

> **已变更**:设计稿中的 `MachineWorkspaceDeleteRequest/Response`(request_id + directory_name,回包 success)与 `MachineStreamMessage` 的 delete_response 字段**未采用**;删除改为上述单向 `DeleteAgentWorkspace`(以 `agent_name` 定位,先 `stopRunner` 再删除整个工作区目录,无 pending 回包)。

`MachineWorkspaceSummary` 与 scan 回包与设计一致(`directory_name` / `total_size_bytes` / `last_modified` / `file_count`)。

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

MachineService(**已变更**:仅保留 scan;独立的 DeleteMachineWorkspace RPC 未采用):

```proto
rpc ListMachineWorkspaces(ListMachineWorkspacesRequest) returns (ListMachineWorkspacesResponse) {
  option (google.api.http) = { post: "/v1/{name=machines/*}:listWorkspaces" };
}

message ListMachineWorkspacesRequest {
  string name = 1;            // machines/{machine}
}
message ListMachineWorkspacesResponse {
  repeated MachineWorkspaceSummary workspaces = 1;
}
```

删除工作区的入口改为 agent 生命周期 API:`AgentService.DeleteAgent`(`backend/manager/api/v1/agent.go`)在软删 agent 行后,经 dispatcher **best-effort** 下发 `SendRemoveAgent` + `SendDeleteAgentWorkspace`(权限同 `DeleteAgent` 本身:owner 或 `laelia.agents.edit`,见 `resolveEditableAgent`);推送失败无害——agent 已软删,不会在下次 `ConnectMachine` 重同步中复活 runner。

**权限门控(handler 内)**:

| RPC / 消息 | 门控 | 说明 |
| --- | --- | --- |
| `ListAgentWorkspace` / `ReadAgentWorkspaceFile` | `canEditAgent(ctx, user, agent)` | owner 或 workspace admin(`laelia.agents.edit`) |
| `ListMachineWorkspaces` | `isMachineAdmin(ctx, iam, user, machine)` | 机器创建者或 workspace admin(与 `Machine.can_manage` 一致) |
| `DeleteAgentWorkspace`(消息) | `DeleteAgent` 的 `canEditAgent` 门控 | 随 agent 删除间接生效,无独立 RPC |

不新增 `laelia.workspace.*` 权限——owner/管理员语义已由现有权限精确覆盖,避免权限矩阵膨胀。`user` 通过现有 auth interceptor 从 ctx 取(参考 `RefreshMachineProviders` 的取法)。

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

Dispatcher 上挂 3 个工作区实例 + 对应 Send/Register/Cancel/Complete 方法(镜像 `SendDiscoverProviders`/`RegisterPendingDiscover`/`CompletePendingDiscover` 签名;`pendingDiscovers` 之外后续又加了 `pendingModels`,与本功能无关):

- `pendingWorkspaceLists` + `SendWorkspaceListRequest(agentID int, requestID, dirPath string, includeHidden bool) error`(发给 `agentID` 的 AgentChannel,经 `agent_channels` 定位)
- `pendingWorkspaceReads` + `SendWorkspaceReadRequest(agentID int, requestID, path string) error`
- `pendingMachineScans` + `SendMachineWorkspaceScan(machineID int, requestID string) error`(经机器控制通道)

> **已变更**:设计稿中的第 4 个实例 `pendingMachineDeletes` + `SendMachineWorkspaceDelete` 未采用——删除走单向 `DeleteAgentWorkspace` 控制消息(`dispatcher.SendDeleteAgentWorkspace(machineID, agentName)`,无 pending/complete 配对)。

**Manager 接收泵接线**:

- `backend/manager/api/v1/agent_command.go` AgentChannel 接收循环新增两个 case:
  - `case *v1pb.AgentStreamMessage_WorkspaceListResponse: s.dispatcher.CompletePendingWorkspaceList(m.WorkspaceListResponse)`
  - `case *v1pb.AgentStreamMessage_WorkspaceReadResponse: s.dispatcher.CompletePendingWorkspaceRead(m.WorkspaceReadResponse)`
- `backend/manager/api/v1/machine_command.go` 接收循环新增一个 case:
  - `MachineStreamMessage_MachineWorkspaceScanResponse` → `CompletePendingMachineWorkspaceScan`
  - (无 delete 回包 case——删除为单向消息)

**Unary handler 范式**(`backend/manager/api/v1/agent_config.go` — 设计稿写 `agent.go`,实际落点随后续拆分移到此处):

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

`ReadAgentWorkspaceFile` 同构(同在 `agent_config.go`);`ListMachineWorkspaces` 在 `machine_workspace.go`,用 `IsMachineConnected(machineID)` 做在线检查(设计稿中的 `DeleteMachineWorkspace` 及"`success=false` 映射 `CodeFailedPrecondition`"随删除重构一并移除)。

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

  // 永不显示:通用高敏凭据目录(.aws/.gnupg/.ssh)。
  // 已变更:机器凭据文件前缀 machine-token- 的规则随旧 token 文件移除;
  // 防暴露改由"machine.json 在浏览根之外"的布局前提保证(见"机器凭据落盘盘点")。
  var neverVisibleHiddenNames = map[string]bool{".aws": true, ".gnupg": true, ".ssh": true}

  func isNeverVisibleEntry(name string) bool // 集合命中(仅 neverVisibleHiddenNames)
  func isHiddenPath(rel string) bool         // 任一 part 以 "." 开头
  func isNeverVisiblePath(rel string) bool   // 任一 part 为永不显示
  func isSecretFilePath(rel string) bool     // 任一 part 命中 secretFilePatterns
  ```

  > 说明:secret 正则编译为**大小写不敏感**(`(?i)`),`TOKEN.json`、`.ENV`、`Credential.json` 等一律拒绝读取;`.aws/.gnupg/.ssh` 是 LLM agent 可能在 workdir 内创建的高敏凭据目录,列表层永不显示;机器凭据 `machine.json` 位于数据根顶层、在所有浏览根之外,不依赖文件名过滤。

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
- `scan.go` — `Scan(root) ([]Summary, error)`(**已变更**:`Delete` 与 `isValidWorkspaceDirectoryName` 已移除——删除不再由 workspace 包承担,见下文 `deleteAgentWorkspace`):
  - `Scan`:读一级目录,仅 `IsDir()` 项(socket/普通文件天然跳过),递归统计 `totalSizeBytes`/`fileCount`/`latestMtime`(`summarizeWorkspaceDirectory`,单项失败容错跳过)

#### 接收泵接线

`backend/agent/client/message_router.go` 的接收泵 switch(**已变更**:工作区 handler 自 `command_stream.go` 迁入 router)新增两个 case(异步处理 + 回包):

```go
case *v1pb.ManagerStreamMessage_WorkspaceListRequest:
    go r.stream.handleWorkspaceList(ctx, sender, m.WorkspaceListRequest)
case *v1pb.ManagerStreamMessage_WorkspaceReadRequest:
    go r.stream.handleWorkspaceRead(ctx, sender, m.WorkspaceReadRequest)
```

- **发送串行化**:`connect-go` 的 `Send` 不允许并发调用,而异步回包 goroutine 与 ping ticker、drain loop 并发发送,因此原始 stream 被包进 `serializedSender`(内部 mutex 串行化 `Send`,仍在 `command_stream.go`),所有发送点统一经它
- `handleWorkspaceList`:root = `executor.AgentWorkingDir(c.machineID, c.agentID)`(即 `<数据根>/<machineID>/<agentID>/`,同时覆盖 pi 布局)→ `workspace.List` → 回 `AgentStreamMessage_WorkspaceListResponse`
- `handleWorkspaceRead`:同 root → `workspace.Read` → 回 `WorkspaceReadResponse`

`backend/agent/client/machine_control.go` 接收泵 switch 新增一个 case(复用现有 `sendStream`):

```go
case *v1pb.ManagerMachineStreamMessage_MachineWorkspaceScanRequest:
    go c.handleMachineWorkspaceScan(ctx, sendStream, m.MachineWorkspaceScanRequest)
```

- `handleMachineWorkspaceScan`:root = `home.Join(c.machineID)`(即 `<数据根>/<machineID>/`;机器凭据 `machine.json` 在数据根顶层、不在本根内,扫描不到)→ `workspace.Scan` → 回 `MachineWorkspaceScanResponse`
- **删除(已变更)**:`ManagerMachineStreamMessage_DeleteAgentWorkspace` case 在 `machine_control.go` **内联**执行(快速目录删除,不占泵太久):`c.stopRunner(agentName)` 先停 runner,再 `c.deleteAgentWorkspace(agentName)`(`runner.go`,`os.RemoveAll(executor.AgentWorkingDir(machineID, agentID))`,目录不存在视为成功)——设计稿的 `handleMachineWorkspaceDelete` + `workspace.Delete` 已移除

### 前端设计

#### Agent:工作区 tab(双栏布局)

- `frontend/src/router/handles.ts`:`AGENT_ROUTE_WORKSPACE = "agent.workspace"`
- `frontend/src/router/routes/dashboard.tsx` agent 子路由新增:
  ```ts
  { path: "workspace", handle: { name: AGENT_ROUTE_WORKSPACE },
    lazy: () => import("@/pages/dashboard/agent-workspace").then(m => ({ Component: m.AgentWorkspacePage })) }
  ```
- `frontend/src/app/layouts/agent-detail-layout.tsx`(布局文件**已迁移**至 `src/app/layouts/`,经共享 `DetailTabsLayout` 渲染 tab):
  - tab 表增加 `key: "workspace"` 项(labelKey `agent.tab-workspace`,路由 `AGENT_ROUTE_WORKSPACE`);详情页现含 profile / chat / mcp / workspace 四个 tab
  - **tab 显隐**:layout 挂载时经 store `getAgent(agentId)` 拉取完整详情(`canEdit` 是 per-caller 字段、不可依赖缓存),`agent.canEdit === true` 才渲染工作区 tab;直接访问 `/workspace` 路由时页面内做同样门控并重定向到 profile
- 新页面 `frontend/src/pages/dashboard/agent-workspace.tsx`:**双栏布局**——左 `w-72` 树、右内容面板,点击左侧文件直接在右侧展示:
  - `frontend/src/components/workspace/workspace-tree.tsx`:递归树,目录节点展开时**按层调用** `ListAgentWorkspace(dirPath)`(懒加载,不预展开);顶部"显示隐藏文件"复选框(切换后从根重新拉取)与刷新按钮;服务端已完成过滤(`node_modules`/永不显示/secret)与排序;隐藏条目以 60% 透明度展示(`opacity-60`);加载/空/错误态(错误态含重试);另支持按名称过滤(`SearchInput`,仅过滤已加载节点——树是懒加载的)
  - `frontend/src/components/workspace/workspace-file-panel.tsx`:文件头(名称、大小、关闭);加载态;`error` 非空 → danger 文案(直接展示机器侧返回的原因,如敏感文件/超限);图片 → `data:<mime>;base64,` 内联展示;其他二进制 → 仅大小元信息;文本 → 等宽 `pre` 展示
  - **markdown 渲染**:`.md`/`.markdown` 且非 binary 的文件经共享 `MarkdownRenderer`(`frontend/src/lib/markdown.tsx`,基于 **streamdown**)渲染(**已变更**:设计稿的 markstream-react `MarkdownRender` 直用方式已替换)
  - **HTML 预览(后续新增)**:`.html/.htm/.xhtml` 且非 binary 的文件经 `frontend/src/components/workspace/html-file-view.tsx` 在沙箱 iframe 内渲染(`buildHtmlPreviewDoc` + `useHtmlPreviewBridge`,链接点击经安全 scheme 白名单外开)
- 新 store slice `frontend/src/stores/workspace.ts`(`createWorkspaceSlice`):`listAgentWorkspaceDir(name, dirPath, includeHidden)`、`readAgentWorkspaceFile(name, path)`、`listMachineWorkspaces(name)`(**已变更**:`deleteMachineWorkspace` 随删除重构移除);经 `@/connect` 的 `agentServiceClient`/`machineServiceClient` 调用,`create(RequestSchema, ...)` 编解码

#### Machine:工作区管理

- `frontend/src/router/handles.ts`:`MACHINE_ROUTE_WORKSPACE = "machine.workspace"`
- `frontend/src/router/routes/dashboard.tsx` machine 子路由 `{ path: "workspace", ... }` → `machine-workspace` 页面
- `frontend/src/app/layouts/machine-detail-layout.tsx`(布局文件**已迁移**至 `src/app/layouts/`,与 agent 一致经共享 `DetailTabsLayout` 渲染)采用 profile / 工作区 tab 布局,工作区 tab 仅当 `machine.canManage` 为 true 时渲染(`GetMachine` 已填充 `canManage`);页面内再校验并重定向
- 新页面 `frontend/src/pages/dashboard/machine-workspace.tsx`:表格列出 `directoryName`/`totalSizeBytes`(`formatBytes`)/`fileCount`/`lastModified`(`formatTimestamp`)+ 刷新按钮;空态/加载/错误态齐全。**已变更**:"删除"按钮(AlertDialog 二次确认)已随删除重构移除——删除 agent 工作区的唯一入口是删除该 agent(`DeleteAgent`,机器先停 runner 再删目录),页面只读展示

#### i18n

`zh-CN.json` / `en-US.json` 的 `workspace.*` keys(当前实际):show-hidden / empty / load-error / binary-file / loading / refresh / directory / size / file-count / last-modified / no-workspaces / close / select-file(**已变更**:preview / sensitive-file / file-too-large / delete-confirm / deleted / delete / delete-error 等未使用——文件读取拒绝原因直接展示机器侧返回的 `error` 字符串,删除功能已移除);另有 `agent.tab-workspace` / `machine.tab-workspace`。

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
- 机器凭据文件(`<数据根>/machine.json`)与两个浏览根不相交,天然不可见;**已变更**:`machine-token-` 前缀过滤规则随旧 token 文件移除,防暴露依赖"凭据在浏览根之外"的布局前提(详见"机器凭据落盘盘点")
- agentID/machineID 均为服务端生成的 UUID(`common.GetAgentResourceID`),天然安全,不依赖路径消毒;**已变更**:删除不再校验裸目录名(以 `agent_name` 定位工作区,经服务端资源名解析,见下文)
- 预览内容经 bidi 流返回,受既有 agent token / machine token 通道鉴权保护,不新增暴露面

## 实现状态与验证

### 实现状态

- 全部功能已实现并提交:`5af25f3 feat: add agent workspace file browser`;其后的演进(均已在代码中):
  - **删除重构**:独立 `DeleteMachineWorkspace` RPC / `workspace.Delete` / 页面删除按钮移除,改为 `DeleteAgentWorkspace` 控制消息(`DeleteAgent` 触发,先停 runner 再删目录)
  - **机器侧接收泵迁移**:工作区 handler 自 `command_stream.go` 迁至 `client/message_router.go`
  - **凭据布局**:逐机 `machine-token-<id>` 文件合并为数据根下的 `machine.json`(0600),`daemon.sock` 移至数据根下
  - **前端**:markdown 预览改用共享 `MarkdownRenderer`(streamdown),新增 HTML 沙箱 iframe 预览,布局文件迁至 `src/app/layouts/`
- 机器侧 `backend/agent/workspace/` 含单元测试:`policy_test.go` / `tree_test.go` / `file_test.go` / `scan_test.go`(覆盖路径穿越与符号链接逃逸、never-visible 目录、secret 正则大小写、大小限制、node_modules、排序、scan 跳过非目录;**已变更**:`Delete` 目录名校验用例随 `Delete` 移除)

### 验证清单

1. Go:`gofmt -w` → `golangci-lint run --allow-parallel-runners`(反复至干净)→ `go test ./backend/agent/workspace/... -count=1` → `go build -ldflags "-w s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`
2. 前端:`pnpm --dir frontend biome:check` → `pnpm --dir frontend type-check` → `pnpm --dir frontend test`
3. 端到端手动验证:owner/管理员可见工作区 tab、普通成员不可见(直接访问 URL 重定向);懒加载展开、隐藏文件开关、名称过滤、刷新;文本/markdown/**HTML**/图片预览、二进制仅元信息;敏感文件(`.env`/`TOKEN.json` 等)拒绝;`.aws/.gnupg/.ssh` 与 `machine.json` 不出现;机器级扫描(只读列表);agent 离线报错;**删除工作区**走 `DeleteAgent`(机器侧先停 runner 再删目录)

## 风险与注意事项

- **不触碰 executor 命令执行路径**:本功能只读工作目录文件,与 `backend/agent/executor/` 的会话/命令状态机无交集;`workspace` 包不依赖 executor,避免测试耦合。**已变更**:删除工作区由 `DeleteAgentWorkspace` 控制消息承担,机器侧先 `stopRunner` 再删目录,不再需要前端/调用方自行确认 runner 状态
- **发送并发**:`message_router.go`/`command_stream.go` 的异步回包与 ping/drain 并发发送,已通过 `serializedSender` 串行化;后续新增异步回包必须复用该发送路径
- **大目录性能**:scan 是递归全量统计,只用于机器级列表(低频、一次性),agent 级浏览保持按层懒加载,不做预展开
- **不引入新权限**:owner/管理员语义复用现有 `canEditAgent` / `isMachineAdmin`,权限矩阵不变
- **凭据文件位置为安全边界**:`machine.json` 位于浏览根之外是当前布局的安全前提,后续若改动数据根布局(如把凭据移入 `<machineID>/`),必须同步更新 `neverVisibleHiddenNames` / 正则规则
