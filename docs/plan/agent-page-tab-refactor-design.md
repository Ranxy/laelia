# 重构 Agents 页面为左右两栏 + Tab 详情

## Context

当前 `AgentsPage`(`frontend/src/pages/dashboard/agents.tsx`)是一个单页表格:列含 name/status/hostname/os/ip/actions,行点击弹出只读详情 `Dialog`,另有一个独立的 ACP 配置 `Sheet`(provider/model/custom-env)由 "Configure" 按钮触发。已有的 `agents/:agentId` 路由(`AgentWorkspaceLayout`)误用了 Tabs(chat 跳到私聊、tasks 实际渲染 commands 子页)。

目标:把 agent 页面重构成左右两栏——左侧 agent 列表,右侧选中 agent 的详情,详情由三个 tab 组成:`profile`(原 Dialog 只读信息 + 原 Sheet 的 ACP 编辑 + token 操作)、`命令`(复用现有 `CommandListPage`)、`Chat`(该 agent 加入的 channel 列表)。选中 agent 与当前 tab 进入 URL(可深链、刷新保留、后退可用)。

关键缺口:后端目前没有"列出某 agent 加入的所有 channel"的 RPC——`ListChannels` 是按当前用户过滤的。`conversation_member` 表已用 `MemberTypeAgent=2` + `member_id=agentResourceID` 记录 agent 成员关系,只需补一个查询。

用户已确认:① 新增后端 RPC;② URL 驱动路由;③ 命令 tab 直接复用现有 commands 页;④ 详情 Dialog 与 ACP Sheet 全部并入 profile tab 并删除弹窗。

---

## Phase 1 — Backend

### 1.1 新增 `ListChannelsForAgent` RPC(`proto/v1/v1/command.proto`)

放在 `CommandService`(不要放 `AgentService`):`agent.proto` 不导入 `command.proto`,而 `command.proto` 已导入 `agent.proto`;若把引用 `Conversation` 的 RPC 放进 `agent.proto` 会形成 proto 循环导入。`CommandService` 本身已有大量带 IAM 注解的 RPC,且 `Conversation` 类型就在 `command.proto`,可直接复用,无需新导入。

在 `command.proto` 的 `ListChannels` RPC 之后追加:

```proto
rpc ListChannelsForAgent(ListChannelsForAgentRequest) returns (ListChannelsForAgentResponse) {
  option (google.api.http) = { get: "/v1/{name=agents/*}/channels" };
  option (laelia.v1.auth_method) = IAM;
  option (laelia.v1.permission) = "laelia.agents.get";
}

message ListChannelsForAgentRequest {
  string name = 1 [
    (google.api.field_behavior) = REQUIRED,
    (google.api.resource_reference).type = "laelia/Agent"
  ];
  int32 page_size = 2;
  string page_token = 3;
}

message ListChannelsForAgentResponse {
  repeated Conversation channels = 1;
  string next_page_token = 2;
}
```

权限 `laelia.agents.get` 已在 `backend/manager/api/v1/iam.go:41` (`PermAgentRead`) 注册,IAM 拦截器会按注解自动鉴权(参考 `iam.go:136` `authorize`),无需在 handler 里手写鉴权。

### 1.2 新增 store 查询 `ListAgentConversations`(`backend/manager/store/conversation.go`)

镜像 `ListUserConversationsWithUnread`(`conversation.go:235`),把 `member_type` 绑定为 `MemberTypeAgent`、`member_id` 绑定为 agent 的 `resourceID` 字符串(与 `findDirectConversation` 中 agent member_id 的存法一致)。`member_id` 即请求里 `agents/{resourceID}` 解出的 resourceID,无需 int↔string 转换。

```go
func (s *Store) ListAgentConversations(ctx context.Context, agentResourceID string, limit, offset int) ([]*UserConversation, error)
```

v1 unread 一律返回 0(agent_channel_cursor 是 agent 自身的已读位置,对"管理员查看 agent 加入的 channel 名单"无意义)。加注释说明刻意为 0。SQL 复用 `ListUserConversationsWithUnread` 的 JOIN,把 `LEFT JOIN user_channel_cursor` 去掉、`WHERE cm.member_type = MemberTypeAgent AND cm.member_id = $1`。

### 1.3 新增 handler(`backend/manager/api/v1/channel.go`)

在 `ListChannels` 之后加 `ListChannelsForAgent`,复用其分页(`parseLimitAndOffset` maximum=100、`limitPlusOne`)、`GetConversationMemberCount`、`resolveUserName`、DM title 回退(`conv.Type==1 && conv.AgentID.Valid` → `store.GetAgent` 取 `agent.Name`)、`convertToV1Conversation`(channel.go:462)。鉴权由 IAM 拦截器按 proto 注解完成;handler 只需 `GetUserFromContext` 做 unauthenticated 兜底。`name` 用现有 `common.GetAgentResourceID`(`agent.go` 中 `GetAgent` 用的同一个解析器)解出 resourceID 传入 store。

### 1.4 注册 handler

`v1connect.NewCommandServiceHandler` 在 `buf generate` 后会带上新方法签名;在 `backend/manager` 现有 `CommandService` handler 注册处一并注册(无需新增独立注册,接口实现即可,编译会强制实现)。

---

## Phase 2 — Proto 重生成

```bash
buf format -w proto
buf lint proto
cd proto && buf generate
```

确认 `frontend/src/types/proto-es/v1/command_pb` 出现 `ListChannelsForAgentRequestSchema`/`ResponseSchema` 及 `CommandService` 上的 `listChannelsForAgent` 方法;`backend/generated-go/v1` 出对应 Go stub。

---

## Phase 3 — Frontend store

### 3.1 `fetchChannelsForAgent` 加进 channel slice(`frontend/src/stores/channel.ts`)

返回 `Conversation[]` 且用 `commandServiceClient`,与 `fetchChannels` 同源,放 channel slice 而非 agent slice。

`frontend/src/stores/types.ts` 的 `ChannelSlice` 增加:

```ts
agentChannelsByAgent: Record<string, Conversation[]>;
agentChannelsLoading: boolean;
fetchChannelsForAgent: (agentName: string) => Promise<void>;
```

实现镜像 `fetchChannels`(channel.ts:33):

```ts
async fetchChannelsForAgent(agentName) {
  set({ agentChannelsLoading: true });
  try {
    const res = await commandServiceClient.listChannelsForAgent(
      create(ListChannelsForAgentRequestSchema, { name: agentName, pageSize: 100, pageToken: "" })
    );
    set((s) => ({ agentChannelsByAgent: { ...s.agentChannelsByAgent, [agentName]: res.channels ?? [] } }));
  } finally {
    set({ agentChannelsLoading: false });
  }
}
```

---

## Phase 4 — Frontend 路由(`frontend/src/router/routes/dashboard.tsx`)

把现有并列的 `agents`(29-35)与 `agents/:agentId`(36-60)合并为父子结构:父路由 `agents` 渲染新的两栏 `AgentsPage`(左列表 + `<Outlet/>` 右详情),子路由 `agents/:agentId` 渲染新的 `AgentDetailLayout`(顶部标题 + Tabs + `<Outlet/>`),tab 用字面路径而非 `:tab` 参数(因为 `commands/:commandId` 是字面子路由,会与 `:tab` 冲突)。

```tsx
{
  path: "agents",
  lazy: () => import("@/pages/dashboard/agents").then((m) => ({ Component: m.AgentsPage })),
  children: [
    { index: true, handle: { name: AGENT_ROUTE_LIST }, element: <AgentDetailEmptyState /> },
    {
      path: ":agentId",
      lazy: () => import("@/app/layouts/agent-detail-layout").then((m) => ({ Component: m.AgentDetailLayout })),
      children: [
        { index: true, handle: { name: AGENT_ROUTE_PROFILE },
          lazy: () => import("@/pages/dashboard/agent-profile").then((m) => ({ Component: m.AgentProfilePage })) },
        { path: "commands", handle: { name: COMMAND_ROUTE_LIST },
          lazy: () => import("@/pages/dashboard/command-list").then((m) => ({ Component: m.CommandListPage })) },
        { path: "commands/:commandId", handle: { name: COMMAND_ROUTE_DETAIL },
          lazy: () => import("@/pages/dashboard/command-detail").then((m) => ({ Component: m.CommandDetailPage })) },
        { path: "chat", handle: { name: AGENT_ROUTE_CHAT },
          lazy: () => import("@/pages/dashboard/agent-chat").then((m) => ({ Component: m.AgentChatPage })) },
      ],
    },
  ],
}
```

`AgentDetailEmptyState` 可以是 `agents.tsx` 导出的一个空态组件,或简单 `<Navigate to=...>`——推荐就地空态文案。

`frontend/src/router/handles.ts` 增加 `AGENT_ROUTE_PROFILE = "agent.profile"`、`AGENT_ROUTE_CHAT = "agent.chat"`;若 `frontend/src/router/route-index.tsx` 有 handle→path 模板注册表,补上对应条目并确认 `resolvePath` 能用。

删除 `frontend/src/app/layouts/agent-workspace-layout.tsx` 及其在 `dashboard.tsx` 的 import。

---

## Phase 5 — Frontend 组件

### 5.1 抽出可复用子组件到 `frontend/src/components/agent/`

把 `agents.tsx` 里的 `StringListEditor`(956)、`KeyValueEnvEditor`(1024)剪贴到:
- `frontend/src/components/agent/string-list-editor.tsx`
- `frontend/src/components/agent/key-value-env-editor.tsx`

纯搬运,无行为变化,供新 profile tab 复用。

### 5.2 重写 `frontend/src/pages/dashboard/agents.tsx` → 两栏 shell

保留:`fetchAgents`、3s 轮询(`anyNonReady`)、Create 按钮 + 创建后展示 bootstrap token 的 Dialog、删除 AlertDialog、`agentLifecycle`、`agentsEqual`。

删除:行点击 Dialog handler、详情 Dialog(404-566)、ACP Sheet(568-750)、rotate/revoke AlertDialog、以及所有相关本地 state(`selectedAgent/detailOpen/acpConfigOpen/executable/args/allowEnv/provider/model/customEnvEntries/saving/saveError/refreshing/refreshError/rotateOpen/revokeOpen/rotating/revoking/actionError`)。token Dialog 与 delete AlertDialog 留在本文件。

布局:

```tsx
<div className="flex h-full">
  <div className="w-[360px] shrink-0 border-r border-control-border overflow-auto">
    {/* header + Create button */}
    {/* Table of agents */}
  </div>
  <div className="flex-1 overflow-hidden"><Outlet /></div>
</div>
```

表格改动:行点击 → `navigate(\`/agents/${agentId}\`)`;高亮选中行(用 `useParams<{agentId}>().agentId` 比对 `agent.name.split("/").pop()`);行内 Chat/Commands action 改为 `navigate(\`/agents/${agentId}/chat\`)` / `navigate(\`/agents/${agentId}/commands\`)`;Delete 保留(打开本文件内 delete AlertDialog)。

### 5.3 新 `frontend/src/app/layouts/agent-detail-layout.tsx`

替代 `AgentWorkspaceLayout`。职责:`useParams` 取 `agentId` → `agentResourceName(agentId)` → `getAgent`(`useEffect`);顶栏含返回按钮(`navigate("/agents")`)、`agent.title`、`<ConnectionBadge>`;Tabs 由 URL 驱动——`useLocation` 判定 activeTab(`/agents/:id` index → `profile`、`.../commands` 或 `.../commands/:commandId` → `commands`、`.../chat` → `chat`);`TabsTrigger` 的 `onClick` 用 `navigate(resolvePath(AGENT_ROUTE_PROFILE, { agentId }))` 等切换;`<Tabs value={activeTab}>` + `TabsList`/`TabsTrigger`(用 `components/ui/tabs.tsx`),实际内容由 `<Outlet/>` 渲染(`TabsPanel` 仅做视觉同步可不渲染内容)。

### 5.4 新 `frontend/src/pages/dashboard/agent-profile.tsx`(`AgentProfilePage`)

把原 Dialog 只读信息 + 原 Sheet 可编辑配置 + token 操作全部搬来:

- 只读 identity/status 网格(原 Dialog 404-566):name/title/status/lifecycle/hostname/os/ip/version/connected/last-heartbeat/created/token-version/last-rotated。`agent` 取 `useAppStore(s => s.agentCache)[agentName]`,mount 时 `getAgent(agentName)`。
- ACP 编辑器(原 Sheet 568-750):provider Select + Refresh 按钮(`refreshAgentProviders` 后 `getAgent({force:true})`)、model Select、custom 时的 executable+args(`StringListEditor`)、custom env(`KeyValueEnvEditor`)、allow-env(`StringListEditor`)。本地 state 用 `useEffect` 在 `agent.name` / `agent.info?.acpConfig` 引用变化时重新 seed。保存按钮调 `updateAgentACPConfig(agentName, acpConfig)`(搬 `handleSaveACPConfig` 282-319)。
- Token 操作:Rotate / Revoke 按钮 + 对应 AlertDialog,以及 rotate 成功后展示 bootstrap token 的 Dialog(把相关 state/handler 从 agents.tsx 搬来)。

### 5.5 新 `frontend/src/pages/dashboard/agent-chat.tsx`(`AgentChatPage`)

`useParams` 取 `agentId` → `agentName`;`useEffect` mount 与 `agentName` 变化时调 `fetchChannelsForAgent(agentName)`;从 store 读 `agentChannelsByAgent[agentName]` 与 `agentChannelsLoading`。渲染:loading skeleton / 空态(`agent.chat-empty`)/ channel 列表。优先复用 `frontend/src/components/chat/conversation-list.tsx` 的 `ConversationRow`(若已导出且接受 `Conversation`);否则渲染轻量行(title、type 图标、memberCount)。点击行 → `navigate(\`/chat/${conv.name.split("/").pop()}\`)`。v1 不轮询(channel 名单稳定)。

### 5.6 命令 tab — 不改

`CommandListPage` 仍通过嵌套 `commands` 路由在 tab 内渲染;确认其仍从 `useParams` 读 `agentId`(原本就在 `agents/:agentId/commands` 下,行为不变)。

---

## Phase 6 — Cleanup

- 删除 `agents.tsx` 中的详情 Dialog、ACP Sheet、rotate/revoke AlertDialog 及其本地 state;保留 delete AlertDialog 与 token Dialog。
- 删除 `frontend/src/app/layouts/agent-workspace-layout.tsx` 与 `dashboard.tsx` 中对应 import。
- i18n(`frontend/src/locales/en-US.json`):新增 `agent.tab-profile` / `agent.tab-commands` / `agent.tab-chat` / `agent.chat-empty` / `agent.no-selection` / `agent.profile.section-identity` / `agent.profile.section-acp` / `agent.profile.section-token`(沿用已有 `agent.*` 与 `workspace.*` key)。删除 `workspace.tab-tasks` 等明显死键前先 grep 确认无引用。

---

## 关键复用点

- `convertToV1Conversation`(`backend/manager/api/v1/channel.go:462`)— 复用,勿重写。
- `ListUserConversationsWithUnread`(`backend/manager/store/conversation.go:235`)— 新 store 函数的模板。
- `MemberTypeAgent=2` 常量(`backend/manager/store/conversation_member.go:13`)。
- `common.GetAgentResourceID`(在 `agent.go` `GetAgent` 中使用)— 解析 `agents/{id}`。
- `parseLimitAndOffset` / `limitPlusOne` 分页模式(`channel.go:43-62`)。
- IAM 注解 `(laelia.v1.auth_method)=IAM` + `(laelia.v1.permission)="laelia.agents.get"` — 拦截器自动鉴权(`iam.go:136`)。
- `Tabs/TabsList/TabsTrigger/TabsPanel`(`frontend/src/components/ui/tabs.tsx`,value 驱动受控)。
- `agentResourceName`(`frontend/src/lib/command-status.ts:66`)与 `resolvePath`(`frontend/src/router/route-index.tsx`)。
- `ConversationRow` 渲染模式(`frontend/src/components/chat/conversation-list.tsx`)。
- agent store 现有 actions(`fetchAgents/getAgent/updateAgentACPConfig/refreshAgentProviders/rotateAgentToken/revokeAgentToken/deleteAgent`),profile/chat tab 直接调用。

---

## Verification

后端:
- `gofmt -w` 改动文件;`golangci-lint run --allow-parallel-runners`(反复跑至无 issue)。
- `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`。
- `go test ./backend/manager/...`(为 `ListAgentConversations` 加一个 store 测试,镜像已有 `ListUserConversationsWithUnread` 测试若存在)。
- `buf format -w proto && buf lint proto && cd proto && buf generate`。

前端:
- `pnpm --dir frontend biome:check`(format + lint + import 排序)。
- `pnpm --dir frontend type-check`。
- `pnpm --dir frontend lint`。
- `pnpm --dir frontend test`。

端到端(用 `run`/`verify` skill 启动 manager + frontend dev):
1. 创建 agent → 左列表出现,点击行 → URL 变 `/agents/:id`,profile tab 展示 identity 网格 + ACP 编辑器。
2. 编辑 ACP(provider/model/env)→ 保存 → 刷新页面 → 值仍在(走 `UpdateAgentACPConfig` 往返)。
3. Rotate token → token Dialog 展示新 bootstrap token;Revoke → 状态正确。
4. 通过 `/chat` 某 channel 的 add-member UI 把 agent 加入 → 访问 `/agents/:id/chat` → 该 channel 出现;点击 → 跳 `/chat/:conversationId`。
5. `/agents/:id/commands` → `CommandListPage` 正常;打开命令 → `/agents/:id/commands/:commandId` 在 tab 内渲染。
6. 深链:新标签页直开 `/agents/:id/chat` → tab 正确激活;后退键遍历 `chat → commands → profile → /agents`;任意 URL 刷新状态保留。
7. 左列表在所有 `agents/:id/*` 状态下始终可见,选中行高亮,3s 轮询在任一 agent 非 ready 时持续。

## Risks

- **Proto 循环导入**:`command.proto` 已 import `agent.proto`,反过来 import 会成环。本方案把 RPC 放 `CommandService` 规避;URL `GET /v1/{name=agents/*}/channels` 仍是 agent-scoped 语义,客户端无感知。
- **`member_id` 编码**:agent 的 `conversation_member.member_id` 存的是 resourceID 字符串(与 `findDirectConversation` 一致),与请求解出的 resourceID 直接可比,无需 int↔string。
- **Tab vs 路由冲突**:用字面 `commands`/`chat`/index-profile,不引入 `:tab`,避免与 `commands/:commandId` 冲突。
- **跨嵌套路由的高亮**:`AgentsPage` 作为父 layout 持续挂载,`useParams<{agentId}>` 在任意深度可读,3s 轮询与滚动位置在子路由切换时保留。
- **handler 鉴权**:必须靠 proto 注解声明 `laelia.agents.get`,否则任意登录用户可枚举任意 agent 的 channel;`PermAgentRead` 已在 IAM 权限集中(`iam.go:66`)。