# Agent 重构:provider 自动发现 + model/env 配置化

## Context

当前 laelia-agent 启动 LLM agent 时,需要管理员在配置页**手填** ACP 协议参数(`executable / args / allow_env`),没有任何 provider 概念、没有 model 选择、没有 key-value 自定义 env。这带来两个问题:

1. 用户必须知道每个 LLM agent 的准确启动命令(npx 包名、opencode 子命令等),门槛高且易错。
2. model 完全不可控——`AgentACPConfig` 里没有 model 字段,模型由 LLM agent 自身默认配置决定,前端无法干预。

目标:agent 守护进程**自动发现**本机已装的 LLM agent(当前 opencode 与 claude-code,预留扩展),前端展示并让用户**选定 provider + model + 自定义 env**,选定后作为该 agent 的**持久默认**,后续启动 LLM agent 时按此配置 spawn 并应用 model。

## ACP 协议对 model 的真实支持(关键约束)

查阅协议文档与本地 `github.com/coder/acp-go-sdk v0.13.5`(`~/go/pkg/mod/github.com/coder/acp-go-sdk@v0.13.5/types_gen.go`):

- `InitializeRequest` / `NewSessionRequest` / `PromptRequest` **均无 model 字段**。`NewSessionRequest` 只有 `cwd` + `mcpServers`(必填)。
- model 选择走 **session config options** 往返:`NewSessionResponse.ConfigOptions[]` 里 agent 会广播一组下拉项,`SessionConfigOptionCategory = "model"` 的那个就是模型选择器(`types_gen.go:4681`),agent 自定义 valueId;客户端调 `SetSessionConfigOption(sessionId, optionId, valueId)` 下发选中值。
- 另有 `UnstableListProviders` 等不稳定 API,不依赖。

**结论**:model 不能随 session 请求下发。方案采用 **config-option 往返为主**:spawn 后 `NewSession` 拿回 `ConfigOptions`,找 `category=="model"` 的项,若用户选的 valueId 在其 `Options` 内则调 `SetSessionConfigOption`。provider CLI flag(如 opencode `--model`)不作为主路径,仅作为 registry 接口的可选扩展点保留给未来。

## 架构总览

发现发生在 **agent 守护进程侧**(`backend/agent/client` 跑在 worker 机器上,是 spawn 子进程的一方,本机有 LLM 工具),而非 manager。流程:

1. agent 守护进程启动 → 探测本机已装 provider(PATH + `--version`)→ 对每个已装 provider **探测 model 列表**(spawn 一次,initialize+newSession,读回 `ConfigOptions` 里 model 项的 `Options`,带超时,并发)→ 缓存到内存 + 落盘 `~/.laelia/<agentID>/providers.json`。
2. 通过 `ConnectAgentRequest.info.available_providers` 上报给 manager;manager 存入 `agent.info` JSONB;前端 `GetAgent` 拿到下拉数据。
3. 用户在配置页选定 `provider + model + custom_env(key-value) + allow_env`,经 `UpdateAgentACPConfig` 持久化(server-owned,存 `agent.info.acp_config`)。
4. 命令执行时,agent 守护进程用选定的 `acp_config`(经 `ConnectAgentResponse.acp_config` 下发,已存在于 `handleServerACPConfig` `client.go:214`)→ `BuildACPConfig` 经 provider registry 解析出 `executable+args` → `NewACP` spawn → `NewSession` 后应用 model。

## 数据模型 / proto 变更

`proto/v1/v1/agent.proto` 与 `proto/store/store/agent.proto` 同步改:

- **`AgentACPConfig`**(扩展,字段号 4/5/6):
  - `string provider = 4;` — 选定的 provider id(`"opencode"` / `"claude-code"` / `"custom"`)
  - `string model = 5;` — 选定的 model valueId(对应探测到的 `Options[].value`)
  - `map<string,string> custom_env = 6;` — 用户自定义 key-value env,注入子进程时**叠加并覆盖** `allow_env` 继承值
  - 保留 `executable / args` 作为 `"custom"` provider 的逃生舱(未知 provider 仍可手填命令);已知 provider 的 executable/args 由 registry 派生,前端对已知 provider 隐藏这两栏。
  - `allow_env = 3` 保留(继承主机 env 的白名单)。

- **`AgentInfo`**(扩展,字段号 9):`repeated AgentProviderInfo available_providers = 9;`(agent 上报,server 不覆盖——复用 `convertToStoreAgentInfo` 对 `AcpConfig` 的"server-owned 不覆盖"模式,`available_providers` 同样 agent-owned)。

- **新 `AgentProviderInfo`**:
  - `string provider_id`、`string display_name`、`string version`、`string executable_path`
  - `repeated AgentModelOption models`(每项 `value`、`name`、`description`)
  - `bool supports_model_config_option`(探测时是否观察到 model config option)
  - `google.protobuf.Timestamp detected_at`

- **新 `AgentModelOption`**:`string value`、`string name`、`string description`。

- **新 unary RPC**(AgentService):`RefreshAgentProviders(RefreshAgentProvidersRequest{string name})` → 经 bidi 下发控制消息触发 agent 重新探测,返回最新 `available_providers`。

- **bidi 控制消息**(`proto/v1/v1/command.proto` 的 `ManagerStreamMessage` / `AgentStreamMessage` oneof 各加一项):
  - `ManagerStreamMessage.discover_providers = 9;`
  - `AgentStreamMessage.providers_discovered = 9;`(携带 `repeated AgentProviderInfo`)
  - manager 侧用一个 pending-response map 把 bidi 回包对应到 unary RPC 的响应(标准 request/response-over-bidi 模式)。

proto 改完按 CLAUDE.md 跑 `buf format -w proto` → `buf lint proto` → `cd proto && buf generate`。`agent.info` 是 JSONB 存整个 proto,新增字段向后兼容,**无需 DB schema 迁移**。

## Provider registry(新包 `backend/agent/provider/`)

接口设计,新增 provider = 加一个实现 + 注册:

```go
type Provider interface {
    ID() string                                          // "opencode" / "claude-code"
    DisplayName() string
    Detect(ctx context.Context) (*Detected, bool, error) // PATH 查找 + --version
    BuildCommand(workspaceDir string) (executable string, args []string)
    ProbeModels(ctx context.Context, workspaceDir string) ([]ModelOption, bool, error)
    // 返回 (models, supportsModelConfigOption, err):spawn→initialize→newSession→读 ConfigOptions
}
```

- `opencodeProvider`:`Detect` 找 `opencode`;`BuildCommand` → `opencode acp --pure --cwd <workspaceDir>`(与现有集成测试 `acp_executor_test.go:351` 一致);`ProbeModels` 走 config-option 往返。
- `claudeCodeProvider`:`Detect` 找 `claude`(及 `npx`);`BuildCommand` → `npx -y @agentclientprotocol/claude-agent-acp@latest`;`ProbeModels` 同上。
- `Registry`:`Lookup(id) Provider`、`All() []Provider`、`DetectAll(ctx)` 并发探测。探测 model 用带超时的 ctx(如 20s/provider,并发),失败/超时则该 provider `models` 为空、`supports_model_config_option=false`,不阻塞其它。

## executor 改动(`backend/agent/executor/`)

- `acp_config.go`:
  - `ACPConfig` 增 `Provider string`、`Model string`、`CustomEnv map[string]string`。
  - `BuildACPConfig`:`provider != ""` 时调 `provider.Registry.Lookup(provider).BuildCommand(workingDir)` 填充 `Executable/Args`;`provider == "custom"` 或为空但 `executable` 非空时退回旧路径(向后兼容)。把 `user.Model` / `user.CustomEnv` 透传到 `ACPConfig`。"未配置"判据从 `Executable==""` 改为 `Provider=="" && Executable==""`。
  - `BuildCapability` 同步调整 gating。

- `acp_executor.go`:
  - `run()` 在 `NewSession` 成功、拿到 `sessionResp` 后(`acp_executor.go:341` 附近),插入 **model 应用逻辑**:遍历 `sessionResp.ConfigOptions`,找 `Category=="model"` 的 `Select`;若 `e.config.Model` 非空且在其 `Options` 内,调 `e.conn.SetSessionConfigOption(...)`;否则记 warn 日志("agent 未广告 model config option,无法应用所选 model")。这是协议 sanctioned 的通用路径。
  - `buildACPEnv`(`acp_executor.go:887`):在 `requestEnv` overlay 之后、bootstrap env(`LAELIA_*`)之前,插入 `cfg.CustomEnv` overlay——即 `os.Environ → allow_env 过滤 → requestEnv → CustomEnv → bootstrap(LAELIA_*)`。CustomEnv 覆盖继承值,但 bootstrap 仍最后写入保证 `LAELIA_*` 不被用户覆盖。

- `runtime.go`:`Request` 不需要改(model 来自 `ACPConfig`,非 per-request)。

## agent 客户端改动(`backend/agent/client/`)

- `client.go`:
  - `collectAgentInfo`(`client.go:492`):启动时调 `provider.Registry.DetectAll(ctx)`,把结果填入 `AgentInfo.available_providers`(并落盘缓存)。
  - `handleServerACPConfig`(`client.go:214`):`BuildACPConfig` 已能消费新的 `provider/model/custom_env` 字段,无需改逻辑,但要确保 `acpConfig==nil` 的 "未配置" 判据与 `BuildACPConfig` 一致。
- `command_stream.go`:`buildRuntime`(`command_stream.go:596`)透传 `c.getAcpConfig()` 不变;env 仍来自 `req.Env`(per-command,生产路径为空)。

## manager 改动(`backend/manager/api/v1/agent.go`)

- `ConnectAgent`(`agent.go:369`):保留 `AcpConfig` re-attach 模式;同时**保留** `available_providers`(agent-owned,不覆盖)——在 `convertToStoreAgentInfo`(`agent.go:886`)里和 `AcpConfig` 一样单独处理:从入参拷贝,不被 agent-reported info 覆盖逻辑抹掉。
- `UpdateAgentACPConfig`(`agent.go:1051`):接受新字段,校验 `provider` 在已知集合内或为 `"custom"`;写 `Info{AcpConfig:...}` 时**一并保留** `available_providers / Hostname / Os / Arch / Capability / Labels`(当前实现会把这些冲掉,见探索报告,顺手修)。
- `CreateAgent`(`agent.go:66`):`AllowEnv` 仍 seed `DefaultAllowEnv`;不再 seed executable(改为 `provider=""` 表示未配置)。
- 新 `RefreshAgentProviders` handler:经该 agent 的活跃 bidi stream 发 `DiscoverProviders`,等 `ProvidersDiscovered` 回包,落 `agent.info.available_providers`,返回给调用方。

## 前端改动(`frontend/src/pages/dashboard/agents.tsx` + `stores/agent.ts`)

- 配置 Sheet 重做:
  - **Provider 下拉**:选项来自 `agent.info.availableProviders`(agent 上报);选已知 provider 后隐藏 `executable/args` 两栏(只显示派生命令只读预览),选 `"custom"` 才显示手填。
  - **Model 下拉**:选项来自所选 provider 的 `models[]`;绑定 `acpConfig.model`。
  - **自定义 ENV**:`StringListEditor` 换成 key-value 编辑器(组件名待定,可在 `agents.tsx` 内联或抽 `KeyValueEnvEditor`),绑定 `acpConfig.customEnv`。
  - **allow_env**:保留现有 `StringListEditor`(继承白名单)。
  - "刷新 provider/model" 按钮:调 `RefreshAgentProviders(agent name)` 后 `getAgent(force:true)` 重载。
- `stores/agent.ts`:`updateAgentACPConfig` 透传新字段;新增 `refreshAgentProviders` action。
- proto 重生成后 `frontend/src/types/proto-es/v1/agent_pb.d.ts` 自动更新;跑 `pnpm --dir frontend biome:check` + `type-check`。

## 扩展性

新增 provider 只需:在 `backend/agent/provider/` 加一个实现 `Provider` 接口的结构体并在 `Registry` 注册;proto 字段不动(provider id 是字符串)。`"custom"` provider 永远保留,作为未内置 provider 的逃生舱,保证"预览对其他 provider 的支持"。

## 关键文件清单

| 改动 | 文件 |
|---|---|
| proto 扩展 | `proto/v1/v1/agent.proto`、`proto/v1/v1/command.proto`、`proto/store/store/agent.proto` |
| provider registry(新) | `backend/agent/provider/registry.go`、`opencode.go`、`claudecode.go`、`provider.go`(接口) |
| executor | `backend/agent/executor/acp_config.go`、`backend/agent/executor/acp_executor.go` |
| agent 客户端 | `backend/agent/client/client.go`、`backend/agent/client/command_stream.go` |
| manager API | `backend/manager/api/v1/agent.go` |
| 前端 | `frontend/src/pages/dashboard/agents.tsx`、`frontend/src/stores/agent.ts` |
| 测试 | `backend/agent/executor/acp_executor_test.go`(model 应用)、`backend/agent/provider/*_test.go`(探测) |

## 验证

1. **单测**:`provider` 包——mock 一个假 ACP server(或用现有 `acp_executor_test.go` 的本地 opencode 路径)验证 `DetectAll` / `ProbeModels` 解析 `ConfigOptions` 正确;executor 包——验证 `buildACPEnv` 中 `CustomEnv` 覆盖 `allow_env` 继承值、bootstrap `LAELIA_*` 仍最后写入;验证 `BuildACPConfig` 对已知 provider 派生命令、对 `"custom"` 走旧路径。
2. **ACP 集成**:`LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`(本机有 opencode 时)验证 model 经 config-option 往返生效。
3. **lint/build**:按 CLAUDE.md 跑 `golangci-lint run --allow-parallel-runners` 与 `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`;前端 `pnpm --dir frontend biome:check` + `type-check` + `test`。
4. **端到端**:启动 manager + agent 守护进程(本机装 opencode),前端 agents 页应自动出现 opencode provider 及其 model 列表;选定 provider+model+一组 custom env 后保存;发起一条命令,确认子进程以派生命令启动、env 含 custom env、model 通过 `SetSessionConfigOption` 下发(可在 agent 守护进程日志确认)。
5. **回归**:确认未配置 provider 的旧 agent 仍能走 `"custom"`/旧 executable 路径正常工作;`UpdateAgentACPConfig` 不再冲掉 `hostname/os/capability` 等上报字段。

## 待实现时确认的假设

- opencode 与 claude-agent-acp 在 `NewSession` 响应里都广播 `category=="model"` 的 config option;若某 provider 不广播,`ProbeModels` 返回空列表 + `supports_model_config_option=false`,前端 model 下拉退化为只读提示"该 provider 不支持经协议选 model",`acp_config.model` 留空。实现时先验证此假设(用 `LAELIA_RUN_OPENCODE_ACP_TESTS=1` 跑一次打印 `sessionResp.ConfigOptions`)。