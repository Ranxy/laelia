# 支持用户本机安装的 pi（host-detected provider）

## Context

目前 pi 只有一种形态：`builtin-pi`。它由 laelia 内置/embed，始终可选，且强制使用 laelia 管理的
LLM API provider（self 或 global）。

用户希望增加第二种形态：**用户自己安装在本机的 pi**。行为对齐 codex / claude：

- agent daemon 在本机 PATH 上探测 `pi`；
- 探测到后，该机器上配置 agent 时可以选择“用户安装的 pi”；
- 与 `builtin-pi` 并列，互不影响；
- 用户安装的 pi 可以：
  1. 使用 **pi 自身的 model/auth**（即 `~/.pi/agent/auth.json` 等，laelia 不接管 key）；
  2. 或者继续使用 **laelia 管理的 self / global provider**，完整复用现有 builtin-pi 的配置能力。

## 已确认的决策

1. 两种认证方式都支持。
2. `builtin-pi` 与用户安装的 `pi` 是两个独立 provider 选项。
3. 探测范围：仅 PATH 上的 `pi`。
4. 需要最低版本兼容检查：
   - 不兼容时仍然显示在 provider 列表中；
   - 但标注“版本不兼容”；
   - 前端不允许选择；后端也拒绝保存/使用。
5. 完整复用 builtin-pi 的能力（global provider、self provider、custom provider、context_window、max_tokens 等）。

## 方案总览

```
Machine daemon (provider.Default().Discover())
 └─ 新增 PiProvider (id="pi")
      ├─ Detect: PATH 找 pi + `pi --version` + 最低版本检查
      ├─ ProbeModels: `pi --list-models`（用于“pi 自身 model”模式）
      └─ 标记为 NonACPRuntime，防止被 ACP executor 误用

Agent config:
  provider = "pi"  (用户安装)
  provider = "builtin-pi"  (内置，保持现状)

Runner:
  pi.IsPiProvider(provider) → 走现有 pi.Session / PiExecutor
     builtin-pi: binary 来自 pi.ResolveBinary()
     pi:         binary 来自 provider.PiProvider.Detect()

Manager:
  对两种 provider 都走 pi runtime 校验/capability
  对用户安装的 pi 额外校验 machine.available_providers 中存在且兼容
```

## 1. Provider 发现（backend/agent/provider）

### 1.1 新增 `PiProvider`

- `ID()` = `"pi"`
- `DisplayName()` = `"Pi (user-installed)"` 或 `"Pi"`
- `Detect(ctx)`：
  - `exec.LookPath("pi")`；
  - 找不到返回 `(nil, false, nil)`；
  - 找到后执行 `pi --version` 获取版本；
  - 与 `minSupportedPiVersion`（建议先用当前 builtin pi 版本，例如 `0.82.1`）比较；
  - 返回 `Detected{ProviderID, DisplayName, Version, ExecutablePath, Compatible, IncompatibilityReason}`。
- `ProbeModels(ctx, _)`：
  - 执行 `pi --list-models`，超时（复用 registry 的 `probeTimeout`）；
  - 解析输出为 `[]ModelOption`；
  - 失败不阻塞发现，返回空列表。
- `BuildCommand` / `ToolCallAdapter`：仅为满足 `Provider` 接口；因实现了 `NonACPRuntime`，不会被 ACP executor 使用。
- 实现 `NonACPRuntime` 接口（新增的可选标记接口），`BuildACPConfig` 对这类 provider 直接返回 nil。

### 1.2 Registry

`provider.Default()` 增加：

```go
return New(
    &OpenCodeProvider{},
    &ClaudeCodeProvider{},
    &CodexProvider{},
    &PiProvider{},
)
```

`Discover()` 因此会自动上报用户安装的 pi 到 `Machine.info.availableProviders`。

## 2. 数据模型 / proto

需要让前端知道“版本不兼容”：

`proto/v1/v1/agent.proto` + `proto/store/store/agent.proto` 的 `AgentProviderInfo` 增加：

```proto
bool compatible = 8;           // 是否满足最低版本/协议兼容
string incompatibility_reason = 9; // 不兼容原因（如 "requires >= 0.82.1"）
```

同步：

- `provider.Discovered` / `provider.Detected` 增加 `Compatible bool` 与 `IncompatibilityReason string`；
- `discoveredToProto` 透传；
- `buf format` / `buf lint` / `buf generate`；
- 前端 generated types 自动更新。

不需要为“用户安装 pi”新增 `AgentACPConfig` 字段：现有 `provider / model / api_provider / api_key / api_base_url / global_provider / global_provider_entry / context_window / max_tokens` 已足够表达两种模式。

## 3. Runner 与 pi runtime

### 3.1 pi 包

- 在 `backend/agent/pi` 增加：
  - `const UserPiProvider = "pi"`；
  - `func IsPiProvider(id string) bool`，返回 `id == BuiltinPiProvider || id == UserPiProvider`。
- `BuildPiConfig` 改为：
  - 接受 `BuiltinPiProvider` 与 `UserPiProvider`；
  - 对 `UserPiProvider` 允许“pi 自身 model”模式：`api_provider == "" && global_provider == ""` 时，不要求 `api_key`，只要求 `model` 非空；
  - 对 `BuiltinPiProvider` 保持原逻辑（必须 api_provider/api_key 或 global）。
- `BuildPiCapability` 同样对两种 provider 返回 `SupportsPi=true`。
- `launchArgs()`：
  - 当 `APIProvider == ""` 时不传 `--provider`，只传 `--model`（pi 支持 `provider/model` 形式或纯 model id）；
  - 其余参数不变。
- `buildPiEnv()`：
  - 当 `APIProvider == ""` 时不注入任何 API key env，让 pi 使用自身 `auth.json`。
- `LaunchFingerprint` 已经包含 `PiBinaryPath`，因此 `builtin-pi` 与用户安装的 `pi` 切换时会自动重启 session。

### 3.2 Runner（backend/agent/client/runner.go）

- `applyAssignment` 分支条件从 `provider == pi.BuiltinPiProvider` 改为 `pi.IsPiProvider(provider)`。
- `buildPiConfig`：
  - `builtin-pi`：继续 `pi.ResolveBinary()`；
  - `"pi"`：通过 `provider.Default().Lookup(provider.PiProviderID).Detect(ctx)` 获取可执行路径，并检查 `Compatible`，不兼容返回 nil 并记 warn。
- 其余 `PiSession`、`PiExecutor`、idle eviction、session resume 全部复用。

### 3.3 Executor 保护

`executor.BuildACPConfig` / `resolvedCommand` 增加对 `NonACPRuntime` 的检查，确保用户安装的 `pi` 不会被误建成 ACP 命令。

## 4. Manager 校验

### 4.1 `knownProviderID`

接受 `"pi"`（加入后天然通过），`"builtin-pi"` 仍特殊保留。

### 4.2 `validateAgentACPConfig`

- 当 `pi.IsPiProvider(cfg.Provider)` 时：
  - 若 `cfg.Provider == "pi"`：
    - 如果 machine 已探测（`machineAvailableProviders` 非空），必须包含 `provider_id == "pi"`；
    - 如果包含但 `compatible == false`，返回“pi 版本不兼容”错误；
  - 模式判断：
    - **pi 自身 model**：`global_provider == "" && api_provider == ""`，要求 `model != ""`，不要求 api_key；
    - **self**：`global_provider == "" && api_provider != ""`，沿用现有 builtin-pi 校验（api_provider 合法、api_key 非空、model 非空）；
    - **global**：沿用现有 global provider 校验。
  - 跳过 ACP provider 的 host/model-config-option 校验。
- `knownProviderID` / `isEmptyAgentACPConfig` 同步覆盖新 provider。

### 4.3 capability

`buildCapabilityForACPConfig` 对 `pi.IsPiProvider` 返回 `pi.BuildPiCapability`。

### 4.4 API key 与 global provider

- `UpdateAgentACPConfig` 中：
  - inline api_key 的权限 gating 从 `provider == builtin-pi` 扩大到 `pi.IsPiProvider`；
  - 对用户安装 pi 的“自身 model”模式（api_provider/global 都为空）不强制 key。
- `GetAgent` 中 api_key 脱敏条件扩展到两种 pi。
- `resolveAcpConfigForDaemon` 对两种 pi 都做 global provider 解析。
- `RefreshAgentModels` / `RefreshMachineModels` 对 `"pi"` 与 `"builtin-pi"` 一样拒绝“on-host ACP model probing”。

## 5. 前端

### 5.1 通用 helper

在 `frontend/src/components/profile-common.tsx` 增加：

```ts
export function isPiProvider(id: string) {
  return id === "builtin-pi" || id === "pi";
}
```

### 5.2 agent-profile.tsx / machine-profile.tsx

- Provider 下拉：
  - `builtin-pi` 仍固定显示；
  - `pi` 从 `availableProviders` 自动出现；
  - 如果 `provider.compatible === false`，该项 `disabled`，并显示 `incompatibility_reason`。
- `isPiProvider` 改用 helper。
- 用户安装 pi 的配置区增加三种模式：
  - `own`：使用 pi 自身 model/auth，model 下拉来自 `selectedProviderInfo.models`（来自 `pi --list-models`）；
  - `global`：与 builtin-pi 的 global provider 完全一致；
  - `self`：与 builtin-pi 的 self provider 完全一致（受 self-provided-key 开关控制）。
- `canSaveFor` / `buildFromDraft` 支持：
  - own 模式只要求 `model`；
  - global / self 沿用现有逻辑。
- 通用 ACP model 区域（`selectedProviderInfo` 相关）只对非 pi 的 provider 渲染，避免用户安装 pi 时出现两套 model 选择器。

### 5.2 machine-profile 的 provider 列表

- 显示 pi 时若 `compatible=false`，追加“版本不兼容”标注。

## 6. 测试

- provider：
  - `PiProvider.Detect`：PATH 有 pi / 无 pi / 版本过低 / 版本满足最低。
  - `PiProvider.ProbeModels`：`pi --list-models` 输出解析。
  - `Registry.Discover` 包含 pi 且不兼容时仍上报但 `Compatible=false`。
- pi/config：
  - `BuildPiConfig` 对 `"pi"` 的 own/self/global 三种模式；
  - `launchArgs` 在 own 模式不传 `--provider`。
- manager：
  - validation 对用户 pi 的 available + compatible 校验；
  - global provider resolve 覆盖 `"pi"`。
- frontend：
  - 类型 / 单测更新，确认 pi 与 builtin-pi 并列、不兼容项 disabled。

## 7. 实施顺序

1. Proto 增加 `compatible` / `incompatibility_reason` 并重新生成。
2. provider 包新增 `PiProvider`、`NonACPRuntime`、`Detected/Discovered` 字段。
3. pi 包扩展 `BuildPiConfig` / `BuildPiCapability` / `launchArgs` / `IsPiProvider`。
4. executor / runner / manager 接入。
5. 前端两处 profile 接入。
6. 单测 + lint + build + 端到端。

## Open Items / 待实现时确认

- 最低版本号建议直接使用当前 builtin pi 的固定版本（例如 `0.82.1`），并在 `PiProvider` 中作为常量。
- `pi --list-models` 的实际输出格式需要在实现时用真实用户安装 pi 验证（可能为纯 model id 列表，也可能需要额外 flag 保证非交互/无登录也能输出）。
