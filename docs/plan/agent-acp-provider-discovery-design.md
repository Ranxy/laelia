# Agent 重构:provider 自动发现 + model/env 配置化

> 状态：2026-09-06 已对照当前代码核对更新。主要变化：本设计已全部落地，且 agent 守护进程已演进为 machine 守护进程（每个 agent 一条 AgentChannel），provider 发现改为 machine 级（`MachineInfo.available_providers`），并新增 codex（acp-v2）与 pi 两类 provider 及 `RefreshAgentModels` 单 provider 探测。

## Context

本节描述设计时的基线（现已全部实现）：当时 laelia-machine 启动 LLM agent 需要管理员在配置页**手填** ACP 协议参数（`executable / args / allow_env`），没有 provider 概念、没有 model 选择、没有 key-value 自定义 env。这带来两个问题：

1. 用户必须知道每个 LLM agent 的准确启动命令(npx 包名、opencode 子命令等),门槛高且易错。
2. model 完全不可控——`AgentACPConfig` 里没有 model 字段,模型由 LLM agent 自身默认配置决定,前端无法干预。

目标(已实现):machine 守护进程**自动发现**本机已装的 LLM agent(现为 opencode、claude-code、codex、pi,`"custom"` 永远保留为逃生舱),前端展示并让用户**选定 provider + model + 自定义 env**,选定后作为该 agent 的**持久默认**,后续启动 LLM agent 时按此配置 spawn 并应用 model。

## ACP 协议对 model 的真实支持(关键约束)

查阅协议文档与本地 `github.com/coder/acp-go-sdk v0.13.5`(`~/go/pkg/mod/github.com/coder/acp-go-sdk@v0.13.5/types_gen.go`):

- `InitializeRequest` / `NewSessionRequest` / `PromptRequest` **均无 model 字段**。`NewSessionRequest` 只有 `cwd` + `mcpServers`(必填)。
- model 选择走 **session config options** 往返:`NewSessionResponse.ConfigOptions[]` 里 agent 会广播一组下拉项,`SessionConfigOptionCategory = "model"` 的那个就是模型选择器(`types_gen.go:4681`),agent 自定义 valueId;客户端调 `SetSessionConfigOption(sessionId, optionId, valueId)` 下发选中值。
- 另有 `UnstableListProviders` 等不稳定 API,不依赖。

**结论**:model 不能随 session 请求下发。实现采用 **config-option 往返为主**(`backend/agent/executor/acp_executor.go` 的 `applySelectedModel`):spawn 后 `NewSession`/`ResumeSession` 拿回 `ConfigOptions`,找 `category=="model"` 的项,若用户选的 valueId 在其 `Options` 内则调 `SetSessionConfigOption`。例外:acp-v2 provider(codex)不走 config-option,而是 `model/list` 列模型、`thread/start` 直接带 model 参数(见 `backend/agent/acp2/` 与 `backend/agent/executor/thread_executor.go`);builtin-pi/user-pi 由 pi 运行时自管 model。

## 架构总览

发现发生在 **machine 守护进程侧**(`backend/agent/client` 的 `MachineClient`,是 spawn 子进程的一方,本机有 LLM 工具),而非 manager——且发现结果按 **machine** 聚合(一台机器承载多个 agent,共享同一份 provider 列表)。流程:

1. machine 守护进程启动 → 后台并发探测本机已装 provider(PATH + `--version` + 探测 model 列表)→ 缓存到内存(`MachineClient.refreshProviders`,**不再落盘** providers.json)。
2. 通过 `ConnectMachineRequest.info.available_providers` 上报给 manager;manager 存入 `machine.info` JSONB(`MachineInfo.available_providers`,字段 9);每个 agent 的配置校验与前端下拉都取所属 machine 的这份列表。
3. 用户在 agent 配置页(或建 agent 表单)选定 `provider + model + custom_env(key-value) + allow_env`,经 `UpdateAgentACPConfig`(或 `CreateAgent` 携带初始 `acp_config`)持久化(server-owned,存 `agent.info.acp_config`)。
4. machine 守护进程经 `ConnectMachineResponse.assigned_agents`(`AgentAssignment.acp_config`)与 MachineChannel 的 `AgentConfigUpdate` / `ReloadAgentAssignment` 拿到该 agent 的配置 → `BuildACPConfig` 经 provider registry 解析出 `executable+args` → `NewACP` spawn → `NewSession` 后应用 model。

## 数据模型 / proto 变更(已全部落地)

`proto/v1/v1/agent.proto` 与 `proto/store/store/agent.proto` 已同步落地:

- **`AgentACPConfig`**(已实现,字段号 4/5/6 与设计一致):
  - `string provider = 4;` — 选定的 provider id(`"opencode"` / `"claude-code"` / `"codex"` / `"pi"` / `"builtin-pi"` / `"custom"`)
  - `string model = 5;` — 选定的 model valueId(对应探测到的 `Options[].value`;acp-v2 provider 为 model id)
  - `map<string,string> custom_env = 6;` — 用户自定义 key-value env,注入子进程时**叠加并覆盖** `allow_env` 继承值
  - 保留 `executable / args` 作为 `"custom"` provider 的逃生舱(未知 provider 仍可手填命令);已知 provider 的 executable/args 由 registry 派生,前端对已知 provider 隐藏这两栏。
  - `allow_env = 3` 保留(继承主机 env 的白名单)。
  - 落地后追加的字段(非本设计范围):`persona_prompt`(7)、`api_provider`(8)、`api_key`(9)、`global_provider`(10)、`global_provider_entry`(11)、`protocol`(12,`acp-v1`/`acp-v2`)、`api_base_url`(13)、`context_window`(14)、`max_tokens`(15)——builtin-pi 运行时的 LLM API 配置见 `docs/plan/pi-user-installed-provider-design.md`。

- **`AgentInfo`**(`repeated AgentProviderInfo available_providers = 9;`)已实现,agent/machine 上报、server 不覆盖(`backend/manager/api/v1/agent_convert.go` 的 `convertToStoreAgentInfo` 与 `backend/manager/api/v1/machine_convert.go` 同样处理 machine 级 `MachineInfo.available_providers`)。注意:machine 重构后 **agent 侧的 available_providers 由所属 machine 的列表镜像**,实际发现与刷新入口是 machine 级的 `RefreshMachineProviders`。

- **`AgentProviderInfo`**(已实现,与设计一致):`provider_id`、`display_name`、`version`、`executable_path`、`repeated AgentModelOption models`、`supports_model_config_option`、`detected_at`;落地时追加了 `compatible` / `incompatibility_reason`(探测到但版本不满足最低要求时 UI 展示但禁止选择,如 user-pi `< 0.82.1`)。

- **`AgentModelOption`**:`string value`、`string name`、`string description`(已实现)。

- **unary RPC(已实现,AgentService)**:`RefreshAgentProviders(RefreshAgentProvidersRequest{string name})` → 经该 agent 的 AgentChannel bidi 下发 `DiscoverProviders` 控制消息,`pending-response` map 关联回包(标准 request/response-over-bidi 模式,`dispatcher.RegisterPendingDiscover`),返回最新 `available_providers` 并落库 `agent.info.available_providers`。另落地了 `RefreshAgentModels`(带 draft `acp_config.custom_env` 探测单 provider 的模型列表,经 MachineChannel `DiscoverModels`/`ModelsDiscovered`,不落库)。

- **bidi 控制消息(已实现)**:
  - per-agent 通道(`proto/v1/v1/command.proto`):`ManagerStreamMessage.discover_providers = 9;`、`AgentStreamMessage.providers_discovered = 9;`(携带 `repeated AgentProviderInfo`,与设计字段号一致)。
  - machine 控制通道(`proto/v1/v1/machine.proto`):`ManagerMachineStreamMessage.discover_providers = 4;`、`MachineStreamMessage.providers_discovered = 3;`,以及 `discover_models = 11` / `models_discovered = 10`。

`agent.info` / `machine.info` 是 JSONB 存整个 proto,新增字段向后兼容,**无需 DB schema 迁移**(与设计预期一致,未发生迁移)。

## Provider registry(包 `backend/agent/provider/`,已落地)

实际接口(新增 provider = 加一个实现 + 在 `Registry.Default()` 注册):

```go
type Provider interface {
    ID() string                                          // "opencode" / "claude-code" / "codex" / "pi"
    DisplayName() string
    Detect(ctx context.Context) (*Detected, bool, error) // PATH 查找 + --version
    BuildCommand(workspaceDir string) (executable string, args []string)
    ProbeModels(ctx context.Context, workspaceDir string) ([]ModelOption, bool, error)
    // 返回 (models, supportsModelConfigOption, err):spawn→initialize→newSession→读 ConfigOptions
    ToolCallAdapter() ToolCallAdapter // 该 provider 的 ToolCall wire shape 适配器
}
```

- `opencodeProvider`:`Detect` 找 `opencode`;`BuildCommand` → `opencode acp --pure --cwd <workspaceDir>`(与集成测试 `backend/agent/executor/acp_executor_test.go` 一致);`ProbeModels` 走 config-option 往返。
- `claudeCodeProvider`:`Detect` 找 `claude`(及 `npx`);`BuildCommand` → `npx -y @agentclientprotocol/claude-agent-acp@latest`;`ProbeModels` 同上。
- 落地后新增:`codexProvider`(实现 `ThreadProvider` 接口,acp-v2 线程协议:`ThreadCommand` → `codex app-server --listen stdio://`,模型经 `ProbeModelsV2`(`model/list`)而非 config-option;兼容性检查 `ThreadCompatChecker`);`piProvider`(`NonACPRuntime`,非 ACP——user-installed pi 由 pi RPC 执行器驱动,不参与 ACP spawn;版本低于 `MinSupportedPiVersion=0.82.1` 报不兼容)。
- `Registry`:`Lookup(id)`、`All()`、`Discover(ctx)`(并发 Detect+ProbeModels;设计稿中的 `DetectAll` 落地时更名)与 `ProbeModelOptions(ctx, providerID, envOverlay)`(单 provider 带 env overlay 的按需探测)。探测超时 30s/provider(`probeTimeout`),失败/超时则该 provider `models` 为空、`supports_model_config_option=false`,不阻塞其它。

## executor 改动(`backend/agent/executor/`,已落地)

- `acp_config.go`:
  - `ACPConfig` 已含 `Provider`、`Model`、`CustomEnv`(及后加的 `Protocol`、`PersonaPrompt`、`McpServers` 等)。
  - `BuildACPConfig`:`provider` 命中 registry 且非 `NonACPRuntime` 时用 `resolvedCommand` 经 `provider.BuildCommand(workingDir)` 派生 `Executable/Args`;`provider == "custom"`、为空或未知时退回 `executable/args` 原值(向后兼容)。`user.Model` / `user.CustomEnv` 透传。"未配置"判据 = 解析后 `Executable==""`(含 `BuildACPConfig` 返回 nil),`Capability()` 据此报 `supports_acp=false`。
  - `BuildCapability` 已同步 gating(经 `BuildACPConfig`→`Capability()`)。
- `acp_executor.go`:
  - `run()` 在 `NewSession`/`ResumeSession` 拿回 `configOpts` 后调用 `applySelectedModel`(`acp_executor.go` 内,`SetSessionConfigOption` 下发);agent 未广告 model config option 或所选 valueId 不在广告列表时记 warn 日志并沿用 agent 默认 model。
  - `buildACPEnv`(现委托 `buildRuntimeEnv`,`acp_executor.go` 末部):顺序为 `os.Environ → allow_env 过滤 → requestEnv → 模板 env → CustomEnv → bootstrap(LAELIA_*)`。CustomEnv 覆盖继承值,bootstrap `LAELIA_*` 仍最后写入保证不被用户覆盖。
- `runtime.go`:`Request` 无 per-request model 字段(model 来自 `ACPConfig`),与设计一致。

## machine 客户端改动(`backend/agent/client/`,已落地)

- `client.go`:`collectMachineInfo`(`client.go:575`)把缓存的 `discoveredProviders` 填入 `MachineInfo.available_providers`;`refreshProviders`(`client.go:602`)调 `provider.Default().Discover(ctx)` 并做内存缓存(设计稿中的 `collectAgentInfo` 与 `providers.json` 落盘缓存已被 machine 重构取代)。
- `runner.go`:`buildAcpConfig(assignment)`(`runner.go:60`)消费 `AgentAssignment.acp_config`(provider/model/custom_env 已内含);`buildRuntimeForAgent`(`runner.go:378`)是 per-turn 运行时分支点:pi → 长驻 `pi.Session`,acp-v2 provider → `NewThread`/`NewThreadWithSession`,其余 → `NewACP`。
- `command_stream.go`:`buildRuntime` 默认 ACP-only 构建器已迁至 `drain_runner.go:503`,由 runner 覆盖;env 仍来自 `req.Env`(per-command,生产路径为空)。

## manager 改动(`backend/manager/api/v1/`,已落地)

- `ConnectAgent`(`agent_connection.go:86`):保留 `AcpConfig` re-attach 模式(server-owned,从库存量回填);`available_providers` agent-owned 不覆盖(`agent_convert.go:152` 的 `convertToStoreAgentInfo`)。machine 重构后每 agent 的数据面通道为 `AgentStreamService.AgentChannel`(machine token 鉴权,首帧 `AgentReady.agent_name` 声明 agent)。
- `UpdateAgentACPConfig`(`agent_config.go:24`):接受新字段,校验 `provider` 在所属 machine 已发现列表内或为 `"custom"`/builtin-pi,并校验 model;写 `Info{AcpConfig:...}` 时保留 `available_providers / Hostname / Os / Arch / Capability` 等其余字段(设计时发现的"冲掉"问题已修复)。
- `CreateAgent`(`agent.go:68`):`AllowEnv` 仍 seed `executor.DefaultAllowEnv`;未配置时 `provider=""` 表示 inert。落地后 `CreateAgent` 也允许直接携带完整初始 `acp_config`(建 agent 即配置 provider/model/env,并校验)。
- `RefreshAgentProviders`(`agent_config.go:213`)已实现;另有 `RefreshAgentModels`、machine 级 `RefreshMachineProviders`(`machine.go`)/`RefreshMachineModels`。

## 前端改动(已落地)

- provider/model 选择器位于 `frontend/src/components/agent/acp-config-editor.tsx`,使用方为 `frontend/src/pages/dashboard/agent-profile.tsx` 与 `frontend/src/pages/dashboard/machine-add-agent-sheet.tsx`(建 agent 表单;`agents.tsx` 仅是列表页,不再承载配置表单):
  - **Provider 下拉**:选项来自所属 machine 的 `availableProviders`(machine 上报);选已知 provider 隐藏 `executable/args`(显示派生命令只读预览),选 `"custom"` 才显示手填。
  - **Model 下拉**:选项来自所选 provider 的 `models[]`,绑定 `acpConfig.model`;builtin-pi 走 `usePiModelOptions`。
  - **自定义 ENV**:`KeyValueEnvEditor`(`frontend/src/components/agent/key-value-env-editor.tsx`,设计稿中"组件名待定"已定名),绑定 `acpConfig.customEnv`。
  - **allow_env**:保留 `StringListEditor`(`frontend/src/components/agent/string-list-editor.tsx`)。
  - "刷新 provider / 探测 model" 按钮:machine 级 `refreshMachineProviders`、agent 级 `refreshAgentModels`(`stores/machine.ts`、`stores/agent.ts`)。
- `frontend/src/stores/agent.ts`:`updateAgentACPConfig`(`agent.ts:322`)透传新字段;另有 `refreshAgentModels`(`agent.ts:378`)。
- proto 生成物 `frontend/src/types/proto-es/v1/agent_pb.d.ts` 已随 buf generate 更新。

## 扩展性

新增 provider 只需:在 `backend/agent/provider/` 加一个实现 `Provider` 接口(ACP v2 则实现 `ThreadProvider`,非 ACP 运行时标记 `NonACPRuntime`)的结构体并在 `Default()` 注册;proto 字段不动(provider id 是字符串)。`"custom"` provider 永远保留,作为未内置 provider 的逃生舱。

## 关键文件清单(落地后的实际位置)

| 改动 | 文件 |
|---|---|
| proto | `proto/v1/v1/agent.proto`、`proto/v1/v1/command.proto`、`proto/v1/v1/machine.proto`、`proto/store/store/agent.proto` |
| provider registry | `backend/agent/provider/provider.go`、`registry.go`、`opencode.go`、`claudecode.go`、`codex.go`、`pi.go`、`thread.go`、`probe.go`、`tool_call.go`、`env.go` |
| executor | `backend/agent/executor/acp_config.go`、`backend/agent/executor/acp_executor.go`、`backend/agent/executor/thread_executor.go`、`backend/agent/acp2/` |
| machine 客户端 | `backend/agent/client/client.go`、`backend/agent/client/runner.go`、`backend/agent/client/drain_runner.go` |
| manager API | `backend/manager/api/v1/agent.go`、`agent_config.go`、`agent_connection.go`、`agent_convert.go`、`machine.go`、`machine_command.go`、`machine_convert.go` |
| 前端 | `frontend/src/components/agent/acp-config-editor.tsx`、`key-value-env-editor.tsx`、`frontend/src/pages/dashboard/agent-profile.tsx`、`machine-add-agent-sheet.tsx`、`frontend/src/stores/agent.ts`、`frontend/src/stores/machine.ts` |
| 测试 | `backend/agent/executor/acp_executor_test.go`、`backend/agent/provider/registry_test.go` 等 |

## 验证

1. **单测**:`provider` 包(`registry_test.go`、`probe_test.go`)验证 `Discover` / `ProbeModels` 解析 `ConfigOptions`;executor 包验证 `buildRuntimeEnv` 中 `CustomEnv` 覆盖 `allow_env` 继承值、bootstrap `LAELIA_*` 仍最后写入;验证 `BuildACPConfig` 对已知 provider 派生命令、对 `"custom"` 走旧路径。
2. **ACP 集成**:`LAELIA_RUN_OPENCODE_ACP_TESTS=1 go test ./backend/agent/executor -count=1`(本机有 opencode 时)验证 model 经 config-option 往返生效;acp-v2 用 `LAELIA_RUN_CODEX_ACP_TESTS=1 CODEX_HOME=<home>` 跑 `TestThreadExecutorCodex`。
3. **lint/build**:按 AGENTS.md 跑 `golangci-lint run --allow-parallel-runners` 与 `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`;前端 `pnpm --dir frontend biome:check` + `type-check` + `test`。
4. **端到端**:启动 manager + machine 守护进程(本机装 opencode),agents 建表单/配置页应出现 machine 发现的 provider 及其 model 列表;选定 provider+model+一组 custom env 保存;下一轮会话确认子进程以派生命令启动、env 含 custom env、model 通过 `SetSessionConfigOption` 下发(可在 machine 守护进程日志确认)。
5. **回归**:未配置 provider 的旧 agent 仍走 `"custom"`/旧 executable 路径;`UpdateAgentACPConfig` 不冲掉 `hostname/os/capability/available_providers` 等上报字段。

## 已验证的设计假设

- opencode 与 claude-agent-acp 在 `NewSession` 响应里都广播 `category=="model"` 的 config option,假设成立;`ProbeModels` 拿到选项列表,`supports_model_config_option=true`。某 provider 不广播时前端 model 下拉退化为只读提示,`acp_config.model` 留空。codex(acp-v2)不走此假设:模型经 `model/list` 广告、`thread/start` 参数下发。